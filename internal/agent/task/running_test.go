package task

import (
	"context"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type testHost struct{}

func (testHost) AppendItems(context.Context, turn.ID, ...rollout.Item) error { return nil }
func (testHost) History() []rollout.Line                                     { return nil }

func TestRunningTaskReportsPanicAsCompletion(t *testing.T) {
	turnContext := &turn.Context{
		ThreadID: "thread-1", TurnID: "turn-1", Provider: "openai", Model: "gpt-test", CWD: "/workspace",
		InitialPermissionMode: turn.PermissionModeDefault,
	}
	running, err := NewRunningTask(context.Background(), testHost{}, FuncTask{
		TaskKind: KindRegular,
		RunFunc: func(context.Context, Host, *turn.Context, []Input) (Result, error) {
			panic("boom")
		},
	}, turnContext, nil)
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
