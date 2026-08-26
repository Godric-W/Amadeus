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
	}{{profile.WorkspaceRoots, RootSourceWorkspace}, {profile.TemporaryRoots, RootSourceTemporary}}
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
