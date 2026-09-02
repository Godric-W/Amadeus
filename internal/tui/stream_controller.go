package tui

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type StreamController struct {
	ItemID protocol.ItemID
	StreamCore
	plan          bool
	headerEmitted bool
}

// PlanStreamController shares the generic StreamCore but owns the Plan item
// presentation boundary. It avoids leaking plan-specific conditionals into
// Assistant stream lifecycle.
type PlanStreamController struct{ *StreamController }

// StreamCore is shared state for Assistant and Plan controllers. It owns one
// live attempt's source, incremental layout, stable queue boundaries, and
// viewport-dependent render inputs. Controllers add only Item identity and
// presentation policy.
type StreamCore struct {
	Source   MarkdownStreamCollector
	Render   StreamingRender
	State    StreamState
	CWD      string
	Mode     HistoryRenderMode
	renderer markdownRenderer
}

type StreamState struct {
	CommitQueue       []MarkdownLine
	EnqueuedStableLen int
	EmittedStableLen  int
	HasSeenDelta      bool
	RebuildSurface    bool
}

func newStreamController(itemID protocol.ItemID, cwd string, mode HistoryRenderMode) (*StreamController, error) {
	if strings.TrimSpace(string(itemID)) == "" {
		return nil, errors.New("stream controller item ID is empty")
	}
	return &StreamController{ItemID: itemID, StreamCore: StreamCore{CWD: cwd, Mode: mode, renderer: newMarkdownRenderer()}}, nil
}

func newPlanStreamController(itemID protocol.ItemID, cwd string, mode HistoryRenderMode) (*PlanStreamController, error) {
	controller, err := newStreamController(itemID, cwd, mode)
	if err != nil {
		return nil, err
	}
	controller.plan = true
	return &PlanStreamController{StreamController: controller}, nil
}

func (controller *StreamController) Push(itemID protocol.ItemID, delta string, reset bool) (bool, error) {
	if controller == nil || itemID != controller.ItemID {
		return false, errors.New("stream delta does not match active item")
	}
	if reset {
		controller.ResetAttempt()
	}
	if delta != "" {
		controller.State.HasSeenDelta = true
	}
	if !controller.Source.Push(delta) {
		return false, nil
	}
	if controller.Render.Append(controller.renderer, newMarkdownSource(controller.Source.CommittedSource(), controller.CWD), controller.Mode) {
		controller.State.CommitQueue = nil
		controller.State.EnqueuedStableLen = 0
		controller.State.EmittedStableLen = 0
		controller.State.RebuildSurface = true
		controller.headerEmitted = false
	}
	targetStableLen := controller.Render.StableRenderedLen
	committedSource := controller.Source.CommittedSource()
	pendingStart := minInt(controller.Render.StableSourceLen, len(committedSource))
	if tableStart := tableHoldbackStart(committedSource[pendingStart:]); tableStart >= 0 {
		tableStart += pendingStart
		prefix := newMarkdownSource(committedSource[:tableStart], controller.CWD)
		targetStableLen = len(controller.renderer.Render(prefix, controller.Mode))
	}
	controller.syncCommitQueue(targetStableLen)
	return true, nil
}

func (controller *StreamController) ResetAttempt() {
	if controller == nil {
		return
	}
	controller.Source.Reset()
	controller.Render.Reset()
	controller.State = StreamState{}
	controller.headerEmitted = false
}

func (controller *StreamController) TailLines() []MarkdownLine {
	if controller == nil {
		return nil
	}
	start := minInt(controller.State.EnqueuedStableLen, len(controller.Render.Lines))
	return cloneMarkdownLines(controller.Render.Lines[start:])
}

func (controller *StreamController) DrainStable() *AgentMessageCell {
	if controller == nil {
		return nil
	}
	if len(controller.State.CommitQueue) == 0 {
		return nil
	}
	lines := cloneMarkdownLines(controller.State.CommitQueue)
	controller.State.CommitQueue = nil
	controller.State.EmittedStableLen += len(lines)
	cell := &AgentMessageCell{ItemID: controller.ItemID, Lines: lines, First: !controller.headerEmitted, Plan: controller.plan}
	controller.headerEmitted = true
	return cell
}

func (controller *StreamController) syncCommitQueue(target int) {
	if controller == nil {
		return
	}
	target = minInt(maxInt(controller.State.EmittedStableLen, target), len(controller.Render.Lines))
	if target < controller.State.EnqueuedStableLen {
		controller.State.CommitQueue = cloneMarkdownLines(controller.Render.Lines[controller.State.EmittedStableLen:target])
		controller.State.EnqueuedStableLen = target
		return
	}
	if target > controller.State.EnqueuedStableLen {
		controller.State.CommitQueue = append(controller.State.CommitQueue, cloneMarkdownLines(controller.Render.Lines[controller.State.EnqueuedStableLen:target])...)
		controller.State.EnqueuedStableLen = target
	}
}

func (controller *StreamController) TailCell() *StreamingAgentTailCell {
	if controller == nil {
		return nil
	}
	lines := controller.TailLines()
	if len(lines) == 0 {
		return nil
	}
	return &StreamingAgentTailCell{ItemID: controller.ItemID, Lines: lines, First: !controller.headerEmitted, Plan: controller.plan}
}

func (controller *StreamController) Finalize(authoritative string) MarkdownSource {
	if controller == nil {
		return MarkdownSource{}
	}
	controller.ResetAttempt()
	return newMarkdownSource(authoritative, controller.CWD)
}

func cloneMarkdownLines(lines []MarkdownLine) []MarkdownLine {
	return cloneMarkdownLinesWithTables(lines, make(map[*MarkdownTable]*MarkdownTable))
}

func cloneMarkdownLinesWithTables(lines []MarkdownLine, tables map[*MarkdownTable]*MarkdownTable) []MarkdownLine {
	cloned := make([]MarkdownLine, len(lines))
	for index, line := range lines {
		cloned[index].Spans = append([]MarkdownSpan(nil), line.Spans...)
		cloned[index].InitialIndent = append([]MarkdownSpan(nil), line.InitialIndent...)
		cloned[index].SubsequentIndent = append([]MarkdownSpan(nil), line.SubsequentIndent...)
		cloned[index].Hyperlinks = append([]HyperlinkRange(nil), line.Hyperlinks...)
		cloned[index].BlockKind = line.BlockKind
		cloned[index].NoWrap = line.NoWrap
		if line.Table != nil {
			if clone, ok := tables[line.Table]; ok {
				cloned[index].Table = clone
			} else {
				cloned[index].Table = cloneMarkdownTableWithTables(line.Table, tables)
			}
		}
		cloned[index].TableRule = line.TableRule
	}
	return cloned
}

func cloneMarkdownTable(table *MarkdownTable) *MarkdownTable {
	return cloneMarkdownTableWithTables(table, make(map[*MarkdownTable]*MarkdownTable))
}

func cloneMarkdownTableWithTables(table *MarkdownTable, tables map[*MarkdownTable]*MarkdownTable) *MarkdownTable {
	if table == nil {
		return nil
	}
	if clone, ok := tables[table]; ok {
		return clone
	}
	clone := &MarkdownTable{Alignments: append([]TableAlignment(nil), table.Alignments...), Prefix: table.Prefix, SourceRange: table.SourceRange}
	tables[table] = clone
	clone.Header = cloneMarkdownTableCells(table.Header)
	clone.Spillover = cloneMarkdownTableCellsWithTables(table.Spillover, tables)
	clone.Rows = make([][]MarkdownTableCell, len(table.Rows))
	for index, row := range table.Rows {
		clone.Rows[index] = cloneMarkdownTableCellsWithTables(row, tables)
	}
	return clone
}

func cloneMarkdownTableCells(cells []MarkdownTableCell) []MarkdownTableCell {
	return cloneMarkdownTableCellsWithTables(cells, make(map[*MarkdownTable]*MarkdownTable))
}

func cloneMarkdownTableCellsWithTables(cells []MarkdownTableCell, tables map[*MarkdownTable]*MarkdownTable) []MarkdownTableCell {
	clone := make([]MarkdownTableCell, len(cells))
	for index, cell := range cells {
		clone[index] = MarkdownTableCell{
			Lines:        cloneMarkdownLinesWithTables(cell.Lines, tables),
			PlainText:    cell.PlainText,
			HardBreaks:   append([]int(nil), cell.HardBreaks...),
			Hyperlinks:   append([]HyperlinkRange(nil), cell.Hyperlinks...),
			DisplayWidth: cell.DisplayWidth,
		}
	}
	return clone
}

func (model *appModel) refreshActiveMarkdownFrames() {
	if model == nil {
		return
	}
	controllers := []*StreamController{model.markdownStreams.assistant}
	if model.markdownStreams.plan != nil {
		controllers = append(controllers, model.markdownStreams.plan.StreamController)
	}
	for _, controller := range controllers {
		if controller != nil {
			model.TranscriptSurface.rewindStream(controller.ItemID)
			controller.Mode = model.historyMode
			controller.Render.Recompute(controller.renderer, newMarkdownSource(controller.Source.CommittedSource(), controller.CWD), controller.Mode)
			controller.State.EmittedStableLen = 0
			controller.State.EnqueuedStableLen = 0
			controller.State.CommitQueue = nil
			controller.headerEmitted = false
			controller.syncCommitQueue(controller.Render.StableRenderedLen)
			model.commitMarkdownStream(controller)
		}
	}
}

func (model *appModel) startAssistantStream(item protocol.TurnItem) error {
	if model.markdownStreams.assistant != nil {
		return errors.New("assistant stream is already active")
	}
	controller, err := newStreamController(item.ID, model.session.Configuration.CWD, model.historyMode)
	if err != nil {
		return err
	}
	model.markdownStreams.assistant = controller
	return nil
}

func (model *appModel) startPlanStream(item protocol.TurnItem) error {
	if model.markdownStreams.plan != nil {
		return errors.New("plan stream is already active")
	}
	controller, err := newPlanStreamController(item.ID, model.session.Configuration.CWD, model.historyMode)
	if err != nil {
		return err
	}
	model.markdownStreams.plan = controller
	return nil
}

func (model *appModel) clearMarkdownStreams() {
	if model.markdownStreams.assistant != nil {
		model.TranscriptSurface.discardStream(model.markdownStreams.assistant.ItemID)
	}
	if model.markdownStreams.plan != nil {
		model.TranscriptSurface.discardStream(model.markdownStreams.plan.ItemID)
	}
	model.markdownStreams.assistant = nil
	model.markdownStreams.plan = nil
	model.markdownStreams.transcriptProtected = false
}

func (model *appModel) commitMarkdownStream(controller *StreamController) {
	if controller == nil {
		return
	}
	if controller.State.RebuildSurface {
		model.TranscriptSurface.rewindStream(controller.ItemID)
		controller.State.RebuildSurface = false
	}
	if cell := controller.DrainStable(); cell != nil {
		model.insertStreamHistoryCell(cell)
	}
	model.TranscriptSurface.setMarkdownTail(controller.TailCell())
}
