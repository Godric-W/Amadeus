package engine

type toolEventPolicy struct {
	emitActivity bool
}

func eventPolicyForTool(name string) toolEventPolicy {
	return toolEventPolicy{emitActivity: name != "update_plan"}
}
