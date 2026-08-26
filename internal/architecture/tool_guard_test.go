package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestOuterCLIArchitectureUsesOwnedPackages(t *testing.T) {
	root := repositoryRoot(t)
	commandDirectory := filepath.Join(root, "cmd", "amadeus")
	entries, err := os.ReadDir(commandDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		if entry.Name() != "main.go" {
			t.Errorf("cmd/amadeus retains non-entry Go file %q", entry.Name())
		}
	}
	mainSource := mustReadArchitectureFile(t, root, "cmd/amadeus/main.go")
	for _, required := range []string{"internal/cli", "cli.Run("} {
		if !strings.Contains(mainSource, required) {
			t.Errorf("thin main is missing %q", required)
		}
	}
	for _, forbidden := range []string{"github.com/spf13/cobra", "ThreadWorkspace", "ThreadManager", "SessionIo"} {
		if strings.Contains(mainSource, forbidden) {
			t.Errorf("thin main owns forbidden concern %q", forbidden)
		}
	}

	for _, relative := range []string{
		"internal/cli/root.go", "internal/cli/agent.go", "internal/cli/config_loader.go",
		"internal/bootstrap/workspace.go", "internal/bootstrap/adapters.go",
		"internal/tui/run.go", "internal/tui/user_message.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Errorf("outer package boundary file is missing: %s: %v", relative, err)
		}
	}
	userMessageFields := architectureStructFields(t, root, "internal/tui/user_message.go", "UserMessage")
	if _, ok := userMessageFields["Text"]; !ok {
		t.Error("TUI UserMessage is missing Text")
	}
	applicationOptions := architectureStructFields(t, root, "internal/tui/application.go", "ApplicationOptions")
	if _, ok := applicationOptions["InitialUserMessage"]; !ok {
		t.Error("ApplicationOptions is missing InitialUserMessage")
	}
	model := architectureStructFields(t, root, "internal/tui/application.go", "appModel")
	if _, ok := model["initialUserMessage"]; !ok {
		t.Error("model does not own pending initialUserMessage")
	}

	for _, relative := range []string{"internal/exec", "internal/render", "internal/tui/inline.go"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err == nil {
			t.Errorf("removed one-shot frontend path still exists: %s", relative)
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect removed path %s: %v", relative, err)
		}
	}

	legacy := []string{
		"type agentController", "type commandRuntime", "type agentInvocation", "type agentCommandFactory",
		"func (runner *agentController) waitTurn", "type ExecRunner", "agentLaunchOnce", "readRootTask", "type InlineRenderer",
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
			for _, symbol := range legacy {
				if strings.Contains(string(content), symbol) {
					t.Errorf("legacy outer CLI concept %q remains in %s", symbol, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}

	for _, relative := range []string{"internal/agent", "internal/contextmanager", "internal/threadstore", "internal/tool", "internal/policy"} {
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
			for _, forbidden := range []string{"internal/cli", "internal/bootstrap", "internal/exec", "internal/tui"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("domain/runtime file %s depends on outer package %q", filepath.ToSlash(path[len(root)+1:]), forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan dependency root %s: %v", relative, err)
		}
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
		"AutoCompactTokenLimit", "ToolOutputMaxTokens", "ToolOutputTokenLimit", "ModelReasoningEffort", "ReasoningEffort",
		"InputModalities", "SupportsOriginalImageDetail",
	} {
		if _, ok := providerFields[field]; ok {
			t.Errorf("ModelProviderInfo owns Model runtime field %q", field)
		}
	}

	configFields := architectureStructFields(t, root, "internal/config/config.go", "Config")
	for _, field := range []string{
		"Model", "ModelProvider", "ModelContextWindow", "ModelReasoningEffort", "ModelInputModalities", "ModelSupportsOriginalImageDetail", "ModelAutoCompactTokenLimit",
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
	if _, ok := configFields["Version"]; ok {
		t.Error("Config retains removed schema Version field")
	}

	legacyIdentifiers := regexp.MustCompile(`\b(?:ProviderConfig|APIMode|DefaultProvider|APIResponses|APIChatCompletions|ToolOutputMaxTokens|EnvProvider|EnvAPI|flagProvider|flagAPI)\b`)
	legacySampling := regexp.MustCompile(`\b(?:Temperature|MaxOutputTokens)\b`)
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

	compactor := mustReadArchitectureFile(t, root, "internal/agent/compact/service.go")
	if !strings.Contains(compactor, "cloneItems(request.Source.PromptItems)") {
		t.Fatal("CompactionService does not use the exact StepContext prompt projection")
	}
	if strings.Contains(compactor, ".modelClient.Model()") || strings.Contains(compactor, ".client.Model()") {
		t.Fatal("Compactor bypasses the effective ModelInfo with Adapter metadata")
	}
}

func TestUserConfigSchemaIsVersionlessAndExampleNameIsCanonical(t *testing.T) {
	root := repositoryRoot(t)
	configRoot := filepath.Join(root, "internal", "config")
	err := filepath.WalkDir(configRoot, func(path string, entry os.DirEntry, walkErr error) error {
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
		for _, forbidden := range []string{"CurrentVersion", `yaml:"version"`, "configured.Version", "patch.Version"} {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("versioned user config concern %q remains in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan versionless config boundary: %v", err)
	}

	currentExample := filepath.Join(root, "configs", "config.yaml.example")
	content, err := os.ReadFile(currentExample)
	if err != nil {
		t.Fatalf("canonical config example is missing: %v", err)
	}
	if regexp.MustCompile(`(?m)^version\s*:`).Match(content) {
		t.Fatal("canonical config example contains removed version field")
	}
	legacyExample := filepath.Join(root, "configs", "amadeus.example.yaml")
	if _, err := os.Stat(legacyExample); err == nil {
		t.Fatal("legacy config example path still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect legacy config example: %v", err)
	}

	for _, relative := range []string{"README.md", "docs/design.md"} {
		source := mustReadArchitectureFile(t, root, relative)
		if strings.Contains(source, "configs/amadeus.example.yaml") {
			t.Errorf("current documentation %s references legacy config example", relative)
		}
	}
}

func TestViewImageArchitectureBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	viewImage := mustReadArchitectureFile(t, root, "internal/tool/builtin/view_image.go")
	if !strings.Contains(viewImage, "internal/imageprep") {
		t.Fatal("view_image does not delegate image preparation")
	}
	for _, forbidden := range []string{"encoding/base64", `"image/gif"`, `"image/png"`, "image.Decode", "gif.Decode", "base64.StdEncoding"} {
		if strings.Contains(viewImage, forbidden) {
			t.Errorf("view_image retains image processing concern %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "imageprep", "prepare.go")); err != nil {
		t.Fatalf("image preparation package is missing: %v", err)
	}

	runtimeTools := mustReadArchitectureFile(t, root, "internal/agent/session/tool_runtime.go")
	if strings.Contains(runtimeTools, "provider.images") || strings.Contains(runtimeTools, "Capabilities().SupportsImages") {
		t.Fatal("view_image visibility still derives from Provider/Dialect capability")
	}
	if !strings.Contains(runtimeTools, "model.image_input") || !strings.Contains(runtimeTools, "SupportsInput(llm.InputModalityImage)") {
		t.Fatal("view_image visibility does not derive from ModelInfo image input")
	}

	adapter := mustReadArchitectureFile(t, root, "internal/llm/openai/adapter.go")
	modelStart := strings.Index(adapter, "func (adapter *Adapter) Model() llm.ModelInfo")
	capabilitiesStart := strings.Index(adapter, "func (adapter *Adapter) Capabilities()")
	if modelStart < 0 || capabilitiesStart < modelStart {
		t.Fatal("locate Adapter.Model boundary")
	}
	if strings.Contains(adapter[modelStart:capabilitiesStart], "SupportsImages") {
		t.Fatal("Adapter.Model derives model modalities from transport image support")
	}

	toolEvents := mustReadArchitectureFile(t, root, "internal/agent/session/tool_events.go")
	if !strings.Contains(toolEvents, "DisplaySafeClone") || strings.Contains(toolEvents, "ToolResult: &result") {
		t.Fatal("completed Tool events can retain canonical image payload")
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
			if strings.Contains(string(content), "provider.images") {
				t.Errorf("legacy provider.images condition remains in %s", filepath.ToSlash(path[len(root)+1:]))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
}
