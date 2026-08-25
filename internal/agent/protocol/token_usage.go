package protocol

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/llm"
)

// TokenUsageInfo separates cumulative request consumption from the most
// recent provider-observed context baseline.
type TokenUsageInfo struct {
	TotalTokenUsage    llm.TokenUsage `json:"total_token_usage"`
	LastTokenUsage     llm.TokenUsage `json:"last_token_usage"`
	ModelContextWindow int64          `json:"model_context_window"`
}

func (info TokenUsageInfo) Clone() TokenUsageInfo { return info }

func NewTokenCountEvent(usage llm.TokenUsage, contextWindow int64, observedThrough uint64) TokenCountEvent {
	info := &TokenUsageInfo{TotalTokenUsage: usage, LastTokenUsage: usage, ModelContextWindow: contextWindow}
	return TokenCountEvent{Info: info, ActiveContextTokens: usage.TotalTokens, ObservedThroughSequence: observedThrough}
}

func (info *TokenUsageInfo) Append(usage llm.TokenUsage, contextWindow int64) {
	if info == nil {
		return
	}
	info.TotalTokenUsage.Add(usage)
	info.LastTokenUsage = usage
	if contextWindow > 0 {
		info.ModelContextWindow = contextWindow
	}
}

func (info TokenUsageInfo) Validate() error {
	if info.ModelContextWindow < 0 {
		return errors.New("token usage model context window is negative")
	}
	if !validTokenUsage(info.TotalTokenUsage) || !validTokenUsage(info.LastTokenUsage) {
		return errors.New("token usage contains a negative value")
	}
	if info.TotalTokenUsage.InputTokens < info.LastTokenUsage.InputTokens ||
		info.TotalTokenUsage.CachedInputTokens < info.LastTokenUsage.CachedInputTokens ||
		info.TotalTokenUsage.OutputTokens < info.LastTokenUsage.OutputTokens ||
		info.TotalTokenUsage.ReasoningTokens < info.LastTokenUsage.ReasoningTokens ||
		info.TotalTokenUsage.TotalTokens < info.LastTokenUsage.TotalTokens {
		return errors.New("total token usage is smaller than last token usage")
	}
	if info.TotalTokenUsage.CachedInputTokens > info.TotalTokenUsage.InputTokens || info.LastTokenUsage.CachedInputTokens > info.LastTokenUsage.InputTokens {
		return errors.New("cached input tokens exceed input tokens")
	}
	return nil
}

func validTokenUsage(usage llm.TokenUsage) bool {
	return usage.InputTokens >= 0 && usage.CachedInputTokens >= 0 &&
		usage.OutputTokens >= 0 && usage.ReasoningTokens >= 0 && usage.TotalTokens >= 0
}
