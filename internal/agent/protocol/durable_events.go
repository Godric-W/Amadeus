package protocol

type ThreadNameUpdatedEvent struct {
	ThreadID ThreadID `json:"thread_id"`
	Name     string   `json:"name"`
}

func (ThreadNameUpdatedEvent) isEventMsg() {}

type ThreadArchivedEvent struct {
	ThreadID ThreadID `json:"thread_id"`
	Archived bool     `json:"archived"`
}

func (ThreadArchivedEvent) isEventMsg() {}

type ContextUpdateEvent struct {
	ThreadID ThreadID `json:"thread_id"`
	TurnID   TurnID   `json:"turn_id,omitempty"`
	Key      string   `json:"key"`
	Content  string   `json:"content,omitempty"`
	Revision string   `json:"revision,omitempty"`
}

func (ContextUpdateEvent) isEventMsg() {}
