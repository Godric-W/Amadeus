package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
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
		"CodingFactory", "CodingRuntime", "PrepareRequest", "TaskFactory", "TaskBuilder", "ensureRuntime",
		"TurnHost", "TaskHost", "PromptHost", "ContextHost", "RolloutHost", "ModelSampler", "RunRequest", "RunTurn",
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
	taskRoot := filepath.Join(root, "internal", "agent", "session")
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
	for _, relative := range []string{"internal/agent/engine"} {
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

func TestPromptConstructionHasCodexOwnershipBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	checks := []struct {
		relative  string
		forbidden []string
	}{
		{relative: "internal/agent/session", forbidden: []string{"BaseInstructions llm.BaseInstructions", "baseInstructions:"}},
		{relative: "cmd/amadeus", forbidden: []string{"BaseInstructions:", "mustBaseInstructions"}},
		{relative: "internal/llm/openai", forbidden: []string{"llm.Prompt{"}},
		{relative: "internal/interface/tui", forbidden: []string{"llm.Prompt{"}},
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
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "migration.go" {
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
			if strings.HasPrefix(relativePath, "internal/interface/tui/") && stringLifecycleCheck.Match(content) {
				t.Errorf("TUI infers reconnect lifecycle from display text in %s", relativePath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}

	compactor := mustReadArchitectureFile(t, root, "internal/agent/engine/compactor.go")
	for _, forbidden := range []string{"runtime.client.Complete(", "Reconnecting...", "responseRetryPolicy"} {
		if strings.Contains(compactor, forbidden) {
			t.Errorf("Compactor owns response retry through %q", forbidden)
		}
	}
	if !strings.Contains(compactor, "request.ModelSession.Complete(") {
		t.Fatal("Compactor does not use the Turn-scoped ModelClientSession")
	}

	continuation := mustReadArchitectureFile(t, root, "internal/agent/session/continuation.go")
	for _, required := range []string{
		"modelSession, err := runtime.NewModelClientSession()",
		"session.compactCallback(runtime, modelSession",
		"ModelSession: modelSession",
	} {
		if !strings.Contains(continuation, required) {
			t.Errorf("automatic compaction does not share ModelClientSession: missing %q", required)
		}
	}

	workingView := mustReadArchitectureFile(t, root, "internal/interface/tui/application_view.go")
	workingStart := strings.Index(workingView, "func (model fullscreenModel) workingLine() string")
	if workingStart < 0 {
		t.Fatal("locate workingLine implementation")
	}
	workingEnd := strings.Index(workingView[workingStart:], "\nfunc (model fullscreenModel) runElapsed()")
	if workingEnd < 0 {
		t.Fatal("locate workingLine implementation boundary")
	}
	if strings.Contains(workingView[workingStart:workingStart+workingEnd], `"Working"`) {
		t.Fatal("workingLine hardcodes the Working status header")
	}

	for _, relative := range []string{"internal/rollout", "internal/context", "internal/state"} {
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

func TestModelProviderConfigurationHasCodexOwnershipBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	providerFields := architectureStructFields(t, root, "internal/config/model_provider.go", "ModelProviderInfo")
	wantProviderFields := []string{
		"WireAPI", "Dialect", "APIKey", "BaseURL", "Timeout",
		"RequestMaxRetries", "StreamMaxRetries", "StreamIdleTimeout",
	}
	for _, field := range wantProviderFields {
		if _, ok := providerFields[field]; !ok {
			t.Errorf("ModelProviderInfo is missing transport field %q", field)
		}
	}
	for _, field := range []string{
		"Model", "Temperature", "MaxOutputTokens", "ContextWindow",
		"AutoCompactTokenLimit", "ToolOutputMaxTokens", "ToolOutputTokenLimit",
	} {
		if _, ok := providerFields[field]; ok {
			t.Errorf("ModelProviderInfo owns Model runtime field %q", field)
		}
	}

	configFields := architectureStructFields(t, root, "internal/config/config.go", "Config")
	for _, field := range []string{
		"Model", "ModelProvider", "ModelContextWindow", "ModelAutoCompactTokenLimit",
		"ToolOutputTokenLimit", "ModelProviders",
	} {
		if _, ok := configFields[field]; !ok {
			t.Errorf("Config is missing Model runtime field %q", field)
		}
	}
	for _, field := range []string{"DefaultProvider", "Providers"} {
		if _, ok := configFields[field]; ok {
			t.Errorf("Config retains legacy field %q", field)
		}
	}

	legacyIdentifiers := regexp.MustCompile(`\b(?:ProviderConfig|APIMode|DefaultProvider|APIResponses|APIChatCompletions|ToolOutputMaxTokens|EnvProvider|EnvAPI|flagProvider|flagAPI)\b`)
	legacySampling := regexp.MustCompile(`\b(?:Temperature|MaxOutputTokens)\b`)
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
			relativePath := filepath.ToSlash(path[len(root)+1:])
			if legacyIdentifiers.Match(content) {
				t.Errorf("legacy Model/Provider identifier remains in %s", relativePath)
			}
			for _, legacyTag := range []string{`yaml:"default_provider"`, `yaml:"providers"`, `yaml:"api"`} {
				if strings.Contains(string(content), legacyTag) {
					t.Errorf("legacy configuration source key %q remains in %s", legacyTag, relativePath)
				}
			}
			if legacySampling.Match(content) && !strings.HasPrefix(relativePath, "internal/tool/") {
				t.Errorf("stable model sampling budget remains in %s", relativePath)
			}
			if strings.HasPrefix(relativePath, "internal/tool/") && strings.Contains(string(content), "ToolOutputTokenLimit") {
				t.Errorf("model-visible Tool Output limit entered Tool execution budget in %s", relativePath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}

	for _, relative := range []string{
		"internal/llm/openai/responses_request.go",
		"internal/llm/openai/chat_completions_request.go",
	} {
		content := mustReadArchitectureFile(t, root, relative)
		for _, forbidden := range []string{"Temperature:", "MaxOutputTokens:", "MaxTokens:", "MaxCompletionTokens:"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("ordinary Provider request forces sampling field %q in %s", forbidden, relative)
			}
		}
	}

	compactor := mustReadArchitectureFile(t, root, "internal/agent/engine/compactor.go")
	if !strings.Contains(compactor, "NormalizeResponseItems(projection.Covered, runtime.ModelInfo(), nil)") {
		t.Fatal("Compactor does not use the effective ModelInfo projection policy")
	}
	if strings.Contains(compactor, "runtime.client.Model()") {
		t.Fatal("Compactor bypasses the effective ModelInfo with Adapter metadata")
	}
}

func architectureStructFields(t *testing.T, root, relative, typeName string) map[string]struct{} {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", relative, err)
	}
	fields := make(map[string]struct{})
	ast.Inspect(parsed, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != typeName {
			return true
		}
		structure, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			t.Fatalf("%s in %s is not a struct", typeName, relative)
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				fields[name.Name] = struct{}{}
			}
		}
		return false
	})
	if len(fields) == 0 {
		t.Fatalf("locate struct %s in %s", typeName, relative)
	}
	return fields
}

func mustReadArchitectureFile(t *testing.T, root, relative string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(content)
}
