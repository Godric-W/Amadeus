package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrPathOutsideRoot = errors.New("path is outside project root")

type Root struct {
	path string
}

func NewRoot(path string) (Root, error) {
	if strings.TrimSpace(path) == "" {
		return Root{}, errors.New("project root path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Root{}, fmt.Errorf("resolve project root absolute path: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Root{}, fmt.Errorf("resolve project root symlinks: %w", err)
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return Root{}, fmt.Errorf("stat project root: %w", err)
	}
	if !info.IsDir() {
		return Root{}, errors.New("project root is not a directory")
	}
	return Root{path: filepath.Clean(realPath)}, nil
}

func (root Root) Path() string {
	return root.path
}

func (root Root) Resolve(relative string) (string, error) {
	if root.path == "" {
		return "", errors.New("project root is not initialized")
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: project path must be relative: %q", ErrPathOutsideRoot, relative)
	}
	cleaned := filepath.Clean(relative)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: project path escapes root: %q", ErrPathOutsideRoot, relative)
	}
	resolved := filepath.Join(root.path, cleaned)
	relativeToRoot, err := filepath.Rel(root.path, resolved)
	if err != nil {
		return "", fmt.Errorf("verify project path: %w", err)
	}
	if relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeToRoot) {
		return "", fmt.Errorf("%w: project path escapes root: %q", ErrPathOutsideRoot, relative)
	}
	return resolved, nil
}

func (root Root) Relative(path string) (string, error) {
	if root.path == "" {
		return "", errors.New("project root is not initialized")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path for project-relative conversion: %w", err)
	}
	relative, err := filepath.Rel(root.path, absolute)
	if err != nil {
		return "", fmt.Errorf("convert path to project-relative form: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: %q", ErrPathOutsideRoot, path)
	}
	return filepath.ToSlash(relative), nil
}
