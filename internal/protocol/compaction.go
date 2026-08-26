package protocol

type CompactionTrigger string

const (
	CompactionTriggerManual CompactionTrigger = "manual"
	CompactionTriggerAuto   CompactionTrigger = "auto"
)

func (value CompactionTrigger) Valid() bool {
	return value == CompactionTriggerManual || value == CompactionTriggerAuto
}

type CompactionReason string

const (
	CompactionReasonUserRequested CompactionReason = "user_requested"
	CompactionReasonContextLimit  CompactionReason = "context_limit"
)

func (value CompactionReason) Valid() bool {
	return value == CompactionReasonUserRequested || value == CompactionReasonContextLimit
}

type CompactionPhase string

const (
	CompactionPhaseStandaloneTurn CompactionPhase = "standalone_turn"
	CompactionPhasePreTurn        CompactionPhase = "pre_turn"
	CompactionPhaseMidTurn        CompactionPhase = "mid_turn"
)

func (value CompactionPhase) Valid() bool {
	return value == CompactionPhaseStandaloneTurn || value == CompactionPhasePreTurn || value == CompactionPhaseMidTurn
}

type ContextCompactionItem struct {
	Trigger CompactionTrigger `json:"trigger"`
	Reason  CompactionReason  `json:"reason"`
	Phase   CompactionPhase   `json:"phase"`
}

func (item ContextCompactionItem) Validate() bool {
	return item.Trigger.Valid() && item.Reason.Valid() && item.Phase.Valid()
}
