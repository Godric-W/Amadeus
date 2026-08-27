package builtin

import (
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type CoreToolOptions struct {
	FileSystemPolicy *project.FileSystemPolicy
	Events           protocol.EventSink
	ReadMaxBytes     int64
	ReadMaxLineBytes int
	Glob             GlobOptions
	Grep             GrepOptions
	ExecuteCommand   ExecuteCommandOptions
}

func DefaultCoreToolOptions() CoreToolOptions {
	return CoreToolOptions{
		ReadMaxBytes: 2 << 20, ReadMaxLineBytes: 32 << 10,
		Glob: GlobOptions{MaxResults: 1_000, MaxOutputBytes: 256 << 10, MaxOutputTokens: 64_000, MaxRGOutputBytes: 4 << 20},
		Grep: GrepOptions{MaxResults: 200, MaxFileBytes: 2 << 20, MaxContextLines: 5, MaxOutputBytes: 256 << 10, MaxOutputTokens: 64_000, MaxRGOutputBytes: 4 << 20},
		ExecuteCommand: ExecuteCommandOptions{
			DefaultTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute,
			DefaultYield: 10 * time.Second, MaxYield: 30 * time.Second,
			MaxOutputBytes: 1 << 20, MaxOutputLines: 5_000, MaxOutputTokens: 64_000,
		},
	}
}

func CoreSpecs() []tool.ToolSpec {
	specs := []tool.ToolSpec{readSpec(), editSpec(), writeSpec(), globSpec(), grepSpec(), executeCommandSpec(), writeStdinSpec(), updatePlanSpec(), requestUserInputSpec()}
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
	updatePlan, err := NewUpdatePlan(UpdatePlanOptions{Events: options.Events})
	if err != nil {
		return err
	}
	requestUserInput := NewRequestUserInput()
	for _, definition := range []tool.ToolDefinition{fileTools.ReadTool(), fileTools.EditTool(), fileTools.WriteTool(), glob, grep, executeCommand, writeStdin, updatePlan, requestUserInput} {
		if err := registry.RegisterDefinition(definition); err != nil {
			return fmt.Errorf("register core tool %q: %w", definition.Spec().Name, err)
		}
	}
	return nil
}
