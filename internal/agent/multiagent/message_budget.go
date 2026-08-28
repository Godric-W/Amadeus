package multiagent

import (
	"strings"

	"github.com/Godric-W/Amadeus/internal/contextmanager"
)

const (
	statusMessageTokenLimit = int64(400)
	statusReasonTokenLimit  = int64(100)
)

func boundMessage(message string) string {
	return boundText(message, statusMessageTokenLimit)
}

func boundReason(message string) string {
	return boundText(message, statusReasonTokenLimit)
}

func boundText(message string, tokenLimit int64) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	estimator := contextmanager.ApproxTokenEstimator{}
	if estimator.EstimateText(message) <= tokenLimit {
		return message
	}
	runes := []rune(message)
	low, high := 0, len(runes)
	for low < high {
		middle := low + (high-low+1)/2
		if estimator.EstimateText(string(runes[:middle])+"…") <= tokenLimit {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return strings.TrimSpace(string(runes[:low])) + "…"
}
