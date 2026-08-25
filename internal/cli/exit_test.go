package cli

import (
	"context"
	"errors"
	"testing"
)

func TestCommandExitErrorControlsProcessSemantics(t *testing.T) {
	err := &commandExitError{code: exitCodePartial, message: "result: partial", reported: true}
	if exitCode(err) != exitCodePartial || !errorAlreadyReported(err) {
		t.Fatalf("unexpected command exit semantics: code=%d reported=%t", exitCode(err), errorAlreadyReported(err))
	}
	if exitCode(errors.New("ordinary")) != exitCodeFailure || errorAlreadyReported(errors.New("ordinary")) {
		t.Fatal("ordinary errors must use unreported failure semantics")
	}
	if exitCode(context.Canceled) != exitCodeCancelled {
		t.Fatalf("cancelled context exit code = %d", exitCode(context.Canceled))
	}
}
