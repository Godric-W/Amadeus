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
	PathGuard      *project.PathGuard
	ApplyPatch     ApplyPatchOptions
	ReadFile       ReadFileOptions
	ListDir        ListDirOptions
	GlobFiles      GlobFilesOptions
	GrepCode       GrepCodeOptions
	ExecuteCommand ExecuteCommandOptions
}

func DefaultMVPOptions() MVPOptions {
	return MVPOptions{
		ApplyPatch: ApplyPatchOptions{Executor: patchExecutorDefaults()},
		ReadFile:   ReadFileOptions{MaxBytes: 2 << 20, MaxLineBytes: 32 << 10},
		ListDir:    ListDirOptions{MaxEntries: 1_000},
		GlobFiles:  GlobFilesOptions{MaxResults: 1_000, MaxRGOutputBytes: 4 << 20},
		GrepCode: GrepCodeOptions{
			MaxResults: 200, MaxFileBytes: 2 << 20, MaxContextLines: 5, MaxRGOutputBytes: 4 << 20,
		},
		ExecuteCommand: ExecuteCommandOptions{
			DefaultTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute,
			DefaultYield: 10 * time.Second, MaxYield: 30 * time.Second,
			MaxOutputBytes: 1 << 20, MaxOutputLines: 5_000, MaxOutputTokens: 64_000,
		},
	}
}

func patchExecutorDefaults() patchtool.ExecutorOptions {
	return patchtool.ExecutorOptions{MaxFileBytes: 2 << 20, FileMode: os.FileMode(0o644)}
}

func MVPSpecs() []tool.Spec {
	specs := []tool.Spec{applyPatchSpec(), executeCommandSpec(), globFilesSpec(), grepCodeSpec(), listDirSpec(), readFileSpec(), writeStdinSpec()}
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
	if options.PathGuard != nil {
		options.ApplyPatch.Executor.PathGuard = options.PathGuard
		options.ReadFile.PathGuard = options.PathGuard
		options.ListDir.PathGuard = options.PathGuard
		options.GlobFiles.PathGuard = options.PathGuard
		options.GrepCode.PathGuard = options.PathGuard
		options.ExecuteCommand.PathGuard = options.PathGuard
	}
	applyPatch, err := NewApplyPatch(root, options.ApplyPatch)
	if err != nil {
		return err
	}
	readFile, err := NewReadFile(root, options.ReadFile)
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
	writeStdin, err := NewWriteStdin(WriteStdinOptions{Manager: executeCommand.ProcessManager()})
	if err != nil {
		return err
	}
	for _, candidate := range []tool.Tool{applyPatch, readFile, listDir, globFiles, grepCode, executeCommand, writeStdin} {
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
		SideEffect:  tool.SideEffectWrite, Concurrency: tool.ToolConcurrencyExclusive, Idempotent: false,
	}
}

func readFileSpec() tool.Spec {
	return tool.Spec{
		Name: "read_file", Description: "Preferred over shell file reads: stream a UTF-8 file allowed by FileSystemPolicy using a one-based start line and optional line limit; accepts absolute or cwd-relative paths and returns stable continuation metadata.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"line":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true,
	}
}

func listDirSpec() tool.Spec {
	return tool.Spec{
		Name: "list_dir", Description: "Preferred over shell directory listing: list a FileSystemPolicy-readable directory in stable name order; accepts absolute or cwd-relative paths.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true,
	}
}

func globFilesSpec() tool.Spec {
	return tool.Spec{
		Name: "glob_files", Description: "Preferred over shell file discovery: match paths below an optional FileSystemPolicy-readable directory using slash globs and ** while honoring workspace ignore files.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"pattern":{"type":"string","minLength":1},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"required":["pattern"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true,
	}
}

func grepCodeSpec() tool.Spec {
	return tool.Spec{
		Name: "grep_code", Description: "Preferred over shell grep for routine search: scan project text with stable file, line and column output plus optional path, glob, type and bounded context filters.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"path":{"type":"string"},"glob":{"type":"string"},"type":{"type":"string"},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"},"context":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["query"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true,
	}
}

func executeCommandSpec() tool.Spec {
	return tool.Spec{
		Name: "execute_command", Description: "Run builds, tests, Git, formatting, generators, project scripts, or legitimate fallback commands. Declare known extra writable roots in requested_permissions; Amadeus does not infer paths from the shell command; never use it to bypass tool policy.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1},"cwd":{"type":"string"},"timeout_ms":{"type":"integer","minimum":1},"yield_time_ms":{"type":"integer","minimum":0},"max_output_tokens":{"type":"integer","minimum":1},"tty":{"type":"boolean"},"requested_permissions":{"type":"object","properties":{"writable_roots":{"type":"array","items":{"type":"string","minLength":1},"uniqueItems":true}},"additionalProperties":false}},"required":["command"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectExecute, Concurrency: tool.ToolConcurrencyExclusive, Idempotent: false,
	}
}
