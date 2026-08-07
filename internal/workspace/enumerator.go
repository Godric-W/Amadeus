package workspace

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

type FileEnumerator struct {
	root    project.Root
	guard   *project.PathGuard
	ignored *IgnoreMatcher
}

type EnumerateOptions struct {
	Path          string
	IncludeHidden bool
	MaxResults    int
}

type FileEntry struct {
	Relative string
	Absolute string
	Size     int64
	Symlink  bool
}

type EnumerateResult struct {
	Files   []FileEntry
	Partial bool
	Scanned int
}

var defaultIgnoredDirectories = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {}, "node_modules": {}, "vendor": {}, "dist": {}, "build": {}, "target": {},
}

func NewFileEnumerator(root project.Root, matcher *IgnoreMatcher) (*FileEnumerator, error) {
	if root.Path() == "" {
		return nil, errors.New("file enumerator project root is empty")
	}
	guard, err := project.NewPathGuard(root)
	if err != nil {
		return nil, err
	}
	return &FileEnumerator{root: root, guard: guard, ignored: matcher}, nil
}

func NewFileEnumeratorWithGuard(root project.Root, matcher *IgnoreMatcher, guard *project.PathGuard) (*FileEnumerator, error) {
	if root.Path() == "" {
		return nil, errors.New("file enumerator project root is empty")
	}
	if guard == nil {
		return nil, errors.New("file enumerator path guard is nil")
	}
	return &FileEnumerator{root: root, guard: guard, ignored: matcher}, nil
}

func (enumerator *FileEnumerator) Enumerate(ctx context.Context, options EnumerateOptions) (EnumerateResult, error) {
	base := strings.TrimSpace(options.Path)
	if base == "" {
		base = "."
	}
	absolute, err := enumerator.guard.ResolveExisting(base, project.PathAny)
	if err != nil {
		return EnumerateResult{}, err
	}
	return enumerator.EnumeratePrepared(ctx, absolute, options)
}

func (enumerator *FileEnumerator) EnumeratePrepared(ctx context.Context, absolute string, options EnumerateOptions) (EnumerateResult, error) {
	info, err := os.Stat(absolute)
	if err != nil {
		return EnumerateResult{}, err
	}
	if info.Mode().IsRegular() {
		relative := enumerator.displayPath(absolute)
		return EnumerateResult{Files: []FileEntry{{Relative: relative, Absolute: absolute, Size: info.Size()}}, Scanned: 1}, nil
	}
	result := EnumerateResult{}
	err = filepath.WalkDir(absolute, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if filePath == absolute {
			return nil
		}
		walkRelative, err := filepath.Rel(absolute, filePath)
		if err != nil {
			return err
		}
		displayPath := enumerator.displayPath(filePath)
		hidden := hasHiddenSegment(walkRelative)
		if entry.IsDir() {
			if _, ignored := defaultIgnoredDirectories[entry.Name()]; ignored || (!options.IncludeHidden && hidden) || enumerator.ignored.Ignored(walkRelative, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if (!options.IncludeHidden && hidden) || enumerator.ignored.Ignored(walkRelative, false) {
			return nil
		}
		resolved := filePath
		symlink := entry.Type()&os.ModeSymlink != 0
		if symlink {
			resolved, err = enumerator.guard.ResolveExisting(filePath, project.PathAny)
			if err != nil {
				return err
			}
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		result.Scanned++
		if options.MaxResults > 0 && len(result.Files) >= options.MaxResults {
			result.Partial = true
			return fs.SkipAll
		}
		result.Files = append(result.Files, FileEntry{Relative: displayPath, Absolute: resolved, Size: info.Size(), Symlink: symlink})
		return nil
	})
	if err != nil {
		return EnumerateResult{}, err
	}
	sort.Slice(result.Files, func(left, right int) bool { return result.Files[left].Relative < result.Files[right].Relative })
	return result, nil
}

func (enumerator *FileEnumerator) displayPath(path string) string {
	if relative, err := enumerator.root.Relative(path); err == nil {
		return relative
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func hasHiddenSegment(relative string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}
