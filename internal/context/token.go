package agentcontext

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/llm"
)

// Estimator provides a provider-independent estimate used before a request is
// sent. Provider-reported usage remains authoritative after completion.
type Estimator interface {
	EstimateText(string) int64
}

// ApproxTokenEstimator uses the Codex four-byte heuristic for ASCII and a
// rune-aware floor for non-ASCII text. It is deliberately named approximate:
// no provider-independent heuristic is a tokenizer contract.
type ApproxTokenEstimator struct{}

func (ApproxTokenEstimator) EstimateText(content string) int64 {
	if content == "" {
		return 0
	}
	var asciiBytes, nonASCII int64
	for len(content) > 0 {
		r, size := utf8.DecodeRuneInString(content)
		if r < utf8.RuneSelf {
			asciiBytes++
		} else {
			nonASCII++
		}
		content = content[size:]
	}
	return (asciiBytes+3)/4 + nonASCII
}

func estimateResponseItem(item llm.ResponseItem, estimator Estimator) int64 {
	total := int64(4) + estimator.EstimateText(string(item.Role))
	total += estimator.EstimateText(item.Content)
	total += estimator.EstimateText(item.Reasoning)
	total += estimator.EstimateText(item.ToolCallID)
	for _, call := range item.ToolCalls {
		total += 4 + estimator.EstimateText(call.ID) + estimator.EstimateText(call.Name)
		total += estimator.EstimateText(string(call.Arguments))
	}
	for _, part := range item.Parts {
		switch part.Kind {
		case llm.ContentText:
			if item.Role != llm.RoleTool {
				total += estimator.EstimateText(part.Text)
			}
		case llm.ContentImage:
			if tokens, ok := toolContentPartTokenEstimate(item.Content, part); ok {
				total += tokens
			}
		}
	}
	return total
}

func EstimateResponseItem(item llm.ResponseItem, estimator Estimator) int64 {
	if estimator == nil {
		estimator = ApproxTokenEstimator{}
	}
	return estimateResponseItem(item, estimator)
}

func estimateToolSpecs(specs []llm.ToolSpec, estimator Estimator) int64 {
	var total int64
	for _, spec := range specs {
		total += 8 + estimator.EstimateText(spec.Name) + estimator.EstimateText(spec.Description)
		if json.Valid(spec.InputSchema) {
			total += estimator.EstimateText(string(spec.InputSchema))
		}
	}
	return total
}
