package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type MVPOptions struct {
	ReadFile       ReadFileOptions
	WriteFile      WriteFileOptions
	ListDir        ListDirOptions
	GlobFiles      GlobFilesOptions
	GrepCode       GrepCodeOptions
	ExecuteCommand ExecuteCommandOptions
}

func DefaultMVPOptions() MVPOptions {
	return MVPOptions{
		ReadFile:  ReadFileOptions{MaxBytes: 2 << 20},
		WriteFile: WriteFileOptions{MaxBytes: 2 << 20},
		ListDir:   ListDirOptions{MaxEntries: 1_000},
		GlobFiles: GlobFilesOptions{MaxResults: 1_000},
		GrepCode: GrepCodeOptions{
			MaxResults: 200, MaxFileBytes: 2 << 20, MaxContextLines: 5, MaxRGOutputBytes: 4 << 20,
		},
		ExecuteCommand: ExecuteCommandOptions{
			DefaultTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute,
			MaxOutputBytes: 1 << 20, MaxOutputLines: 5_000,
		},
	}
}

func MVPSpecs() []tool.Spec {
	specs := []tool.Spec{executeCommandSpec(), globFilesSpec(), grepCodeSpec(), listDirSpec(), readFileSpec(), writeFileSpec()}
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
	for _, candidate := range []tool.Tool{readFile, writeFile, listDir, globFiles, grepCode, executeCommand} {
		if err := registry.Register(candidate); err != nil {
			return fmt.Errorf("register MVP tool %q: %w", candidate.Spec().Name, err)
		}
	}
	return nil
}

func readFileSpec() tool.Spec {
	return tool.Spec{
		Name: "read_file", Description: "Read a UTF-8 text file from the project using a zero-based line offset and optional line limit.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
}

func writeFileSpec() tool.Spec {
	return tool.Spec{
		Name: "write_file", Description: "Atomically write UTF-8 text to a project-relative file, creating parent directories when needed.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectWrite, ParallelSafe: false, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
}

func listDirSpec() tool.Spec {
	return tool.Spec{
		Name: "list_dir", Description: "List a project-relative directory in stable name order.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path"}},
	}
}

func globFilesSpec() tool.Spec {
	return tool.Spec{
		Name: "glob_files", Description: "Find project files matching a slash-separated glob pattern; ** matches zero or more path segments.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","minLength":1},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"required":["pattern"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"pattern"}},
	}
}

func grepCodeSpec() tool.Spec {
	return tool.Spec{
		Name: "grep_code", Description: "Search project text files with stable line numbers, optional regular expressions, and bounded context.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"path":{"type":"string"},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"},"context":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["query"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"path", "query"}},
	}
}

func executeCommandSpec() tool.Spec {
	return tool.Spec{
		Name: "execute_command", Description: "Execute a shell command in a fixed project-relative working directory with timeout and bounded combined output.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1},"cwd":{"type":"string"},"timeout_ms":{"type":"integer","minimum":1}},"required":["command"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectExecute, ParallelSafe: false, Idempotent: false,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive},
	}
}
