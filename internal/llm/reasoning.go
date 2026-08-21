package llm

type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	ReasoningEffortMax     ReasoningEffort = "max"
)

func (effort ReasoningEffort) Valid() bool {
	switch effort {
	case ReasoningEffortNone,
		ReasoningEffortMinimal,
		ReasoningEffortLow,
		ReasoningEffortMedium,
		ReasoningEffortHigh,
		ReasoningEffortXHigh,
		ReasoningEffortMax:
		return true
	default:
		return false
	}
}

func CloneReasoningEffort(effort *ReasoningEffort) *ReasoningEffort {
	if effort == nil {
		return nil
	}
	cloned := *effort
	return &cloned
}

type ReasoningConfig struct {
	Effort *ReasoningEffort
}

func ReasoningConfigForEffort(effort *ReasoningEffort) *ReasoningConfig {
	if effort == nil {
		return nil
	}
	return &ReasoningConfig{Effort: CloneReasoningEffort(effort)}
}

func (config *ReasoningConfig) Clone() *ReasoningConfig {
	if config == nil {
		return nil
	}
	return &ReasoningConfig{Effort: CloneReasoningEffort(config.Effort)}
}
