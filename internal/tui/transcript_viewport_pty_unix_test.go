//go:build !windows

package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/protocol"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
)

func TestTranscriptViewportPTYKeepsOlderHistoryNavigable(t *testing.T) {
	primary, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	defer terminal.Close()
	if err := pty.Setsize(primary, &pty.Winsize{Rows: 12, Cols: 80}); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	items := make([]protocol.TurnItem, 40)
	for index := range items {
		items[index] = protocol.TurnItem{
			ID:          protocol.ItemID(fmt.Sprintf("assistant-%d", index)),
			Kind:        protocol.ItemAssistantMessage,
			Status:      protocol.ItemStatusCompleted,
			CreatedAt:   now,
			CompletedAt: now,
			Text:        fmt.Sprintf("message %d", index),
		}
	}
	fake := newFakeApplicationPort()
	threadID := testThreadID(1)
	app, err := NewApplication(ApplicationOptions{
		Input: terminal, Output: terminal, Application: fake,
		Snapshot: application.ThreadViewSnapshot{
			Generation: 1, SessionID: protocol.SessionIDFromThreadID(threadID), ThreadID: threadID,
			Configuration: protocol.SessionConfiguration{CWD: "/workspace", Model: "test-model", Mode: protocol.ModeKindDefault},
			Items:         items,
		},
		DisableAnimations: true, Width: 80, NoColor: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, runErr := app.Run(ctx)
		result <- runErr
	}()

	bottom := readPTYUntil(t, primary, "message 39", 3*time.Second)
	for _, expected := range []string{">_ Amadeus", "message 0", "message 39"} {
		if !strings.Contains(bottom, expected) {
			t.Fatalf("native scrollback output omitted %q: %q", expected, bottom)
		}
	}

	if _, err := primary.Write([]byte{4}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("TUI exit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("TUI did not exit after viewport contract test")
	}
}

func TestLiveMarkdownCompletionPrintsFullFinalAndRetainsEarlierHistory(t *testing.T) {
	primary, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	defer terminal.Close()
	if err := pty.Setsize(primary, &pty.Winsize{Rows: 12, Cols: 80}); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 28, 11, 0, 0, 0, time.UTC)
	threadID := testThreadID(1)
	fake := newFakeApplicationPort()
	app, err := NewApplication(ApplicationOptions{
		Input: terminal, Output: terminal, Application: fake,
		Snapshot: application.ThreadViewSnapshot{
			Generation: 1, SessionID: protocol.SessionIDFromThreadID(threadID), ThreadID: threadID,
			Configuration: protocol.SessionConfiguration{CWD: "/workspace", Model: "test-model", Mode: protocol.ModeKindDefault},
			Items: []protocol.TurnItem{{
				ID: "user-1", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted,
				CreatedAt: now, CompletedAt: now, Text: "familiarize yourself with this project",
			}},
		},
		DisableAnimations: true, Width: 80, NoColor: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, runErr := app.Run(ctx)
		result <- runErr
	}()

	initialOutput := readPTYUntil(t, primary, "familiarize yourself with this project", 3*time.Second)
	var finalSource strings.Builder
	finalSource.WriteString("## Project Overview\n\n")
	for index := 1; index <= 20; index++ {
		fmt.Fprintf(&finalSource, "final-line-%02d describes the project architecture.\n\n", index)
	}
	source := finalSource.String()
	for _, message := range []protocol.EventMsg{
		protocol.TurnStartedEvent{StartedAt: now},
		protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}},
		protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "streaming preview\n"},
		protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: source}},
		protocol.TurnCompleteEvent{Outcome: protocol.TurnOutcomeCompleted},
	} {
		fake.events <- application.SessionEventObserved{Generation: 1, Event: testProtocolEvent(threadID, "turn-1", message)}
	}
	finalOutput := readPTYUntil(t, primary, "final-line-20", 3*time.Second)
	allOutput := initialOutput + finalOutput
	for _, expected := range []string{"familiarize yourself with this project", "Project Overview", "final-line-01", "final-line-20"} {
		if !strings.Contains(allOutput, expected) {
			t.Fatalf("live completion output omitted %q: %q", expected, allOutput)
		}
	}

	if _, err := primary.Write([]byte{4}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("TUI exit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("TUI did not exit after live completion test")
	}
}

func readPTYUntil(t *testing.T, terminal *os.File, needle string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	if err := terminal.SetReadDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	buffer := make([]byte, 4096)
	for {
		count, err := terminal.Read(buffer)
		if count > 0 {
			output.Write(buffer[:count])
			plain := xansi.Strip(output.String())
			if strings.Contains(plain, needle) {
				return plain
			}
		}
		if err != nil {
			t.Fatalf("read PTY waiting for %q: %v; output=%q", needle, err, xansi.Strip(output.String()))
		}
	}
}
