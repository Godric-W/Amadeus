package sqlite

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type Runtime struct {
	threads  *threadStore
	goals    *goalStore
	dbs      []*database
	close    sync.Once
	closeErr error
}

func Open(ctx context.Context, amadeusHome string, clock func() time.Time) (*Runtime, error) {
	if clock == nil {
		clock = time.Now
	}
	threadDB, err := openDatabase(ctx, amadeusHome, stateFilename, threadSchema)
	if err != nil {
		return nil, err
	}
	goalDB, err := openDatabase(ctx, amadeusHome, goalsFilename, goalSchema)
	if err != nil {
		_ = threadDB.close()
		return nil, err
	}
	return &Runtime{
		threads: &threadStore{db: threadDB.db},
		goals:   &goalStore{db: goalDB.db, clock: clock},
		dbs:     []*database{goalDB, threadDB},
	}, nil
}

func (runtime *Runtime) Threads() threadstore.MetadataDB {
	if runtime == nil {
		return nil
	}
	return runtime.threads
}

func (runtime *Runtime) Goals() state.GoalStore {
	if runtime == nil {
		return nil
	}
	return runtime.goals
}

func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	runtime.close.Do(func() {
		for _, db := range runtime.dbs {
			runtime.closeErr = errors.Join(runtime.closeErr, db.close())
		}
	})
	return runtime.closeErr
}

var _ state.Runtime = (*Runtime)(nil)
