package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func TestPendingUserMessageAdmissionsRouteBySubmissionID(t *testing.T) {
	admissions := newPendingUserMessageAdmissions()
	first, removeFirst := admissions.register("submission-1")
	defer removeFirst()
	second, removeSecond := admissions.register("submission-2")
	defer removeSecond()
	admissions.complete("submission-2", userMessageAdmissionResult{admission: protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionSteered, TurnID: "turn-2"}})
	admissions.complete("submission-1", userMessageAdmissionResult{admission: protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionStarted, TurnID: "turn-1"}})
	if result := <-first; result.admission.TurnID != "turn-1" || result.err != nil {
		t.Fatalf("first result = %#v", result)
	}
	if result := <-second; result.admission.TurnID != "turn-2" || result.err != nil {
		t.Fatalf("second result = %#v", result)
	}
}

func TestPendingUserMessageAdmissionsFailOnShutdown(t *testing.T) {
	admissions := newPendingUserMessageAdmissions()
	result, remove := admissions.register("submission-1")
	defer remove()
	admissions.failAll(errors.New("shutdown"))
	if admitted := <-result; admitted.err == nil || admitted.err.Error() != "shutdown" {
		t.Fatalf("shutdown result = %#v", admitted)
	}
}

func TestSubmitUserInputAdmissionHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	terminated := make(chan struct{})
	io := SessionIo{
		Submissions: make(chan protocol.Submission), Terminated: terminated,
		admissions: newPendingUserMessageAdmissions(),
	}
	_, err := io.SubmitUserInputAndWaitForAdmission(ctx, protocol.Submission{ID: "submission-1", Op: protocol.UserInputOp{Content: "hello"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestSubmitUserInputAdmissionFailsWhenSessionTerminates(t *testing.T) {
	terminated := make(chan struct{})
	close(terminated)
	io := SessionIo{
		Submissions: make(chan protocol.Submission), Terminated: terminated,
		admissions: newPendingUserMessageAdmissions(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := io.SubmitUserInputAndWaitForAdmission(ctx, protocol.Submission{ID: "submission-1", Op: protocol.UserInputOp{Content: "hello"}})
	if err == nil || err.Error() != "session is terminated" {
		t.Fatalf("termination error = %v", err)
	}
}
