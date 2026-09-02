package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAEGoalArchitectureBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/state/state.go",
		"internal/state/sqlite/runtime.go",
		"internal/state/sqlite/goal_store.go",
		"internal/extension/registry.go",
		"internal/extension/goal/extension.go",
		"internal/extension/goal/runtime.go",
		"internal/extension/goal/service.go",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("required AE owner %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "internal/threadstore/local/sqlite")); !os.IsNotExist(err) {
		t.Fatalf("legacy ThreadStore-owned SQLite package still exists: %v", err)
	}

	event := mustReadArchitectureFile(t, root, "internal/protocol/event.go")
	if !strings.Contains(event, "ID  EventID") || strings.Contains(event, "ID  SubmissionID") {
		t.Fatal("Event.ID is not owned by EventID")
	}
	input := mustReadArchitectureFile(t, root, "internal/agent/session/input_queue.go")
	for _, required := range []string{"ResponseItemTurnInput", "rollout.ResponseItem"} {
		if !strings.Contains(input, required) {
			t.Fatalf("typed automatic TurnInput missing %q", required)
		}
	}
	start := mustReadArchitectureFile(t, root, "internal/agent/session/start_if_idle.go")
	for _, required := range []string{"StartIfIdleSubmission", "NotSubmittedPlanMode", "NotSubmittedPendingTriggerTurn"} {
		if !strings.Contains(start, required) {
			t.Fatalf("StartIfIdle contract missing %q", required)
		}
	}
	goalRuntime := mustReadArchitectureFile(t, root, "internal/extension/goal/runtime.go")
	for _, required := range []string{"stateLock", "acquireGoalState", "accountTurnProgress", "continueIfIdle"} {
		if !strings.Contains(goalRuntime, required) {
			t.Fatalf("Goal runtime contract missing %q", required)
		}
	}
}
