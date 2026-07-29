package llm

type Request struct {
	Model           string
	Messages        []Message
	Temperature     float64
	MaxOutputTokens int
}

func NewRequest(model string, messages []Message) Request {
	return Request{
		Model:    model,
		Messages: append([]Message(nil), messages...),
	}
}
