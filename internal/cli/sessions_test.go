package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func TestSessionsListShowsOnlyCurrentProject(t *testing.T) {
	home := t.TempDir()
	projectOne := t.TempDir()
	projectTwo := t.TempDir()
	store, err := bootstrap.DefaultThreadStoreFactory(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstID, secondID := testutil.ThreadID(1), testutil.ThreadID(2)
	if _, err := store.Materialize(context.Background(), threadstore.CreateInput{SessionID: protocol.SessionIDFromThreadID(firstID), ID: firstID, CWD: projectOne, Title: "one", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CloseWriter(context.Background(), firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Materialize(context.Background(), threadstore.CreateInput{SessionID: protocol.SessionIDFromThreadID(secondID), ID: secondID, CWD: projectTwo, Title: "two", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	options := RootOptions{Environment: bootstrap.Environment{AmadeusRoot: home, WorkingDirectory: projectOne, LookupEnv: emptyEnvLookup}}
	command := newRootCommandWithOptions(&configFlags{}, options)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"sessions", "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), firstID.String()) || strings.Contains(output.String(), secondID.String()) {
		t.Fatalf("unexpected sessions list: %s", output.String())
	}
}
