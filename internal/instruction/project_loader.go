package instruction

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	slashpath "path"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

const DefaultMaxProjectInstructionBytes = int64(64 * 1024)

type ProjectLoaderOptions struct {
	MaxBytes int64
}

type ProjectLoader struct {
	root     project.Root
	maxBytes int64
}

func NewProjectLoader(root project.Root, options ProjectLoaderOptions) (*ProjectLoader, error) {
	if root.Path() == "" {
		return nil, errors.New("project instruction root is empty")
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxProjectInstructionBytes
	}
	if maxBytes < 0 {
		return nil, errors.New("project instruction max bytes cannot be negative")
	}
	if maxBytes == math.MaxInt64 {
		return nil, errors.New("project instruction max bytes is too large")
	}
	return &ProjectLoader{root: root, maxBytes: maxBytes}, nil
}

func (loader *ProjectLoader) Root() project.Root {
	if loader == nil {
		return project.Root{}
	}
	return loader.root
}

func (loader *ProjectLoader) MaxBytes() int64 {
	if loader == nil {
		return 0
	}
	return loader.maxBytes
}

func (loader *ProjectLoader) Discover(ctx context.Context, targetDirectory string) ([]InstructionDocument, error) {
	if loader == nil {
		return nil, errors.New("project instruction loader is nil")
	}
	if ctx == nil {
		return nil, errors.New("project instruction context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := normalizeProjectPath(targetDirectory)
	if err != nil {
		return nil, fmt.Errorf("normalize project instruction target directory: %w", err)
	}

	directories := instructionDirectories(target)
	documents := make([]InstructionDocument, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		absoluteDirectory, err := loader.root.Resolve(filepath.FromSlash(directory))
		if err != nil {
			return nil, fmt.Errorf("resolve project instruction directory %q: %w", directory, err)
		}
		exists, err := loader.validateDirectory(absoluteDirectory)
		if err != nil {
			return nil, fmt.Errorf("validate project instruction directory %q: %w", directory, err)
		}
		if !exists {
			break
		}
		scope := ProjectScope()
		if directory != "." {
			scope, err = NewDirectoryScope(directory)
			if err != nil {
				return nil, err
			}
		}
		document, err := loader.load(ctx, filepath.Join(absoluteDirectory, InstructionFileName), scope)
		if err != nil {
			return nil, err
		}
		if document != nil {
			documents = append(documents, *document)
		}
	}
	return documents, nil
}

func (loader *ProjectLoader) validateDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("path %q is not a directory", path)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	if _, err := loader.root.Relative(realPath); err != nil {
		return false, fmt.Errorf("directory resolves outside project root: %w", err)
	}
	return true, nil
}

func (loader *ProjectLoader) load(ctx context.Context, logicalPath string, scope Scope) (*InstructionDocument, error) {
	_, err := os.Lstat(logicalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect project instruction %q: %w", logicalPath, err)
	}
	realPath, err := filepath.EvalSymlinks(logicalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project instruction %q symlinks: %w", logicalPath, err)
	}
	if _, err := loader.root.Relative(realPath); err != nil {
		return nil, fmt.Errorf("project instruction %q resolves outside project root: %w", logicalPath, err)
	}

	file, err := os.Open(realPath)
	if err != nil {
		return nil, fmt.Errorf("open project instruction %q: %w", logicalPath, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat project instruction %q: %w", logicalPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("project instruction %q is not a regular file", logicalPath)
	}
	content, err := io.ReadAll(io.LimitReader(file, loader.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read project instruction %q: %w", logicalPath, err)
	}
	if int64(len(content)) > loader.maxBytes {
		return nil, fmt.Errorf("project instruction %q exceeds %d byte limit", logicalPath, loader.maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := NewInstructionDocument(SourceProject, filepath.Clean(logicalPath), scope, string(content))
	if err != nil {
		return nil, fmt.Errorf("validate project instruction %q: %w", logicalPath, err)
	}
	return &document, nil
}

func instructionDirectories(target string) []string {
	directories := []string{"."}
	if target == "." {
		return directories
	}
	current := ""
	for _, segment := range strings.Split(target, "/") {
		current = slashpath.Join(current, segment)
		directories = append(directories, current)
	}
	return directories
}
