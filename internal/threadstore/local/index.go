package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (store *Store) RebuildIndex(ctx context.Context) error {
	root := filepath.Join(store.home, "sessions")
	threads := make([]threadstore.StoredThread, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		lines, err := rollout.Read(path, protocol.ThreadID{})
		if err != nil {
			return err
		}
		metadata, err := projectMetadata(path, lines)
		if err != nil {
			return err
		}
		threads = append(threads, metadata)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return store.state.ReplaceThreads(ctx, nil)
	}
	if err != nil {
		return fmt.Errorf("scan rollouts for index rebuild: %w", err)
	}
	return store.state.ReplaceThreads(ctx, threads)
}
