package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

const defaultSkillReferenceMaxBytes int64 = 128 << 10

type ReadSkillReferenceOptions struct {
	MaxBytes int64
}

type ReadSkillReference struct {
	catalog *skill.Catalog
	options ReadSkillReferenceOptions
}

type readSkillReferenceArguments struct {
	Skill  string `json:"skill"`
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func NewReadSkillReference(catalog *skill.Catalog, options ReadSkillReferenceOptions) (*ReadSkillReference, error) {
	if catalog == nil {
		return nil, errors.New("read_skill_reference catalog is nil")
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = defaultSkillReferenceMaxBytes
	}
	return &ReadSkillReference{catalog: catalog, options: options}, nil
}

func (reader *ReadSkillReference) Spec() tool.Spec { return readSkillReferenceSpec() }

func (reader *ReadSkillReference) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var arguments readSkillReferenceArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	arguments.Skill = strings.TrimSpace(arguments.Skill)
	arguments.Path = strings.TrimSpace(arguments.Path)
	if arguments.Skill == "" {
		return tool.Result{}, errors.New("read_skill_reference skill is empty")
	}
	if arguments.Path == "" {
		return tool.Result{}, errors.New("read_skill_reference path is empty")
	}
	if arguments.Offset < 0 || arguments.Limit < 0 {
		return tool.Result{}, errors.New("read_skill_reference offset and limit cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	value, ok := reader.catalog.Lookup(arguments.Skill)
	if !ok {
		return tool.Result{}, fmt.Errorf("skill %q is not available", arguments.Skill)
	}
	references, err := skillReferencesRoot(value)
	if err != nil {
		return tool.Result{}, err
	}
	guard, err := project.NewPathGuard(references)
	if err != nil {
		return tool.Result{}, err
	}
	path, err := guard.ResolveExisting(arguments.Path, project.PathFile)
	if err != nil {
		return tool.Result{}, fmt.Errorf("resolve Skill %q reference %q: %w", value.Name, arguments.Path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return tool.Result{}, fmt.Errorf("open Skill %q reference %q: %w", value.Name, arguments.Path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return tool.Result{}, fmt.Errorf("stat Skill %q reference %q: %w", value.Name, arguments.Path, err)
	}
	if !info.Mode().IsRegular() {
		return tool.Result{}, fmt.Errorf("Skill %q reference is not a regular file: %q", value.Name, arguments.Path)
	}
	if info.Size() > reader.options.MaxBytes {
		return tool.Result{}, fmt.Errorf("Skill %q reference size %d exceeds limit %d", value.Name, info.Size(), reader.options.MaxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, reader.options.MaxBytes+1))
	if err != nil {
		return tool.Result{}, fmt.Errorf("read Skill %q reference %q: %w", value.Name, arguments.Path, err)
	}
	if int64(len(content)) > reader.options.MaxBytes {
		return tool.Result{}, fmt.Errorf("Skill %q reference size exceeds limit %d", value.Name, reader.options.MaxBytes)
	}
	if bytesAreBinary(content) {
		return tool.Result{}, fmt.Errorf("Skill %q reference does not support binary or non-UTF-8 file %q", value.Name, arguments.Path)
	}
	lines := splitLines(string(content))
	start := arguments.Offset
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if arguments.Limit > 0 && start+arguments.Limit < end {
		end = start + arguments.Limit
	}
	return tool.Result{
		ToolName: "read_skill_reference", Text: strings.Join(lines[start:end], ""), Partial: start > 0 || end < len(lines),
		Metadata: map[string]any{
			"skill": value.Name, "source": string(value.Source), "path": arguments.Path,
			"offset": start, "lines_returned": end - start, "total_lines": len(lines), "bytes": len(content),
		},
	}, nil
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
	references, err := project.NewRoot(realPath)
	if err != nil {
		return project.Root{}, fmt.Errorf("open Skill %q references directory: %w", value.Name, err)
	}
	return references, nil
}

func readSkillReferenceSpec() tool.Spec {
	return tool.Spec{
		Name: "read_skill_reference", Description: "Read bounded UTF-8 text from an indexed Skill's references directory. Paths are relative to that Skill only.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"skill":{"type":"string","minLength":1},"path":{"type":"string","minLength":1},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}},"required":["skill","path"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectRead, ParallelSafe: true, Idempotent: true,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeArguments, ArgumentPaths: []string{"skill", "path"}},
	}
}

var _ tool.Tool = (*ReadSkillReference)(nil)
