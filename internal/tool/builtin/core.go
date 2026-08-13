package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type CoreToolOptions struct {
	FileSystemPolicy *project.FileSystemPolicy
	Events           event.Sink
	ReadMaxBytes     int64
	ReadMaxLineBytes int
	Glob             GlobOptions
	Grep             GrepOptions
	ExecuteCommand   ExecuteCommandOptions
	PlanUpdater      PlanUpdater
}

func DefaultCoreToolOptions() CoreToolOptions {
	return CoreToolOptions{
		ReadMaxBytes: 2 << 20, ReadMaxLineBytes: 32 << 10,
		Glob: GlobOptions{MaxResults: 1_000, MaxRGOutputBytes: 4 << 20},
		Grep: GrepOptions{MaxResults: 200, MaxFileBytes: 2 << 20, MaxContextLines: 5, MaxRGOutputBytes: 4 << 20},
		ExecuteCommand: ExecuteCommandOptions{
			DefaultTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute,
			DefaultYield: 10 * time.Second, MaxYield: 30 * time.Second,
			MaxOutputBytes: 1 << 20, MaxOutputLines: 5_000, MaxOutputTokens: 64_000,
		},
	}
}

func CoreSpecs() []tool.ToolSpec {
	specs := []tool.ToolSpec{readSpec(), editSpec(), writeSpec(), globSpec(), grepSpec(), executeCommandSpec(), writeStdinSpec(), updatePlanSpec()}
	for index := range specs {
		specs[index] = specs[index].Clone()
	}
	return specs
}

func NewCoreRegistry(root project.Root, options CoreToolOptions) (*tool.Registry, error) {
	registry := tool.NewRegistry()
	if err := RegisterCoreTools(registry, root, options); err != nil {
		return nil, err
	}
	return registry, nil
}

func RegisterCoreTools(registry *tool.Registry, root project.Root, options CoreToolOptions) error {
	if registry == nil {
		return errors.New("register core tools: registry is nil")
	}
	if options.FileSystemPolicy == nil {
		return errors.New("register core tools: filesystem policy is nil")
	}
	fileTools, err := NewFileTools(root, FileToolsOptions{
		FileSystemPolicy: options.FileSystemPolicy,
		MaxBytes:         options.ReadMaxBytes, MaxLineBytes: options.ReadMaxLineBytes,
	})
	if err != nil {
		return err
	}
	options.Glob.FileSystemPolicy = options.FileSystemPolicy
	glob, err := NewGlob(root, options.Glob)
	if err != nil {
		return err
	}
	options.Grep.FileSystemPolicy = options.FileSystemPolicy
	grep, err := NewGrep(root, options.Grep)
	if err != nil {
		return err
	}
	options.ExecuteCommand.FileSystemPolicy = options.FileSystemPolicy
	executeCommand, err := NewExecuteCommand(root, options.ExecuteCommand)
	if err != nil {
		return err
	}
	writeStdin, err := NewWriteStdin(WriteStdinOptions{Manager: executeCommand.ProcessManager()})
	if err != nil {
		return err
	}
	candidates := []tool.Tool{
		fileTools.ReadTool(), fileTools.EditTool(), fileTools.WriteTool(), glob, grep,
		executeCommand, writeStdin,
	}
	if options.PlanUpdater != nil {
		updatePlan, planErr := NewUpdatePlan(options.PlanUpdater, UpdatePlanOptions{Events: options.Events})
		if planErr != nil {
			return planErr
		}
		candidates = append(candidates, updatePlan)
	}
	for _, candidate := range candidates {
		if err := registry.Register(candidate); err != nil {
			return fmt.Errorf("register core tool %q: %w", candidate.Spec().Name, err)
		}
	}
	return nil
}

func readSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "read", Description: "Read a UTF-8 text file with bounded line output.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"line":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}

func editSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "edit", Description: "Make an approved, uniquely matched edit to an existing file and return a structured diff.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"old_string":{"type":"string"},"new_string":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["path","old_string","new_string"],"additionalProperties":false}`), SideEffect: tool.SideEffectWrite, Idempotent: false}
}

func writeSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "write", Description: "Create or overwrite a file after approval and return a structured diff.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`), SideEffect: tool.SideEffectWrite, Idempotent: false}
}

func globSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "glob", Description: "Find files below a directory using bounded glob matching.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"pattern":{"type":"string","minLength":1},"include_hidden":{"type":"boolean"},"limit":{"type":"integer","minimum":1}},"required":["pattern"],"additionalProperties":false}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}

func grepSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "grep", Description: "Search project text with bounded matching and stable file/line output.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"path":{"type":"string"},"glob":{"type":"string"},"type":{"type":"string"},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"},"context":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["query"],"additionalProperties":false}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}

func executeCommandSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "execute_command", Description: "Run a shell command in the requested working directory after safety checks and approval.", InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1},"cwd":{"type":"string"},"timeout_ms":{"type":"integer","minimum":1},"yield_time_ms":{"type":"integer","minimum":0},"max_output_tokens":{"type":"integer","minimum":1},"tty":{"type":"boolean"}},"required":["command"],"additionalProperties":false}`), SideEffect: tool.SideEffectExecute, Idempotent: false}
}
