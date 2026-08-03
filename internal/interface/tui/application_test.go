package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/policy"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFullscreenTextareaPreservesChineseInput(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("请帮我检查中文输入")})
	model = updated.(fullscreenModel)
	if got := model.input.Value(); got != "请帮我检查中文输入" {
		t.Fatalf("Chinese input was corrupted: got %q", got)
	}
	if !strings.Contains(model.inputBox(), "请帮我检查中文输入") {
		t.Fatalf("rendered input omitted Chinese text: %q", model.inputBox())
	}
}

func TestFullscreenTextareaBackspaceRemovesChineseRune(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("你好")})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(fullscreenModel)
	if got := model.input.Value(); got != "你" {
		t.Fatalf("backspace did not remove one Chinese rune: got %q", got)
	}
}

func TestFullscreenTextareaIgnoresMouseControlResponses(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.input.SetValue("保留")
	model.input.CursorEnd()
	updated, _ := model.Update(tea.MouseMsg(tea.MouseEvent{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 10, Y: 10}))
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("<64;80;20M")})
	model = updated.(fullscreenModel)
	if got := model.input.Value(); got != "保留" {
		t.Fatalf("mouse control response polluted input: got %q", got)
	}
}

func TestFullscreenBannerUsesPixelLogoAndDraftSession(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Startup: FullscreenStartup{Version: "dev", Provider: "openai", Model: "test-model"},
		Task:    func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	banner := model.banner()
	for _, expected := range []string{"███", "Amadeus", "draft session"} {
		if !strings.Contains(banner, expected) {
			t.Fatalf("banner omitted %q: %q", expected, banner)
		}
	}
}

func TestFullscreenAcceptsAndQueuesInputWhileRunning(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	model.status = "executing"
	for _, character := range []rune("下一条任务") {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
		model = updated.(fullscreenModel)
	}
	if got := model.input.Value(); got != "下一条任务" {
		t.Fatalf("running input was not editable: %q", got)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if len(model.queuedTasks) != 1 || model.queuedTasks[0] != "下一条任务" || model.input.Value() != "" {
		t.Fatalf("running input was not queued: %#v", model.queuedTasks)
	}
	if !strings.Contains(model.inputBox(), "Enter 排队下一条任务") {
		t.Fatalf("running input box did not expose queue behavior: %q", model.inputBox())
	}
}

func TestFullscreenPlanCommandStartsPlannedTask(t *testing.T) {
	var task string
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(_ context.Context, value string) error { task = value; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.input.SetValue("/plan 修改多个模块")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if !model.running || model.status != "planning" || command == nil {
		t.Fatalf("plan task was not started: %#v", model)
	}
	message := model.runTask("/plan 修改多个模块")()
	if _, ok := message.(fullscreenTaskDoneMsg); !ok || task != "/plan 修改多个模块" {
		t.Fatalf("unexpected plan task dispatch: message=%T task=%q", message, task)
	}
}

func TestFullscreenViewKeepsCommittedHistoryOutOfActiveRegion(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	view := model.View()
	if strings.Contains(view, "你好，我是 Amadeus") || strings.Contains(view, "Coding Agent") {
		t.Fatalf("active region repeated committed terminal history: %q", view)
	}
	for _, expected := range []string{"输入任务", "AMADEUS", "idle"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("active region omitted %q: %q", expected, view)
		}
	}
}

func TestFullscreenFlushCommitsEntriesOnce(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.entries = append(model.entries, fullscreenEntry{kind: "user", content: "检查项目"})
	if command := model.flushTranscript(); command == nil {
		t.Fatal("new transcript entry did not produce a terminal history command")
	}
	if model.committed != len(model.entries) {
		t.Fatalf("committed index = %d, want %d", model.committed, len(model.entries))
	}
	if command := model.flushTranscript(); command != nil {
		t.Fatal("already committed transcript was emitted twice")
	}
}

func TestFullscreenRunUsesTerminalMainScreen(t *testing.T) {
	input := bytes.NewBufferString("\x04")
	var output bytes.Buffer
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: input, Output: &output,
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("run inline TUI: %v", err)
	}
	rendered := output.String()
	for _, sequence := range []string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[?47h", "\x1b[?47l"} {
		if strings.Contains(rendered, sequence) {
			t.Fatalf("inline TUI emitted alternate-screen sequence %q: %q", sequence, rendered)
		}
	}
	if !strings.Contains(rendered, "Amadeus") || !strings.Contains(rendered, "你好，我是") {
		t.Fatalf("inline TUI did not commit startup history: %q", rendered)
	}
}

func TestFullscreenActiveDraftShowsOnlyVisibleTail(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.height = 8
	model.draft = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight"
	draft := model.renderActiveDraft()
	if strings.Contains(draft, "one") || !strings.Contains(draft, "eight") {
		t.Fatalf("active draft did not retain its visible tail: %q", draft)
	}
}

func TestFullscreenModelRendersAgentEvents(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	for _, item := range []event.Event{
		event.PlanUpdated{Cycle: 1, Tasks: []event.PlanTask{{ID: "task-1", Objective: "检查项目", Status: "running"}}},
		event.ToolCallStarted{ToolName: "read_file"},
		event.ToolCallCompleted{ToolName: "read_file", Success: true, Summary: "读取成功"},
		event.TextDelta{Delta: "任务完成。"},
	} {
		updated, _ := model.Update(fullscreenEventMsg{item: item})
		model = updated.(fullscreenModel)
	}
	model.finishDraft()
	content := model.transcriptContent()
	for _, expected := range []string{"Plan", "检查项目", "read_file", "读取成功", "任务完成"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("transcript omitted %q: %s", expected, content)
		}
	}
}

func TestFullscreenApprovalUsesKeyboardDecision(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	response := make(chan fullscreenApprovalResult, 1)
	request := policy.ApprovalRequest{ID: "approval-1", ToolName: "write_file", ArgumentsSHA256: strings.Repeat("a", 64), Risk: policy.CommandRiskHigh, Reason: "writes a file"}
	updated, _ := model.Update(fullscreenApprovalMsg{prompt: &fullscreenApproval{request: request, response: response}})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = updated.(fullscreenModel)
	decision := (<-response).decision
	if decision.Outcome != policy.ApprovalAllow || decision.Scope != policy.ApprovalSession {
		t.Fatalf("unexpected approval decision: %#v", decision)
	}
	if model.approval != nil {
		t.Fatal("approval prompt remained active")
	}
}

func TestFullscreenModelCanExitFromCommand(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.input.SetValue("/exit")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("exit command did not return a quit command")
	}
	message := command()
	if _, ok := message.(tea.QuitMsg); !ok {
		t.Fatalf("exit command returned %T, want tea.QuitMsg", message)
	}
}
