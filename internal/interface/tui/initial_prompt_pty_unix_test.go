//go:build !windows

package tui

import (
	"context"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/creack/pty"
)

func TestInitialPromptPTYSubmitsThenTUIRemainsInteractive(t *testing.T) {
	primary, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	defer terminal.Close()
	if err := pty.Setsize(primary, &pty.Winsize{Rows: 30, Cols: 100}); err != nil {
		t.Fatal(err)
	}

	fake := newFakeFullscreenApplication()
	threadID := testThreadID(1)
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: terminal, Output: terminal, Application: fake,
		InitialUserMessage: &UserMessage{Text: "inspect from PTY"},
		Snapshot: application.ThreadViewSnapshot{
			Generation: 1, SessionID: protocol.SessionIDFromThreadID(threadID), ThreadID: threadID,
			Configuration: protocol.SessionConfiguration{CWD: "/workspace", Model: "test-model", Mode: protocol.ModeKindDefault},
		},
		DisableAnimations: true, Width: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, runErr := app.Run(ctx)
		result <- runErr
	}()

	select {
	case submitted := <-fake.submittedSignal:
		if submitted != "inspect from PTY" {
			t.Fatalf("submitted prompt = %q", submitted)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for initial prompt submission")
	}
	if _, err := primary.Write([]byte{4}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("TUI exit: %v", err)
		}
		if fake.shutdowns != 1 {
			t.Fatalf("shutdown calls = %d", fake.shutdowns)
		}
	case <-ctx.Done():
		t.Fatal("TUI did not remain interactive and exit through Ctrl+D")
	}
}
