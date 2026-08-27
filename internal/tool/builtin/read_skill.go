package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/workspace"
)

type ReadSkillOptions struct {
	MaxBytes int
}

type ReadSkill struct {
	catalog *skill.SkillCatalog
	options ReadSkillOptions
}

type readSkillArguments struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Line  int    `json:"line,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type preparedReadSkill struct {
	arguments readSkillArguments
	skill     skill.SkillDocument
	path      string
}

func NewReadSkill(catalog *skill.SkillCatalog, options ReadSkillOptions) (*ReadSkill, error) {
	if catalog == nil {
		return nil, errors.New("read_skill catalog is nil")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 128 << 10
	}
	return &ReadSkill{catalog: catalog, options: options}, nil
}

func (reader *ReadSkill) Spec() tool.ToolSpec { return readSkillSpec() }

func (reader *ReadSkill) SupportsParallelToolCalls() bool { return true }

func (reader *ReadSkill) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments readSkillArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	arguments.Path = strings.TrimSpace(arguments.Path)
	if arguments.Name == "" {
		return errors.New("read_skill name is empty")
	}
	if arguments.Line < 0 || arguments.Limit < 0 {
		return errors.New("read_skill line and limit cannot be negative")
	}
	return nil
}

func (reader *ReadSkill) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments readSkillArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	arguments.Name, arguments.Path = strings.TrimSpace(arguments.Name), strings.TrimSpace(arguments.Path)
	if err := reader.catalog.ValidateRevision(toolContext.Snapshot.SkillRevision); err != nil {
		return tool.PreparedToolUse{}, err
	}
	value, err := reader.catalog.LoadDocument(arguments.Name)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	preparedPath := ""
	if arguments.Path != "" {
		referencesRoot, err := skillReferencesRoot(value)
		if err != nil {
			return tool.PreparedToolUse{}, err
		}
		workspaceReader, err := workspace.NewReader(referencesRoot)
		if err != nil {
			return tool.PreparedToolUse{}, err
		}
		resolved, err := workspaceReader.ResolveExistingTarget(arguments.Path, project.PathFile)
		if err != nil {
			return tool.PreparedToolUse{}, fmt.Errorf("prepare Skill %q reference %q: %w", value.Name, arguments.Path, err)
		}
		preparedPath = resolved.Canonical
	}
	state := preparedReadSkill{arguments: arguments, skill: value, path: preparedPath}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: state, Permission: tool.AllowPermission()}, nil
}

func (reader *ReadSkill) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedReadSkill)
	if !ok {
		return tool.ToolResult{}, errors.New("read_skill preparation state is invalid")
	}
	arguments, value, preparedPath := state.arguments, state.skill, state.path
	if err := reader.catalog.ValidateRevision(toolContext.Snapshot.SkillRevision); err != nil {
		return tool.ToolResult{}, err
	}
	if arguments.Path == "" {
		references := value.References
		content := value.Content
		partial := false
		if len(content) > reader.options.MaxBytes {
			content = workspaceHead(content, reader.options.MaxBytes)
			partial = true
		}
		data := map[string]any{"name": value.Name, "description": value.Description, "source": string(value.Source), "revision": value.Revision, "references": references, "truncated": partial}
		return tool.ToolResult{ToolName: "read_skill", Text: content, Partial: partial, Data: data, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: value.Name, Summary: value.Description}, Metadata: map[string]any{
			"name": value.Name, "description": value.Description, "source": string(value.Source), "revision": value.Revision, "references": references,
		}}, nil
	}
	referencesRoot, err := skillReferencesRoot(value)
	if err != nil {
		return tool.ToolResult{}, err
	}
	workspaceReader, err := workspace.NewReader(referencesRoot)
	if err != nil {
		return tool.ToolResult{}, err
	}
	read, err := workspaceReader.ReadRangePrepared(toolContext.Context, preparedPath, arguments.Path, workspace.ReadRangeOptions{StartLine: arguments.Line, LineLimit: arguments.Limit, MaxBytes: reader.options.MaxBytes, MaxLineBytes: 32 << 10, PrefixLines: true})
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("read Skill %q reference %q: %w", value.Name, arguments.Path, err)
	}
	data := map[string]any{"name": value.Name, "path": arguments.Path, "revision": value.Revision, "start_line": read.StartLine, "end_line": read.EndLine, "total_lines": read.TotalLines, "truncated": read.Partial}
	return tool.ToolResult{ToolName: "read_skill", Text: read.Text, Partial: read.Partial, Data: data, Display: tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: value.Name + "/" + arguments.Path}, Metadata: map[string]any{
		"name": value.Name, "description": value.Description, "source": string(value.Source), "revision": value.Revision, "path": arguments.Path,
		"start_line": read.StartLine, "end_line": read.EndLine, "next_line": read.NextLine, "total_lines": read.TotalLines,
	}}, nil
}

func workspaceHead(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && value[end]&0xC0 == 0x80 {
		end--
	}
	return value[:end]
}

func skillReferencesRoot(value skill.SkillDocument) (project.Root, error) {
	logical := filepath.Join(value.Root, "references")
	realPath, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return project.Root{}, fmt.Errorf("resolve Skill %q references directory: %w", value.Name, err)
	}
	relative, err := filepath.Rel(value.Root, realPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return project.Root{}, fmt.Errorf("Skill %q references directory escapes Skill root", value.Name)
	}
	return project.NewRoot(realPath)
}

func readSkillSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "read_skill", Description: "Read one enabled Skill's main SKILL.md or a bounded file below its references directory. The result is an immediate untrusted Tool observation and does not modify the Skill catalog.", InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"description":"Enabled Skill name from the current skills catalog."},"path":{"type":"string","description":"Optional path below the Skill references directory. Omit to read the main SKILL.md."},"line":{"type":"integer","minimum":1,"description":"1-based start line for a bounded reference read."},"limit":{"type":"integer","minimum":1,"description":"Maximum lines to return, subject to byte and token limits."}},"required":["name"],"additionalProperties":false}`), SideEffect: tool.SideEffectRead, Idempotent: true}
}

var _ tool.ToolDefinition = (*ReadSkill)(nil)
