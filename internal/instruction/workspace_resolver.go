package instruction

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

var ErrTargetOutsideWorkspace = errors.New("instruction target is outside workspace roots")

type WorkspaceResolver struct {
	user    *UserLoader
	roots   []project.Root
	loaders map[string]*ProjectLoader
}

func NewWorkspaceResolver(user *UserLoader, roots []project.Root, options ProjectLoaderOptions) (*WorkspaceResolver, error) {
	if user == nil {
		return nil, errors.New("workspace instruction Resolver user loader is nil")
	}
	if len(roots) == 0 {
		return nil, errors.New("workspace instruction Resolver has no roots")
	}
	result := &WorkspaceResolver{user: user, loaders: make(map[string]*ProjectLoader)}
	for _, root := range roots {
		if root.Path() == "" {
			return nil, errors.New("workspace instruction Resolver root is empty")
		}
		if _, exists := result.loaders[root.Path()]; exists {
			continue
		}
		loader, err := NewProjectLoader(root, options)
		if err != nil {
			return nil, err
		}
		result.roots = append(result.roots, root)
		result.loaders[root.Path()] = loader
	}
	sort.Slice(result.roots, func(left, right int) bool { return len(result.roots[left].Path()) > len(result.roots[right].Path()) })
	return result, nil
}

func (resolver *WorkspaceResolver) ResolveTarget(ctx context.Context, target string, kind TargetKind) (ResolveRequest, Resolution, error) {
	if resolver == nil || resolver.user == nil {
		return ResolveRequest{}, Resolution{}, errors.New("workspace instruction Resolver is nil")
	}
	absolute, err := filepath.Abs(strings.TrimSpace(target))
	if err != nil {
		return ResolveRequest{}, Resolution{}, fmt.Errorf("resolve instruction target: %w", err)
	}
	absolute = filepath.Clean(absolute)
	root, ok := resolver.matchRoot(absolute)
	if !ok {
		return ResolveRequest{}, Resolution{}, fmt.Errorf("%w: %q", ErrTargetOutsideWorkspace, absolute)
	}
	relative, err := filepath.Rel(root.Path(), absolute)
	if err != nil {
		return ResolveRequest{}, Resolution{}, err
	}
	relative = filepath.ToSlash(relative)
	if relative == "" {
		relative = "."
	}
	request, err := NewResolveRequest(root, relative, kind)
	if err != nil {
		return ResolveRequest{}, Resolution{}, err
	}
	layered, err := NewLayeredResolver(resolver.user, resolver.loaders[root.Path()])
	if err != nil {
		return ResolveRequest{}, Resolution{}, err
	}
	resolution, err := layered.Resolve(ctx, request)
	return request, resolution, err
}

func (resolver *WorkspaceResolver) matchRoot(target string) (project.Root, bool) {
	for _, root := range resolver.roots {
		relative, err := filepath.Rel(root.Path(), target)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return root, true
		}
	}
	return project.Root{}, false
}
