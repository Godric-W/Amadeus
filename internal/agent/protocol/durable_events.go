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

type InstructionDocumentRef struct {
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	SHA256 string `json:"sha256"`
}

type InstructionScopeResolution struct {
	TargetPath string                   `json:"target_path"`
	TargetKind string                   `json:"target_kind"`
	Documents  []InstructionDocumentRef `json:"documents,omitempty"`
}

type ContextUpdateEvent struct {
	ThreadID              ThreadID                    `json:"thread_id"`
	TurnID                TurnID                      `json:"turn_id,omitempty"`
	Key                   string                      `json:"key"`
	Content               string                      `json:"content,omitempty"`
	Revision              string                      `json:"revision,omitempty"`
	InstructionResolution *InstructionScopeResolution `json:"instruction_resolution,omitempty"`
}

func (ContextUpdateEvent) isEventMsg() {}
