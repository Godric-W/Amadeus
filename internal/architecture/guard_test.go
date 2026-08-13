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
	for _, relative := range []string{"internal/agent/engine", "internal/agent/reflect", "internal/agent/runtime", "internal/session", "internal/snapshot"} {
		if _, err := os.Stat(filepath.Join(root, relative)); err == nil {
			t.Errorf("removed production package still exists: %s", relative)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect %s: %v", relative, err)
		}
	}
}

func TestPromptArchitectureKeepsAssetsInternalAndReactorDecoupled(t *testing.T) {
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

	reactorRoot := filepath.Join(root, "internal", "agent", "react")
	err := filepath.WalkDir(reactorRoot, func(path string, entry os.DirEntry, walkErr error) error {
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
		for _, forbidden := range []string{"internal/prompt", "AgentSystem("} {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("Reactor owns Prompt asset through %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Reactor Prompt dependencies: %v", err)
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
