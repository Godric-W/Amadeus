package session

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	contextmanager "github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type servicesTestClient struct{ model llm.ModelInfo }

func (*servicesTestClient) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("unexpected Complete call")
}

func (*servicesTestClient) Stream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("unexpected Stream call")
}

func (client *servicesTestClient) Model() llm.ModelInfo { return client.model }

func (*servicesTestClient) Capabilities() llm.Capabilities {
	return llm.Capabilities{SupportsStreaming: true}
}

type servicesTestCloser struct{ calls int }

func (closer *servicesTestCloser) Close() error {
	closer.calls++
	return nil
}

func TestSessionServicesAreConstructedOnceAndReusedAcrossTasks(t *testing.T) {
	configured := config.Default()
	configured.Model = "test-model"
	configured.ModelProvider = "mock"
	configured.ModelContextWindow = 8_192
	configured.ModelInputModalities = []llm.InputModality{llm.InputModalityText, llm.InputModalityImage}
	configured.ModelSupportsOriginalImageDetail = true
	configured.ModelProviders = map[string]config.ModelProviderInfo{
		"mock": {
			WireAPI: config.WireAPIResponses, Dialect: config.DialectStandard,
			APIKey: "test-key", BaseURL: "https://example.invalid/v1", Timeout: 2 * time.Minute,
			RequestMaxRetries: 4, StreamMaxRetries: 5, StreamIdleTimeout: 5 * time.Minute,
		},
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	compactionAssets, err := internalprompt.LoadCompactionAssets()
	if err != nil {
		t.Fatal(err)
	}
	closer := &servicesTestCloser{}
	modelMessages, err := internalprompt.LoadModelMessages()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	session := newTestSession(nil, contextmanager.NewManager(nil))
	session.state.Configuration = Configuration{Runtime: configured, CWD: root, AmadeusRoot: t.TempDir(), Mode: ModeKindDefault}
	session.services.Clock = func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, location) }
	services, err := buildSessionServices(context.Background(), session, session.services, ServiceAdapters{
		ModelMessages: modelMessages, CompactionAssets: compactionAssets,
		ClientFactory: func(providerName, model string, _ config.ModelProviderInfo) (llm.Client, error) {
			return &servicesTestClient{model: llm.ModelInfo{Provider: providerName, Name: model}}, nil
		},
		AuditFactory: func() (audit.Sink, io.Closer, error) { return audit.NewMemorySink(), closer, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	session.services = services

	modelInfo := session.services.ModelInfo()
	if modelInfo.Provider != "mock" || modelInfo.Name != "test-model" || modelInfo.ContextWindow != 8_192 || modelInfo.AutoCompactTokenLimit != 7_372 || modelInfo.ToolOutputTokenLimit != 10_000 || !modelInfo.SupportsInput(llm.InputModalityImage) || !modelInfo.SupportsOriginalImageDetail {
		t.Fatalf("ModelInfo did not resolve SessionState configuration: %#v", modelInfo)
	}
	if _, ok := session.services.tools.LookupVisible("view_image", session.services.visibility); !ok {
		t.Fatal("image-capable Session did not expose view_image")
	}
	requestContext := TurnContext{
		SubmissionID: "submission-1", SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Provider: configured.ModelProvider,
		Model: configured.Model, CWD: root, Mode: ModeKindDefault,
	}
	preparedTask, preparedContext, err := session.createTask(context.Background(), UserTurnInput{Content: "inspect project"}, requestContext, TaskKindRegular, newTurnState())
	if err != nil {
		t.Fatal(err)
	}
	regular, ok := preparedTask.(*regularTask)
	if !ok || regular.runtime != &session.services {
		t.Fatalf("prepared task does not use Session-owned services: %#v", preparedTask)
	}
	if preparedContext.CurrentDate != "2026-08-14" || preparedContext.Timezone != "Asia/Shanghai" {
		t.Fatalf("resolved environment snapshot = %#v", preparedContext)
	}
	secondContext := requestContext
	secondContext.TurnID = "turn-2"
	secondTask, _, err := session.createTask(context.Background(), UserTurnInput{Content: "inspect again"}, secondContext, TaskKindRegular, newTurnState())
	if err != nil {
		t.Fatal(err)
	}
	if secondTask.(*regularTask).runtime != regular.runtime {
		t.Fatal("regular tasks did not reuse SessionServices")
	}
	if closer.calls != 0 {
		t.Fatal("task creation closed Session-scoped resources")
	}
	servicesCopy := session.services
	if err := servicesCopy.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.services.Close(); err != nil {
		t.Fatal(err)
	}
	if closer.calls != 1 {
		t.Fatalf("SessionServices copies closed audit resources %d times", closer.calls)
	}
}
