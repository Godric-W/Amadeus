package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	RootSourceRun       RootSource = "run"
	RootSourceSession   RootSource = "session"
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
	return fmt.Sprintf("permission_required: write access to %q requires request_permissions for writable root %q", err.CanonicalPath, err.WritableRoot)
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

type AdditionalPermissions struct {
	WritableRoots []string
}

func (permissions AdditionalPermissions) Clone() AdditionalPermissions {
	permissions.WritableRoots = append([]string(nil), permissions.WritableRoots...)
	return permissions
}

type PermissionSnapshotSource interface {
	Snapshot() AdditionalPermissions
}

type PermissionStore struct {
	mutex sync.RWMutex
	roots []string
}

func NewPermissionStore() *PermissionStore { return &PermissionStore{} }

func (store *PermissionStore) Snapshot() AdditionalPermissions {
	if store == nil {
		return AdditionalPermissions{}
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	return AdditionalPermissions{WritableRoots: append([]string(nil), store.roots...)}
}

func (store *PermissionStore) GrantWritableRoots(roots []string) error {
	if store == nil {
		return errors.New("permission store is nil")
	}
	canonical := make([]string, 0, len(roots))
	for _, root := range roots {
		value, err := canonicalDirectory(root)
		if err != nil {
			return fmt.Errorf("grant writable root %q: %w", root, err)
		}
		canonical = appendUniquePath(canonical, value)
	}
	store.mutex.Lock()
	for _, root := range canonical {
		store.roots = appendUniquePath(store.roots, root)
	}
	sort.Slice(store.roots, func(left, right int) bool { return len(store.roots[left]) > len(store.roots[right]) })
	store.mutex.Unlock()
	return nil
}

func (store *PermissionStore) Clear() {
	if store == nil {
		return
	}
	store.mutex.Lock()
	store.roots = nil
	store.mutex.Unlock()
}

type EffectivePermissionProfile struct {
	Base    PermissionProfile
	Run     AdditionalPermissions
	Session AdditionalPermissions
}

func (profile EffectivePermissionProfile) WritableRoots() []string {
	roots := make([]string, 0, len(profile.Base.WorkspaceRoots)+len(profile.Base.TemporaryRoots)+len(profile.Run.WritableRoots)+len(profile.Session.WritableRoots))
	for _, values := range [][]string{profile.Base.WorkspaceRoots, profile.Base.TemporaryRoots, profile.Run.WritableRoots, profile.Session.WritableRoots} {
		for _, root := range values {
			roots = appendUniquePath(roots, root)
		}
	}
	sort.Slice(roots, func(left, right int) bool { return len(roots[left]) > len(roots[right]) })
	return roots
}

type FileSystemPolicyOptions struct {
	CWD                string
	Profile            PermissionProfile
	RunPermissions     PermissionSnapshotSource
	SessionPermissions PermissionSnapshotSource
}

type FileSystemPolicy struct {
	resolver           *PathResolver
	base               PermissionProfile
	runPermissions     PermissionSnapshotSource
	sessionPermissions PermissionSnapshotSource
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
	return &FileSystemPolicy{resolver: resolver, base: profile, runPermissions: options.RunPermissions, sessionPermissions: options.SessionPermissions}, nil
}

func (policy *FileSystemPolicy) EffectiveProfile() EffectivePermissionProfile {
	if policy == nil {
		return EffectivePermissionProfile{}
	}
	result := EffectivePermissionProfile{Base: policy.base.Clone()}
	if policy.runPermissions != nil {
		result.Run = policy.runPermissions.Snapshot()
	}
	if policy.sessionPermissions != nil {
		result.Session = policy.sessionPermissions.Snapshot()
	}
	return result
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
	effective := policy.EffectiveProfile()
	if root := longestMatch(effective.Base.DeniedRoots, resolved.Canonical); root != "" {
		return ResolvedPath{}, &PathDeniedError{Requested: requested, Reason: fmt.Sprintf("is denied by %q", root)}
	}
	if root := longestMatch(effective.Base.ReadOnlyRoots, resolved.Canonical); root != "" {
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
	effective := policy.EffectiveProfile()
	if root := longestMatch(effective.Base.DeniedRoots, resolved.Canonical); root != "" {
		return ResolvedPath{}, &PathDeniedError{Requested: resolved.Requested, Reason: fmt.Sprintf("is denied by %q", root)}
	}
	if root := longestMatch(effective.Base.ReadOnlyRoots, resolved.Canonical); root != "" {
		if requestedAccess == AccessWrite {
			return ResolvedPath{}, &PathDeniedError{Requested: resolved.Requested, Reason: fmt.Sprintf("is read-only under %q", root)}
		}
		resolved.Access, resolved.MatchedRoot, resolved.RootSource = AccessRead, root, RootSourceReadOnly
		return resolved, nil
	}
	if root, source := writableMatch(effective, resolved.Canonical); root != "" {
		resolved.Access, resolved.MatchedRoot, resolved.RootSource = requestedAccess, root, source
		return resolved, nil
	}
	if requestedAccess == AccessWrite {
		return ResolvedPath{}, &PermissionRequiredError{RequestedPath: resolved.Requested, CanonicalPath: resolved.Canonical, WritableRoot: suggestedWritableRoot(resolved.Canonical)}
	}
	if !effective.Base.ReadHost {
		return ResolvedPath{}, &PathDeniedError{Requested: resolved.Requested, Reason: "is outside declared readable roots"}
	}
	resolved.Access, resolved.MatchedRoot, resolved.RootSource = AccessRead, string(filepath.Separator), RootSourceHost
	return resolved, nil
}

func (policy *FileSystemPolicy) WritableRoots() []string {
	return policy.EffectiveProfile().WritableRoots()
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

func writableMatch(profile EffectivePermissionProfile, candidate string) (string, RootSource) {
	groups := []struct {
		roots  []string
		source RootSource
	}{
		{profile.Run.WritableRoots, RootSourceRun}, {profile.Session.WritableRoots, RootSourceSession},
		{profile.Base.WorkspaceRoots, RootSourceWorkspace}, {profile.Base.TemporaryRoots, RootSourceTemporary},
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
