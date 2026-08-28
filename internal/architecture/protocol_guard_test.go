package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolContainsContractsWithoutUIProjection(t *testing.T) {
	root := repositoryRoot(t)
	protocolRoot := filepath.Join(root, "internal", "protocol")
	forbidden := []string{
		"type TranscriptState", "func NewTranscriptState", "type EventReducer",
		"ProjectThreadItems", "replaceTranscriptItem", "applyDelta(",
	}
	err := filepath.WalkDir(protocolRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, symbol := range forbidden {
			if strings.Contains(string(content), symbol) {
				t.Errorf("Protocol owns UI/replay projection %q in %s", symbol, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCurrentSchemasHaveNoMigrationProductionFiles(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/config/migration.go",
		"internal/rollout/migration.go",
		"internal/rollout/legacy.go",
		"internal/threadstore/local/migration.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err == nil {
			t.Errorf("legacy schema production file still exists: %s", relative)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect %s: %v", relative, err)
		}
	}
}

func TestCanonicalWritersUseTypedPayloads(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, rawWriter := range []string{
				"rollout.NewItem(", "rollout.NewRawItem(", "rollout.DecodePayload[", "rollout.DecodeResponseItem(",
			} {
				if strings.Contains(string(content), rawWriter) {
					t.Errorf("canonical writer bypasses typed payload through %q in %s", rawWriter, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
}

func TestBasicMultiAgentArchitectureBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/agent/multiagent/domain.go",
		"internal/agent/multiagent/control.go",
		"internal/agent/multiagent/reservation.go",
		"internal/agent/multiagent/lifecycle.go",
		"internal/agent/multiagent/message_budget.go",
		"internal/agent/multiagent/status.go",
		"internal/agent/multiagent/shutdown.go",
		"internal/threadmanager/agent_host.go",
		"internal/contextmanager/subagent_notification.go",
		"internal/rollout/agent_edge.go",
		"internal/protocol/collaboration.go",
		"internal/tool/builtin/multi_agent.go",
		"internal/prompt/builtin/templates/agent/subagent.md",
		"internal/tui/history_cell_multiagent.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("required multi-agent boundary file is missing: %s: %v", relative, err)
		}
	}
	toolSource := mustReadArchitectureFile(t, root, "internal/tool/builtin/multi_agent.go")
	for _, forbidden := range []string{"internal/agent/session", "internal/llm", "run_turn", "ClientFactory", "ThreadStore"} {
		if strings.Contains(toolSource, forbidden) {
			t.Errorf("multi-agent Tool owns forbidden runtime concern %q", forbidden)
		}
	}
	managerSource := mustReadArchitectureFile(t, root, "internal/threadmanager/manager.go")
	if !strings.Contains(managerSource, "agentsession.Spawn") {
		t.Error("ThreadManager no longer owns Session spawn")
	}
	agentHostSource := mustReadArchitectureFile(t, root, "internal/threadmanager/agent_host.go")
	for _, required := range []string{"func (manager *ThreadManager) SpawnChild", "NewDraftLiveThread", "NewSubAgentSessionSource", "RecordSpawnEdge", "NotifyParent"} {
		if !strings.Contains(agentHostSource, required) {
			t.Errorf("ThreadManager child host is missing %q", required)
		}
	}
	for _, forbidden := range []string{"encoding/json", "<subagent_notification>"} {
		if strings.Contains(agentHostSource, forbidden) {
			t.Errorf("ThreadManager formats model-visible notification through %q", forbidden)
		}
	}
	sessionSource := mustReadArchitectureFile(t, root, "internal/agent/session/step_context.go")
	for _, required := range []string{`case "read", "glob", "grep", "read_skill", "web_search":`, "source.IsSubAgent()"} {
		if !strings.Contains(sessionSource, required) {
			t.Errorf("sub-agent exact ToolRouter filter is missing %q", required)
		}
	}
	tuiSource := mustReadArchitectureFile(t, root, "internal/tui/history_cell_multiagent.go")
	for _, forbidden := range []string{"json.Unmarshal", "item.Text", "ToolResult.Text"} {
		if strings.Contains(tuiSource, forbidden) {
			t.Errorf("multi-agent TUI reconstructs typed state from text via %q", forbidden)
		}
	}
	protocolFields := architectureStructFields(t, root, "internal/protocol/collaboration.go", "CollabAgentToolCallItem")
	for _, required := range []string{"ID", "Tool", "Status", "SenderThreadID", "ReceiverAgents", "Prompt", "AgentsStates", "CreatedAt", "CompletedAt"} {
		if _, exists := protocolFields[required]; !exists {
			t.Errorf("CollabAgentToolCallItem is missing field %q", required)
		}
	}
	turnCompleteFields := architectureStructFields(t, root, "internal/protocol/turn_events.go", "TurnCompleteEvent")
	if _, exists := turnCompleteFields["LastAgentMessage"]; !exists {
		t.Error("TurnCompleteEvent has no authoritative LastAgentMessage")
	}
	agentStateFields := architectureStructFields(t, root, "internal/protocol/collaboration.go", "CollabAgentState")
	if _, exists := agentStateFields["LastTurn"]; !exists {
		t.Error("CollabAgentState drops the terminal AgentTurnResult")
	}
	for _, relative := range []string{"internal/agent/multiagent", "internal/threadmanager", "internal/tool/builtin/multi_agent.go"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		paths := []string{relative}
		if info.IsDir() {
			paths = nil
			err = filepath.WalkDir(filepath.Join(root, filepath.FromSlash(relative)), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if !entry.IsDir() && filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") {
					paths = append(paths, filepath.ToSlash(path[len(root)+1:]))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, path := range paths {
			content := mustReadArchitectureFile(t, root, path)
			for _, forbidden := range []string{"latestAssistant", "persistedAgentStatus", "statusFromEvent", "maxStatusMessageRunes", "AgentPath", "followup_task", "send_message", "resume_agent"} {
				if strings.Contains(content, forbidden) {
					t.Errorf("Basic Multi-Agent retains forbidden legacy/V2 concept %q in %s", forbidden, path)
				}
			}
		}
	}
	for _, forbidden := range []string{"AgentTaskBus", "NestedSessionTask", "map[protocol.ThreadID]protocol.AgentStatus"} {
		for _, relative := range []string{"cmd", "internal/tui"} {
			err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				if strings.Contains(string(content), forbidden) {
					t.Errorf("UI/application owns forbidden agent truth %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scan %s: %v", relative, err)
			}
		}
	}
}

func TestThreadSessionUUIDIdentityArchitectureBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/protocol/identity/thread.go",
		"internal/protocol/identity/session.go",
		"internal/threadmanager/child_resume.go",
		"internal/llm/request_metadata.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("required identity boundary file is missing: %s: %v", relative, err)
		}
	}

	identityFields := architectureStructFields(t, root, "internal/protocol/identity/thread.go", "ThreadID")
	if _, exists := identityFields["value"]; !exists || len(identityFields) != 1 {
		t.Fatalf("ThreadID must be a single-field UUID value object: %#v", identityFields)
	}
	metaFields := architectureStructFields(t, root, "internal/rollout/items.go", "SessionMetaItem")
	for _, required := range []string{"SessionID", "ID", "ParentThreadID", "Source"} {
		if _, exists := metaFields[required]; !exists {
			t.Errorf("SessionMetaItem is missing field %q", required)
		}
	}
	if _, exists := metaFields["ThreadID"]; exists {
		t.Error("SessionMetaItem retains legacy ThreadID field")
	}
	storedFields := architectureStructFields(t, root, "internal/threadstore/metadata.go", "StoredThread")
	if _, exists := storedFields["SessionID"]; exists {
		t.Error("SQLite StoredThread must not duplicate SessionID")
	}
	invocationFields := architectureStructFields(t, root, "internal/tool/domain.go", "Invocation")
	for _, required := range []string{"SessionID", "ThreadID", "TurnID"} {
		if _, exists := invocationFields[required]; !exists {
			t.Errorf("Tool Invocation is missing field %q", required)
		}
	}

	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "/internal/testutil/") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			source := string(content)
			for _, forbidden := range []string{`NextID("thread")`, "protocol.ThreadID(", "identity.ThreadID("} {
				if strings.Contains(source, forbidden) {
					t.Errorf("legacy identity construction %q remains in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s identity boundaries: %v", relative, err)
		}
	}
}
