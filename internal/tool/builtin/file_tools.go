package builtin

import (
	"errors"
	"os"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type FileToolsOptions struct {
	FileSystemPolicy *project.FileSystemPolicy
	MaxBytes         int64
	MaxLineBytes     int
	FileMode         os.FileMode
}

type FileTools struct {
	policy       *project.FileSystemPolicy
	maxBytes     int64
	maxLineBytes int
	fileMode     os.FileMode
	root         project.Root
}

func NewFileTools(root project.Root, options FileToolsOptions) (*FileTools, error) {
	if root.Path() == "" {
		return nil, errors.New("file tools project root is empty")
	}
	if options.FileSystemPolicy == nil {
		var err error
		options.FileSystemPolicy, err = project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
			CWD: root.Path(), Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{root.Path()}},
		})
		if err != nil {
			return nil, err
		}
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 2 << 20
	}
	if options.MaxLineBytes <= 0 {
		options.MaxLineBytes = 32 << 10
	}
	if options.FileMode == 0 {
		options.FileMode = 0o644
	}
	return &FileTools{root: root, policy: options.FileSystemPolicy, maxBytes: options.MaxBytes,
		maxLineBytes: options.MaxLineBytes, fileMode: options.FileMode}, nil
}

func (files *FileTools) ReadSpec() tool.ToolSpec {
	return readSpec()
}

func (files *FileTools) EditSpec() tool.ToolSpec {
	return editSpec()
}

func (files *FileTools) WriteSpec() tool.ToolSpec {
	return writeSpec()
}

type readTool struct{ files *FileTools }
type editTool struct{ files *FileTools }
type writeTool struct{ files *FileTools }

func (files *FileTools) ReadTool() tool.ToolDefinition  { return readTool{files: files} }
func (files *FileTools) EditTool() tool.ToolDefinition  { return editTool{files: files} }
func (files *FileTools) WriteTool() tool.ToolDefinition { return writeTool{files: files} }

var _ tool.ToolDefinition = readTool{}
var _ tool.ToolDefinition = editTool{}
var _ tool.ToolDefinition = writeTool{}
