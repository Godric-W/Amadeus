package turn

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type ModeKind = protocol.ModeKind

const (
	ModeKindDefault = protocol.ModeKindDefault
	ModeKindPlan    = protocol.ModeKindPlan
)

type Personality string

type TurnContext struct {
	SubmissionID    protocol.SubmissionID `json:"-"`
	SessionID       protocol.SessionID    `json:"session_id"`
	ThreadID        protocol.ThreadID     `json:"thread_id"`
	ParentThreadID  *protocol.ThreadID    `json:"-"`
	TurnID          protocol.TurnID       `json:"turn_id"`
	Provider        string                `json:"provider"`
	Model           string                `json:"model"`
	ReasoningEffort *llm.ReasoningEffort  `json:"reasoning_effort,omitempty"`
	CWD             string                `json:"cwd"`
	Shell           string                `json:"shell,omitempty"`

	CurrentDate string `json:"current_date,omitempty"`
	Timezone    string `json:"timezone,omitempty"`

	Mode               ModeKind        `json:"mode"`
	Personality        Personality     `json:"personality,omitempty"`
	OutputSchema       json.RawMessage `json:"output_schema,omitempty"`
	OutputSchemaStrict bool            `json:"output_schema_strict,omitempty"`
}

func (value TurnContext) Validate() error {
	if value.SessionID.IsZero() || value.ThreadID.IsZero() || value.TurnID == "" || strings.TrimSpace(value.Provider) == "" || strings.TrimSpace(value.Model) == "" || strings.TrimSpace(value.CWD) == "" {
		return errors.New("turn context is incomplete")
	}
	if value.ParentThreadID != nil && (value.ParentThreadID.IsZero() || *value.ParentThreadID == value.ThreadID) {
		return errors.New("turn parent thread identity is invalid")
	}
	if value.Mode != ModeKindDefault && value.Mode != ModeKindPlan {
		return errors.New("turn mode is invalid")
	}
	if value.ReasoningEffort != nil && !value.ReasoningEffort.Valid() {
		return errors.New("turn reasoning effort is invalid")
	}
	if len(value.OutputSchema) > 0 && !json.Valid(value.OutputSchema) {
		return errors.New("turn output schema is invalid")
	}
	return nil
}
