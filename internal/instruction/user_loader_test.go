package instruction

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserLoaderReturnsNilWhenAgentsFileIsMissing(t *testing.T) {
	home := t.TempDir()
	loader := newTestUserLoader(t, home, UserLoaderOptions{})
	document, err := loader.Load(context.Background())
	if err != nil || document != nil {
		t.Fatalf("unexpected missing user instruction result: document=%#v err=%v", document, err)
	}
	if loader.AmadeusHome() != home || loader.Path() != filepath.Join(home, InstructionFileName) || loader.MaxBytes() != DefaultMaxUserInstructionBytes {
		t.Fatalf("unexpected user loader metadata: home=%q path=%q max=%d", loader.AmadeusHome(), loader.Path(), loader.MaxBytes())
	}
}

func TestUserLoaderLoadsDocumentWithCanonicalSource(t *testing.T) {
	home := t.TempDir()
	external := filepath.Join(t.TempDir(), "user-agents.md")
	content := "Use focused tests.\nPreserve user changes.\n"
	if err := os.WriteFile(external, []byte(content), 0o600); err != nil {
		t.Fatalf("write external user instruction: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(home, InstructionFileName)); err != nil {
		t.Fatalf("symlink user instruction: %v", err)
	}
	loader := newTestUserLoader(t, home, UserLoaderOptions{})

	document, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("load user instruction: %v", err)
	}
	if document == nil || document.Source != SourceUser || document.Scope != UserScope() || document.Path != external || document.Content != content || len(document.SHA256) != 64 {
		t.Fatalf("unexpected user instruction document: %#v", document)
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("validate loaded user instruction: %v", err)
	}
}

func TestUserLoaderEnforcesByteBudget(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError bool
	}{
		{name: "exact boundary", content: "1234"},
		{name: "over boundary", content: "12345", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, InstructionFileName), []byte(test.content), 0o600); err != nil {
				t.Fatalf("write user instruction: %v", err)
			}
			loader := newTestUserLoader(t, home, UserLoaderOptions{MaxBytes: 4})
			document, err := loader.Load(context.Background())
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "exceeds 4 byte limit") || document != nil {
					t.Fatalf("unexpected over-budget result: document=%#v err=%v", document, err)
				}
				return
			}
			if err != nil || document == nil || document.Content != test.content {
				t.Fatalf("unexpected boundary result: document=%#v err=%v", document, err)
			}
		})
	}
}

func TestUserLoaderRejectsInvalidExistingFiles(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(string) error
		contains string
	}{
		{
			name: "invalid UTF-8",
			prepare: func(path string) error {
				return os.WriteFile(path, []byte{0xff, 0xfe}, 0o600)
			},
			contains: "not valid UTF-8",
		},
		{
			name: "empty content",
			prepare: func(path string) error {
				return os.WriteFile(path, []byte(" \n"), 0o600)
			},
			contains: "content is empty",
		},
		{
			name: "directory",
			prepare: func(path string) error {
				return os.Mkdir(path, 0o700)
			},
			contains: "not a regular file",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			if err := test.prepare(filepath.Join(home, InstructionFileName)); err != nil {
				t.Fatalf("prepare invalid user instruction: %v", err)
			}
			loader := newTestUserLoader(t, home, UserLoaderOptions{})
			if document, err := loader.Load(context.Background()); err == nil || !strings.Contains(err.Error(), test.contains) || document != nil {
				t.Fatalf("unexpected invalid file result: document=%#v err=%v", document, err)
			}
		})
	}
}

func TestUserLoaderHonorsCancellation(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, InstructionFileName), []byte("rules"), 0o600); err != nil {
		t.Fatalf("write user instruction: %v", err)
	}
	loader := newTestUserLoader(t, home, UserLoaderOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if document, err := loader.Load(ctx); !errors.Is(err, context.Canceled) || document != nil {
		t.Fatalf("unexpected cancelled load result: document=%#v err=%v", document, err)
	}
	if document, err := loader.Load(nil); err == nil || !strings.Contains(err.Error(), "context is nil") || document != nil {
		t.Fatalf("unexpected nil context result: document=%#v err=%v", document, err)
	}
}

func TestNewUserLoaderValidatesRootAndOptions(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write non-directory home: %v", err)
	}
	tests := []struct {
		name     string
		home     string
		options  UserLoaderOptions
		contains string
	}{
		{name: "empty home", contains: "home is empty"},
		{name: "missing home", home: filepath.Join(t.TempDir(), "missing"), contains: "symlinks"},
		{name: "home is file", home: file, contains: "not a directory"},
		{name: "negative max", home: t.TempDir(), options: UserLoaderOptions{MaxBytes: -1}, contains: "cannot be negative"},
		{name: "overflow max", home: t.TempDir(), options: UserLoaderOptions{MaxBytes: math.MaxInt64}, contains: "too large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewUserLoader(test.home, test.options); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected loader construction error: %v", err)
			}
		})
	}

	realHome := t.TempDir()
	parent := t.TempDir()
	symlinkHome := filepath.Join(parent, "home-link")
	if err := os.Symlink(realHome, symlinkHome); err != nil {
		t.Fatalf("symlink Amadeus home: %v", err)
	}
	loader := newTestUserLoader(t, symlinkHome, UserLoaderOptions{})
	if loader.AmadeusHome() != realHome || loader.Path() != filepath.Join(realHome, InstructionFileName) {
		t.Fatalf("user loader did not canonicalize Amadeus home: home=%q path=%q", loader.AmadeusHome(), loader.Path())
	}
}

func TestNilUserLoaderIsSafe(t *testing.T) {
	var loader *UserLoader
	if loader.AmadeusHome() != "" || loader.Path() != "" || loader.MaxBytes() != 0 {
		t.Fatal("nil user loader metadata access is not safe")
	}
	if document, err := loader.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "loader is nil") || document != nil {
		t.Fatalf("unexpected nil loader result: document=%#v err=%v", document, err)
	}
}

func newTestUserLoader(t *testing.T, home string, options UserLoaderOptions) *UserLoader {
	t.Helper()
	loader, err := NewUserLoader(home, options)
	if err != nil {
		t.Fatalf("create user instruction loader: %v", err)
	}
	return loader
}
