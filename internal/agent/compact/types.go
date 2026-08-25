package compact

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type ErrorKind string

const (
	ErrorNoHistory     ErrorKind = "compaction_no_history"
	ErrorStale         ErrorKind = "compaction_stale"
	ErrorInvalidOutput ErrorKind = "compaction_invalid_output"
	ErrorInsufficient  ErrorKind = "compaction_insufficient"
	ErrorPersistence   ErrorKind = "compaction_persistence_failed"
)

type Error struct {
	Kind ErrorKind
	Err  error
}

func (err *Error) Error() string {
	if err == nil || err.Err == nil {
		return string(err.Kind)
	}
	return fmt.Sprintf("%s: %v", err.Kind, err.Err)
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

type Source struct {
	HistoryVersion         uint64
	CoveredThroughSequence int64
	SourceHash             string
	CanonicalHistory       []llm.ResponseItem
	UserMessages           []llm.ResponseItem
	PromptItems            []llm.ResponseItem
}

func (source Source) Validate() error {
	if source.HistoryVersion == 0 || source.CoveredThroughSequence <= 0 || strings.TrimSpace(source.SourceHash) == "" {
		return &Error{Kind: ErrorNoHistory, Err: errors.New("compaction source identity is incomplete")}
	}
	if len(source.CanonicalHistory) == 0 || len(source.UserMessages) == 0 || len(source.PromptItems) == 0 {
		return &Error{Kind: ErrorNoHistory, Err: errors.New("conversation has no model-visible history")}
	}
	return nil
}

type Request struct {
	Trigger      protocol.CompactionTrigger
	Reason       protocol.CompactionReason
	Phase        protocol.CompactionPhase
	Source       Source
	Prompt       llm.Prompt
	Model        llm.ModelInfo
	ModelSession *engine.ModelClientSession
	Reasoning    *llm.ReasoningConfig
	Metadata     llm.RequestMetadata
	Events       protocol.EventSink
	Estimator    agentcontext.Estimator
}

func (request Request) Validate() error {
	if !request.Trigger.Valid() || !request.Reason.Valid() || !request.Phase.Valid() {
		return errors.New("compaction lifecycle is invalid")
	}
	if err := request.Source.Validate(); err != nil {
		return err
	}
	if request.ModelSession == nil || request.Events == nil {
		return errors.New("compaction model runtime is incomplete")
	}
	if strings.TrimSpace(request.Prompt.BaseInstructions.Text) == "" {
		return errors.New("compaction base instructions are empty")
	}
	return request.Metadata.Validate()
}

type Output struct {
	Message            llm.ResponseItem
	FinishReason       llm.FinishReason
	ReplacementHistory []llm.ResponseItem
	TokenUsage         llm.TokenUsage
}

func (output Output) Validate() error {
	if output.FinishReason != llm.FinishReasonStop {
		return &Error{Kind: ErrorInvalidOutput, Err: fmt.Errorf("compaction finish reason is %q", output.FinishReason)}
	}
	if strings.TrimSpace(output.Message.Content) == "" || len(output.Message.ToolCalls) > 0 {
		return &Error{Kind: ErrorInvalidOutput, Err: errors.New("compaction model returned no usable summary")}
	}
	if len(output.ReplacementHistory) == 0 {
		return &Error{Kind: ErrorInvalidOutput, Err: errors.New("compaction replacement history is empty")}
	}
	return nil
}
