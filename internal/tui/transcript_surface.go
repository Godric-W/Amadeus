package tui

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type streamAttachment struct {
	itemID   protocol.ItemID
	kind     protocol.ItemKind
	runStart int
}

// TranscriptSurface is the single model-owned history surface. Every
// HistoryCell stays here until attachment replacement or clear, while viewport
// state controls which derived visual lines enter the bounded Bubble Tea frame.
type TranscriptSurface struct {
	sessionHeader        HistoryCell
	sessionHeaderPrinted bool
	historyCells         []HistoryCell
	historyPrintCursor   int
	printedVisible       bool
	lastPrintedCell      HistoryCell
	activeMarkdownTail   *StreamingAgentTailCell
	activeStream         *streamAttachment
	viewport             transcriptViewportState
}

type historyPrintCell struct {
	cell             HistoryCell
	leadingBlankRows int
}

func (surface *TranscriptSurface) setSessionHeader(header HistoryCell) {
	if surface != nil {
		surface.sessionHeader = header
	}
}

func (surface *TranscriptSurface) takePrintableCells() []historyPrintCell {
	if surface == nil {
		return nil
	}
	prints := make([]historyPrintCell, 0)
	appendCell := func(cell HistoryCell) {
		if cell == nil {
			return
		}
		leadingBlankRows := 0
		if surface.printedVisible {
			leadingBlankRows = historyBoundaryBlankRows(surface.lastPrintedCell, cell)
		}
		prints = append(prints, historyPrintCell{cell: cell, leadingBlankRows: leadingBlankRows})
		surface.printedVisible = true
		surface.lastPrintedCell = cell
	}
	if !surface.sessionHeaderPrinted && surface.sessionHeader != nil {
		appendCell(surface.sessionHeader)
		surface.sessionHeaderPrinted = true
	}
	for surface.historyPrintCursor < len(surface.historyCells) {
		cell := surface.historyCells[surface.historyPrintCursor]
		if transientStreamHistoryCell(cell) {
			break
		}
		appendCell(cell)
		surface.historyPrintCursor++
	}
	return prints
}

func transientStreamHistoryCell(cell HistoryCell) bool {
	switch cell.(type) {
	case AgentMessageCell, *AgentMessageCell:
		return true
	default:
		return false
	}
}

func (surface TranscriptSurface) unprintedCells(activeCell ActiveHistoryCell) []HistoryCell {
	cells := make([]HistoryCell, 0, len(surface.historyCells)-minInt(surface.historyPrintCursor, len(surface.historyCells))+2)
	start := minInt(maxInt(0, surface.historyPrintCursor), len(surface.historyCells))
	cells = append(cells, surface.historyCells[start:]...)
	if activeCell != nil {
		cells = append(cells, activeCell)
	}
	if surface.activeMarkdownTail != nil {
		cells = append(cells, surface.activeMarkdownTail)
	}
	return cells
}

func (surface TranscriptSurface) render(cells []HistoryCell, mode HistoryRenderMode, ctx HistoryRenderContext, height int) string {
	rendered := renderHistoryCells(cells, mode, ctx)
	if surface.printedVisible && len(cells) > 0 && rendered != "" {
		rendered = strings.Repeat("\n", historyBoundaryBlankRows(surface.lastPrintedCell, cells[0])) + rendered
	}
	return surface.viewport.project(rendered, height)
}

func (surface *TranscriptSurface) scroll(cells []HistoryCell, mode HistoryRenderMode, ctx HistoryRenderContext, height, delta int) bool {
	if surface == nil {
		return false
	}
	rendered := renderHistoryCells(cells, mode, ctx)
	if surface.printedVisible && len(cells) > 0 && rendered != "" {
		rendered = strings.Repeat("\n", historyBoundaryBlankRows(surface.lastPrintedCell, cells[0])) + rendered
	}
	return surface.viewport.scroll(rendered, height, delta)
}

func (surface *TranscriptSurface) reset() {
	if surface == nil {
		return
	}
	*surface = TranscriptSurface{}
}

// reflowCells returns the immutable history prefix for terminal scrollback.
// A transient stream run remains in the active frame until its authoritative
// completed item replaces the attachment.
func (surface TranscriptSurface) reflowCells() []HistoryCell {
	cells := make([]HistoryCell, 0, len(surface.historyCells)+1)
	if surface.sessionHeader != nil {
		cells = append(cells, surface.sessionHeader)
	}
	for _, cell := range surface.historyCells {
		if transientStreamHistoryCell(cell) {
			break
		}
		cells = append(cells, cell)
	}
	return cells
}

// markReflowed advances the native print watermark to the first transient
// stream cell, or to the end when all retained cells are immutable.
func (surface *TranscriptSurface) markReflowed() {
	if surface == nil {
		return
	}
	surface.sessionHeaderPrinted = surface.sessionHeader != nil
	surface.historyPrintCursor = len(surface.historyCells)
	for index, cell := range surface.historyCells {
		if transientStreamHistoryCell(cell) {
			surface.historyPrintCursor = index
			break
		}
	}
	surface.printedVisible = false
	surface.lastPrintedCell = nil
	for _, cell := range surface.reflowCells() {
		surface.printedVisible = true
		surface.lastPrintedCell = cell
	}
}

func (surface *TranscriptSurface) setMarkdownTail(tail *StreamingAgentTailCell) {
	if surface != nil {
		surface.activeMarkdownTail = tail
	}
}

func (surface *TranscriptSurface) clearMarkdownTail(itemID protocol.ItemID) {
	if surface != nil && surface.activeMarkdownTail != nil && surface.activeMarkdownTail.ItemID == itemID {
		surface.activeMarkdownTail = nil
	}
}

func (surface *TranscriptSurface) beginStream(itemID protocol.ItemID, kind protocol.ItemKind) error {
	if surface == nil {
		return errors.New("transcript surface is nil")
	}
	if surface.activeStream != nil {
		if surface.activeStream.itemID == itemID && surface.activeStream.kind == kind {
			return nil
		}
		return errors.New("another transcript stream attachment is active")
	}
	surface.activeStream = &streamAttachment{itemID: itemID, kind: kind, runStart: len(surface.historyCells)}
	return nil
}

func (surface *TranscriptSurface) rewindStream(itemID protocol.ItemID) bool {
	if surface == nil || surface.activeStream == nil || surface.activeStream.itemID != itemID {
		return false
	}
	start := minInt(maxInt(0, surface.activeStream.runStart), len(surface.historyCells))
	surface.historyCells = surface.historyCells[:start]
	surface.clearMarkdownTail(itemID)
	return true
}

func (surface *TranscriptSurface) discardStream(itemID protocol.ItemID) {
	if surface == nil || surface.activeStream == nil || surface.activeStream.itemID != itemID {
		return
	}
	surface.rewindStream(itemID)
	surface.activeStream = nil
}

func (surface *TranscriptSurface) consolidateStream(itemID protocol.ItemID, final HistoryCell) {
	if surface == nil || final == nil {
		return
	}
	if surface.activeStream == nil || surface.activeStream.itemID != itemID {
		surface.historyCells = append(surface.historyCells, final)
		surface.clearMarkdownTail(itemID)
		return
	}
	start := minInt(maxInt(0, surface.activeStream.runStart), len(surface.historyCells))
	surface.historyCells = append(surface.historyCells[:start], final)
	surface.clearMarkdownTail(itemID)
	surface.activeStream = nil
}
