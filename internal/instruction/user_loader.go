package instruction

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	InstructionFileName            = "AGENTS.md"
	DefaultMaxUserInstructionBytes = int64(64 * 1024)
)

type UserLoaderOptions struct {
	MaxBytes int64
}

type UserLoader struct {
	amadeusHome string
	path        string
	maxBytes    int64
}

func NewUserLoader(amadeusHome string, options UserLoaderOptions) (*UserLoader, error) {
	amadeusHome = strings.TrimSpace(amadeusHome)
	if amadeusHome == "" {
		return nil, errors.New("user instruction Amadeus home is empty")
	}
	absolute, err := filepath.Abs(amadeusHome)
	if err != nil {
		return nil, fmt.Errorf("resolve user instruction Amadeus home: %w", err)
	}
	realHome, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve user instruction Amadeus home symlinks: %w", err)
	}
	info, err := os.Stat(realHome)
	if err != nil {
		return nil, fmt.Errorf("stat user instruction Amadeus home: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("user instruction Amadeus home is not a directory")
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxUserInstructionBytes
	}
	if maxBytes < 0 {
		return nil, errors.New("user instruction max bytes cannot be negative")
	}
	if maxBytes == math.MaxInt64 {
		return nil, errors.New("user instruction max bytes is too large")
	}
	return &UserLoader{
		amadeusHome: filepath.Clean(realHome),
		path:        filepath.Join(realHome, InstructionFileName),
		maxBytes:    maxBytes,
	}, nil
}

func (loader *UserLoader) AmadeusHome() string {
	if loader == nil {
		return ""
	}
	return loader.amadeusHome
}

func (loader *UserLoader) Path() string {
	if loader == nil {
		return ""
	}
	return loader.path
}

func (loader *UserLoader) MaxBytes() int64 {
	if loader == nil {
		return 0
	}
	return loader.maxBytes
}

func (loader *UserLoader) Load(ctx context.Context) (*InstructionDocument, error) {
	if loader == nil {
		return nil, errors.New("user instruction loader is nil")
	}
	if ctx == nil {
		return nil, errors.New("user instruction context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := os.Open(loader.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open user instruction %q: %w", loader.path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat user instruction %q: %w", loader.path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("user instruction %q is not a regular file", loader.path)
	}
	content, err := io.ReadAll(io.LimitReader(file, loader.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read user instruction %q: %w", loader.path, err)
	}
	if int64(len(content)) > loader.maxBytes {
		return nil, fmt.Errorf("user instruction %q exceeds %d byte limit", loader.path, loader.maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	realPath, err := filepath.EvalSymlinks(loader.path)
	if err != nil {
		return nil, fmt.Errorf("resolve user instruction %q symlinks: %w", loader.path, err)
	}
	document, err := NewInstructionDocument(SourceUser, realPath, UserScope(), string(content))
	if err != nil {
		return nil, fmt.Errorf("validate user instruction %q: %w", loader.path, err)
	}
	return &document, nil
}
