package agentsmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

const (
	FileName        = "AGENTS.md"
	DefaultMaxBytes = int64(64 * 1024)
)

type Options struct {
	MaxBytes int64
}

type AgentsMdManager struct {
	mu        sync.Mutex
	userPath  string
	roots     []project.Root
	knownDirs map[string]struct{}
	maxBytes  int64
	loaded    LoadedAgentsMd
	cache     map[string]cachedDocument
}

type documentFingerprint struct {
	exists  bool
	size    int64
	modTime int64
	mode    uint32
}

type cachedDocument struct {
	fingerprint documentFingerprint
	document    *Document
}

func NewManager(amadeusHome string, roots []project.Root, options Options) (*AgentsMdManager, error) {
	if strings.TrimSpace(amadeusHome) == "" {
		return nil, errors.New("AGENTS.md manager home is empty")
	}
	home, err := filepath.Abs(amadeusHome)
	if err != nil {
		return nil, fmt.Errorf("resolve AGENTS.md manager home: %w", err)
	}
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		return nil, errors.New("AGENTS.md manager home is not a directory")
	}
	if len(roots) == 0 {
		return nil, errors.New("AGENTS.md manager has no workspace roots")
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxBytes < 0 {
		return nil, errors.New("AGENTS.md max bytes cannot be negative")
	}
	manager := &AgentsMdManager{
		userPath:  filepath.Join(filepath.Clean(home), FileName),
		knownDirs: make(map[string]struct{}), cache: make(map[string]cachedDocument), maxBytes: maxBytes,
		loaded: newLoaded(nil),
	}
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		if root.Path() == "" {
			return nil, errors.New("AGENTS.md workspace root is empty")
		}
		if _, ok := seen[root.Path()]; ok {
			continue
		}
		seen[root.Path()] = struct{}{}
		manager.roots = append(manager.roots, root)
	}
	sort.Slice(manager.roots, func(left, right int) bool { return len(manager.roots[left].Path()) > len(manager.roots[right].Path()) })
	return manager, nil
}

func (manager *AgentsMdManager) Refresh(ctx context.Context, cwd string) (LoadedAgentsMd, bool, error) {
	if manager == nil {
		return LoadedAgentsMd{}, false, errors.New("AGENTS.md manager is nil")
	}
	directory, err := targetDirectory(cwd, tool.ContextTargetDirectory)
	if err != nil {
		return LoadedAgentsMd{}, false, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.rememberDirectory(directory)
	loaded, err := manager.load(ctx, false)
	if err != nil {
		return LoadedAgentsMd{}, false, err
	}
	changed := loaded.Revision != manager.loaded.Revision
	manager.loaded = loaded
	return loaded.Clone(), changed, nil
}

func (manager *AgentsMdManager) Current() LoadedAgentsMd {
	if manager == nil {
		return newLoaded(nil)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.loaded.Clone()
}

func (manager *AgentsMdManager) ObserveTarget(ctx context.Context, target tool.ContextTarget, snapshot tool.RequestSnapshot) error {
	if manager == nil {
		return errors.New("AGENTS.md manager is nil")
	}
	directory, err := targetDirectory(target.Path, target.Kind)
	if err != nil {
		return err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	newDirectory := !manager.rememberDirectory(directory)
	if newDirectory || target.SideEffect == tool.SideEffectWrite || target.SideEffect == tool.SideEffectExecute {
		loaded, loadErr := manager.load(ctx, target.SideEffect == tool.SideEffectWrite || target.SideEffect == tool.SideEffectExecute)
		if loadErr != nil {
			return loadErr
		}
		manager.loaded = loaded
	}
	current := manager.loaded.Revision
	if (target.SideEffect == tool.SideEffectWrite || target.SideEffect == tool.SideEffectExecute) && snapshot.AgentsMdRevision != current {
		return &StaleError{Target: target.Path, Expected: snapshot.AgentsMdRevision, Current: current}
	}
	return nil
}

func (manager *AgentsMdManager) rememberDirectory(directory string) bool {
	if _, ok := manager.matchRoot(directory); !ok {
		return true
	}
	if _, exists := manager.knownDirs[directory]; exists {
		return true
	}
	manager.knownDirs[directory] = struct{}{}
	return false
}

func (manager *AgentsMdManager) load(ctx context.Context, forceFresh bool) (LoadedAgentsMd, error) {
	if ctx == nil {
		return LoadedAgentsMd{}, errors.New("AGENTS.md load context is nil")
	}
	if err := ctx.Err(); err != nil {
		return LoadedAgentsMd{}, err
	}
	documents := make(map[string]Document)
	remaining := manager.maxBytes
	if document, used, err := manager.loadDocument(ctx, manager.userPath, SourceUser, "", "", remaining, forceFresh); err != nil {
		return LoadedAgentsMd{}, err
	} else if document != nil {
		documents[document.Path] = *document
		remaining -= used
	}
	directories := make([]string, 0, len(manager.knownDirs))
	for directory := range manager.knownDirs {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	for _, directory := range directories {
		root, ok := manager.matchRoot(directory)
		if !ok {
			continue
		}
		for _, candidateDirectory := range directoriesFromRoot(root.Path(), directory) {
			if remaining == 0 {
				break
			}
			path := filepath.Join(candidateDirectory, FileName)
			if _, exists := documents[path]; exists {
				continue
			}
			relative, err := filepath.Rel(root.Path(), candidateDirectory)
			if err != nil {
				return LoadedAgentsMd{}, err
			}
			if relative == "" {
				relative = "."
			}
			document, used, err := manager.loadDocument(ctx, path, SourceProject, root.Path(), relative, remaining, forceFresh)
			if err != nil {
				return LoadedAgentsMd{}, err
			}
			if document != nil {
				documents[document.Path] = *document
				remaining -= used
			}
		}
	}
	ordered := make([]Document, 0, len(documents))
	for _, document := range documents {
		ordered = append(ordered, document)
	}
	return newLoaded(ordered), nil
}

func (manager *AgentsMdManager) loadDocument(ctx context.Context, path string, source Source, root, directory string, remaining int64, forceFresh bool) (*Document, int64, error) {
	fingerprint, err := fingerprintFor(path)
	if err != nil {
		return nil, 0, err
	}
	if !forceFresh {
		if cached, ok := manager.cache[path]; ok && cached.fingerprint == fingerprint {
			if cached.document == nil || remaining < int64(len(cached.document.Content)) {
				return nil, 0, nil
			}
			copy := *cached.document
			return &copy, int64(len(copy.Content)), nil
		}
	}
	document, used, err := loadDocument(ctx, path, source, root, directory, remaining)
	if err != nil {
		return nil, 0, err
	}
	var cached *Document
	if document != nil {
		copy := *document
		cached = &copy
	}
	manager.cache[path] = cachedDocument{fingerprint: fingerprint, document: cached}
	return document, used, nil
}

func fingerprintFor(path string) (documentFingerprint, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return documentFingerprint{}, nil
	}
	if err != nil {
		return documentFingerprint{}, fmt.Errorf("stat AGENTS.md %q: %w", path, err)
	}
	return documentFingerprint{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano(), mode: uint32(info.Mode())}, nil
}

func (manager *AgentsMdManager) matchRoot(target string) (project.Root, bool) {
	for _, root := range manager.roots {
		relative, err := filepath.Rel(root.Path(), target)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return root, true
		}
	}
	return project.Root{}, false
}

func targetDirectory(path string, kind tool.ContextTargetKind) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("resolve AGENTS.md target: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if kind == tool.ContextTargetFile {
		return filepath.Dir(absolute), nil
	}
	if kind != tool.ContextTargetDirectory && kind != tool.ContextTargetCommandCWD {
		return "", fmt.Errorf("unsupported AGENTS.md target kind %q", kind)
	}
	return absolute, nil
}

func directoriesFromRoot(root, target string) []string {
	directories := []string{target}
	for target != root {
		parent := filepath.Dir(target)
		if parent == target || len(parent) < len(root) {
			break
		}
		target = parent
		directories = append(directories, target)
	}
	for left, right := 0, len(directories)-1; left < right; left, right = left+1, right-1 {
		directories[left], directories[right] = directories[right], directories[left]
	}
	return directories
}

func loadDocument(ctx context.Context, path string, source Source, root, directory string, remaining int64) (*Document, int64, error) {
	if remaining == 0 {
		return nil, 0, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open AGENTS.md %q: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, nil
	}
	content, err := io.ReadAll(io.LimitReader(file, remaining))
	if err != nil {
		return nil, 0, err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(string(content)) == "" {
		return nil, 0, nil
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, 0, err
	}
	document, err := newDocument(source, realPath, root, directory, string(content))
	if err != nil {
		return nil, 0, err
	}
	return &document, int64(len(content)), nil
}

type StaleError struct {
	Target   string
	Expected string
	Current  string
}

func (err *StaleError) Error() string {
	return fmt.Sprintf("AGENTS.md snapshot is stale for target %q", err.Target)
}

func (err *StaleError) ToolErrorKind() string { return "stale_agents_md" }

var _ tool.TargetObserver = (*AgentsMdManager)(nil)
