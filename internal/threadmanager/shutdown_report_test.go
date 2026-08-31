package threadmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestThreadShutdownReportKeepsTypedOutcomesAndDiagnostics(t *testing.T) {
	completed, failed, timedOut := testutil.ThreadID(1), testutil.ThreadID(2), testutil.ThreadID(3)
	submitErr := errors.New("submission rejected")
	report := ThreadShutdownReport{Results: []ThreadShutdownResult{
		{ThreadID: timedOut, Outcome: ThreadShutdownTimedOut, Err: context.DeadlineExceeded},
		{ThreadID: completed, Outcome: ThreadShutdownCompleted},
		{ThreadID: failed, Outcome: ThreadShutdownSubmitFail, Err: submitErr},
	}}
	if got := report.Completed(); len(got) != 1 || got[0] != completed {
		t.Fatalf("completed = %#v", got)
	}
	if got := report.SubmitFailed(); len(got) != 1 || got[0] != failed {
		t.Fatalf("submit failed = %#v", got)
	}
	if got := report.TimedOut(); len(got) != 1 || got[0] != timedOut {
		t.Fatalf("timed out = %#v", got)
	}
	if report.Err() == nil || !errors.Is(report.Err(), context.DeadlineExceeded) || !errors.Is(report.Err(), submitErr) {
		t.Fatalf("report error lost diagnostics: %v", report.Err())
	}
}

func TestClassifyThreadShutdownError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := classifyThreadShutdown(ctx, context.Canceled); got != ThreadShutdownSubmitFail {
		t.Fatalf("canceled shutdown = %q", got)
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), contextDeadlineInPast())
	defer deadlineCancel()
	if got := classifyThreadShutdown(deadlineCtx, context.DeadlineExceeded); got != ThreadShutdownTimedOut {
		t.Fatalf("deadline shutdown = %q", got)
	}
	if got := classifyThreadShutdown(context.Background(), nil); got != ThreadShutdownCompleted {
		t.Fatalf("successful shutdown = %q", got)
	}
}

// Kept in this file to avoid relying on wall-clock sleeps in the classification test.
func contextDeadlineInPast() (deadline time.Time) {
	return time.Now().Add(-time.Second)
}
