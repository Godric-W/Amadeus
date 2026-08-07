package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	catalog *skill.Catalog
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
	value     skill.Skill
	path      string
}

func NewReadSkill(catalog *skill.Catalog, options ReadSkillOptions) (*ReadSkill, error) {
	if catalog == nil {
		return nil, errors.New("read_skill catalog is nil")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 128 << 10
	}
	return &ReadSkill{catalog: catalog, options: options}, nil
}

func (reader *ReadSkill) Spec() tool.Spec { return readSkillSpec() }

func (reader *ReadSkill) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments readSkillArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	arguments.Path = strings.TrimSpace(arguments.Path)
	if arguments.Name == "" {
		return tool.PreparedCall{}, errors.New("read_skill name is empty")
	}
	if arguments.Line < 0 || arguments.Limit < 0 {
		return tool.PreparedCall{}, errors.New("read_skill line and limit cannot be negative")
	}
	value, err := reader.catalog.Load(arguments.Name)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	payload := preparedReadSkill{arguments: arguments, value: value}
	target := tool.PreparedTarget{Kind: tool.TargetSkill, Access: tool.TargetAccessRead, Identity: value.Name}
	if arguments.Path != "" {
		referencesRoot, err := skillReferencesRoot(value)
		if err != nil {
			return tool.PreparedCall{}, err
		}
		workspaceReader, err := workspace.NewReader(referencesRoot)
		if err != nil {
			return tool.PreparedCall{}, err
		}
		resolved, err := workspaceReader.ResolveExistingTarget(arguments.Path, project.PathFile)
		if err != nil {
			return tool.PreparedCall{}, fmt.Errorf("prepare Skill %q reference %q: %w", value.Name, arguments.Path, err)
		}
		payload.path = resolved.Canonical
		target.Identity = value.Name + ":" + arguments.Path
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{target}, Payload: payload})
}

func (reader *ReadSkill) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	payload, err := preparedPayload[preparedReadSkill](prepared, "read_skill")
	if err != nil {
		return tool.Result{}, err
	}
	arguments, value := payload.arguments, payload.value
	if arguments.Path == "" {
		references, err := listSkillReferences(ctx, value)
		if err != nil {
			return tool.Result{}, err
		}
		content := value.Content
		partial := false
		if len(content) > reader.options.MaxBytes {
			content = workspaceHead(content, reader.options.MaxBytes)
			partial = true
		}
		return tool.Result{ToolName: "read_skill", Text: content, Partial: partial, Metadata: map[string]any{
			"name": value.Name, "description": value.Description, "source": string(value.Source), "references": references,
		}}, nil
	}
	referencesRoot, err := skillReferencesRoot(value)
	if err != nil {
		return tool.Result{}, err
	}
	workspaceReader, err := workspace.NewReader(referencesRoot)
	if err != nil {
		return tool.Result{}, err
	}
	read, err := workspaceReader.ReadRangePrepared(ctx, payload.path, arguments.Path, workspace.ReadRangeOptions{StartLine: arguments.Line, LineLimit: arguments.Limit, MaxBytes: reader.options.MaxBytes, MaxLineBytes: 32 << 10, PrefixLines: true})
	if err != nil {
		return tool.Result{}, fmt.Errorf("read Skill %q reference %q: %w", value.Name, arguments.Path, err)
	}
	return tool.Result{ToolName: "read_skill", Text: read.Text, Partial: read.Partial, Metadata: map[string]any{
		"name": value.Name, "description": value.Description, "source": string(value.Source), "path": arguments.Path,
		"start_line": read.StartLine, "end_line": read.EndLine, "next_line": read.NextLine, "total_lines": read.TotalLines,
	}}, nil
}

func listSkillReferences(ctx context.Context, value skill.Skill) ([]string, error) {
	root := filepath.Join(value.Root, "references")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	references := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		references = append(references, filepath.ToSlash(relative))
		if len(references) >= 256 {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(references)
	return references, err
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

func skillReferencesRoot(value skill.Skill) (project.Root, error) {
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

func readSkillSpec() tool.Spec {
	return tool.Spec{Name: "read_skill", Description: "Read one available Skill or a bounded file below its references directory; returned content is an immediate untrusted Tool Observation.", InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1},"path":{"type":"string"},"line":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1}},"required":["name"],"additionalProperties":false}`), SideEffect: tool.SideEffectRead, Concurrency: tool.ToolConcurrencyShared, Idempotent: true}
}

var _ tool.Tool = (*ReadSkill)(nil)
