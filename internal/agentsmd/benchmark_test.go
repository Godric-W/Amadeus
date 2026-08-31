package agentsmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func BenchmarkRefreshCachedDocuments(b *testing.B) {
	home := b.TempDir()
	rootPath := b.TempDir()
	for index := 0; index < 8; index++ {
		directory := filepath.Join(rootPath, "pkg", string(rune('a'+index)))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, FileName), []byte("stable project instructions"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		b.Fatal(err)
	}
	manager, err := NewManager(home, []project.Root{root}, Options{})
	if err != nil {
		b.Fatal(err)
	}
	if _, _, err := manager.Refresh(context.Background(), filepath.Join(rootPath, "pkg", "a")); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, _, err := manager.Refresh(context.Background(), filepath.Join(rootPath, "pkg", "a")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRefreshFreshMutationObservation(b *testing.B) {
	home := b.TempDir()
	rootPath := b.TempDir()
	path := filepath.Join(rootPath, FileName)
	if err := os.WriteFile(path, []byte("mutable instructions"), 0o600); err != nil {
		b.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		b.Fatal(err)
	}
	manager, err := NewManager(home, []project.Root{root}, Options{})
	if err != nil {
		b.Fatal(err)
	}
	loaded, _, err := manager.Refresh(context.Background(), rootPath)
	if err != nil {
		b.Fatal(err)
	}
	target := tool.ContextTarget{Path: filepath.Join(rootPath, "file.go"), Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectWrite}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := os.WriteFile(path, []byte("mutable instructions"+string(rune(index))), 0o600); err != nil {
			b.Fatal(err)
		}
		if err := manager.ObserveTarget(context.Background(), target, tool.RequestSnapshot{AgentsMdRevision: loaded.Revision}); err == nil {
			// The initial snapshot must become stale after each mutation.
			loaded = manager.Current()
		} else {
			loaded = manager.Current()
		}
	}
}
