package project

import (
	"errors"
	"fmt"
)

type PathType string

const (
	PathAny       PathType = "any"
	PathFile      PathType = "file"
	PathDirectory PathType = "directory"
)

type PathGuard struct {
	policy *FileSystemPolicy
}

func NewPathGuard(root Root) (*PathGuard, error) {
	if root.Path() == "" {
		return nil, errors.New("path guard project root is empty")
	}
	policy, err := NewFileSystemPolicy(FileSystemPolicyOptions{CWD: root.Path(), Profile: PermissionProfile{WorkspaceRoots: []string{root.Path()}}})
	if err != nil {
		return nil, err
	}
	return NewPathGuardWithPolicy(policy)
}

func NewPathGuardWithPolicy(policy *FileSystemPolicy) (*PathGuard, error) {
	if policy == nil {
		return nil, errors.New("path guard filesystem policy is nil")
	}
	return &PathGuard{policy: policy}, nil
}

func (guard *PathGuard) ResolveExisting(requested string, expected PathType) (string, error) {
	resolved, err := guard.ResolveExistingTarget(requested, expected)
	if err != nil {
		return "", err
	}
	return resolved.Canonical, nil
}

func (guard *PathGuard) ResolveExistingTarget(requested string, expected PathType) (ResolvedPath, error) {
	if guard == nil || guard.policy == nil {
		return ResolvedPath{}, errors.New("path guard is nil")
	}
	resolved, err := guard.policy.ResolveExisting(requested, expected)
	if err != nil {
		return ResolvedPath{}, err
	}
	return resolved, nil
}

func (guard *PathGuard) ResolveForWrite(requested string) (string, error) {
	resolved, err := guard.ResolveForWriteTarget(requested)
	if err != nil {
		return "", err
	}
	return resolved.Canonical, nil
}

func (guard *PathGuard) ResolveForWriteTarget(requested string) (ResolvedPath, error) {
	if guard == nil || guard.policy == nil {
		return ResolvedPath{}, errors.New("path guard is nil")
	}
	resolved, err := guard.policy.ResolveForWrite(requested)
	if err != nil {
		return ResolvedPath{}, err
	}
	return resolved, nil
}

func (guard *PathGuard) ResolveWritableDirectory(requested string) (string, error) {
	resolved, err := guard.ResolveWritableDirectoryTarget(requested)
	if err != nil {
		return "", err
	}
	return resolved.Canonical, nil
}

func (guard *PathGuard) ResolveWritableDirectoryTarget(requested string) (ResolvedPath, error) {
	if guard == nil || guard.policy == nil {
		return ResolvedPath{}, errors.New("path guard is nil")
	}
	resolved, err := guard.policy.ResolveWritableDirectory(requested)
	if err != nil {
		return ResolvedPath{}, err
	}
	return resolved, nil
}

func (pathType PathType) valid() bool {
	return pathType == PathAny || pathType == PathFile || pathType == PathDirectory
}

func invalidPathType(expected PathType) error {
	return fmt.Errorf("path guard expected type %q is invalid", expected)
}
