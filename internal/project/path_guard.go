package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PathType string

const (
	PathAny       PathType = "any"
	PathFile      PathType = "file"
	PathDirectory PathType = "directory"
)

type PathGuard struct {
	root Root
}

func NewPathGuard(root Root) (*PathGuard, error) {
	if root.Path() == "" {
		return nil, errors.New("path guard project root is empty")
	}
	return &PathGuard{root: root}, nil
}

func (guard *PathGuard) ResolveExisting(relative string, expected PathType) (string, error) {
	if guard == nil || guard.root.Path() == "" {
		return "", errors.New("path guard is nil")
	}
	if !expected.valid() {
		return "", fmt.Errorf("path guard expected type %q is invalid", expected)
	}
	logical, err := guard.root.Resolve(relative)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return "", fmt.Errorf("resolve project path %q symlinks: %w", relative, err)
	}
	if err := guard.ensureInside(realPath, relative); err != nil {
		return "", err
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", fmt.Errorf("stat project path %q: %w", relative, err)
	}
	if expected == PathFile && !info.Mode().IsRegular() {
		return "", fmt.Errorf("project path is not a regular file: %q", relative)
	}
	if expected == PathDirectory && !info.IsDir() {
		return "", fmt.Errorf("project path is not a directory: %q", relative)
	}
	return filepath.Clean(realPath), nil
}

func (guard *PathGuard) ResolveForWrite(relative string) (string, error) {
	if guard == nil || guard.root.Path() == "" {
		return "", errors.New("path guard is nil")
	}
	logical, err := guard.root.Resolve(relative)
	if err != nil {
		return "", err
	}
	projectRelative, err := guard.root.Relative(logical)
	if err != nil {
		return "", err
	}
	components := strings.Split(filepath.FromSlash(projectRelative), string(filepath.Separator))
	current := guard.root.Path()
	for index, component := range components {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return logical, nil
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect project write path %q: %w", relative, statErr)
		}
		last := index == len(components)-1
		if info.Mode()&os.ModeSymlink != 0 {
			realPath, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", fmt.Errorf("resolve project write path %q symlinks: %w", relative, resolveErr)
			}
			if err := guard.ensureInside(realPath, relative); err != nil {
				return "", err
			}
			if last {
				return "", fmt.Errorf("project write target cannot be a symlink: %q", relative)
			}
			info, statErr = os.Stat(realPath)
			if statErr != nil {
				return "", fmt.Errorf("stat project write path %q: %w", relative, statErr)
			}
		}
		if !last && !info.IsDir() {
			return "", fmt.Errorf("project write parent is not a directory: %q", relative)
		}
		if last && !info.Mode().IsRegular() {
			return "", fmt.Errorf("project write target is not a regular file: %q", relative)
		}
	}
	return logical, nil
}

func (guard *PathGuard) ensureInside(realPath, input string) error {
	if _, err := guard.root.Relative(realPath); err != nil {
		return fmt.Errorf("project path %q resolves outside project root: %w", input, err)
	}
	return nil
}

func (pathType PathType) valid() bool {
	return pathType == PathAny || pathType == PathFile || pathType == PathDirectory
}
