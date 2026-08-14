package process

import (
	"context"
	"errors"
	"io"
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

func TestManagerSerializesWritesPerProcess(t *testing.T) {
	writer := &blockingWriteCloser{entered: make(chan struct{}, 2), release: make(chan struct{}, 2)}
	manager := managerWithManagedProcess("process", writer)
	done := make(chan error, 2)
	go func() {
		_, err := manager.WriteContext(context.Background(), "process", "owner", "first", false, 0)
		done <- err
	}()
	<-writer.entered
	go func() {
		_, err := manager.WriteContext(context.Background(), "process", "owner", "second", false, 0)
		done <- err
	}()
	select {
	case <-writer.entered:
		t.Fatal("second write entered before the first released its process lock")
	case <-time.After(20 * time.Millisecond):
	}
	writer.release <- struct{}{}
	<-writer.entered
	writer.release <- struct{}{}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerAllowsWritesToDifferentProcessesInParallel(t *testing.T) {
	first := &blockingWriteCloser{entered: make(chan struct{}, 1), release: make(chan struct{}, 1)}
	second := &blockingWriteCloser{entered: make(chan struct{}, 1), release: make(chan struct{}, 1)}
	manager := NewManager()
	manager.processes["first"] = testManagedProcess("first", first)
	manager.processes["second"] = testManagedProcess("second", second)
	done := make(chan error, 2)
	go func() {
		_, err := manager.WriteContext(context.Background(), "first", "owner", "a", false, 0)
		done <- err
	}()
	go func() {
		_, err := manager.WriteContext(context.Background(), "second", "owner", "b", false, 0)
		done <- err
	}()
	select {
	case <-first.entered:
	case <-time.After(time.Second):
		t.Fatal("first process write did not enter")
	}
	select {
	case <-second.entered:
	case <-time.After(time.Second):
		t.Fatal("second process write was blocked by another process")
	}
	first.release <- struct{}{}
	second.release <- struct{}{}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

type blockingWriteCloser struct {
	entered chan struct{}
	release chan struct{}
}

func (writer *blockingWriteCloser) Write(content []byte) (int, error) {
	writer.entered <- struct{}{}
	<-writer.release
	return len(content), nil
}

func (*blockingWriteCloser) Close() error { return nil }

func managerWithManagedProcess(id ID, stdin io.WriteCloser) *Manager {
	manager := NewManager()
	manager.processes[id] = testManagedProcess(id, stdin)
	return manager
}

func testManagedProcess(id ID, stdin io.WriteCloser) *managed {
	return &managed{id: id, owner: "owner", stdin: stdin, output: newTranscript(1024), state: StateRunning, startedAt: time.Now(), done: make(chan struct{})}
}
