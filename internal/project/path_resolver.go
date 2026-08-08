package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type PathType string

const (
	PathAny       PathType = "any"
	PathFile      PathType = "file"
	PathDirectory PathType = "directory"
)

func (pathType PathType) valid() bool {
	return pathType == PathAny || pathType == PathFile || pathType == PathDirectory
}

func invalidPathType(expected PathType) error {
	return fmt.Errorf("filesystem policy expected type %q is invalid", expected)
}

type PathResolver struct {
	cwd string
}

func NewPathResolver(cwd string) (*PathResolver, error) {
	canonical, err := canonicalDirectory(cwd)
	if err != nil {
		return nil, fmt.Errorf("path resolver cwd: %w", err)
	}
	return &PathResolver{cwd: canonical}, nil
}

func (resolver *PathResolver) ResolveExisting(requested string, expected PathType) (ResolvedPath, error) {
	if resolver == nil || resolver.cwd == "" {
		return ResolvedPath{}, errors.New("path resolver is nil")
	}
	if !expected.valid() {
		return ResolvedPath{}, fmt.Errorf("path resolver expected type %q is invalid", expected)
	}
	logical, err := resolver.logicalPath(requested)
	if err != nil {
		return ResolvedPath{}, err
	}
	canonical, err := filepath.EvalSymlinks(logical)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResolvedPath{}, fmt.Errorf("path_not_found: %q", requested)
		}
		return ResolvedPath{}, fmt.Errorf("resolve path %q symlinks: %w", requested, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return ResolvedPath{}, fmt.Errorf("stat path %q: %w", requested, err)
	}
	if expected == PathFile && !info.Mode().IsRegular() {
		return ResolvedPath{}, fmt.Errorf("path_type_mismatch: %q is not a regular file", requested)
	}
	if expected == PathDirectory && !info.IsDir() {
		return ResolvedPath{}, fmt.Errorf("path_type_mismatch: %q is not a directory", requested)
	}
	return ResolvedPath{Requested: requested, Logical: logical, Canonical: filepath.Clean(canonical)}, nil
}

func (resolver *PathResolver) ResolveForWrite(requested string) (ResolvedPath, error) {
	if resolver == nil || resolver.cwd == "" {
		return ResolvedPath{}, errors.New("path resolver is nil")
	}
	logical, err := resolver.logicalPath(requested)
	if err != nil {
		return ResolvedPath{}, err
	}
	ancestor := logical
	missing := make([]string, 0)
	for {
		info, statErr := os.Lstat(ancestor)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 && ancestor == logical {
				return ResolvedPath{}, fmt.Errorf("symlink_escape: write target cannot be a symlink: %q", requested)
			}
			canonicalAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
			if resolveErr != nil {
				return ResolvedPath{}, fmt.Errorf("resolve write path %q symlinks: %w", requested, resolveErr)
			}
			canonicalInfo, statErr := os.Stat(canonicalAncestor)
			if statErr != nil {
				return ResolvedPath{}, fmt.Errorf("stat write path %q: %w", requested, statErr)
			}
			if len(missing) > 0 && !canonicalInfo.IsDir() {
				return ResolvedPath{}, fmt.Errorf("path_type_mismatch: write parent for %q is not a directory", requested)
			}
			if len(missing) == 0 && !canonicalInfo.Mode().IsRegular() {
				return ResolvedPath{}, fmt.Errorf("path_type_mismatch: write target %q is not a regular file", requested)
			}
			canonical := canonicalAncestor
			for index := len(missing) - 1; index >= 0; index-- {
				canonical = filepath.Join(canonical, missing[index])
			}
			return ResolvedPath{Requested: requested, Logical: logical, Canonical: filepath.Clean(canonical)}, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) && !errors.Is(statErr, syscall.ENOTDIR) {
			return ResolvedPath{}, fmt.Errorf("inspect write path %q: %w", requested, statErr)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return ResolvedPath{}, fmt.Errorf("path_not_found: no existing ancestor for %q", requested)
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
}

func (resolver *PathResolver) logicalPath(requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", errors.New("path is empty")
	}
	if strings.ContainsRune(requested, '\x00') {
		return "", errors.New("path contains NUL")
	}
	candidate := requested
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(resolver.cwd, candidate)
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %q: %w", requested, err)
	}
	return filepath.Clean(absolute), nil
}
