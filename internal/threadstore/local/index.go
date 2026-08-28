package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func (store *Store) RebuildIndex(ctx context.Context) error {
	root := filepath.Join(store.home, "sessions")
	threads := make([]threadstore.StoredThread, 0)
	type edgeProjection struct {
		state     protocol.AgentSpawnEdgeState
		updatedAt time.Time
	}
	edges := make(map[protocol.ThreadID]edgeProjection)
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
		for _, line := range lines {
			edge, ok := line.Item.(rollout.AgentSpawnEdgeItem)
			if !ok {
				continue
			}
			edges[edge.AgentID] = edgeProjection{state: edge.State, updatedAt: edge.UpdatedAt}
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return store.state.ReplaceThreads(ctx, nil)
	}
	if err != nil {
		return fmt.Errorf("scan rollouts for index rebuild: %w", err)
	}
	for index := range threads {
		edge, ok := edges[threads[index].ID]
		if !ok || !threads[index].Source.IsSubAgent() {
			continue
		}
		threads[index].AgentEdgeState = edge.state
		if edge.updatedAt.After(threads[index].UpdatedAt) {
			threads[index].UpdatedAt = edge.updatedAt
		}
	}
	return store.state.ReplaceThreads(ctx, threads)
}
