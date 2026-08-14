package agentcontext

import "github.com/Godric-W/Amadeus/internal/llm"

func NormalizeResponseItems(items []llm.ResponseItem, model llm.ModelInfo, estimator Estimator) []llm.ResponseItem {
	if estimator == nil {
		estimator = ConservativeEstimator{}
	}
	return normalizeHistory(cloneResponseItems(items), model.Normalized(), estimator)
}
