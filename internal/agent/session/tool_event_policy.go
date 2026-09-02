package session

type toolEventPolicy struct {
	emitActivity bool
}

func eventPolicyForTool(name string) toolEventPolicy {
	return toolEventPolicy{emitActivity: !isControlTool(name)}
}

func isControlTool(name string) bool {
	switch name {
	case "update_plan", "get_goal", "create_goal", "update_goal":
		return true
	default:
		return false
	}
}
