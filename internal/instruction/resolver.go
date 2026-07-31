package instruction

import (
	"context"
	"errors"
	"fmt"
	slashpath "path"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

type ResolveRequest struct {
	Project    project.Root `json:"-"`
	TargetPath string       `json:"target_path"`
	TargetKind TargetKind   `json:"target_kind"`
}

type TargetKind string

const (
	TargetFile       TargetKind = "file"
	TargetDirectory  TargetKind = "directory"
	TargetCommandCWD TargetKind = "command_cwd"
)

func (kind TargetKind) Valid() bool {
	switch kind {
	case TargetFile, TargetDirectory, TargetCommandCWD:
		return true
	default:
		return false
	}
}

func NewResolveRequest(root project.Root, targetPath string, targetKind TargetKind) (ResolveRequest, error) {
	normalized, err := normalizeProjectPath(targetPath)
	if err != nil {
		return ResolveRequest{}, fmt.Errorf("normalize instruction target path: %w", err)
	}
	request := ResolveRequest{Project: root, TargetPath: normalized, TargetKind: targetKind}
	if err := request.Validate(); err != nil {
		return ResolveRequest{}, err
	}
	return request, nil
}

func (request ResolveRequest) Validate() error {
	if request.Project.Path() == "" {
		return errors.New("instruction resolve project root is empty")
	}
	if !request.TargetKind.Valid() {
		return fmt.Errorf("instruction resolve target kind %q is invalid", request.TargetKind)
	}
	normalized, err := normalizeProjectPath(request.TargetPath)
	if err != nil {
		return fmt.Errorf("instruction resolve target path: %w", err)
	}
	if normalized != request.TargetPath {
		return fmt.Errorf("instruction resolve target path %q is not normalized", request.TargetPath)
	}
	return nil
}

type Resolution struct {
	TargetPath string                `json:"target_path"`
	TargetKind TargetKind            `json:"target_kind"`
	Documents  []InstructionDocument `json:"documents,omitempty"`
}

func (resolution Resolution) Validate(request ResolveRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if resolution.TargetPath != request.TargetPath {
		return fmt.Errorf("instruction resolution target %q does not match request %q", resolution.TargetPath, request.TargetPath)
	}
	if resolution.TargetKind != request.TargetKind {
		return fmt.Errorf("instruction resolution target kind %q does not match request %q", resolution.TargetKind, request.TargetKind)
	}
	seenPaths := make(map[string]struct{}, len(resolution.Documents))
	seenScopes := make(map[string]struct{}, len(resolution.Documents))
	previousSpecificity := -1
	effectiveDirectory := instructionTargetDirectory(request)
	for index, document := range resolution.Documents {
		if err := document.Validate(); err != nil {
			return fmt.Errorf("instruction resolution document %d: %w", index, err)
		}
		applies, err := document.Scope.AppliesTo(effectiveDirectory)
		if err != nil {
			return fmt.Errorf("instruction resolution document %d scope: %w", index, err)
		}
		if !applies {
			return fmt.Errorf("instruction document %q does not apply to target %q", document.Path, request.TargetPath)
		}
		if _, ok := seenPaths[document.Path]; ok {
			return fmt.Errorf("instruction document path %q is duplicated", document.Path)
		}
		seenPaths[document.Path] = struct{}{}
		scopeKey := string(document.Scope.Kind) + ":" + document.Scope.Path
		if _, ok := seenScopes[scopeKey]; ok {
			return fmt.Errorf("instruction scope %q is duplicated", scopeKey)
		}
		seenScopes[scopeKey] = struct{}{}
		specificity := document.Scope.Specificity()
		if specificity < previousSpecificity {
			return errors.New("instruction documents must be ordered from broadest to most specific scope")
		}
		previousSpecificity = specificity
		if document.Source == SourceProject {
			if err := validateProjectDocumentLocation(request.Project, document); err != nil {
				return fmt.Errorf("instruction resolution document %d: %w", index, err)
			}
		}
	}
	return nil
}

func instructionTargetDirectory(request ResolveRequest) string {
	if request.TargetKind == TargetFile {
		return slashpath.Dir(request.TargetPath)
	}
	return request.TargetPath
}

func (resolution Resolution) Clone() Resolution {
	return Resolution{
		TargetPath: resolution.TargetPath,
		TargetKind: resolution.TargetKind,
		Documents:  append([]InstructionDocument(nil), resolution.Documents...),
	}
}

type Resolver interface {
	Resolve(context.Context, ResolveRequest) (Resolution, error)
}

func validateProjectDocumentLocation(root project.Root, document InstructionDocument) error {
	relative, err := root.Relative(document.Path)
	if err != nil {
		return fmt.Errorf("project instruction path: %w", err)
	}
	directory := slashpath.Dir(relative)
	if directory == "" {
		directory = "."
	}
	if directory != document.Scope.Path {
		return fmt.Errorf("project instruction path %q does not match scope %q", relative, document.Scope.Path)
	}
	return nil
}

func normalizeProjectPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("project-relative path is empty")
	}
	if strings.Contains(value, "\\") {
		return "", errors.New("project-relative path must use forward slashes")
	}
	if slashpath.IsAbs(value) {
		return "", errors.New("project-relative path must not be absolute")
	}
	normalized := slashpath.Clean(value)
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", errors.New("project-relative path escapes project root")
	}
	return normalized, nil
}
