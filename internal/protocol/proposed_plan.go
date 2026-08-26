package protocol

type PlanDeltaEvent struct {
	ThreadID ThreadID
	TurnID   TurnID
	ItemID   ItemID
	Delta    string
}

func (PlanDeltaEvent) isEventMsg() {}
