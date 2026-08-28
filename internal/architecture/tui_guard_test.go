package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusLineArchitectureUsesTypedSessionStateAndPureFooter(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/tui/session_state.go",
		"internal/tui/status_line.go",
		"internal/tui/status_line_workspace.go",
		"internal/tui/footer.go",
		"internal/agent/session/configuration.go",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("required statusline boundary file is missing: %s: %v", relative, err)
		}
	}

	snapshotFields := architectureStructFields(t, root, "internal/app/interactive_contract.go", "ThreadViewSnapshot")
	for _, required := range []string{"Generation", "ThreadID", "Title", "Configuration", "Items", "TokenInfo", "ActiveContextTokens"} {
		if _, ok := snapshotFields[required]; !ok {
			t.Errorf("ThreadViewSnapshot is missing field %q", required)
		}
	}
	for _, forbidden := range []string{"Mode", "Provider", "Model", "CWD"} {
		if _, ok := snapshotFields[forbidden]; ok {
			t.Errorf("ThreadViewSnapshot retains duplicate configuration field %q", forbidden)
		}
	}

	startupFields := architectureStructFields(t, root, "internal/tui/application.go", "Startup")
	if len(startupFields) != 1 {
		t.Fatalf("Startup fields = %#v, want Version only", startupFields)
	}
	if _, ok := startupFields["Version"]; !ok {
		t.Fatal("Startup is missing Version")
	}

	settingsFields := architectureStructFields(t, root, "internal/protocol/session_events.go", "ThreadSettingsAppliedEvent")
	if _, ok := settingsFields["Configuration"]; !ok {
		t.Fatal("ThreadSettingsAppliedEvent does not carry SessionConfiguration")
	}
	if _, ok := settingsFields["Mode"]; ok {
		t.Fatal("ThreadSettingsAppliedEvent retains the mode-only payload")
	}

	viewSource := mustReadArchitectureFile(t, root, "internal/tui/application_view.go")
	for _, forbidden := range []string{
		"Application.Status()", "ResolveWorkspaceBranch(", "exec.Command", "os.Stat", "statusBar(",
		"bottomAnchorView", "cursorAnchoredWriter", "terminalCursor", "CursorUp(", "CursorDown(",
	} {
		if strings.Contains(viewSource, forbidden) {
			t.Errorf("View/render path owns forbidden statusline concern %q", forbidden)
		}
	}
	historyCellSource := mustReadArchitectureFile(t, root, "internal/tui/history_cell.go")
	if !strings.Contains(historyCellSource, "const transcriptRegionBlankRows = 2") || !strings.Contains(viewSource, "transcriptRegionSeparator") {
		t.Error("TUI regions do not share the Codex two-blank-row spacing contract")
	}
	if !strings.Contains(historyCellSource, "func (FinalMessageSeparator) HistoryBoundaryBlankRows() int") || !strings.Contains(historyCellSource, "historyBoundaryBlankRows(previous, current HistoryCell)") {
		t.Error("FinalMessageSeparator does not override spacing to one blank row")
	}
	applicationSource := mustReadArchitectureFile(t, root, "internal/tui/application.go")
	if !strings.Contains(applicationSource, "tea.WithOutput(app.options.Output)") {
		t.Error("Bubble Tea output is not wired directly to the configured writer")
	}
	for _, forbidden := range []string{"cursorAnchoredWriter", "terminalCursor", "CursorUp(", "CursorDown("} {
		if strings.Contains(applicationSource, forbidden) {
			t.Errorf("TUI application reintroduced terminal cursor protocol %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "internal/tui/terminal_cursor.go")); err == nil {
		t.Error("legacy terminal cursor adapter still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect legacy terminal cursor adapter: %v", err)
	}
	footerSource := mustReadArchitectureFile(t, root, "internal/tui/footer.go")
	for _, required := range []string{"type footerState struct", "type footerProps struct", "func renderFooter("} {
		if !strings.Contains(footerSource, required) {
			t.Errorf("Footer boundary is missing %q", required)
		}
	}
	for _, forbidden := range []string{"Application.Status()", "ResolveWorkspaceBranch(", "exec.Command", "os.Stat"} {
		if strings.Contains(footerSource, forbidden) {
			t.Errorf("pure Footer renderer owns forbidden concern %q", forbidden)
		}
	}

	for _, relative := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, forbidden := range []string{"type statusBarPart", "func (model model) statusBar(", "startup.Project", "startup.Branch", "startup.Session", "startup.ContextWindow"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("legacy statusline concept %q remains in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relative, err)
		}
	}
}

func TestTUIExitArchitectureUsesTypedLifecycleAndFrameDrain(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/tui/application_exit.go",
		"internal/app/interactive_shutdown.go",
		"internal/cli/tui_exit.go",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("required exit lifecycle boundary file is missing: %s: %v", relative, err)
		}
	}

	exitFields := architectureStructFields(t, root, "internal/tui/application_exit.go", "AppExitInfo")
	for _, required := range []string{"TokenUsage", "ThreadID", "ThreadName", "ResumeHint", "ExitReason", "Error"} {
		if _, ok := exitFields[required]; !ok {
			t.Errorf("AppExitInfo is missing field %q", required)
		}
	}

	exitSource := mustReadArchitectureFile(t, root, "internal/tui/application_exit.go")
	for _, required := range []string{
		"type ExitMode uint8", "ExitModeShutdownFirst", "ExitModeImmediate",
		"type ExitReason uint8", "ExitReasonUserRequested", "ExitReasonFatal",
		"exitPhaseDrainingFrame", "exitFrameDrainedMsg", "func (model appModel) appExitInfo() AppExitInfo",
	} {
		if !strings.Contains(exitSource, required) {
			t.Errorf("exit lifecycle boundary is missing %q", required)
		}
	}
	if strings.Count(exitSource, "return tea.Quit") != 1 {
		t.Errorf("exit lifecycle owns %d final tea.Quit paths, want 1", strings.Count(exitSource, "return tea.Quit"))
	}

	applicationSource := mustReadArchitectureFile(t, root, "internal/tui/application.go")
	if !strings.Contains(applicationSource, "Run(ctx context.Context) (AppExitInfo, error)") {
		t.Fatal("Application.Run does not return AppExitInfo")
	}
	appEventsSource := mustReadArchitectureFile(t, root, "internal/tui/application_app_events.go")
	for _, forbidden := range []string{"tea.Quit", "ShutdownStarted", "ShutdownFinished", "Shutting down…"} {
		if strings.Contains(appEventsSource, forbidden) {
			t.Errorf("Application event projection retains legacy exit concern %q", forbidden)
		}
	}
	interactiveTypes := mustReadArchitectureFile(t, root, "internal/app/interactive_contract.go")
	for _, forbidden := range []string{"type ShutdownStarted", "type ShutdownFinished"} {
		if strings.Contains(interactiveTypes, forbidden) {
			t.Errorf("InteractiveEvent retains TUI exit lifecycle event %q", forbidden)
		}
	}

	for _, relative := range []string{"internal/tui", "internal/app", "cmd/amadeus"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			source := string(content)
			relativePath := filepath.ToSlash(path[len(root)+1:])
			if relativePath != "internal/tui/application_exit.go" && strings.Contains(source, "tea.Quit") {
				t.Errorf("direct tea.Quit remains outside exit lifecycle in %s", relativePath)
			}
			for _, forbidden := range []string{"shutdownRequested", "NewNoticeHistoryCell(\"Shutting down", "cursorAnchoredWriter", "terminal-height padding", "CursorUp(", "CursorDown("} {
				if strings.Contains(source, forbidden) {
					t.Errorf("legacy exit implementation %q remains in %s", forbidden, relativePath)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan exit lifecycle in %s: %v", relative, err)
		}
	}
}

func TestWebFetchArchitectureKeepsSplitSafetyAndProjectionBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/webfetch/document.go",
		"internal/webfetch/errors.go",
		"internal/webfetch/url.go",
		"internal/webfetch/transport.go",
		"internal/webfetch/proxy.go",
		"internal/webfetch/fetch.go",
		"internal/webfetch/content.go",
		"internal/webfetch/markdown.go",
		"internal/tool/builtin/web_fetch.go",
		"internal/tool/builtin/web_search.go",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("required Web boundary file is missing: %s: %v", relative, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "internal/tool/builtin/web.go")); err == nil {
		t.Error("legacy combined web Tool file still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect legacy web Tool file: %v", err)
	}

	fetchSource := mustReadArchitectureFile(t, root, "internal/webfetch/fetch.go")
	for _, forbidden := range []string{"regexp.", "titleTag", "scriptStyle", "tags.ReplaceAllString", "HTTPClient"} {
		if strings.Contains(fetchSource, forbidden) {
			t.Errorf("web fetch orchestration owns removed implementation detail %q", forbidden)
		}
	}
	fetchTool := mustReadArchitectureFile(t, root, "internal/tool/builtin/web_fetch.go")
	if strings.Contains(fetchTool, "type WebSearch") || strings.Contains(fetchTool, "webSearchSpec") {
		t.Error("web_fetch Tool file reintroduced web_search ownership")
	}

	documentFields := architectureStructFields(t, root, "internal/webfetch/document.go", "Document")
	for _, required := range []string{"URL", "ContentType", "Title", "Markdown", "Partial", "Bytes"} {
		if _, ok := documentFields[required]; !ok {
			t.Errorf("webfetch.Document missing field %s", required)
		}
	}
	if _, legacy := documentFields["Text"]; legacy {
		t.Error("webfetch.Document retains legacy flat Text field")
	}
	optionFields := architectureStructFields(t, root, "internal/webfetch/document.go", "Options")
	if _, legacy := optionFields["HTTPClient"]; legacy {
		t.Error("webfetch.Options retains HTTPClient bypass around pinned transport")
	}
}

func TestUserInputSubmissionIsNeverDeferred(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "internal", "agent", "session", "submission.go")
	set := token.NewFileSet()
	parsed, err := parser.ParseFile(set, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok || len(clause.List) != 1 {
			return true
		}
		selector, ok := clause.List[0].(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "UserInputOp" {
			return true
		}
		found = true
		ast.Inspect(clause, func(child ast.Node) bool {
			if field, ok := child.(*ast.SelectorExpr); ok && field.Sel.Name == "deferred" {
				t.Errorf("UserInputOp case writes Session deferred queue at %s", set.Position(field.Pos()))
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("UserInputOp submission case was not found")
	}
}

func TestTurnSteerHasDedicatedCoordinationBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	steer := mustReadArchitectureFile(t, root, "internal/agent/session/steer_input.go")
	for _, forbidden := range []string{"UserInputAnswerOp", "ApprovalDecisionOp", "rollout.", "ContextUpdate("} {
		if strings.Contains(steer, forbidden) {
			t.Errorf("steer input owns forbidden boundary %q", forbidden)
		}
	}
	queue := mustReadArchitectureFile(t, root, "internal/agent/session/input_queue.go")
	for _, forbidden := range []string{"rollout.", "ResponseUserMessage", "AppendItems", "ContextUpdate"} {
		if strings.Contains(queue, forbidden) {
			t.Errorf("InputQueue became a second history owner through %q", forbidden)
		}
	}
	update := mustReadArchitectureFile(t, root, "internal/tui/application_update.go")
	inputStart := strings.Index(update, `case "enter":`)
	inputEnd := strings.Index(update[inputStart:], "\n\tvar command tea.Cmd")
	if inputStart < 0 || inputEnd < 0 {
		t.Fatal("locate TUI ordinary input handler")
	}
	ordinaryInput := update[inputStart : inputStart+inputEnd]
	for _, forbidden := range []string{"model.running = true", "model.runStartedAt =", "newTranscriptDetailStore("} {
		if strings.Contains(ordinaryInput, forbidden) {
			t.Errorf("ordinary user input resets Turn UI through %q", forbidden)
		}
	}
}

func TestNextTurnQueueRemainsATUIInputBoundary(t *testing.T) {
	root := repositoryRoot(t)
	queuePath := "internal/tui/input_queue.go"
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(queuePath))); err != nil {
		t.Fatalf("required next-turn queue boundary is missing: %v", err)
	}

	queuedFields := architectureStructFields(t, root, queuePath, "QueuedUserMessage")
	for _, required := range []string{"Message", "Mode", "ThreadID", "AttachmentGeneration"} {
		if _, ok := queuedFields[required]; !ok {
			t.Errorf("QueuedUserMessage is missing field %q", required)
		}
	}
	queueFields := architectureStructFields(t, root, queuePath, "NextTurnQueue")
	for _, required := range []string{"Pending", "InFlight"} {
		if _, ok := queueFields[required]; !ok {
			t.Errorf("NextTurnQueue is missing field %q", required)
		}
	}
	inputFields := architectureStructFields(t, root, "internal/tui/slash_command.go", "InputResult")
	if _, ok := inputFields["Queue"]; !ok {
		t.Error("InputResult does not distinguish queued composer input")
	}

	queueSource := mustReadArchitectureFile(t, root, queuePath)
	for _, forbidden := range []string{"internal/agent/session", "internal/contextmanager", "internal/rollout", "Session.deferred", "UserMessageAdmissionQueued", "QueuedInputOp"} {
		if strings.Contains(queueSource, forbidden) {
			t.Errorf("next-turn queue owns forbidden Runtime boundary %q", forbidden)
		}
	}

	for _, relative := range []string{"internal/agent", "internal/contextmanager", "internal/rollout"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, forbidden := range []string{"NextTurnQueue", "QueuedUserInput", "QueuedUserMessage", "UserMessageAdmissionQueued", "QueuedInputOp"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("Runtime/canonical package owns TUI queue symbol %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan next-turn queue boundaries in %s: %v", relative, err)
		}
	}
}

func TestQueueHintRemainsTransientFooterGuidance(t *testing.T) {
	root := repositoryRoot(t)
	footerPath := "internal/tui/footer.go"
	propsFields := architectureStructFields(t, root, footerPath, "footerProps")
	if _, ok := propsFields["HasQueueableDraft"]; !ok {
		t.Error("footerProps does not carry the pure queueable-draft input")
	}
	stateFields := architectureStructFields(t, root, footerPath, "footerState")
	for _, forbidden := range []string{"HasQueueableDraft", "QueueHint", "QueueableDraft"} {
		if _, exists := stateFields[forbidden]; exists {
			t.Errorf("footerState persists transient queue hint field %q", forbidden)
		}
	}

	footerSource := mustReadArchitectureFile(t, root, footerPath)
	for _, required := range []string{"footerQueueHintFull", "footerQueueHintShort", "renderQueueHintFooter", "HasQueueableDraft"} {
		if !strings.Contains(footerSource, required) {
			t.Errorf("footer queue hint boundary is missing %q", required)
		}
	}
	for _, forbidden := range []string{"Application.Status(", "SubmitUser(", "HistoryCell", "protocol.Event"} {
		if strings.Contains(footerSource, forbidden) {
			t.Errorf("pure footer queue hint owns forbidden dependency %q", forbidden)
		}
	}

	statusSource := mustReadArchitectureFile(t, root, "internal/tui/status_line.go")
	for _, forbidden := range []string{"statusLineItemQueue", "statusLineItemQueueHint", "HasQueueableDraft", "tab to queue"} {
		if strings.Contains(statusSource, forbidden) {
			t.Errorf("fixed statusline owns transient queue hint %q", forbidden)
		}
	}

	tuiRoot := filepath.Join(root, "internal", "tui")
	err := filepath.WalkDir(tuiRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if filepath.ToSlash(path[len(root)+1:]) != footerPath && strings.Contains(string(content), "tab to queue") {
			t.Errorf("queue hint presentation leaked outside footer boundary into %s", filepath.ToSlash(path[len(root)+1:]))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan queue hint presentation boundary: %v", err)
	}

	for _, relative := range []string{"internal/agent", "internal/contextmanager", "internal/rollout"} {
		err := filepath.WalkDir(filepath.Join(root, relative), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, forbidden := range []string{"HasQueueableDraft", "QueueHint", "tab to queue"} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("Runtime/canonical package owns queue hint %q in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan queue hint Runtime boundaries in %s: %v", relative, err)
		}
	}
}
