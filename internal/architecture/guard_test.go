package architecture_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTargetArchitectureRejectsRemovedProductionSymbols(t *testing.T) {
	root := repositoryRoot(t)
	forbidden := []string{
		"ChatSession", "PreviousWork", "ConversationSession", "ConversationSummary", "SessionRuntime",
		"RunRuntime", "RunContext", "RunState", "RunID", "TurnRuntime",
		"ResourceStrategy", "ParallelSafe", "ResourceExecutor", "revert_run", "internal/snapshot",
		"Planner", "Replanner", "Scheduler", "ExecutionGraph", "PlanController", "ReActTaskExecutor",
		"Evidence", "Verified", "CriterionIDs", "NoEvidenceThreshold",
		"TargetStrategy", "ArgumentPaths", "preflightPaths",
		"PrimaryRoot", "ProtectedRoots", "ModeDegraded", "ModeWorkspaceWrite", "GrantCache",
		"AllowCommandFilesystemPaths", "RunApprovalStore", "DeniedGlobs", "TemporaryWritePaths",
		"PreparedCall", "PreparedToolCall", "ToolDispatcher", "ToolExecutor", "ToolExecutionGate",
		"ToolAuthorizer", "PostExecutionHook", "PreExecutionHook", "PathGuard",
		"ToolConcurrencyShared", "ToolConcurrencyExclusive",
		"codingTaskFactory", "codingTaskRequest", "taskFactories",
		"executeCodingTurn", "executeCompactTurn", "executeReactorTurn",
	}
	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "migration.go" {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, symbol := range forbidden {
				if strings.Contains(string(content), symbol) {
					t.Errorf("removed architecture symbol %q remains in %s", symbol, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
	for _, relative := range []string{"internal/agent/react", "internal/agent/runtime", "internal/agent/reflect", "internal/session", "internal/snapshot"} {
		if _, err := os.Stat(filepath.Join(root, relative)); err == nil {
			t.Errorf("removed production package still exists: %s", relative)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect %s: %v", relative, err)
		}
	}
}

func TestSessionTaskDependencyDirection(t *testing.T) {
	root := repositoryRoot(t)
	taskRoot := filepath.Join(root, "internal", "agent", "task")
	forbidden := []string{
		"cmd/amadeus", "internal/interface/tui", "internal/app/bootstrap",
		"github.com/spf13/cobra", "cobra.Command", "tea.Model",
	}
	err := filepath.WalkDir(taskRoot, func(path string, entry os.DirEntry, walkErr error) error {
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
		for _, dependency := range forbidden {
			if strings.Contains(string(content), dependency) {
				t.Errorf("SessionTask depends on interface/application detail %q in %s", dependency, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentEngineDoesNotReintroduceLegacyComposition(t *testing.T) {
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
			for _, forbidden := range []string{"internal/agent/react", "internal/agent/runtime", "ProgressMonitor", "StopBudgetExhausted"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("legacy Agent Engine concept %q remains in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplicationOwnsCurrentThreadSelection(t *testing.T) {
	root := repositoryRoot(t)
	controllerFiles, err := filepath.Glob(filepath.Join(root, "cmd", "amadeus", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	forbiddenState := []string{"currentThread *", "threadManager *", "threadMutex sync."}
	for _, path := range controllerFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, symbol := range forbiddenState {
			if strings.Contains(string(content), symbol) {
				t.Errorf("CLI owns application Thread selection through %q in %s", symbol, filepath.Base(path))
			}
		}
	}
	applicationPath := filepath.Join(root, "internal", "app", "thread_workspace.go")
	content, err := os.ReadFile(applicationPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"type ThreadWorkspace struct", "current *threadmanager.AmadeusThread", "func (workspace *ThreadWorkspace) EnsureCurrent", "func (workspace *ThreadWorkspace) Resume"} {
		if !strings.Contains(string(content), required) {
			t.Errorf("Application ThreadWorkspace is missing %q", required)
		}
	}
	for _, forbidden := range []string{"github.com/spf13/cobra", "internal/interface/tui", "cmd/amadeus"} {
		if strings.Contains(string(content), forbidden) {
			t.Errorf("Application service depends on interface detail %q", forbidden)
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
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "migration.go" {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, rawWriter := range []string{
				"NewRawItem(rollout.KindResponseItem", "NewRawItem(rollout.KindCompaction",
				"NewRawItem(KindResponseItem", "NewRawItem(KindCompaction",
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

func TestPromptArchitectureKeepsAssetsInternalAndEngineDecoupled(t *testing.T) {
	root := repositoryRoot(t)
	legacyRoot := filepath.Join(root, "prompts")
	if _, err := os.Stat(legacyRoot); err == nil {
		err = filepath.WalkDir(legacyRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				t.Errorf("legacy top-level Prompt package still contains %s", filepath.ToSlash(path[len(root)+1:]))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("inspect legacy Prompt package: %v", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect legacy Prompt package: %v", err)
	}

	engineRoot := filepath.Join(root, "internal", "agent", "engine")
	err := filepath.WalkDir(engineRoot, func(path string, entry os.DirEntry, walkErr error) error {
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
		for _, forbidden := range []string{"AgentSystem("} {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("Engine owns legacy Prompt assembly through %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Engine Prompt dependencies: %v", err)
	}
}

func TestContextArchitectureHasOneCanonicalWriteAndProjectionChain(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/agent/engine", "internal/agent/task"} {
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
			for _, forbidden := range []string{"*agentcontext.Manager", "Context() *agentcontext.Manager", "ReplayToolResults", "observed.Replay"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("Context write/replay compatibility %q remains in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	manager, err := os.ReadFile(filepath.Join(root, "internal", "context", "manager.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"func (manager *Manager) Record", "func (manager *Manager) Replace", "func (manager *Manager) ReplaceUpdate", "func (manager *Manager) UpdateUsage", "func (manager *Manager) ForPrompt", "func (manager *Manager) EstimatePromptTokens"} {
		if strings.Contains(string(manager), forbidden) {
			t.Errorf("legacy ContextManager mutation/projection API remains: %s", forbidden)
		}
	}
	sessionHost, err := os.ReadFile(filepath.Join(root, "internal", "agent", "session", "host.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sessionHost), "session.state.Context.Rebuild(session.History())") {
		t.Fatal("Session is not the canonical ContextManager rebuild owner")
	}
}

func TestToolServiceOwnsArgumentValidation(t *testing.T) {
	root := repositoryRoot(t)
	servicePath := filepath.Join(root, "internal", "tool", "execution_service.go")
	service, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(service), "service.validator.Normalize") {
		t.Fatal("ToolService does not own Tool argument validation")
	}
	aggregatorPath := filepath.Join(root, "internal", "llm", "openai", "tool_call_aggregator.go")
	aggregator, err := os.ReadFile(aggregatorPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(aggregator), "json.Valid") {
		t.Fatal("Provider Tool Call aggregator rejects arguments before ToolService validation")
	}
}

func TestToolsOnlyExecuteThroughToolExecutionService(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "execution_service.go" {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(content), ".Call(ctx, Invocation{") || strings.Contains(string(content), ".Call(ctx, tool.Invocation{") {
				t.Errorf("Tool Handler is invoked outside ToolService in %s", filepath.ToSlash(path[len(root)+1:]))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
}

func TestRunDiffConsumesOnlyTypedPatchDeltas(t *testing.T) {
	root := repositoryRoot(t)
	tracker, err := os.ReadFile(filepath.Join(root, "internal", "diff", "tracker.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(tracker)
	if !strings.Contains(content, "[]patchtool.AppliedPatchDelta") {
		t.Fatal("RunDiff Projector does not consume typed AppliedPatchDelta values")
	}
	for _, forbidden := range []string{"tool.Output", `Metadata["operations"]`, "decodeOperations"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("RunDiff Projector still depends on generic Tool metadata through %q", forbidden)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}
