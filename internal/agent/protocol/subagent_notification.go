package protocol

type SubagentNotificationEvent struct {
	ThreadID ThreadID `json:"thread_id"`
	AgentID  ThreadID `json:"agent_id"`
	Content  string   `json:"content"`
}

func (SubagentNotificationEvent) isEventMsg() {}
