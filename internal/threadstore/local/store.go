package local

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type Store struct {
	mu        sync.Mutex
	home      string
	state     threadstore.MetadataDB
	clock     rollout.Clock
	recorders map[protocol.ThreadID]durableRecorder
}

func NewStore(home string, stateDB threadstore.MetadataDB, clock rollout.Clock) (*Store, error) {
	if strings.TrimSpace(home) == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return nil, errors.New("local thread store home must be a clean absolute path")
	}
	if stateDB == nil {
		return nil, errors.New("local thread store state DB is nil")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Store{home: home, state: stateDB, clock: clock, recorders: make(map[protocol.ThreadID]durableRecorder)}, nil
}

func (store *Store) Close() error {
	store.mu.Lock()
	recorders := make([]durableRecorder, 0, len(store.recorders))
	for id, recorder := range store.recorders {
		recorders = append(recorders, recorder)
		delete(store.recorders, id)
	}
	store.mu.Unlock()
	var result error
	for _, recorder := range recorders {
		result = errors.Join(result, recorder.Close(context.Background()))
	}
	return errors.Join(result, store.state.Close())
}

func (store *Store) rolloutPath(id protocol.ThreadID, at time.Time) string {
	stamp := at.UTC().Format("2006-01-02T15-04-05.000000000Z")
	return filepath.Join(store.home, "sessions", at.UTC().Format("2006"), at.UTC().Format("01"), at.UTC().Format("02"), fmt.Sprintf("rollout-%s-%s.jsonl", stamp, id.String()))
}
