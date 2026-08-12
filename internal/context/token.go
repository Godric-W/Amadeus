package agentcontext

// Estimator provides a provider-independent estimate used before a request is sent.
// Provider-reported usage remains authoritative after the request completes.
type Estimator interface {
	EstimateText(string) int64
}

// ConservativeEstimator intentionally overestimates text size so prompt projection
// leaves a safety margin when an exact provider tokenizer is unavailable.
type ConservativeEstimator struct{}

func (ConservativeEstimator) EstimateText(content string) int64 {
	if content == "" {
		return 0
	}
	return int64((len([]byte(content)) + 2) / 3)
}
