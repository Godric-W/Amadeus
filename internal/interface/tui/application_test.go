package tui

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

type delayedReader struct {
	reader io.Reader
	delay  time.Duration
	once   sync.Once
}

func (reader *delayedReader) Read(buffer []byte) (int, error) {
	reader.once.Do(func() { time.Sleep(reader.delay) })
	return reader.reader.Read(buffer)
}

func TestFullscreenTextareaPreservesChineseInput(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
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
		Task: func(context.Context, TaskSubmission) error { return nil },
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
		Task: func(context.Context, TaskSubmission) error { return nil },
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

func TestFullscreenBannerUsesPixelLogoAndRestrainedMetadata(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Startup: FullscreenStartup{Version: "dev", Provider: "openai", Model: "test-model"},
		Task:    func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	banner := model.banner()
	for _, expected := range []string{"⠸⠿", "Amadeus", "test-model"} {
		if !strings.Contains(banner, expected) {
			t.Fatalf("banner omitted %q: %q", expected, banner)
		}
	}
	for _, omitted := range []string{"provider:", "branch:", "session:"} {
		if strings.Contains(banner, omitted) {
			t.Fatalf("banner retained %q: %q", omitted, banner)
		}
	}
}

func TestFullscreenBannerKeepsRightTerminalMargin(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 100,
		Startup: FullscreenStartup{Version: "dev", Provider: "openai", Model: "test-model"},
		Task:    func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	for _, line := range strings.Split(model.banner(), "\n") {
		plain := xansi.Strip(line)
		if strings.HasPrefix(plain, "╭") {
			if got := lipgloss.Width(line); got != model.width-4 {
				t.Fatalf("panel width = %d, want %d columns with right margin", got, model.width-4)
			}
			return
		}
	}
	t.Fatal("banner panel border not found")
}

func TestFullscreenAcceptsAndQueuesInputWhileRunning(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
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
	if len(model.queuedTasks) != 1 || model.queuedTasks[0].Content != "下一条任务" || model.queuedTasks[0].Mode != CollaborationExecute || model.input.Value() != "" {
		t.Fatalf("running input was not queued: %#v", model.queuedTasks)
	}
	if !strings.Contains(model.inputBox(), "Enter 排队下一条任务") {
		t.Fatalf("running input box did not expose queue behavior: %q", model.inputBox())
	}
}

func TestFullscreenPlanCommandSwitchesNextTaskMode(t *testing.T) {
	var task TaskSubmission
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(_ context.Context, value TaskSubmission) error { task = value; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.input.SetValue("/plan")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if model.running || model.collaboration != CollaborationPlan || command == nil {
		t.Fatalf("plan mode was not selected: %#v", model)
	}
	message := model.runTask(TaskSubmission{Content: "修改多个模块", Mode: CollaborationPlan})()
	if _, ok := message.(fullscreenTaskDoneMsg); !ok || task.Content != "修改多个模块" || task.Mode != CollaborationPlan {
		t.Fatalf("unexpected plan task dispatch: message=%T task=%#v", message, task)
	}
}

func TestFullscreenShiftTabCyclesCollaborationMode(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationPlan {
		t.Fatalf("Shift+Tab mode = %q", model.collaboration)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationExecute {
		t.Fatalf("second Shift+Tab mode = %q", model.collaboration)
	}
	model.running = true
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationExecute || !strings.Contains(model.entries[len(model.entries)-1].content, "cannot change") {
		t.Fatalf("running Shift+Tab changed mode: mode=%q entries=%#v", model.collaboration, model.entries)
	}
}

func TestFullscreenViewKeepsCommittedHistoryOutOfActiveRegion(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	view := model.View()
	if len(model.entries) != 0 || strings.Contains(view, "你好，我是 Amadeus") || strings.Contains(view, "Coding Agent") {
		t.Fatalf("startup created an unsolicited transcript entry: entries=%#v view=%q", model.entries, view)
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
		Task: func(context.Context, TaskSubmission) error { return nil },
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
	input := &delayedReader{reader: strings.NewReader("\x04"), delay: 25 * time.Millisecond}
	var output bytes.Buffer
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: input, Output: &output,
		Task: func(context.Context, TaskSubmission) error { return nil },
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
	for _, sequence := range []string{
		"\x1b[?1049h", "\x1b[?1049l", "\x1b[?47h", "\x1b[?47l",
		"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h", "\x1b[?1006h",
	} {
		if strings.Contains(rendered, sequence) {
			t.Fatalf("inline TUI emitted alternate-screen sequence %q: %q", sequence, rendered)
		}
	}
	if !strings.Contains(rendered, "Amadeus") {
		t.Fatalf("inline TUI did not commit startup history: %q", rendered)
	}
	if strings.Contains(rendered, "你好，我是") {
		t.Fatalf("inline TUI emitted the removed startup greeting: %q", rendered)
	}
}

func TestFullscreenEscAndCtrlCCancelActiveRun(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		t.Run(key.String(), func(t *testing.T) {
			app, err := NewFullscreenApplication(FullscreenOptions{
				Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
				Task: func(context.Context, TaskSubmission) error { return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			runContext, cancel := context.WithCancel(context.Background())
			app.setActiveRun(cancel)
			model := newFullscreenModel(context.Background(), app)
			model.running = true
			updated, _ := model.Update(tea.KeyMsg{Type: key})
			model = updated.(fullscreenModel)
			select {
			case <-runContext.Done():
			default:
				t.Fatalf("%s did not cancel the active Run", key.String())
			}
			if model.status != "cancelling" {
				t.Fatalf("%s status = %q", key.String(), model.status)
			}
		})
	}
}

func TestFullscreenWorkingTickChangesFrameWithoutAppendingHistory(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	model.runStartedAt = time.Now().Add(-2 * time.Second)
	beforeEntries := len(model.entries)
	before := model.workingLine()
	updated, command := model.Update(fullscreenWorkingTickMsg(time.Now()))
	model = updated.(fullscreenModel)
	after := model.workingLine()
	if command == nil || model.workingFrame != 1 {
		t.Fatalf("working animation did not advance: before=%q after=%q frame=%d", before, after, model.workingFrame)
	}
	if xansi.Strip(before) != xansi.Strip(after) {
		t.Fatalf("working animation changed text instead of light intensity: before=%q after=%q", xansi.Strip(before), xansi.Strip(after))
	}
	if len(model.entries) != beforeEntries {
		t.Fatalf("working tick polluted terminal history: before=%d after=%d", beforeEntries, len(model.entries))
	}
}

func TestFullscreenWorkingUsesMonochromeBeamAndSafeMarker(t *testing.T) {
	if fullscreenWorkingInterval != 36*time.Millisecond {
		t.Fatalf("working interval = %s", fullscreenWorkingInterval)
	}
	if workingBeamDistance(0, 0, len("Working")) == workingBeamDistance(1, 0, len("Working")) {
		t.Fatal("working beam did not move between adjacent subframes")
	}
	for frame := range 24 {
		word := animatedWorkingWord(frame)
		if got := xansi.Strip(word); got != "Working" {
			t.Fatalf("animated word frame %d = %q", frame, got)
		}
	}
	scanFrames := len("Working")*workingBeamSubframes + 2*workingBeamPadding
	for frame := scanFrames; frame < scanFrames+workingBeamPauseFrames; frame++ {
		for index := range len("Working") {
			if distance := workingBeamDistance(frame, index, len("Working")); distance < scanFrames {
				t.Fatalf("working beam remained visible during pause: frame=%d index=%d distance=%d", frame, index, distance)
			}
		}
	}
	for _, distance := range []int{0, 2, 6, 10, 20} {
		color, ok := workingBeamColor(distance).(lipgloss.AdaptiveColor)
		if !ok {
			t.Fatalf("beam color at distance %d is %T", distance, workingBeamColor(distance))
		}
		for _, value := range []string{color.Light, color.Dark} {
			if value == "42" || value == "51" {
				t.Fatalf("beam retained colored accent %q", value)
			}
		}
	}

	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	for frame := range 16 {
		model.workingFrame = frame
		line := xansi.Strip(model.workingLine())
		if strings.ContainsAny(line, "●◉○") {
			t.Fatalf("working line used an unsafe marker: %q", line)
		}
		if !strings.HasPrefix(line, "• ") && !strings.HasPrefix(line, "◦ ") {
			t.Fatalf("working line marker = %q", line)
		}
	}
	if workingMarker(0) != "•" || workingMarker(8) != "◦" || workingMarker(16) != "•" {
		t.Fatalf("working marker did not alternate: %q %q %q", workingMarker(0), workingMarker(8), workingMarker(16))
	}
}

func TestFullscreenWorkingKeepsOneBlankLineFromAgentOutput(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	model.runStartedAt = time.Now().Add(-40 * time.Second)
	model.draft = "正在检查文件"
	lines := strings.Split(xansi.Strip(model.View()), "\n")
	draftLine := -1
	workingLine := -1
	for index, line := range lines {
		switch {
		case strings.Contains(line, "正在检查文件"):
			draftLine = index
		case strings.Contains(line, "Working"):
			workingLine = index
		}
	}
	if draftLine < 0 || workingLine != draftLine+2 || strings.TrimSpace(lines[draftLine+1]) != "" {
		t.Fatalf("working line spacing is not one blank line: %q", xansi.Strip(model.View()))
	}
	if !strings.Contains(lines[workingLine], "(40s • esc to interrupt)") {
		t.Fatalf("working line omitted elapsed time or interrupt hint: %q", lines[workingLine])
	}
}

func TestFullscreenTaskCompletionCommitsWorkedDuration(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	model.runStartedAt = time.Now().Add(-2 * time.Second)
	updated, _ := model.Update(fullscreenTaskDoneMsg{})
	model = updated.(fullscreenModel)
	if len(model.entries) == 0 || model.entries[len(model.entries)-1].kind != "worked" {
		t.Fatalf("successful completion omitted worked entry: %#v", model.entries)
	}
	if got := formatRunDuration(3*time.Hour + 4*time.Minute + 5*time.Second); got != "3h 4m 5s" {
		t.Fatalf("formatted duration = %q", got)
	}
	line := xansi.Strip(model.renderEntry(fullscreenEntry{kind: "worked", content: "3h 4m 5s"}))
	if !strings.HasPrefix(line, "─Worked for 3h 4m 5s") || lipgloss.Width(line) != model.width-3 {
		t.Fatalf("worked separator = %q width=%d", line, lipgloss.Width(line))
	}
}

func TestFullscreenTranscriptSpacingForThinkAndWorked(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	think := xansi.Strip(model.renderCommittedEntries([]fullscreenEntry{{kind: "assistant", content: "正在分析"}}, ""))
	if !strings.HasSuffix(think, "\n") {
		t.Fatalf("think content omitted trailing newline: %q", think)
	}
	worked := xansi.Strip(model.renderCommittedEntries([]fullscreenEntry{{kind: "worked", content: "4m 5s"}}, ""))
	if !strings.HasPrefix(worked, "─Worked for 4m 5s") || strings.HasPrefix(worked, "\n") || strings.HasSuffix(worked, "\n") {
		t.Fatalf("worked line retained its own leading or trailing newline: %q", worked)
	}
	combined := xansi.Strip(model.renderCommittedEntries([]fullscreenEntry{
		{kind: "assistant", content: "任务完成"},
		{kind: "worked", content: "4m 5s"},
	}, ""))
	lines := strings.Split(combined, "\n")
	if len(lines) < 3 || !strings.Contains(lines[0], "任务完成") || strings.TrimSpace(lines[1]) != "" || !strings.HasPrefix(lines[2], "─Worked for 4m 5s") {
		t.Fatalf("worked line spacing after final answer = %q", combined)
	}
	nextUser := xansi.Strip(model.renderCommittedEntries([]fullscreenEntry{{kind: "user", content: "继续检查"}}, "worked"))
	if !strings.HasPrefix(nextUser, "\n› 继续检查") {
		t.Fatalf("next user message did not separate from worked line: %q", nextUser)
	}
}

func TestFullscreenInputKeepsTwoBlankLinesFromContent(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGap := func(rendered string) {
		t.Helper()
		lines := strings.Split(xansi.Strip(rendered), "\n")
		promptLine := -1
		for index, line := range lines {
			if strings.HasPrefix(line, "> ") {
				promptLine = index
				break
			}
		}
		if promptLine < 2 || strings.TrimSpace(lines[promptLine-1]) != "" || strings.TrimSpace(lines[promptLine-2]) != "" {
			t.Fatalf("input does not retain two blank lines: %q", xansi.Strip(rendered))
		}
	}
	model := newFullscreenModel(context.Background(), app)
	assertGap(model.View())
	model.draft = "正在检查文件"
	assertGap(model.View())
}

func TestFullscreenLogoAndInputPromptUseTerminalDefaultInk(t *testing.T) {
	if _, ok := fullscreenLogoStyle.GetForeground().(lipgloss.NoColor); !ok {
		t.Fatalf("logo foreground = %T, want terminal default", fullscreenLogoStyle.GetForeground())
	}
	if _, ok := fullscreenInputPromptStyle.GetForeground().(lipgloss.NoColor); !ok {
		t.Fatalf("input prompt foreground = %T, want terminal default", fullscreenInputPromptStyle.GetForeground())
	}
	if got := xansi.Strip(fullscreenInputPromptStyle.Render(fullscreenInputPrompt)); got != "> " {
		t.Fatalf("input prompt = %q", got)
	}
}

func TestFullscreenActiveDraftShowsOnlyVisibleTail(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
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
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	for _, item := range []event.Event{
		event.PlanUpdated{Revision: 1, Items: []event.PlanItem{{Step: "检查项目", Status: "in_progress"}}},
		event.IterationStarted{Iteration: 1},
		event.ToolCallStarted{CallID: "read-1", Iteration: 1, ToolName: "read_file", SideEffect: "read", ActionSummary: "Read planner.go"},
		event.ToolCallCompleted{CallID: "read-1", Iteration: 1, ToolName: "read_file", Success: true, Summary: "读取成功"},
		event.IterationCompleted{Iteration: 1, Status: "completed"},
		event.TextDelta{Delta: "任务完成。"},
	} {
		updated, _ := model.Update(fullscreenEventMsg{item: item})
		model = updated.(fullscreenModel)
	}
	model.finishDraft()
	content := model.transcriptContent()
	for _, expected := range []string{"Plan", "检查项目", "Explored", "Read planner.go", "任务完成"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("transcript omitted %q: %s", expected, content)
		}
	}
	if strings.Contains(content, "\n•\n") {
		t.Fatalf("assistant bullet rendered on a standalone line: %q", content)
	}
}

func TestPrefixRenderedBlockSkipsANSIOnlyBlankLines(t *testing.T) {
	input := "\x1b[38;5;252m\x1b[0m\n\x1b[38;5;252m正文\x1b[0m\n\x1b[0m"
	output := prefixRenderedBlock(input, "• ")
	if !strings.HasPrefix(output, "• ") || !strings.Contains(output, "正文") {
		t.Fatalf("prefixed block = %q", output)
	}
	if strings.Contains(output, "• \n") {
		t.Fatalf("prefix remained on a blank line: %q", output)
	}
}

func TestFullscreenStatusUsesContextWindowUpdateInsteadOfProviderUsage(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Startup: FullscreenStartup{Model: "test", ContextWindow: 100_000},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.width = 120
	model.applyEvent(event.UsageUpdated{Usage: llm.Usage{InputTokens: 90_000}})
	model.applyEvent(event.ContextWindowUpdated{EstimatedInputTokens: 25_000, ContextWindow: 100_000})
	status := xansi.Strip(model.statusBar())
	if !strings.Contains(status, "Context 25% used") || strings.Contains(status, "90%") {
		t.Fatalf("status used the wrong token semantic: %q", status)
	}
}

func TestFullscreenStatusUsesSemanticAccentColors(t *testing.T) {
	styles := []lipgloss.Style{
		fullscreenStatusModelStyle,
		fullscreenStatusPathStyle,
		fullscreenStatusGitStyle,
		statusContextStyle(20),
		statusContextStyle(75),
		statusContextStyle(95),
	}
	for index, style := range styles {
		if _, ok := style.GetForeground().(lipgloss.AdaptiveColor); !ok {
			t.Fatalf("status style %d foreground = %T", index, style.GetForeground())
		}
	}
	if reflect.DeepEqual(statusContextStyle(20).GetForeground(), statusContextStyle(75).GetForeground()) ||
		reflect.DeepEqual(statusContextStyle(75).GetForeground(), statusContextStyle(95).GetForeground()) {
		t.Fatal("context status thresholds do not change color")
	}

	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Startup: FullscreenStartup{Model: "GPT-TOP", Project: "/workspace/amadeus", Branch: "main", ContextWindow: 128_000},
		Task:    func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{40, 60, 100, 160} {
		model := newFullscreenModel(context.Background(), app)
		model.width = width
		model.contextUsage = 96_000
		if got := lipgloss.Width(model.statusBar()); got > width {
			t.Fatalf("status width %d exceeds terminal width %d", got, width)
		}
	}
}

func TestFullscreenCtrlTOpensAndClosesBoundedTranscriptViewer(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.details.Add("call", "Ran go test", strings.Repeat("output\n", 100))
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	model = updated.(fullscreenModel)
	if !model.viewingDetails || !strings.Contains(xansi.Strip(model.View()), "Transcript Details") || !strings.Contains(xansi.Strip(model.View()), "Ran go test") {
		t.Fatalf("detail viewer did not open: %q", model.View())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(fullscreenModel)
	if model.viewingDetails {
		t.Fatal("detail viewer remained open after Esc")
	}
}

func TestFullscreenTranscriptViewerSupportsPageNavigation(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 60,
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.height = 10
	model.details.Add("call", "Ran command", strings.Repeat("output line\n", 100))
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = updated.(fullscreenModel)
	if model.detailViewport.YOffset == 0 {
		t.Fatal("PgDown did not scroll transcript viewer")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(fullscreenModel)
	if model.detailViewport.YOffset != 0 {
		t.Fatalf("PgUp did not return viewer to top: %d", model.detailViewport.YOffset)
	}
}

func TestFullscreenResponsiveLayoutFitsWidthMatrix(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Startup: FullscreenStartup{
			Version: "dev", Provider: "openai", Model: "GPT-TOP",
			Project: "/非常长的项目目录/with/a/very/long/path/that/must/not/overflow/the/terminal",
			Branch:  "feature/非常长的分支名称", Session: "draft", ContextWindow: 258_000,
		},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{40, 60, 100, 160} {
		model := newFullscreenModel(context.Background(), app)
		updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		model = updated.(fullscreenModel)
		for _, rendered := range []string{model.banner(), model.View(), model.renderEntry(fullscreenEntry{kind: "assistant", content: "中文与 emoji 🎼 内容需要保持可读。"})} {
			for _, line := range strings.Split(rendered, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width %d overflowed with line width %d: %q", width, lipgloss.Width(line), xansi.Strip(line))
				}
			}
		}
	}
}

func TestFullscreenInitialBannerUsesDetectedWidthBeforeResize(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 40,
		Startup: FullscreenStartup{Model: "test", Project: "/a/very/long/project/path", Session: "draft"},
		Task:    func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	if model.width != 40 || terminalLogo(model.width) != compactLogo {
		t.Fatalf("initial width was not applied: width=%d logo=%q", model.width, terminalLogo(model.width))
	}
	for _, line := range strings.Split(model.banner(), "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("initial banner overflowed before resize: width=%d line=%q", lipgloss.Width(line), xansi.Strip(line))
		}
	}
}

func TestFullscreenNoColorProjectionContainsNoANSI(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, NoColor: true,
		Startup: FullscreenStartup{Version: "dev", Model: "GPT-TOP", Project: "/project"},
		Task:    func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	for name, rendered := range map[string]string{
		"banner": model.banner(),
		"view":   model.View(),
		"entry":  model.renderEntry(fullscreenEntry{kind: "assistant", content: "**hello**"}),
	} {
		if strings.Contains(rendered, "\x1b[") {
			t.Fatalf("%s retained ANSI in no-color mode: %q", name, rendered)
		}
	}
}

func TestFullscreenMarkdownInlineCodeUsesCyanWithoutBackground(t *testing.T) {
	renderer, err := newFullscreenMarkdownRenderer(80)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderer.Render("Use `read_file` here.")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "\x1b[48;") {
		t.Fatalf("inline code retained a background color: %q", rendered)
	}
	if !strings.Contains(rendered, "38;2;103;232;249") {
		t.Fatalf("inline code did not use Read/Search cyan: %q", rendered)
	}
}

func TestFullscreenApprovalUsesKeyboardDecision(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	response := make(chan fullscreenApprovalResult, 1)
	request := policy.ApprovalRequest{ID: "approval-1", ToolName: "write_file", ArgumentsSHA256: strings.Repeat("a", 64), Risk: policy.CommandRiskHigh, Reason: "writes a file"}
	updated, _ := model.Update(fullscreenApprovalMsg{prompt: &fullscreenApproval{request: request, response: response}})
	model = updated.(fullscreenModel)
	if model.input.Focused() {
		t.Fatal("approval did not take exclusive input focus")
	}
	prompt := xansi.Strip(model.inputBox())
	for _, expected := range []string{"› Yes, allow once", "Yes, allow for this session", "No, deny", "↑/↓ select", "Enter confirm"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("approval selector omitted %q: %q", expected, prompt)
		}
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(fullscreenModel)
	if model.selection == nil || model.selection.Selected != 1 || !strings.Contains(xansi.Strip(model.inputBox()), "› Yes, allow for this session") {
		t.Fatalf("approval selection did not move down: selection=%#v prompt=%q", model.selection, xansi.Strip(model.inputBox()))
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	decision := (<-response).decision
	if decision.Outcome != policy.ApprovalAllow || decision.Scope != policy.ApprovalSession {
		t.Fatalf("unexpected approval decision: %#v", decision)
	}
	if model.approval != nil {
		t.Fatal("approval prompt remained active")
	}
	if command == nil {
		t.Fatal("approval resolution did not restore input and working commands")
	}
}

func TestFullscreenApprovalPausesWorkingAnimation(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	model.approval = &fullscreenApproval{}
	updated, command := model.Update(fullscreenWorkingTickMsg(time.Now()))
	model = updated.(fullscreenModel)
	if command != nil || model.workingFrame != 0 {
		t.Fatalf("approval retained working animation: command=%v frame=%d", command != nil, model.workingFrame)
	}
}

func TestFullscreenProgramRoutesArrowAndEnterToApproval(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()

	var output bytes.Buffer
	decisionReceived := make(chan policy.ApprovalDecision, 1)
	approvalStarted := make(chan struct{})
	request, err := policy.NewApprovalRequest("approval-program-1", "write_file", []byte(`{"path":"notes.md"}`), policy.CommandRiskHigh, "writes a file")
	if err != nil {
		t.Fatal(err)
	}
	var app *FullscreenApplication
	app, err = NewFullscreenApplication(FullscreenOptions{
		Input: inputReader, Output: &output,
		Task: func(ctx context.Context, _ TaskSubmission) error {
			close(approvalStarted)
			decision, decideErr := app.Decide(ctx, request)
			if decideErr == nil {
				decisionReceived <- decision
			}
			return decideErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(context.Background()) }()
	if _, err := inputWriter.Write([]byte("trigger\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-approvalStarted:
	case <-time.After(time.Second):
		t.Fatal("approval was not requested")
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := inputWriter.Write([]byte("\x1b[B\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case decision := <-decisionReceived:
		if decision.Outcome != policy.ApprovalAllow || decision.Scope != policy.ApprovalSession {
			t.Fatalf("unexpected approval decision: %#v", decision)
		}
	case <-time.After(time.Second):
		t.Fatalf("arrow and enter did not resolve approval; output=%q", xansi.Strip(output.String()))
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := inputWriter.Write([]byte{0x04}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run fullscreen application: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fullscreen application did not exit")
	}
}

func TestFullscreenResumeSelectorUsesBorderlessYellowList(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 80,
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.sessions = []SessionOption{
		{ID: "session-1", Title: "first", Current: true},
		{ID: "session-2", Title: "second"},
	}
	model.selection = &selectionOverlay{Title: "Resume Session", Items: []selectionItem{{Name: "session-1", Description: "first · current"}, {Name: "session-2", Description: "second"}}}
	model.selectionKind = "resume"
	prompt := xansi.Strip(model.inputBox())
	if strings.ContainsAny(prompt, "╭╮╰╯│") || !strings.Contains(prompt, "› session-1") {
		t.Fatalf("resume selector retained a box or omitted selection: %q", prompt)
	}
	color, ok := fullscreenResumeAccentStyle.GetForeground().(lipgloss.AdaptiveColor)
	if !ok || color.Light != "#A16207" || color.Dark != "#FDE68A" {
		t.Fatalf("resume accent color = %#v", fullscreenResumeAccentStyle.GetForeground())
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(fullscreenModel)
	if model.selection.Selected != 1 || !strings.Contains(xansi.Strip(model.inputBox()), "› session-2") {
		t.Fatalf("resume selection did not move: selected=%d prompt=%q", model.selection.Selected, xansi.Strip(model.inputBox()))
	}
}

func TestFullscreenExitQuits(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.input.SetValue("/exit")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated.(fullscreenModel)
	if command == nil {
		t.Fatal("exit command did not return a quit command")
	}
	message := command()
	if _, ok := message.(tea.QuitMsg); !ok {
		t.Fatalf("exit command returned %T, want tea.QuitMsg", message)
	}
}

func TestFullscreenCopyUsesInjectedClipboard(t *testing.T) {
	var copied string
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task:           func(context.Context, TaskSubmission) error { return nil },
		ClipboardWrite: func(value string) error { copied = value; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.lastAssistantMarkdown = "**done**"
	updated, _ := model.submitCommand("/copy")
	model = updated.(fullscreenModel)
	if copied != "**done**" || len(model.entries) != 1 || !strings.Contains(model.entries[0].content, "Copied") {
		t.Fatalf("copy command result: copied=%q entries=%#v", copied, model.entries)
	}
}

func TestFullscreenClearResetsTransientStateAndPlanMode(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
		Command: func(_ context.Context, command string) (string, error) {
			if command != "/clear" {
				t.Fatalf("clear command = %q", command)
			}
			return "Started a new chat", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.entries = []fullscreenEntry{{kind: "assistant", content: "old"}}
	model.committed = 1
	model.draft = "streaming"
	model.lastAssistantMarkdown = "old"
	model.collaboration = CollaborationPlan
	updated, command := model.submitCommand("/clear")
	model = updated.(fullscreenModel)
	if command == nil {
		t.Fatal("clear command did not start async action")
	}
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if len(model.entries) != 0 || model.committed != 0 || model.draft != "" || model.lastAssistantMarkdown != "" || model.collaboration != CollaborationExecute {
		t.Fatalf("clear retained transient state: %#v", model)
	}
}

func TestFullscreenResumeUsesSearchOverlayAndResetsPlanMode(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
		Sessions: func(context.Context) ([]SessionOption, error) {
			return []SessionOption{{ID: "session-1", Title: "first"}, {ID: "session-2", Title: "second"}}, nil
		},
		Resume: func(_ context.Context, id string) (string, error) { return "resumed " + id, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.collaboration = CollaborationPlan
	updated, command := model.submitCommand("/resume")
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if model.selection == nil || !model.selection.Search || model.selectionKind != "resume" {
		t.Fatalf("resume did not open search overlay: %#v", model.selection)
	}
	for _, character := range []rune("second") {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
		model = updated.(fullscreenModel)
	}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationExecute || model.selection != nil || !strings.Contains(model.entries[len(model.entries)-1].content, "session-2") {
		t.Fatalf("resume result: collaboration=%q selection=%#v entries=%#v", model.collaboration, model.selection, model.entries)
	}
}

func TestFullscreenRenameDeleteAndMCPCommands(t *testing.T) {
	var renamed string
	deleted := false
	var delegated string
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task:                func(context.Context, TaskSubmission) error { return nil },
		CurrentSessionTitle: func() string { return "Current title" },
		Rename:              func(_ context.Context, title string) (string, error) { renamed = title; return "renamed", nil },
		Delete:              func(context.Context) (string, error) { deleted = true; return "deleted", nil },
		Command:             func(_ context.Context, command string) (string, error) { delegated = command; return "mcp output", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	updated, _ := model.submitCommand("/rename")
	model = updated.(fullscreenModel)
	if model.selection == nil || model.selection.Value != "Current title" {
		t.Fatalf("rename prompt was not prefilled: %#v", model.selection)
	}
	model.selection.Value = "New title"
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if renamed != "New title" {
		t.Fatalf("renamed title = %q", renamed)
	}
	updated, _ = model.submitCommand("/delete")
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(fullscreenModel)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if !deleted {
		t.Fatal("delete callback was not invoked")
	}
	model.selection = nil
	updated, command = model.submitCommand("/mcp verbose")
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if delegated != "/mcp verbose" || !strings.Contains(model.entries[len(model.entries)-1].content, "mcp output") {
		t.Fatalf("MCP command was not delegated: delegated=%q entries=%#v", delegated, model.entries)
	}
}

func TestFullscreenRunningCommandGatingAndSkillDisabledReason(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task:   func(context.Context, TaskSubmission) error { return nil },
		Skills: func(context.Context) ([]SkillOption, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.running = true
	updated, _ := model.submitCommand("/clear")
	model = updated.(fullscreenModel)
	if len(model.entries) == 0 || !strings.Contains(model.entries[len(model.entries)-1].content, "disabled") {
		t.Fatalf("running clear was not rejected: %#v", model.entries)
	}
	updated, _ = model.submitCommand("/skills")
	model = updated.(fullscreenModel)
	if model.selection == nil || len(model.selection.Items) != 2 || !model.selection.Items[1].Disabled || model.selection.Items[1].DisabledReason == "" {
		t.Fatalf("running Skill mutation was not disabled with reason: %#v", model.selection)
	}
}

func TestFullscreenSkillsOverlayListsAndTogglesAuthoritativeState(t *testing.T) {
	var toggledName string
	var toggledEnabled bool
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{},
		Task: func(context.Context, TaskSubmission) error { return nil },
		Skills: func(context.Context) ([]SkillOption, error) {
			return []SkillOption{{Name: "review", Description: "Review code", Source: "project", Enabled: true}}, nil
		},
		SetSkill: func(_ context.Context, name string, enabled bool) error {
			toggledName, toggledEnabled = name, enabled
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	updated, _ := model.submitCommand("/skills")
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(fullscreenModel)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if model.selectionKind != "skills" || model.selection == nil || !model.selection.Search {
		t.Fatalf("Skill toggle overlay was not opened: %#v", model.selection)
	}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(command())
	model = updated.(fullscreenModel)
	if toggledName != "review" || toggledEnabled || model.skills[0].Enabled {
		t.Fatalf("Skill state was not toggled: name=%q enabled=%v skills=%#v", toggledName, toggledEnabled, model.skills)
	}
}
