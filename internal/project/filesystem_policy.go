package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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

func normalizePermissionProfile(profile PermissionProfile) (PermissionProfile, error) {
	var err error
	profile.WorkspaceRoots, err = canonicalDirectories(profile.WorkspaceRoots, "workspace")
	if err != nil {
		return PermissionProfile{}, err
	}
	profile.TemporaryRoots, err = canonicalDirectories(profile.TemporaryRoots, "temporary")
	if err != nil {
		return PermissionProfile{}, err
	}
	profile.ReadOnlyRoots, err = canonicalExistingRoots(profile.ReadOnlyRoots, "read-only")
	if err != nil {
		return PermissionProfile{}, err
	}
	profile.DeniedRoots, err = canonicalExistingRoots(profile.DeniedRoots, "denied")
	if err != nil {
		return PermissionProfile{}, err
	}
	return profile, nil
}

func canonicalDirectories(values []string, kind string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		canonical, err := canonicalDirectory(value)
		if err != nil {
			return nil, fmt.Errorf("filesystem policy %s root %q: %w", kind, value, err)
		}
		result = appendUniquePath(result, canonical)
	}
	sort.Slice(result, func(left, right int) bool { return len(result[left]) > len(result[right]) })
	return result, nil
}

func canonicalExistingRoots(values []string, kind string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		canonical, err := canonicalExistingPath(value)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("filesystem policy %s root %q: %w", kind, value, err)
		}
		result = appendUniquePath(result, canonical)
	}
	sort.Slice(result, func(left, right int) bool { return len(result[left]) > len(result[right]) })
	return result, nil
}

func writableMatch(profile PermissionProfile, candidate string) (string, RootSource) {
	groups := []struct {
		roots  []string
		source RootSource
	}{
		{profile.WorkspaceRoots, RootSourceWorkspace}, {profile.TemporaryRoots, RootSourceTemporary},
	}
	best, source := "", RootSource("")
	for _, group := range groups {
		if root := longestMatch(group.roots, candidate); len(root) > len(best) {
			best, source = root, group.source
		}
	}
	return best, source
}

func longestMatch(roots []string, candidate string) string {
	best := ""
	for _, root := range roots {
		if pathWithin(root, candidate) && len(root) > len(best) {
			best = root
		}
	}
	return best
}

func suggestedWritableRoot(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func DefaultDeniedRoots() []string {
	roots := []string{"/dev", "/proc", "/sys"}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		for _, relative := range []string{".ssh", ".gnupg", ".aws", ".azure", filepath.Join(".config", "gcloud")} {
			roots = append(roots, filepath.Join(home, relative))
		}
	}
	return roots
}

func DefaultTemporaryRoots() []string {
	if runtime.GOOS == "windows" {
		return []string{os.TempDir()}
	}
	roots := []string{"/tmp"}
	if value := strings.TrimSpace(os.Getenv("TMPDIR")); value != "" {
		roots = appendUniquePath(roots, value)
	}
	return roots
}

func appendUniquePath(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func canonicalDirectory(path string) (string, error) {
	canonical, err := canonicalExistingPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return canonical, nil
}

func canonicalExistingPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(canonical), nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
