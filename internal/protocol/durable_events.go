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
