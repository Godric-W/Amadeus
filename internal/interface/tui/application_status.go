package tui

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func (model *fullscreenModel) showRetryStatus(event protocol.StreamErrorEvent) {
	if !model.retryStatus.active {
		model.retryStatus = savedStatus{header: model.status, details: model.statusDetails, active: true}
	}
	model.status = strings.TrimSpace(event.Message)
	if model.status == "" {
		model.status = "Reconnecting..."
	}
	model.statusDetails = ""
	if event.AdditionalDetails != nil {
		model.statusDetails = strings.TrimSpace(*event.AdditionalDetails)
	}
}

func (model *fullscreenModel) restoreRetryStatus() {
	if !model.retryStatus.active {
		return
	}
	model.status = model.retryStatus.header
	model.statusDetails = model.retryStatus.details
	model.retryStatus = savedStatus{}
}

func (model *fullscreenModel) clearRetryStatus() {
	model.retryStatus = savedStatus{}
	model.statusDetails = ""
}

func statusHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Working"
	}
	switch value {
	case "working":
		return "Working"
	case "thinking":
		return "Thinking"
	case "planning":
		return "Planning"
	case "compacting context":
		return "Compacting context"
	}
	return value
}
