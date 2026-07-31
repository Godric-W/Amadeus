package instruction

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

func (source Source) Valid() bool {
	return source == SourceUser || source == SourceProject
}

type ScopeKind string

const (
	ScopeUser      ScopeKind = "user"
	ScopeProject   ScopeKind = "project"
	ScopeDirectory ScopeKind = "directory"
)

func (kind ScopeKind) Valid() bool {
	switch kind {
	case ScopeUser, ScopeProject, ScopeDirectory:
		return true
	default:
		return false
	}
}

type Scope struct {
	Kind ScopeKind `json:"kind"`
	Path string    `json:"path,omitempty"`
}

func UserScope() Scope {
	return Scope{Kind: ScopeUser}
}

func ProjectScope() Scope {
	return Scope{Kind: ScopeProject, Path: "."}
}

func NewDirectoryScope(relativeDirectory string) (Scope, error) {
	normalized, err := normalizeProjectPath(relativeDirectory)
	if err != nil {
		return Scope{}, fmt.Errorf("normalize instruction directory scope: %w", err)
	}
	if normalized == "." {
		return Scope{}, errors.New("directory instruction scope must be below project root")
	}
	return Scope{Kind: ScopeDirectory, Path: normalized}, nil
}

func (scope Scope) Validate() error {
	if !scope.Kind.Valid() {
		return fmt.Errorf("instruction scope kind %q is invalid", scope.Kind)
	}
	switch scope.Kind {
	case ScopeUser:
		if scope.Path != "" {
			return errors.New("user instruction scope path must be empty")
		}
	case ScopeProject:
		if scope.Path != "." {
			return errors.New("project instruction scope path must be .")
		}
	case ScopeDirectory:
		normalized, err := normalizeProjectPath(scope.Path)
		if err != nil {
			return fmt.Errorf("directory instruction scope path: %w", err)
		}
		if normalized == "." {
			return errors.New("directory instruction scope must be below project root")
		}
		if normalized != scope.Path {
			return fmt.Errorf("directory instruction scope path %q is not normalized", scope.Path)
		}
	}
	return nil
}

func (scope Scope) AppliesTo(targetPath string) (bool, error) {
	if err := scope.Validate(); err != nil {
		return false, err
	}
	target, err := normalizeProjectPath(targetPath)
	if err != nil {
		return false, fmt.Errorf("normalize instruction target path: %w", err)
	}
	switch scope.Kind {
	case ScopeUser, ScopeProject:
		return true, nil
	case ScopeDirectory:
		return target == scope.Path || strings.HasPrefix(target, scope.Path+"/"), nil
	default:
		return false, nil
	}
}

func (scope Scope) Specificity() int {
	switch scope.Kind {
	case ScopeUser:
		return 0
	case ScopeProject:
		return 1
	case ScopeDirectory:
		return 2 + strings.Count(scope.Path, "/")
	default:
		return -1
	}
}

type InstructionDocument struct {
	Source  Source `json:"source"`
	Path    string `json:"path"`
	Scope   Scope  `json:"scope"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

func NewInstructionDocument(source Source, path string, scope Scope, content string) (InstructionDocument, error) {
	document := InstructionDocument{
		Source:  source,
		Path:    filepath.Clean(path),
		Scope:   scope,
		SHA256:  instructionHash(content),
		Content: content,
	}
	if err := document.Validate(); err != nil {
		return InstructionDocument{}, err
	}
	return document, nil
}

func (document InstructionDocument) Validate() error {
	if !document.Source.Valid() {
		return fmt.Errorf("instruction source %q is invalid", document.Source)
	}
	if strings.TrimSpace(document.Path) == "" {
		return errors.New("instruction path is empty")
	}
	if !filepath.IsAbs(document.Path) {
		return fmt.Errorf("instruction path must be absolute: %q", document.Path)
	}
	if filepath.Clean(document.Path) != document.Path {
		return fmt.Errorf("instruction path %q is not normalized", document.Path)
	}
	if err := document.Scope.Validate(); err != nil {
		return fmt.Errorf("instruction scope: %w", err)
	}
	if document.Source == SourceUser && document.Scope.Kind != ScopeUser {
		return errors.New("user instruction source requires user scope")
	}
	if document.Source == SourceProject && document.Scope.Kind == ScopeUser {
		return errors.New("project instruction source cannot use user scope")
	}
	if !utf8.ValidString(document.Content) {
		return errors.New("instruction content is not valid UTF-8")
	}
	if strings.TrimSpace(document.Content) == "" {
		return errors.New("instruction content is empty")
	}
	if len(document.SHA256) != sha256.Size*2 {
		return errors.New("instruction SHA-256 must contain 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(document.SHA256); err != nil {
		return errors.New("instruction SHA-256 is not hexadecimal")
	}
	if document.SHA256 != instructionHash(document.Content) {
		return errors.New("instruction SHA-256 does not match content")
	}
	return nil
}

func instructionHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
