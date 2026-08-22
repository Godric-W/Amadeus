package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestPresentFullscreenExitPrintsUsageAndResumeHint(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	err := presentFullscreenExit(tui.AppExitInfo{
		TokenUsage: llm.Usage{InputTokens: 8, OutputTokens: 5, TotalTokens: 13},
		ThreadID:   protocol.ThreadID("thread-1"),
		ResumeHint: "amadeus --resume thread-1",
		ExitReason: tui.ExitReasonUserRequested,
	}, &output, &errorOutput, false)
	if err != nil {
		t.Fatal(err)
	}
	want := "Token usage: total=13 input=8 output=5\nTo continue this session, run amadeus --resume thread-1\n"
	if output.String() != want || errorOutput.Len() != 0 {
		t.Fatalf("output=%q errorOutput=%q", output.String(), errorOutput.String())
	}
}

func TestPresentFullscreenExitOmitsZeroUsageAndUnavailableResume(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	if err := presentFullscreenExit(tui.AppExitInfo{ExitReason: tui.ExitReasonUserRequested}, &output, &errorOutput, false); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 || errorOutput.Len() != 0 {
		t.Fatalf("output=%q errorOutput=%q", output.String(), errorOutput.String())
	}
}

func TestPresentFullscreenExitReportsWarningWithoutFailure(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	warning := errors.New("shutdown timed out")
	if err := presentFullscreenExit(tui.AppExitInfo{ExitReason: tui.ExitReasonUserRequested, Error: warning}, &output, &errorOutput, false); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 || !strings.Contains(errorOutput.String(), "WARNING: shutdown timed out") {
		t.Fatalf("output=%q errorOutput=%q", output.String(), errorOutput.String())
	}
}

func TestPresentFullscreenExitReportsFatalAndSessionID(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	fatalErr := errors.New("renderer failed")
	err := presentFullscreenExit(tui.AppExitInfo{
		ThreadID: protocol.ThreadID("thread-1"), ExitReason: tui.ExitReasonFatal, Error: fatalErr,
	}, &output, &errorOutput, false)
	if !errors.Is(err, fatalErr) || !errorAlreadyReported(err) || exitCode(err) != exitCodeFailure {
		t.Fatalf("error = %v", err)
	}
	if output.String() != "Session ID: thread-1\n" || !strings.Contains(errorOutput.String(), "ERROR: renderer failed") {
		t.Fatalf("output=%q errorOutput=%q", output.String(), errorOutput.String())
	}
}

func TestPresentFullscreenExitHighlightsOnlyResumeCommand(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	err := presentFullscreenExit(tui.AppExitInfo{
		ResumeHint: "amadeus --resume thread-1",
		ExitReason: tui.ExitReasonUserRequested,
	}, &output, &errorOutput, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "To continue this session, run \x1b[36mamadeus --resume thread-1\x1b[39m\n"
	if output.String() != want || errorOutput.Len() != 0 {
		t.Fatalf("output=%q errorOutput=%q", output.String(), errorOutput.String())
	}
}
