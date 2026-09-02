package architecture_test

import (
	"os"
	"path/filepath"
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
		"CodingFactory", "CodingRuntime", "PrepareRequest", "TaskFactory", "TaskBuilder", "ensureRuntime",
		"TurnHost", "TaskHost", "PromptHost", "ContextHost", "RolloutHost", "ModelSampler", "RunRequest", "RunTurn",
		"UserInputResponseOp",
		"KindTurnItemCompleted", "TurnItemCompleted", "NewCompletedItem", "ProjectThreadItems",
		"AgentServices", "ServicesBuilder", "SessionSetup", "TaskConstructors", "CapabilityView",
		"ExtensionAssembly", "WorkspaceResolver", "InstructionScope", "targetInstructionScope",
		"InteractiveRequest", "AllowedTools", "ToolRevision", "context_refresh_required",
		"PlanUpdater", "SessionState.Plan", "pendingModeTask", "CollaborationExecute", "CollaborationPlan", "ModeState",
		"InstructionResolution", "migrateConfigDocument", "SteerOp", "compact bool",
		"NewApplyPatch", "ApplyPatchOptions", "applyPatchSpec", "type ApplyPatch struct",
		"ContextUpdateEvent", "contextUpdateState", "ToolPromptOrder", "toolGuidance(",
		"SubagentDeveloperInstructions", "SummarizationPrompt           string", "SummaryPrefix                 string",
		"step.Prompt",
		"schema_migrations", "func migrate(",
	}
	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
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
	for _, relative := range []string{
		"internal/agent/react", "internal/agent/runtime", "internal/agent/reflect", "internal/session", "internal/snapshot",
		"internal/agent/engine", "internal/agent/turn",
		"internal/instruction", "internal/tool/patch", "internal/diff",
		"internal/prompt/builtin/templates/tools",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err == nil {
			t.Errorf("removed production package still exists: %s", relative)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect %s: %v", relative, err)
		}
	}
}
