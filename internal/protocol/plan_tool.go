package protocol

import (
	"errors"
	"fmt"
	"strings"
)

type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepCompleted  StepStatus = "completed"
)

func (status StepStatus) Valid() bool {
	return status == StepPending || status == StepInProgress || status == StepCompleted
}

type PlanItemArg struct {
	Step   string     `json:"step"`
	Status StepStatus `json:"status"`
}

type UpdatePlanArgs struct {
	Explanation string        `json:"explanation,omitempty"`
	Plan        []PlanItemArg `json:"plan"`
}

func (args UpdatePlanArgs) Validate() error {
	inProgress := 0
	for index, item := range args.Plan {
		if strings.TrimSpace(item.Step) == "" {
			return fmt.Errorf("plan item %d step is empty", index)
		}
		if !item.Status.Valid() {
			return fmt.Errorf("plan item %d status %q is invalid", index, item.Status)
		}
		if item.Status == StepInProgress {
			inProgress++
		}
	}
	if inProgress > 1 {
		return errors.New("plan contains more than one in_progress item")
	}
	return nil
}

type PlanUpdateEvent struct {
	ThreadID ThreadID `json:"thread_id,omitempty"`
	TurnID   TurnID   `json:"turn_id,omitempty"`
	UpdatePlanArgs
}

func (PlanUpdateEvent) isEventMsg() {}
