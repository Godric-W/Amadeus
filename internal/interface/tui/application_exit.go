package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
)

type ExitMode uint8

const (
	ExitModeShutdownFirst ExitMode = iota
	ExitModeImmediate
)

type ExitReason uint8

const (
	ExitReasonUserRequested ExitReason = iota
	ExitReasonFatal
)

type AppExitInfo struct {
	TokenUsage llm.Usage
	ThreadID   protocol.ThreadID
	ThreadName string
	ResumeHint string
	ExitReason ExitReason
	Error      error
}

type fullscreenExitPhase uint8

const (
	fullscreenExitPhaseRunning fullscreenExitPhase = iota
	fullscreenExitPhaseShuttingDown
	fullscreenExitPhaseDrainingFrame
	fullscreenExitPhaseFinished
)

const (
	fullscreenShutdownOperationTimeout = 2 * time.Second
	fullscreenShutdownEscapeTimeout    = 3 * time.Second
)

var errFullscreenShutdownTimedOut = errors.New("shutdown timed out; some resources may still be cleaning up")

type fullscreenExitState struct {
	phase  fullscreenExitPhase
	mode   ExitMode
	reason ExitReason
	err    error
	target fullscreenExitTarget
}

type fullscreenExitTarget struct {
	generation uint64
	threadID   protocol.ThreadID
	threadName string
	tokenUsage llm.Usage
	resumable  bool
}

type fullscreenShutdownFinishedMsg struct{ err error }
type fullscreenShutdownTimeoutMsg struct{}
type fullscreenExitFrameDrainedMsg struct{}

func (state fullscreenExitState) active() bool {
	return state.phase != fullscreenExitPhaseRunning
}

func (state fullscreenExitState) shuttingDown() bool {
	return state.phase == fullscreenExitPhaseShuttingDown
}

func (state fullscreenExitState) drainingFrame() bool {
	return state.phase == fullscreenExitPhaseDrainingFrame || state.phase == fullscreenExitPhaseFinished
}

func (model *fullscreenModel) requestExit(mode ExitMode, reason ExitReason, exitErr error) tea.Cmd {
	if model == nil || model.exit.active() {
		return nil
	}
	model.closeInteractiveSurfacesForExit()
	model.exit = fullscreenExitState{
		phase: fullscreenExitPhaseShuttingDown, mode: mode, reason: reason, err: exitErr,
		target: fullscreenExitTarget{
			generation: model.session.Generation,
			threadID:   model.session.ThreadID,
			threadName: strings.TrimSpace(model.session.Title),
			tokenUsage: model.session.Usage,
			resumable:  !model.session.ThreadID.IsZero(),
		},
	}
	if mode == ExitModeImmediate {
		return model.beginExitFrameDrain(reason, exitErr)
	}
	return tea.Batch(model.shutdownApplication(), fullscreenShutdownTimeout())
}

func (model *fullscreenModel) completeShutdown(exitErr error) tea.Cmd {
	if model == nil || !model.exit.shuttingDown() {
		return nil
	}
	reason := model.exit.reason
	if exitErr != nil && !isShutdownTimeoutError(exitErr) {
		reason = ExitReasonFatal
	}
	return model.beginExitFrameDrain(reason, exitErr)
}

func isShutdownTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isShutdownTimeoutError(child) {
				return false
			}
		}
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return isShutdownTimeoutError(wrapped)
	}
	return err == context.DeadlineExceeded || err == context.Canceled
}

func (model *fullscreenModel) expireShutdown() tea.Cmd {
	if model == nil || !model.exit.shuttingDown() {
		return nil
	}
	return model.beginExitFrameDrain(ExitReasonUserRequested, errFullscreenShutdownTimedOut)
}

func (model *fullscreenModel) beginExitFrameDrain(reason ExitReason, exitErr error) tea.Cmd {
	model.closeInteractiveSurfacesForExit()
	model.exit.phase = fullscreenExitPhaseDrainingFrame
	model.exit.reason = reason
	model.exit.err = exitErr
	return func() tea.Msg { return fullscreenExitFrameDrainedMsg{} }
}

func (model *fullscreenModel) finishExitFrameDrain() tea.Cmd {
	if model == nil || model.exit.phase != fullscreenExitPhaseDrainingFrame {
		return nil
	}
	model.exit.phase = fullscreenExitPhaseFinished
	return tea.Quit
}

func (model *fullscreenModel) closeInteractiveSurfacesForExit() {
	model.selection = nil
	model.selectionKind = ""
	model.slashPopup = slashCommandPopup{}
	model.approval = nil
	model.approvalDialog = nil
	model.userInputRequest = nil
	model.userInputDialog = nil
	model.viewingDetails = false
	model.input.Blur()
}

func (model fullscreenModel) shutdownApplication() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(model.ctx), fullscreenShutdownOperationTimeout)
		defer cancel()
		return fullscreenShutdownFinishedMsg{err: model.app.options.Application.Shutdown(ctx)}
	}
}

func fullscreenShutdownTimeout() tea.Cmd {
	return tea.Tick(fullscreenShutdownEscapeTimeout, func(time.Time) tea.Msg {
		return fullscreenShutdownTimeoutMsg{}
	})
}

func (model *fullscreenModel) captureExitAppEvent(event application.InteractiveEvent) {
	switch event := event.(type) {
	case application.SessionEventObserved:
		if event.Generation != model.exit.target.generation || protocol.ThreadIDOf(event.Event.Msg) != model.exit.target.threadID {
			return
		}
		if usage, ok := event.Event.Msg.(protocol.TokenCountEvent); ok {
			model.session.applyTokenCount(usage)
			model.exit.target.tokenUsage = usage.Usage
		}
	case application.ThreadNameUpdated:
		if event.Generation == model.exit.target.generation && event.ThreadID == model.exit.target.threadID {
			model.session.Title = event.Name
			model.exit.target.threadName = strings.TrimSpace(event.Name)
		}
	}
}

func (model fullscreenModel) shutdownView() string {
	return model.palette.dim().Render("Shutting down…")
}

func (model fullscreenModel) appExitInfo() AppExitInfo {
	target := model.exit.target
	if !model.exit.active() {
		target = fullscreenExitTarget{
			generation: model.session.Generation,
			threadID:   model.session.ThreadID,
			threadName: strings.TrimSpace(model.session.Title),
			tokenUsage: model.session.Usage,
			resumable:  !model.session.ThreadID.IsZero(),
		}
	}
	info := AppExitInfo{
		TokenUsage: target.tokenUsage,
		ThreadID:   target.threadID,
		ThreadName: target.threadName,
		ExitReason: model.exit.reason,
		Error:      model.exit.err,
	}
	if target.resumable && !info.ThreadID.IsZero() {
		info.ResumeHint = fmt.Sprintf("amadeus --resume %s", info.ThreadID)
	}
	if !model.exit.active() {
		info.ExitReason = ExitReasonFatal
		info.Error = errors.New("fullscreen TUI exited without completing the application exit lifecycle")
	}
	return info
}
