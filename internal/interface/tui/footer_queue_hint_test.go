package tui

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func queueHintProps(width int, plan bool, palette terminalPalette) footerProps {
	indicator := collaborationModeIndicator(0)
	if plan {
		indicator = collaborationModeIndicatorPlan
	}
	return footerProps{
		Width: width, Running: true, HasQueueableDraft: true,
		State: footerState{
			StatusLine:             statusLineState{Segments: []statusLineSegment{{Item: statusLineItemModelWithReasoning, Text: "test-model"}}},
			CollaborationIndicator: indicator,
		},
		Palette: palette, LeftPadding: fullscreenFooterLeftPadding, RightPadding: fullscreenFooterRightPadding,
	}
}

func TestQueueHintFooterReplacesPassiveStatusLine(t *testing.T) {
	props := queueHintProps(80, false, terminalPalette{Level: colorLevelNone, NoColor: true})
	plain := xansi.Strip(renderFooter(props))
	if !strings.Contains(plain, footerQueueHintFull) || strings.Contains(plain, "test-model") {
		t.Fatalf("queue footer = %q", plain)
	}
	if width := lipgloss.Width(plain); width != props.Width {
		t.Fatalf("queue footer width = %d, want %d: %q", width, props.Width, plain)
	}
}

func TestQueueHintFooterWidthFallbackAndPlanPriority(t *testing.T) {
	for _, test := range []struct {
		name     string
		width    int
		wantHint string
		wantPlan bool
	}{
		{name: "full with plan", width: 40, wantHint: footerQueueHintFull, wantPlan: true},
		{name: "short with plan", width: 28, wantHint: footerQueueHintShort, wantPlan: true},
		{name: "full without plan", width: 24, wantHint: footerQueueHintFull, wantPlan: false},
		{name: "short without plan", width: 16, wantHint: footerQueueHintShort, wantPlan: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			props := queueHintProps(test.width, true, terminalPalette{Level: colorLevelNone, NoColor: true})
			plain := xansi.Strip(renderFooter(props))
			if !strings.Contains(plain, test.wantHint) {
				t.Fatalf("footer omitted %q: %q", test.wantHint, plain)
			}
			if test.wantHint == footerQueueHintShort && strings.Contains(plain, footerQueueHintFull) {
				t.Fatalf("footer did not use short hint: %q", plain)
			}
			if strings.Contains(plain, "Plan mode") != test.wantPlan {
				t.Fatalf("footer Plan mode=%v, want %v: %q", strings.Contains(plain, "Plan mode"), test.wantPlan, plain)
			}
			if width := lipgloss.Width(plain); width != test.width {
				t.Fatalf("footer width = %d, want %d: %q", width, test.width, plain)
			}
		})
	}
}

func TestHasQueueableDraftUsesComposerAndOverlayState(t *testing.T) {
	_, baseline := newTestFullscreen(t, nil)
	baseline.running = true
	baseline.input.SetValue("next task")
	if !baseline.hasQueueableDraft() {
		t.Fatal("running ordinary draft is not queueable")
	}

	for _, test := range []struct {
		name   string
		mutate func(*fullscreenModel)
	}{
		{name: "idle", mutate: func(model *fullscreenModel) { model.running = false }},
		{name: "empty", mutate: func(model *fullscreenModel) { model.input.SetValue("  ") }},
		{name: "slash command", mutate: func(model *fullscreenModel) { model.input.SetValue("/status") }},
		{name: "invalid slash", mutate: func(model *fullscreenModel) { model.input.SetValue("/unknown") }},
		{name: "selection", mutate: func(model *fullscreenModel) { model.selection = &selectionOverlay{} }},
		{name: "approval", mutate: func(model *fullscreenModel) { model.approvalDialog = &approvalDialog{} }},
		{name: "user input", mutate: func(model *fullscreenModel) { model.userInputDialog = &requestUserInputDialog{} }},
		{name: "slash popup", mutate: func(model *fullscreenModel) {
			model.input.SetValue("/res")
			model.slashPopup.sync(model.input.Value(), model.running)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := baseline
			test.mutate(&model)
			if model.hasQueueableDraft() {
				t.Fatalf("%s state exposed queue hint", test.name)
			}
		})
	}
}

func TestFullscreenQueueHintTransitionsAroundTabAndQueuedPreview(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.input.SetValue("second")
	before := xansi.Strip(model.composerAuxiliaryView())
	if !strings.Contains(before, footerQueueHintFull) || strings.Contains(before, "test-model") {
		t.Fatalf("pre-enqueue footer = %q", before)
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(fullscreenModel)
	if command != nil {
		t.Fatal("Tab enqueue returned command")
	}
	after := xansi.Strip(model.composerAuxiliaryView())
	if strings.Contains(after, footerQueueHintFull) || !strings.Contains(after, "Queued (1)") || !strings.Contains(after, "test-model") {
		t.Fatalf("post-enqueue auxiliary = %q", after)
	}

	model.input.SetValue("third")
	withNextDraft := xansi.Strip(model.composerAuxiliaryView())
	if !strings.Contains(withNextDraft, "Queued (1)") || !strings.Contains(withNextDraft, footerQueueHintFull) || strings.Contains(withNextDraft, "test-model") {
		t.Fatalf("queued preview plus draft hint = %q", withNextDraft)
	}
}

func TestQueueHintFooterColorMatrix(t *testing.T) {
	original := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	for _, test := range []struct {
		name    string
		palette terminalPalette
		profile termenv.Profile
		ansi    bool
	}{
		{name: "true-color", palette: terminalPalette{Level: colorLevelTrueColor, Dark: true, Foreground: terminalRGB{225, 225, 225}, Background: terminalRGB{18, 18, 18}}, profile: termenv.TrueColor, ansi: true},
		{name: "ansi256", palette: terminalPalette{Level: colorLevelANSI256, Dark: true}, profile: termenv.ANSI256, ansi: true},
		{name: "ansi16", palette: terminalPalette{Level: colorLevelANSI16, Dark: true}, profile: termenv.ANSI, ansi: true},
		{name: "no-color", palette: terminalPalette{Level: colorLevelNone, NoColor: true}, profile: termenv.Ascii, ansi: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			lipgloss.SetColorProfile(test.profile)
			rendered := renderFooter(queueHintProps(40, true, test.palette))
			if !strings.Contains(xansi.Strip(rendered), footerQueueHintFull) {
				t.Fatalf("queue hint missing: %q", rendered)
			}
			if strings.Contains(rendered, "\x1b[") != test.ansi {
				t.Fatalf("ANSI=%v rendered=%q", test.ansi, rendered)
			}
		})
	}
}

func TestQueueHintFooterSnapshot(t *testing.T) {
	props := queueHintProps(40, true, terminalPalette{Level: colorLevelNone, NoColor: true})
	if got, want := xansi.Strip(renderFooter(props)), "  tab to queue message       Plan mode  "; got != want {
		t.Fatalf("queue hint snapshot\n got: %q\nwant: %q", got, want)
	}
	props.Width = 28
	if got, want := xansi.Strip(renderFooter(props)), "  tab to queue   Plan mode  "; got != want {
		t.Fatalf("short queue hint snapshot\n got: %q\nwant: %q", got, want)
	}
}

func TestQueueHintDoesNotChangeSessionOrQueuedState(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.input.SetValue("draft")
	beforeSession := model.session
	beforeQueue := model.nextTurnQueue
	_ = model.footerView()
	if model.session != beforeSession || len(model.nextTurnQueue.Pending) != len(beforeQueue.Pending) || model.nextTurnQueue.InFlight != beforeQueue.InFlight {
		t.Fatalf("footer render mutated state: session=%#v queue=%#v", model.session, model.nextTurnQueue)
	}
	if model.session.Configuration.Mode != protocol.ModeKindDefault {
		t.Fatalf("footer render changed mode: %q", model.session.Configuration.Mode)
	}
}
