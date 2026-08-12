package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestInlineRendererKeepsTextAndStatusBlocksSeparate(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := renderer.Publish(ctx, event.TextDelta{Delta: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Publish(ctx, event.ToolCallStarted{ToolName: "read_file"}); err != nil {
		t.Fatal(err)
	}
	if text.String() != "hello\n" || !strings.Contains(status.String(), "tool started: read_file") || !strings.Contains(status.String(), "status: phase=executing") {
		t.Fatalf("unexpected inline output: text=%q status=%q", text.String(), status.String())
	}
}

func TestInlineRendererRendersPlanApprovalAndSafeToolSummary(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	events := []event.Event{
		event.PlanUpdated{TurnID: "run-1", Revision: 1, Items: []event.PlanItem{{Step: "Read source", Status: "pending"}}},
		event.TurnStatusChanged{TurnID: "run-1", Entity: "run", EntityID: "run-1", From: "starting", To: "running"},
		event.ApprovalRequested{ToolName: "write_file", Risk: "high", Reason: "Authorization: Bearer should-not-leak"},
		event.ApprovalResolved{ToolName: "write_file", Outcome: "allow", Scope: "once", Source: "user"},
		event.ToolCallStarted{ToolName: "write_file"},
		event.ToolCallCompleted{ToolName: "write_file", Success: true, Summary: "token=should-not-leak"},
		event.UsageUpdated{Usage: llm.Usage{InputTokens: 3, OutputTokens: 5}},
		event.TurnCompleted{Status: "completed", Reason: "done"},
	}
	for _, runtimeEvent := range events {
		if err := renderer.Publish(ctx, runtimeEvent); err != nil {
			t.Fatalf("publish %T: %v", runtimeEvent, err)
		}
	}
	output := status.String()
	for _, fragment := range []string{"plan 1:", "plan-1 [pending]: Read source", "approval required: write_file", "tool completed: write_file", "usage: input=3 output=5 total=8", "status: phase=idle"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("inline transcript omitted %q: %s", fragment, output)
		}
	}
	if strings.Contains(output, "should-not-leak") {
		t.Fatalf("inline transcript leaked sensitive event content: %s", output)
	}
}

func TestInlineRendererSuppressesGenericUpdatePlanToolStatus(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, runtimeEvent := range []event.Event{
		event.ToolCallStarted{CallID: "plan-1", ToolName: "update_plan"},
		event.PlanUpdated{Revision: 1, Items: []event.PlanItem{{Step: "Inspect", Status: "in_progress"}}},
		event.ToolCallCompleted{CallID: "plan-1", ToolName: "update_plan", Success: true},
	} {
		if err := renderer.Publish(ctx, runtimeEvent); err != nil {
			t.Fatalf("publish %T: %v", runtimeEvent, err)
		}
	}
	if output := status.String(); !strings.Contains(output, "plan 1:") || strings.Contains(output, "tool started: update_plan") || strings.Contains(output, "tool completed: update_plan") {
		t.Fatalf("unexpected inline update_plan output: %q", output)
	}
}

func TestInlineRendererKeepsDiffStateOutOfTranscript(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeEvent := range []event.Event{
		event.RunDiffUpdated{Revision: 1, Changes: []event.RunDiffChange{{Path: "/work/a.go", Kind: "updated"}}},
		event.RunDiffInvalidated{Revision: 2, Reason: "malformed Patch delta"},
	} {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("publish %T: %v", runtimeEvent, err)
		}
	}
	if text.Len() != 0 || status.Len() != 0 {
		t.Fatalf("Diff state leaked into inline transcript: text=%q status=%q", text.String(), status.String())
	}
}

func TestInlineRendererGoldenTranscript(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, runtimeEvent := range []event.Event{
		event.TurnStarted{TurnID: "run-1"},
		event.PlanUpdated{TurnID: "run-1", Revision: 1, Items: []event.PlanItem{{Step: "Read README", Status: "pending"}}},
		event.TurnStatusChanged{TurnID: "run-1", Entity: "run", EntityID: "run-1", From: "starting", To: "running"},
		event.TextDelta{LLMCallID: "turn-1", Delta: "answer"},
		event.ToolCallStarted{TurnID: "run-1", CallID: "read-1", ToolName: "read_file"},
		event.ToolCallCompleted{TurnID: "run-1", CallID: "read-1", ToolName: "read_file", Success: true, Summary: "README contents"},
		event.LLMCallCompleted{LLMCallID: "turn-1"},
		event.TurnCompleted{TurnID: "run-1", Status: "completed", Reason: "done"},
	} {
		if err := renderer.Publish(ctx, runtimeEvent); err != nil {
			t.Fatalf("publish %T: %v", runtimeEvent, err)
		}
	}
	if text.String() != "answer\n" {
		t.Fatalf("unexpected golden text: %q", text.String())
	}
	want := "" +
		"run started: run-1\n" +
		"status: phase=starting tools=0 usage=0/0\n" +
		"plan 1:\n" +
		"  plan-1 [pending]: Read README\n" +
		"status: phase=planning tools=0 usage=0/0\n" +
		"run run-1: starting -> running\n" +
		"status: phase=executing tools=0 usage=0/0\n" +
		"tool started: read_file\n" +
		"status: phase=executing tools=1 usage=0/0\n" +
		"tool completed: read_file in 0s: README contents\n" +
		"status: phase=executing tools=1 usage=0/0\n" +
		"run completed: done\n" +
		"status: phase=idle tools=1 usage=0/0\n"
	if status.String() != want {
		t.Fatalf("unexpected golden status:\n%s", status.String())
	}
}

func TestTerminalInteractionControllerDispatchesTasksAndCommands(t *testing.T) {
	var tasks, commands []string
	var status bytes.Buffer
	controller, err := NewTerminalInteractionController(strings.NewReader("hello\n/status\n"), &bytes.Buffer{}, &status, func(_ context.Context, command string) error {
		commands = append(commands, command)
		return nil
	}, func(_ context.Context, task TaskSubmission) error {
		tasks = append(tasks, task.Content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0] != "hello" || len(commands) != 1 || commands[0] != "/status" || status.String() != "amadeus> amadeus> amadeus> " {
		t.Fatalf("unexpected dispatch: tasks=%v commands=%v", tasks, commands)
	}
}

func TestTerminalInteractionControllerExitQuits(t *testing.T) {
	var commands []string
	controller, err := NewTerminalInteractionController(strings.NewReader("/exit\nignored\n"), &bytes.Buffer{}, &bytes.Buffer{}, func(_ context.Context, command string) error {
		commands = append(commands, command)
		return ErrQuit
	}, func(_ context.Context, task TaskSubmission) error {
		t.Fatalf("task executed after /exit: %#v", task)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.WithPrompt("")
	if err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0] != "/exit" {
		t.Fatalf("exit dispatch = %v", commands)
	}
}

func TestTerminalInteractionControllerCreatesAndCancelsTaskContext(t *testing.T) {
	var cancelled bool
	controller, err := NewTerminalInteractionController(strings.NewReader("task\n"), &bytes.Buffer{}, &bytes.Buffer{}, nil, func(ctx context.Context, task TaskSubmission) error {
		if task.Content != "task" || task.Mode != CollaborationExecute || ctx.Err() != nil {
			t.Fatalf("unexpected task context: task=%#v err=%v", task, ctx.Err())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.WithPrompt("").WithTaskContextFactory(func(parent context.Context) (context.Context, context.CancelFunc, error) {
		ctx, cancel := context.WithCancel(parent)
		return ctx, func() {
			cancelled = true
			cancel()
		}, nil
	})
	if err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("task context was not cancelled after task completion")
	}
}

func TestSlashPaletteProvidesOnlySupportedCommands(t *testing.T) {
	if matches := CompleteSlashCommand("/res"); len(matches) != 1 || matches[0] != "/resume" {
		t.Fatalf("unexpected slash completion: %v", matches)
	}
	if matches := CompleteSlashCommand("/pl"); len(matches) != 1 || matches[0] != "/plan" {
		t.Fatalf("plan command was not available: %v", matches)
	}
}

func TestTerminalInteractionControllerDispatchesPlanAsTask(t *testing.T) {
	var task string
	controller, err := NewTerminalInteractionController(
		strings.NewReader("/plan\ninspect repository\n"),
		&bytes.Buffer{}, &bytes.Buffer{},
		func(context.Context, string) error {
			t.Fatal("plan command was dispatched as a UI command")
			return nil
		},
		func(_ context.Context, value TaskSubmission) error {
			if value.Mode != CollaborationPlan {
				t.Fatalf("planned task mode = %q", value.Mode)
			}
			task = value.Content
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	controller.WithPrompt("")
	if err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if task != "inspect repository" {
		t.Fatalf("unexpected planned task: %q", task)
	}
}

func TestTerminalInteractionControllerNavigatesInMemoryHistory(t *testing.T) {
	controller, err := NewTerminalInteractionController(strings.NewReader("first\nsecond\n"), &bytes.Buffer{}, &bytes.Buffer{}, nil, func(context.Context, TaskSubmission) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	controller.WithPrompt("")
	if err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := controller.PreviousHistory(); got != "second" {
		t.Fatalf("previous history = %q", got)
	}
	if got := controller.PreviousHistory(); got != "first" {
		t.Fatalf("previous history = %q", got)
	}
	if got := controller.NextHistory(); got != "second" {
		t.Fatalf("next history = %q", got)
	}
	if got := controller.NextHistory(); got != "" {
		t.Fatalf("history should return input draft after newest entry, got %q", got)
	}
}

func TestTerminalInteractionControllerIgnoresIdleControlKeys(t *testing.T) {
	var tasks []string
	controller, err := NewTerminalInteractionController(strings.NewReader("\x03\n\x1b\n"), &bytes.Buffer{}, &bytes.Buffer{}, nil, func(_ context.Context, task TaskSubmission) error {
		tasks = append(tasks, task.Content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.WithPrompt("")
	if err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 || len(controller.History()) != 0 {
		t.Fatalf("idle control keys became tasks/history: tasks=%v history=%v", tasks, controller.History())
	}
}

func TestRawInputSupportsHistoryCompletionAndCancel(t *testing.T) {
	var tasks []string
	var status bytes.Buffer
	controller, err := NewTerminalInteractionController(strings.NewReader(""), &bytes.Buffer{}, &status, nil, func(_ context.Context, task TaskSubmission) error {
		tasks = append(tasks, task.Content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	controller.history = []string{"previous"}
	line, action, err := controller.readRawLine(context.Background(), strings.NewReader("/res\t\n"))
	if err != nil || action != rawActionSubmit || line != "/resume" {
		t.Fatalf("unexpected completion input: line=%q action=%d err=%v", line, action, err)
	}
	line, action, err = controller.readRawLine(context.Background(), strings.NewReader("\x1b[A\n"))
	if err != nil || action != rawActionSubmit || line != "previous" {
		t.Fatalf("unexpected history input: line=%q action=%d err=%v", line, action, err)
	}
	line, action, err = controller.readRawLine(context.Background(), strings.NewReader("draft\x03"))
	if err != nil || action != rawActionCancel || line != "" {
		t.Fatalf("unexpected cancel input: line=%q action=%d err=%v", line, action, err)
	}
	if len(tasks) != 0 {
		t.Fatalf("raw editor dispatched tasks directly: %v", tasks)
	}
}
