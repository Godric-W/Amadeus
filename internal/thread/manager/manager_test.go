package manager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	statesqlite "github.com/Godric-W/Amadeus/internal/state/sqlite"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/thread"
	"github.com/Godric-W/Amadeus/internal/thread/local"
)

type terminalFailStore struct {
	thread.ThreadStore
}

type materializeFailStore struct {
	thread.ThreadStore
}

type turnStartFailStore struct {
	thread.ThreadStore
}

type managerTestClient struct {
	mode  string
	calls *atomic.Int32
}

type identityCaptureClient struct {
	mu       sync.Mutex
	requests []llm.Request
}

func (*identityCaptureClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *identityCaptureClient) Stream(_ context.Context, request llm.Request) (llm.Stream, error) {
	client.mu.Lock()
	client.requests = append(client.requests, request)
	client.mu.Unlock()
	return &managerTestStream{chunks: []llm.StreamChunk{{ContentDelta: "done"}, {FinishReason: llm.FinishReasonStop}}}, nil
}

func (*identityCaptureClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 100_000}
}

func (*identityCaptureClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

func (client *identityCaptureClient) snapshot() []llm.Request {
	client.mu.Lock()
	defer client.mu.Unlock()
	return append([]llm.Request(nil), client.requests...)
}

func (*managerTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *managerTestClient) Stream(ctx context.Context, _ llm.Request) (llm.Stream, error) {
	if client.calls != nil {
		client.calls.Add(1)
	}
	switch client.mode {
	case "failed":
		return nil, errors.New("task failed")
	case "panic":
		panic("task panic")
	case "block":
		return &managerTestStream{ctx: ctx}, nil
	default:
		return &managerTestStream{chunks: []llm.StreamChunk{{ContentDelta: "done"}, {FinishReason: llm.FinishReasonStop}}}, nil
	}
}

func (*managerTestClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 100_000}
}

func (*managerTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type managerTestStream struct {
	ctx    context.Context
	chunks []llm.StreamChunk
}

func (stream *managerTestStream) Recv() (llm.StreamChunk, error) {
	if stream.ctx != nil {
		<-stream.ctx.Done()
		return llm.StreamChunk{}, stream.ctx.Err()
	}
	if len(stream.chunks) == 0 {
		return llm.StreamChunk{}, io.EOF
	}
	chunk := stream.chunks[0]
	stream.chunks = stream.chunks[1:]
	return chunk, nil
}

func (*managerTestStream) Close() error { return nil }

type sameTurnTestClient struct {
	mu       sync.Mutex
	requests []llm.Request
	gate     chan struct{}
	blocked  chan struct{}
	once     sync.Once
}

func (*sameTurnTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (client *sameTurnTestClient) Stream(ctx context.Context, request llm.Request) (llm.Stream, error) {
	client.mu.Lock()
	client.requests = append(client.requests, request)
	call := len(client.requests)
	client.mu.Unlock()
	if call == 1 {
		return &sameTurnFirstStream{ctx: ctx, gate: client.gate, blocked: client.blocked, once: &client.once}, nil
	}
	return &managerTestStream{chunks: []llm.StreamChunk{{ContentDelta: "follow-up"}, {FinishReason: llm.FinishReasonStop}}}, nil
}

func (*sameTurnTestClient) Model() llm.ModelInfo {
	return llm.ModelInfo{Provider: "mock", Name: "model", ContextWindow: 100_000}
}

func (*sameTurnTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

func (client *sameTurnTestClient) requestTexts() []string {
	client.mu.Lock()
	defer client.mu.Unlock()
	texts := make([]string, len(client.requests))
	for index, request := range client.requests {
		parts := make([]string, len(request.Prompt.Input))
		for messageIndex, message := range request.Prompt.Input {
			parts[messageIndex] = message.Content
		}
		texts[index] = strings.Join(parts, "\n")
	}
	return texts
}

type sameTurnFirstStream struct {
	ctx     context.Context
	gate    <-chan struct{}
	blocked chan struct{}
	once    *sync.Once
	step    int
}

func (stream *sameTurnFirstStream) Recv() (llm.StreamChunk, error) {
	switch stream.step {
	case 0:
		stream.step++
		return llm.StreamChunk{ContentDelta: "first answer"}, nil
	case 1:
		stream.step++
		stream.once.Do(func() { close(stream.blocked) })
		select {
		case <-stream.gate:
			return llm.StreamChunk{FinishReason: llm.FinishReasonStop}, nil
		case <-stream.ctx.Done():
			return llm.StreamChunk{}, stream.ctx.Err()
		}
	default:
		return llm.StreamChunk{}, io.EOF
	}
}

func (*sameTurnFirstStream) Close() error { return nil }

func (store terminalFailStore) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (thread.AppendResult, error) {
	for _, item := range items {
		if isTerminalRolloutItem(item) {
			return thread.AppendResult{}, errors.New("terminal persistence failed")
		}
	}
	return store.ThreadStore.AppendItems(ctx, id, turnID, items...)
}

func (store materializeFailStore) Materialize(context.Context, thread.CreateInput) (thread.AppendResult, error) {
	return thread.AppendResult{}, errors.New("materialize failed")
}

func (store turnStartFailStore) AppendItems(ctx context.Context, id protocol.ThreadID, turnID protocol.TurnID, items ...rollout.RolloutItem) (thread.AppendResult, error) {
	for _, item := range items {
		if _, ok := item.(rollout.TurnContextItem); ok {
			return thread.AppendResult{}, errors.New("turn start append failed")
		}
		if event, ok := item.(rollout.EventMsgItem); ok {
			if _, started := event.Msg.(protocol.TurnStartedEvent); started {
				return thread.AppendResult{}, errors.New("turn start append failed")
			}
		}
	}
	return store.ThreadStore.AppendItems(ctx, id, turnID, items...)
}

func TestThreadManagerMaterializesOnFirstInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, store := newTestManager(t, ctx, "completed", nil)
	defer manager.Close(context.Background())
	configuration := testConfiguration(t)
	effort := llm.ReasoningEffortHigh
	configuration.Runtime.ModelReasoningEffort = &effort
	value, err := manager.StartThread(ctx, StartInput{Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := manager.ListThreads(ctx, state.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("draft thread was persisted: %#v", listed)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "inspect files"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), false)
	listed, err = manager.ListThreads(ctx, state.ListQuery{CWD: configuration.CWD})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != value.ID() || listed[0].Preview != "inspect files" {
		t.Fatalf("threads = %#v", listed)
	}
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{
		"session_meta", "turn_context", "response:user_message",
		"event:turn_started", "response:assistant_message", "event:item_completed", "event:turn_complete",
	}
	actualKinds := make([]string, len(history.Lines))
	for index, line := range history.Lines {
		actualKinds[index] = rolloutItemKind(line.Item)
	}
	next := 0
	for _, actual := range actualKinds {
		if next < len(wantKinds) && actual == wantKinds[next] {
			next++
		}
	}
	if next != len(wantKinds) {
		t.Fatalf("history kinds = %v, missing ordered suffix from %v", actualKinds, wantKinds[next:])
	}
	for _, line := range history.Lines {
		contextItem, ok := line.Item.(rollout.TurnContextItem)
		if !ok {
			continue
		}
		if contextItem.ReasoningEffort == nil || *contextItem.ReasoningEffort != llm.ReasoningEffortHigh {
			t.Fatalf("turn context effort = %#v, want high", contextItem.ReasoningEffort)
		}
		return
	}
	t.Fatal("turn context item was not persisted")
}

func TestAgentControlSpawnsFullChildSessionAndPersistsNotification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manager, store := newTestManager(t, ctx, "", nil)
	defer manager.Close(context.Background())
	root, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Submit(ctx, protocol.UserInputOp{Content: "establish root"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, root.Io(), false)
	spawned, err := root.agentControl.Spawn(ctx, root.ID(), "inspect the repository architecture")
	if err != nil {
		t.Fatal(err)
	}
	waited, err := root.agentControl.Wait(ctx, []protocol.ThreadID{spawned.AgentID}, 5*time.Second)
	statuses := waited.Statuses
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status.Kind != protocol.AgentStatusCompleted || statuses[0].Status.Message != "done" {
		t.Fatalf("child statuses = %#v", statuses)
	}
	child, err := store.GetThread(ctx, spawned.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if !child.Source.IsSubAgent() || child.Source.SubAgent.ParentThreadID != root.ID() || child.Source.SubAgent.AgentNickname != spawned.Nickname || child.Source.SubAgent.AgentRole != "explorer" {
		t.Fatalf("child metadata = %#v", child)
	}
	topLevel, err := manager.ListThreads(ctx, state.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(topLevel) != 1 || topLevel[0].ID != root.ID() {
		t.Fatalf("top-level threads = %#v", topLevel)
	}
	allThreads, err := manager.ListThreads(ctx, state.ListQuery{IncludeSubAgents: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(allThreads) != 2 {
		t.Fatalf("all threads = %#v", allThreads)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		history, historyErr := root.History(ctx)
		if historyErr != nil {
			t.Fatal(historyErr)
		}
		notifications := 0
		for _, line := range history {
			if event, ok := line.Item.(rollout.EventMsgItem); ok {
				if _, ok := event.Msg.(protocol.SubagentNotificationEvent); ok {
					notifications++
				}
			}
		}
		if notifications == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("root history notification count = %d", notifications)
		}
		time.Sleep(10 * time.Millisecond)
	}
	previous, err := root.agentControl.CloseAgent(ctx, spawned.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Kind != protocol.AgentStatusCompleted {
		t.Fatalf("previous status = %#v", previous)
	}
	if _, exists := manager.GetThread(spawned.AgentID); exists {
		t.Fatal("closed child remained in live ThreadManager registry")
	}
	if _, err := manager.ResumeThread(ctx, spawned.AgentID, StartInput{Configuration: testConfiguration(t)}); err == nil || !strings.Contains(err.Error(), "sub-agent threads cannot be resumed directly") {
		t.Fatalf("direct child resume error = %v", err)
	}
}

func TestRootResumeRestoresPersistedChildAndLazilyResumesRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	manager, store := newTestManager(t, ctx, "", nil)
	defer manager.Close(context.Background())

	root, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Submit(ctx, protocol.UserInputOp{Content: "materialize root"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, root.Io(), false)
	spawned, err := root.agentControl.Spawn(ctx, root.ID(), "inspect persisted child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.agentControl.Wait(ctx, []protocol.ThreadID{spawned.AgentID}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	rootID, sessionID := root.ID(), root.SessionID()
	if err := manager.ShutdownThread(ctx, rootID); err != nil {
		t.Fatal(err)
	}
	if _, loaded := manager.GetThread(spawned.AgentID); loaded {
		t.Fatal("child runtime remained loaded after root shutdown")
	}

	resumed, err := manager.ResumeThread(ctx, rootID, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.SessionID() != sessionID || resumed.ID() != rootID {
		t.Fatalf("resumed root identity = session %q thread %q", resumed.SessionID(), resumed.ID())
	}
	record, ok := resumed.agentControl.Record(spawned.AgentID)
	if !ok || record.Metadata.ParentThreadID != rootID || record.Metadata.ThreadID != spawned.AgentID {
		t.Fatalf("persisted child record = %#v, present=%v", record, ok)
	}
	if _, loaded := manager.GetThread(spawned.AgentID); loaded {
		t.Fatal("persisted child was eagerly loaded during root resume")
	}
	if err := resumed.agentControl.SendInput(ctx, spawned.AgentID, "continue after resume", false); err != nil {
		t.Fatal(err)
	}
	child, loaded := manager.GetThread(spawned.AgentID)
	if !loaded || child.SessionID() != sessionID || child.ParentThreadID() == nil || *child.ParentThreadID() != rootID {
		t.Fatalf("lazily resumed child = %#v, loaded=%v", child, loaded)
	}
	if _, err := resumed.agentControl.Wait(ctx, []protocol.ThreadID{spawned.AgentID}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	history, err := store.LoadHistory(ctx, spawned.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	meta := history.Lines[0].Item.(rollout.SessionMetaItem)
	if meta.SessionID != sessionID || meta.ID != spawned.AgentID || meta.ParentThreadID == nil || *meta.ParentThreadID != rootID {
		t.Fatalf("child session metadata = %#v", meta)
	}
}

func TestRootAndChildProviderRequestsShareSessionIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := &identityCaptureClient{}
	manager, _ := newTestManagerWithClient(t, ctx, client)
	defer manager.Close(context.Background())
	root, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Submit(ctx, protocol.UserInputOp{Content: "root request"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, root.Io(), false)
	spawned, err := root.agentControl.Spawn(ctx, root.ID(), "child request")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.agentControl.Wait(ctx, []protocol.ThreadID{spawned.AgentID}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	requests := client.snapshot()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want root and child", len(requests))
	}
	byThread := make(map[protocol.ThreadID]llm.RequestMetadata, len(requests))
	for _, request := range requests {
		byThread[request.Metadata.ThreadID] = request.Metadata
	}
	rootMetadata, rootOK := byThread[root.ID()]
	childMetadata, childOK := byThread[spawned.AgentID]
	if !rootOK || !childOK || rootMetadata.SessionID != root.SessionID() || childMetadata.SessionID != root.SessionID() {
		t.Fatalf("provider identity metadata = root %#v child %#v", rootMetadata, childMetadata)
	}
	if rootMetadata.ParentThreadID != nil || childMetadata.ParentThreadID == nil || *childMetadata.ParentThreadID != root.ID() || rootMetadata.TurnID == "" || childMetadata.TurnID == "" {
		t.Fatalf("provider parent/turn metadata = root %#v child %#v", rootMetadata, childMetadata)
	}
}

func TestThreadUserInputAdmissionContinuesSameTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &sameTurnTestClient{gate: make(chan struct{}), blocked: make(chan struct{})}
	manager, store := newTestManagerWithClient(t, ctx, client)
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	started, err := value.SubmitUserInputAndWaitForAdmission(ctx, protocol.UserInputOp{Content: "first prompt", ClientUserMessageID: "client-1"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Kind != protocol.UserMessageAdmissionStarted || started.TurnID == "" {
		t.Fatalf("started admission = %#v", started)
	}
	select {
	case <-client.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("first model request did not reach the gate")
	}
	steered, err := value.SubmitUserInputAndWaitForAdmission(ctx, protocol.UserInputOp{Content: "second prompt", ClientUserMessageID: "client-2"})
	if err != nil {
		t.Fatal(err)
	}
	if steered.Kind != protocol.UserMessageAdmissionSteered || steered.TurnID != started.TurnID {
		t.Fatalf("steered admission = %#v, started = %#v", steered, started)
	}
	third, err := value.SubmitUserInputAndWaitForAdmission(ctx, protocol.UserInputOp{Content: "third prompt", ClientUserMessageID: "client-3"})
	if err != nil {
		t.Fatal(err)
	}
	if third.Kind != protocol.UserMessageAdmissionSteered || third.TurnID != started.TurnID {
		t.Fatalf("third admission = %#v, started = %#v", third, started)
	}
	close(client.gate)
	startedEvents, terminalEvents := 0, 0
	userIDs := make([]string, 0, 3)
	timeout := time.After(5 * time.Second)
	for terminalEvents == 0 {
		select {
		case event := <-value.Io().Events:
			switch message := event.Msg.(type) {
			case protocol.TurnStartedEvent:
				startedEvents++
			case protocol.ItemCompletedEvent:
				if message.Item.Kind == protocol.ItemUserMessage {
					userIDs = append(userIDs, message.Item.ClientUserMessageID)
				}
			case protocol.TurnCompleteEvent:
				terminalEvents++
			case protocol.TurnAbortedEvent:
				t.Fatalf("turn aborted: %#v", message)
			}
		case <-timeout:
			t.Fatal("timed out waiting for same-turn completion")
		}
	}
	if startedEvents != 1 || terminalEvents != 1 {
		t.Fatalf("lifecycle counts = started %d terminal %d", startedEvents, terminalEvents)
	}
	if len(userIDs) != 3 || userIDs[0] != "client-1" || userIDs[1] != "client-2" || userIDs[2] != "client-3" {
		t.Fatalf("live user IDs = %v", userIDs)
	}
	requests := client.requestTexts()
	if len(requests) != 2 {
		t.Fatalf("request texts = %#v", requests)
	}
	secondIndex := strings.Index(requests[1], "second prompt")
	thirdIndex := strings.Index(requests[1], "third prompt")
	if strings.Contains(requests[0], "second prompt") || secondIndex < 0 || thirdIndex <= secondIndex {
		t.Fatalf("request texts = %#v", requests)
	}
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	canonicalIDs := make([]string, 0, 3)
	for _, line := range history.Lines {
		eventItem, ok := line.Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		completed, ok := eventItem.Msg.(protocol.ItemCompletedEvent)
		if ok && completed.Item.Kind == protocol.ItemUserMessage {
			canonicalIDs = append(canonicalIDs, completed.Item.ClientUserMessageID)
		}
	}
	if len(canonicalIDs) != 3 || canonicalIDs[0] != "client-1" || canonicalIDs[1] != "client-2" || canonicalIDs[2] != "client-3" {
		t.Fatalf("canonical user IDs = %v", canonicalIDs)
	}
}

func TestInterruptedTurnRecordsAcceptedPendingInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &sameTurnTestClient{gate: make(chan struct{}), blocked: make(chan struct{})}
	manager, store := newTestManagerWithClient(t, ctx, client)
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	started, err := value.SubmitUserInputAndWaitForAdmission(ctx, protocol.UserInputOp{Content: "wait", ClientUserMessageID: "client-1"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("first model request did not reach the gate")
	}
	steered, err := value.SubmitUserInputAndWaitForAdmission(ctx, protocol.UserInputOp{Content: "record before abort", ClientUserMessageID: "client-2"})
	if err != nil {
		t.Fatal(err)
	}
	if steered.Kind != protocol.UserMessageAdmissionSteered || steered.TurnID != started.TurnID {
		t.Fatalf("steered admission = %#v", steered)
	}
	if err := value.Submit(ctx, protocol.InterruptOp{}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), true)
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range history.Lines {
		eventItem, ok := line.Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		completed, ok := eventItem.Msg.(protocol.ItemCompletedEvent)
		if ok && completed.Item.Kind == protocol.ItemUserMessage && completed.Item.ClientUserMessageID == "client-2" {
			found = true
		}
	}
	if !found {
		t.Fatal("accepted pending input was not persisted before abort")
	}
}

func TestThreadManagerInterruptPersistsAbortedBeforeEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, store := newTestManager(t, ctx, "block", nil)
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "wait"}); err != nil {
		t.Fatal(err)
	}
	waitForStarted(t, value.Io())
	if err := value.Submit(ctx, protocol.InterruptOp{}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), true)
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	if actual := rolloutItemKind(history.Lines[len(history.Lines)-1].Item); actual != "event:turn_aborted" {
		t.Fatalf("last rollout item = %q", actual)
	}
}

func TestSessionHistorySupportsConcurrentTaskAppends(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, store := newTestManager(t, ctx, "completed", nil)
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "parallel history"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), false)
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			item, itemErr := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseAssistantMessage, Role: "assistant", Content: "parallel"})
			if itemErr == nil {
				itemErr = value.session.AppendItems(ctx, "turn-concurrent", item)
			}
			errorsChannel <- itemErr
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for appendErr := range errorsChannel {
		if appendErr != nil {
			t.Fatal(appendErr)
		}
	}
	history, err := store.LoadHistory(ctx, value.ID())
	if err != nil {
		t.Fatal(err)
	}
	if value.RolloutItemCount() != len(history.Lines) {
		t.Fatalf("context/store item count differs: context=%d store=%d", value.RolloutItemCount(), len(history.Lines))
	}
	for index, line := range history.Lines {
		if line.Sequence != uint64(index+1) {
			t.Fatalf("history sequence[%d] = %d", index, line.Sequence)
		}
	}
}

func TestRenameActiveThreadUpdatesCanonicalSessionHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, _ := newTestManager(t, ctx, "completed", nil)
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "materialize"}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, value.Io(), false)
	if err := manager.RenameThread(ctx, value.ID(), "Renamed Thread"); err != nil {
		t.Fatal(err)
	}
	history, err := value.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 {
		t.Fatalf("history after rename = %#v", history)
	}
	eventItem, ok := history[len(history)-1].Item.(rollout.EventMsgItem)
	if !ok {
		t.Fatalf("rename item = %T", history[len(history)-1].Item)
	}
	update, ok := eventItem.Msg.(protocol.ThreadNameUpdatedEvent)
	if !ok || update.Name != "Renamed Thread" {
		t.Fatalf("rename update = %#v", update)
	}
}

func TestTerminalPersistenceFailureDoesNotPublishTerminalEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, baseStore := newTestManager(t, ctx, "completed", nil)
	manager.store = terminalFailStore{ThreadStore: baseStore}
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "fail terminal"}); err != nil {
		t.Fatal(err)
	}
	var streamError bool
	for event := range value.Io().Events {
		switch event.Msg.(type) {
		case protocol.StreamErrorEvent:
			streamError = true
		case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
			t.Fatal("terminal event was published without durable terminal rollout")
		}
	}
	if !streamError {
		t.Fatal("terminal persistence failure was not reported")
	}
}

func TestTurnStartFailurePublishesRejectedWithoutBlockingSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager, baseStore := newTestManager(t, ctx, "completed", nil)
	manager.store = materializeFailStore{ThreadStore: baseStore}
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := value.Submit(ctx, protocol.UserInputOp{Content: "retry"}); err != nil {
			t.Fatal(err)
		}
		timeout := time.After(5 * time.Second)
		for {
			select {
			case event := <-value.Io().Events:
				if rejected, ok := event.Msg.(protocol.ErrorEvent); ok {
					if rejected.Message != "materialize failed" {
						t.Fatalf("rejected = %#v", rejected)
					}
					goto nextAttempt
				}
			case <-timeout:
				t.Fatal("timed out waiting for ErrorEvent")
			}
		}
	nextAttempt:
	}
}

func TestSessionPublishesAndPersistsExactlyOneTerminal(t *testing.T) {
	for _, mode := range []string{"completed", "failed", "panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			manager, store := newTestManager(t, ctx, mode, nil)
			defer manager.Close(context.Background())
			value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
			if err != nil {
				t.Fatal(err)
			}
			if err := value.Submit(ctx, protocol.UserInputOp{Content: "run"}); err != nil {
				t.Fatal(err)
			}
			terminalEvents := 0
			timeout := time.After(5 * time.Second)
			for terminalEvents == 0 {
				select {
				case event := <-value.Io().Events:
					switch event.Msg.(type) {
					case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
						terminalEvents++
					}
				case <-timeout:
					t.Fatal("timed out waiting for terminal")
				}
			}
			if err := value.Submit(ctx, protocol.ShutdownOp{}); err != nil {
				t.Fatal(err)
			}
			for event := range value.Io().Events {
				switch event.Msg.(type) {
				case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
					terminalEvents++
				}
			}
			if terminalEvents != 1 {
				t.Fatalf("terminal event count = %d", terminalEvents)
			}
			history, err := store.LoadHistory(ctx, value.ID())
			if err != nil {
				t.Fatal(err)
			}
			terminalLines := 0
			for _, line := range history.Lines {
				if isTerminalRolloutItem(line.Item) {
					terminalLines++
				}
			}
			if terminalLines != 1 {
				t.Fatalf("terminal rollout count = %d", terminalLines)
			}
		})
	}
}

func TestDurableTurnStartFailureDoesNotRunTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	manager, baseStore := newTestManager(t, ctx, "completed", &calls)
	manager.store = turnStartFailStore{ThreadStore: baseStore}
	defer manager.Close(context.Background())
	value, err := manager.StartThread(ctx, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Submit(ctx, protocol.UserInputOp{Content: "fail durable start"}); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-value.Io().Events:
			if rejected, ok := event.Msg.(protocol.ErrorEvent); ok {
				if rejected.Message != "turn start append failed" {
					t.Fatalf("rejected = %#v", rejected)
				}
				if calls.Load() != 0 {
					t.Fatalf("model call count after rejected start = %d", calls.Load())
				}
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for rejected turn")
		}
	}
}

func TestResumeRecoversIncompleteToolCall(t *testing.T) {
	ctx := context.Background()
	manager, store := newTestManager(t, ctx, "completed", nil)
	now := time.Now().UTC()
	threadID := testutil.ThreadID(77)
	live, err := thread.NewDraftLiveThread(threadID, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.Materialize(ctx, thread.CreateInput{SessionID: protocol.SessionIDFromThreadID(threadID), CWD: testConfiguration(t).CWD, Title: "recover", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	turnContext := testTurnContext(t, threadID, "turn-old")
	contextItem := rollout.TurnContextItem{
		ThreadID: turnContext.ThreadID, TurnID: turnContext.TurnID,
		Provider: turnContext.Provider, Model: turnContext.Model, CWD: turnContext.CWD, Mode: string(turnContext.Mode),
	}
	userItem, _ := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: "user", Content: "run"})
	startedItem := rollout.EventMsgItem{Msg: protocol.TurnStartedEvent{StartedAt: now}}
	toolCall, _ := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseToolCall, Role: "assistant", CallID: "call-1", Name: "execute_command", Arguments: json.RawMessage(`{}`)})
	if _, err := live.AppendItems(ctx, "turn-old", contextItem, userItem, startedItem, toolCall); err != nil {
		t.Fatal(err)
	}
	if err := live.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	resumed, err := manager.ResumeThread(ctx, threadID, StartInput{Configuration: testConfiguration(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Shutdown(context.Background())
	history, err := store.LoadHistory(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Lines) < 2 || rolloutItemKind(history.Lines[len(history.Lines)-1].Item) != "event:turn_aborted" {
		t.Fatalf("recovered history = %#v", history.Lines)
	}
	recovered, ok := history.Lines[len(history.Lines)-2].Item.(rollout.ResponseItem)
	if !ok {
		t.Fatalf("recovered result item = %T", history.Lines[len(history.Lines)-2].Item)
	}
	if recovered.Type != rollout.ResponseToolResult || recovered.CallID != "call-1" || recovered.Status != "cancelled" {
		t.Fatalf("recovered result = %#v", recovered)
	}
}

func isTerminalRolloutItem(item rollout.RolloutItem) bool {
	event, ok := item.(rollout.EventMsgItem)
	if !ok {
		return false
	}
	switch event.Msg.(type) {
	case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
		return true
	default:
		return false
	}
}

func rolloutItemKind(item rollout.RolloutItem) string {
	switch item := item.(type) {
	case rollout.SessionMetaItem:
		return "session_meta"
	case rollout.TurnContextItem:
		return "turn_context"
	case rollout.ResponseItem:
		return "response:" + string(item.Type)
	case rollout.CompactedItem:
		return "compacted"
	case rollout.EventMsgItem:
		switch item.Msg.(type) {
		case protocol.TurnStartedEvent:
			return "event:turn_started"
		case protocol.TurnCompleteEvent:
			return "event:turn_complete"
		case protocol.TurnAbortedEvent:
			return "event:turn_aborted"
		case protocol.ItemCompletedEvent:
			return "event:item_completed"
		case protocol.ThreadNameUpdatedEvent:
			return "event:thread_name_updated"
		default:
			return "event"
		}
	default:
		return "unknown"
	}
}

func newTestManager(t *testing.T, ctx context.Context, mode string, calls *atomic.Int32) (*ThreadManager, thread.ThreadStore) {
	t.Helper()
	return newTestManagerWithClient(t, ctx, &managerTestClient{mode: mode, calls: calls})
}

func newTestManagerWithClient(t *testing.T, ctx context.Context, client llm.Client) (*ThreadManager, thread.ThreadStore) {
	t.Helper()
	home := t.TempDir()
	database, err := statesqlite.Open(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	stateStore, err := statesqlite.NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	localStore, err := local.NewStore(home, stateStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sequence atomic.Uint64
	modelMessages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(ctx, localStore, SharedServices{
		SessionAdapters: session.ServiceAdapters{
			ModelMessages: modelMessages,
			ClientFactory: func(string, string, config.ModelProviderInfo) (llm.Client, error) { return client, nil },
			AuditFactory:  func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), nil, nil },
		},
		NextID: func(prefix string) string {
			return prefix + "-" + time.Unix(0, int64(sequence.Add(1))).UTC().Format("150405.000000000")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager, localStore
}

func testConfiguration(t *testing.T) session.Configuration {
	t.Helper()
	configured := config.Default()
	configured.ModelProvider = "mock"
	configured.Model = "gpt-test"
	configured.ModelContextWindow = 100_000
	configured.ModelAutoCompactTokenLimit = 90_000
	configured.ToolOutputTokenLimit = 10_000
	configured.ModelProviders = map[string]config.ModelProviderInfo{
		"mock": {WireAPI: config.WireAPIResponses, Dialect: config.DialectStandard, APIKey: "test", BaseURL: "https://example.invalid/v1", Timeout: time.Second, StreamIdleTimeout: time.Minute},
	}
	return session.Configuration{Runtime: configured, CWD: filepath.Clean(t.TempDir()), AmadeusRoot: t.TempDir(), Mode: turn.ModeKindDefault}
}

func testTurnContext(t *testing.T, threadID protocol.ThreadID, turnID protocol.TurnID) turn.TurnContext {
	t.Helper()
	configuration := testConfiguration(t)
	return turn.TurnContext{
		SessionID: protocol.SessionIDFromThreadID(threadID), ThreadID: threadID, TurnID: turnID, Provider: configuration.Runtime.ModelProvider, Model: configuration.Runtime.Model,
		CWD: configuration.CWD, Mode: turn.ModeKindDefault,
	}
}

func waitForStarted(t *testing.T, io session.SessionIo) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-io.Events:
			if _, ok := event.Msg.(protocol.TurnStartedEvent); ok {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for TurnStarted")
		}
	}
}

func waitForTerminal(t *testing.T, io session.SessionIo, aborted bool) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event := <-io.Events:
			switch event.Msg.(type) {
			case protocol.TurnCompleteEvent:
				if aborted {
					t.Fatal("received TurnCompleted, want TurnAborted")
				}
				return
			case protocol.TurnAbortedEvent:
				if !aborted {
					t.Fatal("received TurnAborted, want TurnCompleted")
				}
				return
			case protocol.StreamErrorEvent:
				t.Fatalf("stream error: %#v", event.Msg)
			}
		case <-timeout:
			t.Fatal("timed out waiting for terminal event")
		}
	}
}
