package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSessionTaskDependencyDirection(t *testing.T) {
	root := repositoryRoot(t)
	taskRoot := filepath.Join(root, "internal", "agent", "session")
	forbidden := []string{
		"cmd/amadeus", "internal/cli", "internal/bootstrap", "internal/exec", "internal/tui",
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
		for _, identifier := range []string{"RuntimeOptions", "Prepared"} {
			if regexp.MustCompile(`\b` + identifier + `\b`).Match(content) {
				t.Errorf("removed architecture symbol %q remains in %s", identifier, filepath.ToSlash(path[len(root)+1:]))
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
	for _, forbidden := range []string{"github.com/spf13/cobra", "internal/tui", "cmd/amadeus"} {
		if strings.Contains(string(content), forbidden) {
			t.Errorf("Application service depends on interface detail %q", forbidden)
		}
	}
}

func TestPromptArchitectureKeepsAssetsInternalAndModelClientDecoupled(t *testing.T) {
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

	modelClientRoot := filepath.Join(root, "internal", "agent", "modelclient")
	err := filepath.WalkDir(modelClientRoot, func(path string, entry os.DirEntry, walkErr error) error {
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
				t.Errorf("ModelClient owns legacy Prompt assembly through %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan ModelClient Prompt dependencies: %v", err)
	}
}

func TestContextArchitectureHasOneCanonicalWriteAndProjectionChain(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{"internal/agent/modelclient"} {
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
			for _, forbidden := range []string{"*contextmanager.Manager", "Context() *contextmanager.Manager", "ReplayToolResults", "observed.Replay"} {
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
	manager, err := os.ReadFile(filepath.Join(root, "internal", "contextmanager", "manager.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"func (manager *Manager) Replace", "func (manager *Manager) ReplaceUpdate", "func (manager *Manager) UpdateUsage", "func (manager *Manager) ForPrompt", "func (manager *Manager) EstimatePromptTokens"} {
		if strings.Contains(string(manager), forbidden) {
			t.Errorf("legacy ContextManager mutation/projection API remains: %s", forbidden)
		}
	}
	sessionHost, err := os.ReadFile(filepath.Join(root, "internal", "agent", "session", "persistence.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"session.state.Context.ValidateRecord", "session.state.Context.Record"} {
		if !strings.Contains(string(sessionHost), required) {
			t.Fatalf("Session does not own incremental ContextManager commit through %q", required)
		}
	}
	for _, forbidden := range []string{"SessionState.History", "session.History()", "session.state.Context.Rebuild"} {
		if strings.Contains(string(sessionHost), forbidden) {
			t.Fatalf("Session retains legacy ContextManager rebuild path %q", forbidden)
		}
	}
}

func TestPromptConstructionHasCodexOwnershipBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	checks := []struct {
		relative  string
		forbidden []string
	}{
		{relative: "internal/agent/session", forbidden: []string{"BaseInstructions llm.BaseInstructions", "baseInstructions:"}},
		{relative: "cmd/amadeus", forbidden: []string{"BaseInstructions:", "mustBaseInstructions"}},
		{relative: "internal/llm/openai", forbidden: []string{"llm.Prompt{"}},
		{relative: "internal/tui", forbidden: []string{"llm.Prompt{"}},
	}
	for _, check := range checks {
		err := filepath.WalkDir(filepath.Join(root, check.relative), func(path string, entry os.DirEntry, walkErr error) error {
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
			for _, forbidden := range check.forbidden {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("Prompt ownership violation %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", check.relative, err)
		}
	}
}

func TestResponseStreamReconnectHasCodexOwnershipBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	legacyMaxRetries := regexp.MustCompile(`\bMaxRetries\b`)
	stringLifecycleCheck := regexp.MustCompile(`strings\.(?:Contains|HasPrefix|HasSuffix)\([^\n]*[Rr]econnect`)
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
			relativePath := filepath.ToSlash(path[len(root)+1:])
			if legacyMaxRetries.Match(content) {
				t.Errorf("legacy MaxRetries production field remains in %s", relativePath)
			}
			if strings.HasPrefix(relativePath, "internal/tui/") && stringLifecycleCheck.Match(content) {
				t.Errorf("TUI infers reconnect lifecycle from display text in %s", relativePath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}

	compactor := mustReadArchitectureFile(t, root, "internal/agent/compact/service.go")
	for _, forbidden := range []string{"runtime.client.Complete(", "Reconnecting...", "responseRetryPolicy"} {
		if strings.Contains(compactor, forbidden) {
			t.Errorf("Compactor owns response retry through %q", forbidden)
		}
	}
	if !strings.Contains(compactor, "request.ModelSession.Complete(") {
		t.Fatal("Compactor does not use the Turn-scoped ModelClientSession")
	}

	regularTask := mustReadArchitectureFile(t, root, "internal/agent/session/regular_task.go")
	if !strings.Contains(regularTask, "modelSession, err := sessionTask.runtime.NewModelClientSession()") {
		t.Fatal("RegularTask does not create one Turn-scoped ModelClientSession")
	}
	continuation := mustReadArchitectureFile(t, root, "internal/agent/session/continuation.go")
	if !strings.Contains(continuation, "session.runCompaction(ctx, runtime, modelSession") {
		t.Fatal("automatic compaction does not receive the Turn-scoped ModelClientSession")
	}

	workingView := mustReadArchitectureFile(t, root, "internal/tui/composer_view.go")
	workingStart := strings.Index(workingView, "func (model appModel) workingLine() string")
	if workingStart < 0 {
		t.Fatal("locate workingLine implementation")
	}
	workingEnd := strings.Index(workingView[workingStart:], "\nfunc (model appModel) runElapsed()")
	if workingEnd < 0 {
		t.Fatal("locate workingLine implementation boundary")
	}
	if strings.Contains(workingView[workingStart:workingStart+workingEnd], `"Working"`) {
		t.Fatal("workingLine hardcodes the Working status header")
	}

	for _, relative := range []string{"internal/rollout", "internal/contextmanager", "internal/threadstore"} {
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
			if strings.Contains(string(content), "WillRetry") || strings.Contains(string(content), "StreamError") {
				t.Errorf("transient stream retry entered canonical/replay package %s", filepath.ToSlash(path[len(root)+1:]))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
}

func TestContextAccountingAndCompactionHaveWOwnershipBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "agent", "engine", "compactor.go")); !os.IsNotExist(err) {
		t.Fatal("legacy engine Compactor remains")
	}
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
			relativePath := filepath.ToSlash(path[len(root)+1:])
			for _, forbidden := range []string{"type Usage struct", "ContextCompactedEvent", "ReplacementMessage", "compactFunc", "compactCallback", "UsageItem("} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("legacy W symbol %q remains in %s", forbidden, relativePath)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
	service := mustReadArchitectureFile(t, root, "internal/agent/compact/service.go")
	for _, forbidden := range []string{"internal/agent/session", "internal/rollout"} {
		if strings.Contains(service, forbidden) {
			t.Errorf("CompactionService depends on owner %q", forbidden)
		}
	}
	rolloutItems := mustReadArchitectureFile(t, root, "internal/rollout/items.go")
	if strings.Contains(rolloutItems, "internal/agent/compact") {
		t.Fatal("Rollout depends on runtime CompactionService package")
	}
	sessionCompaction := mustReadArchitectureFile(t, root, "internal/agent/session/compaction.go")
	for _, required := range []string{"recordTokenUsage(", "appendItemsDurable(", "CompactedItem", "ActiveContextTokens"} {
		if !strings.Contains(sessionCompaction, required) {
			t.Errorf("Session compaction lacks %q", required)
		}
	}
}

func TestReasoningEffortHasTurnScopedProviderBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	reasoningFields := architectureStructFields(t, root, "internal/llm/reasoning.go", "ReasoningConfig")
	if len(reasoningFields) != 1 {
		t.Fatalf("ReasoningConfig fields = %v, want only Effort", reasoningFields)
	}
	if _, ok := reasoningFields["Effort"]; !ok {
		t.Fatal("ReasoningConfig is missing Effort")
	}
	for _, forbidden := range []string{"Enabled", "Preserve"} {
		if _, ok := reasoningFields[forbidden]; ok {
			t.Errorf("ReasoningConfig retains legacy field %q", forbidden)
		}
	}

	turnFields := architectureStructFields(t, root, "internal/agent/session/turn_context.go", "TurnContext")
	if _, ok := turnFields["ReasoningEffort"]; !ok {
		t.Fatal("TurnContext does not freeze ReasoningEffort")
	}
	rolloutFields := architectureStructFields(t, root, "internal/rollout/items.go", "TurnContextItem")
	if _, ok := rolloutFields["ReasoningEffort"]; !ok {
		t.Fatal("TurnContextItem does not persist ReasoningEffort")
	}

	continuation := mustReadArchitectureFile(t, root, "internal/agent/session/continuation.go")
	compactionRuntime := mustReadArchitectureFile(t, root, "internal/agent/session/compaction.go")
	if !strings.Contains(continuation, "ReasoningConfigForEffort(turnContext.ReasoningEffort)") || !strings.Contains(compactionRuntime, "ReasoningConfigForEffort(turnContext.ReasoningEffort)") {
		t.Fatal("regular sampling and automatic compaction do not share frozen Turn effort")
	}
	compactTask := mustReadArchitectureFile(t, root, "internal/agent/session/compact_task.go")
	if !strings.Contains(compactTask, "session.runCompaction(") {
		t.Fatal("manual compaction does not use the Session-owned compaction lifecycle")
	}
	compactor := mustReadArchitectureFile(t, root, "internal/agent/compact/service.go")
	if !strings.Contains(compactor, "Reasoning: request.Reasoning.Clone()") {
		t.Fatal("Compactor does not propagate reasoning into the wire request")
	}

	responses := mustReadArchitectureFile(t, root, "internal/llm/openai/responses_request.go")
	if !strings.Contains(responses, "shared.ReasoningParam{Effort:") {
		t.Fatal("Responses request does not serialize reasoning.effort")
	}
	chat := mustReadArchitectureFile(t, root, "internal/llm/openai/reasoning_request.go")
	for _, required := range []string{"params.ReasoningEffort", `"enable_thinking": false`, `"type": "disabled"`} {
		if !strings.Contains(chat, required) {
			t.Errorf("Chat effort mapping is missing %q", required)
		}
	}
	dialect := mustReadArchitectureFile(t, root, "internal/llm/openai/dialect.go")
	qwenStart := strings.Index(dialect, "case config.DialectQwen:")
	if qwenStart < 0 || !strings.Contains(dialect[qwenStart:], "supportedWireAPIs: bothAPIs") {
		t.Fatal("Qwen dialect does not declare Responses support")
	}
}
