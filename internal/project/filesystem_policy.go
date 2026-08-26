package project

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
)

type FileAccess string

const (
	AccessDeny  FileAccess = "deny"
	AccessRead  FileAccess = "read"
	AccessWrite FileAccess = "write"
)

type RootSource string

const (
	RootSourceHost      RootSource = "host"
	RootSourceWorkspace RootSource = "workspace"
	RootSourceTemporary RootSource = "temporary"
	RootSourceReadOnly  RootSource = "read_only"
	RootSourceDenied    RootSource = "denied"
)

type ResolvedPath struct {
	Requested   string
	Logical     string
	Canonical   string
	Access      FileAccess
	MatchedRoot string
	RootSource  RootSource
}

var (
	ErrPathDenied         = errors.New("path denied")
	ErrPermissionRequired = errors.New("permission required")
)

type PathDeniedError struct {
	Requested string
	Reason    string
}

func (err *PathDeniedError) Error() string {
	return fmt.Sprintf("path_denied: %q %s", err.Requested, err.Reason)
}

func (err *PathDeniedError) Unwrap() error         { return ErrPathDenied }
func (err *PathDeniedError) ToolErrorKind() string { return "permission_denied" }

type PermissionRequiredError struct {
	RequestedPath string
	CanonicalPath string
	WritableRoot  string
}

func (err *PermissionRequiredError) Error() string {
	return fmt.Sprintf("permission_required: write access to %q requires writable root %q", err.CanonicalPath, err.WritableRoot)
}

func (err *PermissionRequiredError) Unwrap() error         { return ErrPermissionRequired }
func (err *PermissionRequiredError) ToolErrorKind() string { return "permission_required" }

type PermissionProfile struct {
	ReadHost       bool
	WorkspaceRoots []string
	TemporaryRoots []string
	ReadOnlyRoots  []string
	DeniedRoots    []string
}

func (profile PermissionProfile) Clone() PermissionProfile {
	profile.WorkspaceRoots = append([]string(nil), profile.WorkspaceRoots...)
	profile.TemporaryRoots = append([]string(nil), profile.TemporaryRoots...)
	profile.ReadOnlyRoots = append([]string(nil), profile.ReadOnlyRoots...)
	profile.DeniedRoots = append([]string(nil), profile.DeniedRoots...)
	return profile
}

type FileSystemPolicyOptions struct {
	CWD     string
	Profile PermissionProfile
}

type FileSystemPolicy struct {
	resolver *PathResolver
	base     PermissionProfile
}

func NewFileSystemPolicy(options FileSystemPolicyOptions) (*FileSystemPolicy, error) {
	resolver, err := NewPathResolver(options.CWD)
	if err != nil {
		return nil, err
	}
	profile, err := normalizePermissionProfile(options.Profile)
	if err != nil {
		return nil, err
	}
	return &FileSystemPolicy{resolver: resolver, base: profile}, nil
}

func (policy *FileSystemPolicy) EffectiveProfile() PermissionProfile {
	if policy == nil {
		return PermissionProfile{}
	}
	return policy.base.Clone()
}

func (policy *FileSystemPolicy) ResolveExisting(requested string, expected PathType) (ResolvedPath, error) {
	if policy == nil || policy.resolver == nil {
		return ResolvedPath{}, errors.New("filesystem policy is nil")
	}
	resolved, err := policy.resolver.ResolveExisting(requested, expected)
	if err != nil {
		return ResolvedPath{}, err
	}
	return policy.authorizeResolved(resolved, AccessRead)
}

func (policy *FileSystemPolicy) ResolveForWrite(requested string) (ResolvedPath, error) {
	if policy == nil || policy.resolver == nil {
		return ResolvedPath{}, errors.New("filesystem policy is nil")
	}
	resolved, err := policy.ResolveForWriteTarget(requested)
	if err != nil {
		return ResolvedPath{}, err
	}
	return policy.authorizeResolved(resolved, AccessWrite)
}

// ResolveForWriteTarget resolves a write target and applies only immutable
// deny/read-only rules. The caller may use the returned target to construct a
// user approval request before applying a temporary or session grant.
func (policy *FileSystemPolicy) ResolveForWriteTarget(requested string) (ResolvedPath, error) {
	if policy == nil || policy.resolver == nil {
		return ResolvedPath{}, errors.New("filesystem policy is nil")
	}
	resolved, err := policy.resolver.ResolveForWrite(requested)
	if err != nil {
		return ResolvedPath{}, err
	}
	profile := policy.EffectiveProfile()
	if root := longestMatch(profile.DeniedRoots, resolved.Canonical); root != "" {
		return ResolvedPath{}, &PathDeniedError{Requested: requested, Reason: fmt.Sprintf("is denied by %q", root)}
	}
	if root := longestMatch(profile.ReadOnlyRoots, resolved.Canonical); root != "" {
		return ResolvedPath{}, &PathDeniedError{Requested: requested, Reason: fmt.Sprintf("is read-only under %q", root)}
	}
	return resolved, nil
}

func (policy *FileSystemPolicy) ResolveWritableDirectory(requested string) (ResolvedPath, error) {
	if policy == nil || policy.resolver == nil {
		return ResolvedPath{}, errors.New("filesystem policy is nil")
	}
	resolved, err := policy.resolver.ResolveExisting(requested, PathDirectory)
	if err != nil {
		return ResolvedPath{}, err
	}
	return policy.authorizeResolved(resolved, AccessWrite)
}

func (policy *FileSystemPolicy) ResolveGrantRoot(requested string) (string, error) {
	if policy == nil || policy.resolver == nil {
		return "", errors.New("filesystem policy is nil")
	}
	resolved, err := policy.resolver.ResolveExisting(requested, PathDirectory)
	if err != nil {
		return "", err
	}
	if root := longestMatch(policy.base.DeniedRoots, resolved.Canonical); root != "" {
		return "", &PathDeniedError{Requested: requested, Reason: fmt.Sprintf("is denied by %q", root)}
	}
	if root := longestMatch(policy.base.ReadOnlyRoots, resolved.Canonical); root != "" {
		return "", &PathDeniedError{Requested: requested, Reason: fmt.Sprintf("is read-only under %q", root)}
	}
	return resolved.Canonical, nil
}

func (policy *FileSystemPolicy) authorizeResolved(resolved ResolvedPath, requestedAccess FileAccess) (ResolvedPath, error) {
	profile := policy.EffectiveProfile()
	if root := longestMatch(profile.DeniedRoots, resolved.Canonical); root != "" {
		return ResolvedPath{}, &PathDeniedError{Requested: resolved.Requested, Reason: fmt.Sprintf("is denied by %q", root)}
	}
	if root := longestMatch(profile.ReadOnlyRoots, resolved.Canonical); root != "" {
		if requestedAccess == AccessWrite {
			return ResolvedPath{}, &PathDeniedError{Requested: resolved.Requested, Reason: fmt.Sprintf("is read-only under %q", root)}
		}
		resolved.Access, resolved.MatchedRoot, resolved.RootSource = AccessRead, root, RootSourceReadOnly
		return resolved, nil
	}
	if root, source := writableMatch(profile, resolved.Canonical); root != "" {
		resolved.Access, resolved.MatchedRoot, resolved.RootSource = requestedAccess, root, source
		return resolved, nil
	}
	if requestedAccess == AccessWrite {
		return ResolvedPath{}, &PermissionRequiredError{RequestedPath: resolved.Requested, CanonicalPath: resolved.Canonical, WritableRoot: suggestedWritableRoot(resolved.Canonical)}
	}
	if !profile.ReadHost {
		return ResolvedPath{}, &PathDeniedError{Requested: resolved.Requested, Reason: "is outside declared readable roots"}
	}
	resolved.Access, resolved.MatchedRoot, resolved.RootSource = AccessRead, string(filepath.Separator), RootSourceHost
	return resolved, nil
}

func (policy *FileSystemPolicy) WritableRoots() []string {
	if policy == nil {
		return nil
	}
	roots := append([]string(nil), policy.base.WorkspaceRoots...)
	roots = append(roots, policy.base.TemporaryRoots...)
	sort.Slice(roots, func(left, right int) bool { return len(roots[left]) > len(roots[right]) })
	return roots
}

func (policy *FileSystemPolicy) ReadOnlyRoots() []string {
	if policy == nil {
		return nil
	}
	return append([]string(nil), policy.base.ReadOnlyRoots...)
}

func (policy *FileSystemPolicy) DeniedRoots() []string {
	if policy == nil {
		return nil
	}
	return append([]string(nil), policy.base.DeniedRoots...)
}

func (policy *FileSystemPolicy) ReadHost() bool {
	return policy != nil && policy.base.ReadHost
}
