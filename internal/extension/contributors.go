package extension

import (
	"context"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ThreadIdleCause string

const (
	ThreadIdleCompleted   ThreadIdleCause = "completed"
	ThreadIdleInterrupted ThreadIdleCause = "interrupted"
	ThreadIdleFailed      ThreadIdleCause = "failed"
)

type ThreadStartInput struct {
	Config                         config.Config
	SessionSource                  protocol.SessionSource
	PersistentThreadStateAvailable bool
	SessionData                    *Data
	ThreadData                     *Data
}

type ThreadReadyInput struct {
	Config        config.Config
	SessionSource protocol.SessionSource
	SessionData   *Data
	ThreadData    *Data
}

type ThreadResumeInput struct{ SessionData, ThreadData *Data }

type ThreadIdleInput struct {
	Cause                   ThreadIdleCause
	SessionData, ThreadData *Data
}

type ThreadStopInput struct{ SessionData, ThreadData *Data }

type ThreadLifecycleContributor interface {
	OnThreadStart(context.Context, ThreadStartInput) error
	OnThreadReady(context.Context, ThreadReadyInput) error
	OnThreadResume(context.Context, ThreadResumeInput) error
	OnThreadIdle(context.Context, ThreadIdleInput) error
	OnThreadStop(context.Context, ThreadStopInput) error
}

type ThreadLifecycleDefaults struct{}

func (ThreadLifecycleDefaults) OnThreadStart(context.Context, ThreadStartInput) error   { return nil }
func (ThreadLifecycleDefaults) OnThreadReady(context.Context, ThreadReadyInput) error   { return nil }
func (ThreadLifecycleDefaults) OnThreadResume(context.Context, ThreadResumeInput) error { return nil }
func (ThreadLifecycleDefaults) OnThreadIdle(context.Context, ThreadIdleInput) error     { return nil }
func (ThreadLifecycleDefaults) OnThreadStop(context.Context, ThreadStopInput) error     { return nil }

type TurnStartInput struct {
	TurnID                            protocol.TurnID
	Mode                              protocol.ModeKind
	TokenUsageAtStart                 llm.TokenUsage
	SessionData, ThreadData, TurnData *Data
}

type TurnStopInput struct{ SessionData, ThreadData, TurnData *Data }

type TurnAbortInput struct {
	Reason                            string
	SessionData, ThreadData, TurnData *Data
}

type TurnErrorInput struct {
	TurnID                            protocol.TurnID
	Err                               error
	SessionData, ThreadData, TurnData *Data
}

type TurnLifecycleContributor interface {
	OnTurnStart(context.Context, TurnStartInput) error
	OnTurnStop(context.Context, TurnStopInput) error
	OnTurnAbort(context.Context, TurnAbortInput) error
	OnTurnError(context.Context, TurnErrorInput) error
}

type TurnLifecycleDefaults struct{}

func (TurnLifecycleDefaults) OnTurnStart(context.Context, TurnStartInput) error { return nil }
func (TurnLifecycleDefaults) OnTurnStop(context.Context, TurnStopInput) error   { return nil }
func (TurnLifecycleDefaults) OnTurnAbort(context.Context, TurnAbortInput) error { return nil }
func (TurnLifecycleDefaults) OnTurnError(context.Context, TurnErrorInput) error { return nil }

type ConfigContributor interface {
	OnConfigChanged(context.Context, *Data, *Data, config.Config, config.Config) error
}

type TokenUsageInput struct {
	SessionData, ThreadData, TurnData *Data
	Usage                             protocol.TokenUsageInfo
}

type TokenUsageContributor interface {
	OnTokenUsage(context.Context, TokenUsageInput) error
}

type ToolCallOutcome string

const (
	ToolCallCompleted       ToolCallOutcome = "completed"
	ToolCallBlocked         ToolCallOutcome = "blocked"
	ToolCallFailedBeforeRun ToolCallOutcome = "failed_before_run"
	ToolCallFailedAfterRun  ToolCallOutcome = "failed_after_run"
	ToolCallAborted         ToolCallOutcome = "aborted"
)

type ToolFinishInput struct {
	TurnID                            protocol.TurnID
	CallID                            string
	ToolName                          string
	Outcome                           ToolCallOutcome
	SessionData, ThreadData, TurnData *Data
}

type ToolLifecycleContributor interface {
	OnToolFinish(context.Context, ToolFinishInput) error
}

type ToolContributor interface {
	Tools(sessionData, threadData, stepData *Data) []tool.ToolDefinition
}
