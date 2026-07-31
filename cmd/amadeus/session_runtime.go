package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	sessionsqlite "github.com/Godric-W/Amadeus/internal/session/sqlite"
)

type sessionStoreFactory func(context.Context, string) (sessiondomain.Store, io.Closer, error)

var persistentIDSequence atomic.Uint64

func defaultSessionStoreFactory(ctx context.Context, amadeusRoot string) (sessiondomain.Store, io.Closer, error) {
	database, err := sessionsqlite.Open(ctx, amadeusRoot)
	if err != nil {
		return nil, nil, err
	}
	store, err := sessionsqlite.NewStore(database)
	if err != nil {
		return nil, nil, errors.Join(err, database.Close())
	}
	return store, database, nil
}

func nextPersistentID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UTC().UnixNano(), persistentIDSequence.Add(1))
}
