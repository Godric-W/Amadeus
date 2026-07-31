package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/prompts"
)

const BuiltinSource = "builtin"

var variablePattern = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*)\s*\}\}`)

type Source struct {
	Kind      string   `json:"kind"`
	Path      string   `json:"path"`
	SHA256    string   `json:"sha256"`
	Variables []string `json:"variables,omitempty"`
}

type Document struct {
	Content string
	Source  Source
}

type Repository struct {
	files      fs.FS
	sourceKind string
}

func NewRepository(files fs.FS, sourceKind string) (*Repository, error) {
	if files == nil {
		return nil, errors.New("prompt repository filesystem is nil")
	}
	sourceKind = strings.TrimSpace(sourceKind)
	if sourceKind == "" {
		return nil, errors.New("prompt repository source kind is empty")
	}
	return &Repository{files: files, sourceKind: sourceKind}, nil
}

func NewBuiltinRepository() (*Repository, error) {
	return NewRepository(prompts.Embedded(), BuiltinSource)
}

func (repository *Repository) Load(path string) (Document, error) {
	if repository == nil {
		return Document{}, errors.New("prompt repository is nil")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return Document{}, errors.New("prompt path is empty")
	}
	if !fs.ValidPath(path) || path == "." {
		return Document{}, fmt.Errorf("prompt path %q is invalid", path)
	}
	content, err := fs.ReadFile(repository.files, path)
	if err != nil {
		return Document{}, fmt.Errorf("read prompt %q from %s: %w", path, repository.sourceKind, err)
	}
	normalized := strings.TrimSpace(string(content))
	if normalized == "" {
		return Document{}, fmt.Errorf("prompt %q from %s is empty", path, repository.sourceKind)
	}
	variables, err := extractVariables(normalized)
	if err != nil {
		return Document{}, fmt.Errorf("parse prompt %q from %s: %w", path, repository.sourceKind, err)
	}
	return Document{
		Content: normalized,
		Source: Source{
			Kind:      repository.sourceKind,
			Path:      path,
			SHA256:    contentHash(normalized),
			Variables: variables,
		},
	}, nil
}

func extractVariables(content string) ([]string, error) {
	matches := variablePattern.FindAllStringSubmatch(content, -1)
	remainder := variablePattern.ReplaceAllString(content, "")
	if strings.Contains(remainder, "{{") || strings.Contains(remainder, "}}") {
		return nil, errors.New("invalid variable placeholder; expected {{variable_name}}")
	}
	unique := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		unique[match[1]] = struct{}{}
	}
	variables := make([]string, 0, len(unique))
	for variable := range unique {
		variables = append(variables, variable)
	}
	sort.Strings(variables)
	return variables, nil
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
