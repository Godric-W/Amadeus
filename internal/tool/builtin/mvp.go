package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
)

type MVPOptions struct {
	ApplyPatch     ApplyPatchOptions
	ReadFile       ReadFileOptions
	WriteFile      WriteFileOptions
	ListDir        ListDirOptions
	GlobFiles      GlobFilesOptions
	GrepCode       GrepCodeOptions
	ExecuteCommand ExecuteCommandOptions
}

func DefaultMVPOptions() MVPOptions {
	return MVPOptions{
		ApplyPatch: ApplyPatchOptions{Executor: patchExecutorDefaults()},
		ReadFile:   ReadFileOptions{MaxBytes: 2 << 20},
		WriteFile:  WriteFileOptions{MaxBytes: 2 << 20},
		ListDir:    ListDirOptions{MaxEntries: 1_000},
		GlobFiles:  GlobFilesOptions{MaxResults: 1_000},
		GrepCode: GrepCodeOptions{
			MaxResults: 200, MaxFileBytes: 2 << 20, MaxContextLines: 5, MaxRGOutputBytes: 4 << 20,
		},
		ExecuteCommand: ExecuteCommandOptions{
			DefaultTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute,
			MaxOutputBytes: 1 << 20, MaxOutputLines: 5_000,
		},
	}
}

func patchExecutorDefaults() patchtool.ExecutorOptions {
	return patchtool.ExecutorOptions{MaxFileBytes: 2 << 20, FileMode: os.FileMode(0o644)}
}

func MVPSpecs() []tool.Spec {
	specs := []tool.Spec{applyPatchSpec(), executeCommandSpec(), globFilesSpec(), grepCodeSpec(), listDirSpec(), readFileSpec(), writeFileSpec()}
	for index := range specs {
		specs[index] = specs[index].Clone()
	}
	return specs
}

func NewMVPRegistry(root project.Root, options MVPOptions) (*tool.Registry, error) {
	registry := tool.NewRegistry()
	if err := RegisterMVP(registry, root, options); err != nil {
		return nil, err
	}
	return registry, nil
}

func RegisterMVP(registry *tool.Registry, root project.Root, options MVPOptions) error {
	if registry == nil {
		return errors.New("register MVP tools: registry is nil")
	}
	applyPatch, err := NewApplyPatch(root, options.ApplyPatch)
	if err != nil {
		return err
	}
	readFile, err := NewReadFile(root, options.ReadFile)
	if err != nil {
		return err
	}
	writeFile, err := NewWriteFile(root, options.WriteFile)
	if err != nil {
		return err
	}
	listDir, err := NewListDir(root, options.ListDir)
	if err != nil {
		return err
	}
	globFiles, err := NewGlobFiles(root, options.GlobFiles)
	if err != nil {
		return err
	}
	grepCode, err := NewGrepCode(root, options.GrepCode)
	if err != nil {
		return err
	}
	executeCommand, err := NewExecuteCommand(root, options.ExecuteCommand)
	if err != nil {
		return err
	}
	for _, candidate := range []tool.Tool{applyPatch, readFile, writeFile, listDir, globFiles, grepCode, executeCommand} {
		if err := registry.Register(candidate); err != nil {
			return fmt.Errorf("register MVP tool %q: %w", candidate.Spec().Name, err)
		}
	}
	return nil
}

func applyPatchSpec() tool.Spec {
	return tool.Spec{
		Name: "apply_patch", Description: "Preferred tool for editing existing files: apply a versioned, uniquely context-matched create/update/delete patch after full preflight.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"patch":{"type":"string","minLength":1,"maxLength":1048576}},"required":["patch"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectWrite, ParallelSafe: false, Idempotent: false,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive},
	}
}

func readFileSpec() tool.Spec {
	return tool.Spec{
		Name: "read_file", Description: "Preferred over shell file reads: read a UTF-8 project file using a zero-based line offset and optional line limit.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
}

func writeFileSpec() tool.Spec {
	return tool.Spec{
		Name: "write_file", Description: "Atomically create a new UTF-8 project file or explicitly replace an existing whole file; use apply_patch for normal edits.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"content":{"type":"string"},"mode":{"type":"string","enum":["create","replace"]}},"required":["path","content","mode"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectWrite, ParallelSafe: false, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
}

func listDirSpec() tool.Spec {
	return tool.Spec{
		Name: "list_dir", Description: "Preferred over shell directory listing: list a project-relative directory in stable name order.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
}

func globFilesSpec() tool.Spec {
	return tool.Spec{
		Name: "glob_files", Description: "Preferred over shell find/glob: discover project files with a slash-separated pattern; ** matches zero or more path segments.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","minLength":1},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"required":["pattern"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"pattern"}},
	}
}

func grepCodeSpec() tool.Spec {
	return tool.Spec{
		Name: "grep_code", Description: "Preferred over shell grep for routine search: scan project text files with stable line numbers, optional regular expressions, and bounded context.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"path":{"type":"string"},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"},"context":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["query"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path", "query"}},
	}
}

func executeCommandSpec() tool.Spec {
	return tool.Spec{
		Name: "execute_command", Description: "Run builds, tests, Git, formatting, generators, project scripts, or legitimate fallback commands in a fixed project-relative directory; never use it to bypass tool policy.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1},"cwd":{"type":"string"},"timeout_ms":{"type":"integer","minimum":1}},"required":["command"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectExecute, ParallelSafe: false, Idempotent: false,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive},
	}
}
