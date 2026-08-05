package process

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManagerYieldsWritesAndCompletesOwnedProcess(t *testing.T) {
	manager := NewManager()
	id, err := manager.Start("run-1", Command{Shell: "/bin/sh", Command: "read value; printf 'got:%s' \"$value\"", Directory: t.TempDir(), Timeout: 2 * time.Second, MaxOutputBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := manager.Snapshot(id, "run-1", 10*time.Millisecond)
	if err != nil || initial.State != StateRunning {
		t.Fatalf("unexpected initial snapshot: %#v err=%v", initial, err)
	}
	completed, err := manager.Write(id, "run-1", "hello\n", false, time.Second)
	if err != nil || completed.State != StateCompleted || !strings.Contains(completed.Output, "got:hello") {
		t.Fatalf("unexpected completed snapshot: %#v err=%v", completed, err)
	}
	if _, err := manager.Snapshot(id, "run-2", 0); !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("unexpected owner error: %v", err)
	}
}

func TestManagerPTYAndOwnerCleanup(t *testing.T) {
	manager := NewManager()
	id, err := manager.Start("run-pty", Command{Shell: "/bin/sh", Command: "read value; printf 'tty:%s' \"$value\"", Directory: t.TempDir(), Timeout: 2 * time.Second, TTY: true, MaxOutputBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := manager.WriteContext(context.Background(), id, "run-pty", "hello\n", false, time.Second)
	if err != nil || completed.State != StateCompleted || !strings.Contains(completed.Output, "tty:hello") {
		t.Fatalf("unexpected PTY snapshot: %#v err=%v", completed, err)
	}

	id, err = manager.Start("run-cancel", Command{Shell: "/bin/sh", Command: "sleep 10", Directory: t.TempDir(), Timeout: 20 * time.Second, MaxOutputBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.CloseOwner("run-cancel")
	manager.CloseOwner("run-cancel")
	snapshot, err := manager.Snapshot(id, "run-cancel", time.Second)
	if err != nil || snapshot.State != StateCancelled {
		t.Fatalf("unexpected cancelled snapshot: %#v err=%v", snapshot, err)
	}
}

func TestManagerReportsNonZeroExitAndCancelIsIdempotent(t *testing.T) {
	manager := NewManager()
	t.Cleanup(manager.Close)
	failedID, err := manager.Start("run-failed", Command{Shell: "/bin/sh", Command: "printf failed; exit 7", Directory: t.TempDir(), Timeout: 2 * time.Second, MaxOutputBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := manager.Snapshot(failedID, "run-failed", time.Second)
	if err != nil || failed.State != StateFailed || failed.ExitCode != 7 || failed.Output != "failed" {
		t.Fatalf("unexpected failed snapshot: %#v err=%v", failed, err)
	}

	cancelID, err := manager.Start("run-cancel", Command{Shell: "/bin/sh", Command: "sleep 10", Directory: t.TempDir(), Timeout: 20 * time.Second, MaxOutputBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(cancelID, "run-cancel"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(cancelID, "run-cancel"); err != nil {
		t.Fatalf("second cancel failed: %v", err)
	}
	cancelled, err := manager.Snapshot(cancelID, "run-cancel", time.Second)
	if err != nil || cancelled.State != StateCancelled {
		t.Fatalf("unexpected cancelled snapshot: %#v err=%v", cancelled, err)
	}
}
