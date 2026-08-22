package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type panicSessionTask struct{}

func (panicSessionTask) Kind() TaskKind { return TaskKindRegular }

func (panicSessionTask) Run(context.Context, *Session, *turn.TurnContext) (TaskOutput, error) {
	panic("boom")
}

func TestRunningTaskReportsPanicAsCompletion(t *testing.T) {
	turnContext := &turn.TurnContext{
		SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: "turn-1", Provider: "openai", Model: "gpt-test", CWD: "/workspace",
		Mode: turn.ModeKindDefault,
	}
	running, err := NewRunningTask(context.Background(), &Session{}, panicSessionTask{}, turnContext)
	if err != nil {
		t.Fatal(err)
	}
	completion, ok := <-running.Start()
	if !ok {
		t.Fatal("completion channel closed without a result")
	}
	if completion.Error == nil || !strings.Contains(completion.Error.Error(), "boom") {
		t.Fatalf("completion error = %v", completion.Error)
	}
	if _, ok := <-running.Start(); ok {
		t.Fatal("completion channel produced more than one result")
	}
}
