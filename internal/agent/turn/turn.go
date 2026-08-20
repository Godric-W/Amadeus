package turn

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type ModeKind = protocol.ModeKind

const (
	ModeKindDefault = protocol.ModeKindDefault
	ModeKindPlan    = protocol.ModeKindPlan
)

type Personality string

type TurnContext struct {
	SubmissionID protocol.SubmissionID `json:"-"`
	ThreadID     protocol.ThreadID     `json:"thread_id"`
	TurnID       protocol.TurnID       `json:"turn_id"`
	Provider     string                `json:"provider"`
	Model        string                `json:"model"`
	CWD          string                `json:"cwd"`
	Shell        string                `json:"shell,omitempty"`

	CurrentDate string `json:"current_date,omitempty"`
	Timezone    string `json:"timezone,omitempty"`

	Mode               ModeKind        `json:"mode"`
	Personality        Personality     `json:"personality,omitempty"`
	OutputSchema       json.RawMessage `json:"output_schema,omitempty"`
	OutputSchemaStrict bool            `json:"output_schema_strict,omitempty"`
}

func (value TurnContext) Validate() error {
	if value.ThreadID == "" || value.TurnID == "" || strings.TrimSpace(value.Provider) == "" || strings.TrimSpace(value.Model) == "" || strings.TrimSpace(value.CWD) == "" {
		return errors.New("turn context is incomplete")
	}
	if value.Mode != ModeKindDefault && value.Mode != ModeKindPlan {
		return errors.New("turn mode is invalid")
	}
	if len(value.OutputSchema) > 0 && !json.Valid(value.OutputSchema) {
		return errors.New("turn output schema is invalid")
	}
	return nil
}
