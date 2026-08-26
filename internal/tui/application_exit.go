package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
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
	TokenUsage llm.TokenUsage
	ThreadID   protocol.ThreadID
	ThreadName string
	ResumeHint string
	ExitReason ExitReason
	Error      error
}

type exitPhase uint8

const (
	exitPhaseRunning exitPhase = iota
	exitPhaseShuttingDown
	exitPhaseDrainingFrame
	exitPhaseFinished
)

const (
	shutdownOperationTimeout = 2 * time.Second
	shutdownEscapeTimeout    = 3 * time.Second
)

var errShutdownTimedOut = errors.New("shutdown timed out; some resources may still be cleaning up")

type exitState struct {
	phase  exitPhase
	mode   ExitMode
	reason ExitReason
	err    error
	target exitTarget
}

type exitTarget struct {
	generation uint64
	threadID   protocol.ThreadID
	threadName string
	tokenUsage llm.TokenUsage
	resumable  bool
}

type shutdownFinishedMsg struct{ err error }
type shutdownTimeoutMsg struct{}
type exitFrameDrainedMsg struct{}

func (state exitState) active() bool {
	return state.phase != exitPhaseRunning
}

func (state exitState) shuttingDown() bool {
	return state.phase == exitPhaseShuttingDown
}

func (state exitState) drainingFrame() bool {
	return state.phase == exitPhaseDrainingFrame || state.phase == exitPhaseFinished
}

func (model *appModel) requestExit(mode ExitMode, reason ExitReason, exitErr error) tea.Cmd {
	if model == nil || model.exit.active() {
		return nil
	}
	model.closeInteractiveSurfacesForExit()
	model.exit = exitState{
		phase: exitPhaseShuttingDown, mode: mode, reason: reason, err: exitErr,
		target: exitTarget{
			generation: model.session.Generation,
			threadID:   model.session.ThreadID,
			threadName: strings.TrimSpace(model.session.Title),
			tokenUsage: model.session.totalTokenUsage(),
			resumable:  !model.session.ThreadID.IsZero(),
		},
	}
	if mode == ExitModeImmediate {
		return model.beginExitFrameDrain(reason, exitErr)
	}
	return tea.Batch(model.shutdownApplication(), shutdownTimeout())
}

func (model *appModel) completeShutdown(exitErr error) tea.Cmd {
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

func (model *appModel) expireShutdown() tea.Cmd {
	if model == nil || !model.exit.shuttingDown() {
		return nil
	}
	return model.beginExitFrameDrain(ExitReasonUserRequested, errShutdownTimedOut)
}

func (model *appModel) beginExitFrameDrain(reason ExitReason, exitErr error) tea.Cmd {
	model.closeInteractiveSurfacesForExit()
	model.exit.phase = exitPhaseDrainingFrame
	model.exit.reason = reason
	model.exit.err = exitErr
	return func() tea.Msg { return exitFrameDrainedMsg{} }
}

func (model *appModel) finishExitFrameDrain() tea.Cmd {
	if model == nil || model.exit.phase != exitPhaseDrainingFrame {
		return nil
	}
	model.exit.phase = exitPhaseFinished
	return tea.Quit
}

func (model *appModel) closeInteractiveSurfacesForExit() {
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

func (model appModel) shutdownApplication() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(model.ctx), shutdownOperationTimeout)
		defer cancel()
		return shutdownFinishedMsg{err: model.app.options.Application.Shutdown(ctx)}
	}
}

func shutdownTimeout() tea.Cmd {
	return tea.Tick(shutdownEscapeTimeout, func(time.Time) tea.Msg {
		return shutdownTimeoutMsg{}
	})
}

func (model *appModel) captureExitAppEvent(event application.InteractiveEvent) {
	switch event := event.(type) {
	case application.SessionEventObserved:
		if event.Generation != model.exit.target.generation || protocol.ThreadIDOf(event.Event.Msg) != model.exit.target.threadID {
			return
		}
		if usage, ok := event.Event.Msg.(protocol.TokenCountEvent); ok {
			model.session.applyTokenCount(usage)
			model.exit.target.tokenUsage = model.session.totalTokenUsage()
		}
	case application.ThreadNameUpdated:
		if event.Generation == model.exit.target.generation && event.ThreadID == model.exit.target.threadID {
			model.session.Title = event.Name
			model.exit.target.threadName = strings.TrimSpace(event.Name)
		}
	}
}

func (model appModel) shutdownView() string {
	return model.palette.dim().Render("Shutting down…")
}

func (model appModel) appExitInfo() AppExitInfo {
	target := model.exit.target
	if !model.exit.active() {
		target = exitTarget{
			generation: model.session.Generation,
			threadID:   model.session.ThreadID,
			threadName: strings.TrimSpace(model.session.Title),
			tokenUsage: model.session.totalTokenUsage(),
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
		info.Error = errors.New("TUI exited without completing the application exit lifecycle")
	}
	return info
}
