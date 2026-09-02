package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

const proposedPlanOpen = "<proposed_plan>"
const proposedPlanClose = "</proposed_plan>"

type ProposedPlanStreamParser struct {
	state     int
	buffer    string
	assistant strings.Builder
	plan      strings.Builder
}

type ProposedPlanChunk struct {
	Assistant string
	Plan      string
}

func (parser *ProposedPlanStreamParser) Feed(delta string) ([]ProposedPlanChunk, error) {
	parser.buffer += delta
	var chunks []ProposedPlanChunk
	for {
		switch parser.state {
		case 0:
			if index := strings.Index(parser.buffer, proposedPlanOpen); index >= 0 {
				if index > 0 {
					chunks = append(chunks, parser.assistantChunk(parser.buffer[:index]))
				}
				parser.buffer = parser.buffer[index+len(proposedPlanOpen):]
				parser.state = 1
				continue
			}
			keep := longestTagPrefix(parser.buffer, proposedPlanOpen)
			if len(parser.buffer) > keep {
				chunks = append(chunks, parser.assistantChunk(parser.buffer[:len(parser.buffer)-keep]))
				parser.buffer = parser.buffer[len(parser.buffer)-keep:]
			}
			return chunks, nil
		case 1:
			if strings.Contains(parser.buffer, proposedPlanOpen) {
				return nil, errors.New("proposed plan block is nested")
			}
			if index := strings.Index(parser.buffer, proposedPlanClose); index >= 0 {
				if index > 0 {
					chunks = append(chunks, parser.planChunk(parser.buffer[:index]))
				}
				parser.buffer = parser.buffer[index+len(proposedPlanClose):]
				parser.state = 2
				continue
			}
			keep := longestTagPrefix(parser.buffer, proposedPlanClose)
			if len(parser.buffer) > keep {
				chunks = append(chunks, parser.planChunk(parser.buffer[:len(parser.buffer)-keep]))
				parser.buffer = parser.buffer[len(parser.buffer)-keep:]
			}
			return chunks, nil
		case 2:
			if strings.Contains(parser.buffer, proposedPlanOpen) || strings.Contains(parser.buffer, proposedPlanClose) {
				return nil, errors.New("proposed plan block is duplicated")
			}
			keep := maxIntValue(longestTagPrefix(parser.buffer, proposedPlanOpen), longestTagPrefix(parser.buffer, proposedPlanClose))
			if len(parser.buffer) > keep {
				chunks = append(chunks, parser.assistantChunk(parser.buffer[:len(parser.buffer)-keep]))
				parser.buffer = parser.buffer[len(parser.buffer)-keep:]
			}
			return chunks, nil
		}
	}
}

func (parser *ProposedPlanStreamParser) Flush() ([]ProposedPlanChunk, error) {
	if parser.state == 1 {
		return nil, errors.New("proposed plan block is not closed")
	}
	if parser.state == 0 {
		return nil, errors.New("response has no proposed plan block")
	}
	if strings.Contains(parser.buffer, "<proposed_plan") || strings.Contains(parser.buffer, "</proposed_plan") {
		return nil, errors.New("proposed plan tag is malformed")
	}
	if parser.buffer == "" {
		return nil, nil
	}
	chunk := parser.assistantChunk(parser.buffer)
	parser.buffer = ""
	return []ProposedPlanChunk{chunk}, nil
}

func (parser *ProposedPlanStreamParser) AssistantText() string { return parser.assistant.String() }
func (parser *ProposedPlanStreamParser) PlanText() string      { return parser.plan.String() }
func (parser *ProposedPlanStreamParser) assistantChunk(value string) ProposedPlanChunk {
	parser.assistant.WriteString(value)
	return ProposedPlanChunk{Assistant: value}
}
func (parser *ProposedPlanStreamParser) planChunk(value string) ProposedPlanChunk {
	parser.plan.WriteString(value)
	return ProposedPlanChunk{Plan: value}
}

func longestTagPrefix(value, tag string) int {
	for size := minIntValue(len(value), len(tag)-1); size > 0; size-- {
		if strings.HasSuffix(value, tag[:size]) {
			return size
		}
	}
	return 0
}
func minIntValue(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxIntValue(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type ProposedPlanEventSink struct {
	delegate protocol.EventSink
	parser   ProposedPlanStreamParser
	planID   protocol.ItemID
	started  bool
}

func NewProposedPlanEventSink(delegate protocol.EventSink, planID protocol.ItemID) (*ProposedPlanEventSink, error) {
	if delegate == nil || strings.TrimSpace(string(planID)) == "" {
		return nil, errors.New("proposed plan event sink is incomplete")
	}
	return &ProposedPlanEventSink{delegate: delegate, planID: planID}, nil
}

func (sink *ProposedPlanEventSink) Publish(ctx context.Context, event protocol.Event) error {
	delta, ok := event.Msg.(protocol.AgentMessageContentDeltaEvent)
	if !ok {
		return sink.delegate.Publish(ctx, event)
	}
	if delta.Reset {
		sink.parser = ProposedPlanStreamParser{}
		if sink.started {
			sink.started = false
			if err := sink.delegate.Publish(ctx, protocol.Event{ID: event.ID, Msg: protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: sink.planID, Kind: protocol.ItemPlan, Status: protocol.ItemInProgress, CreatedAt: time.Now().UTC()}}}); err != nil {
				return err
			}
		}
		return sink.delegate.Publish(ctx, event)
	}
	chunks, err := sink.parser.Feed(delta.Delta)
	if err != nil {
		return err
	}
	return sink.publishChunks(ctx, event.ID, delta, chunks)
}

func (sink *ProposedPlanEventSink) Flush(ctx context.Context) error {
	chunks, err := sink.parser.Flush()
	if err != nil {
		return err
	}
	return sink.publishChunks(ctx, "", protocol.AgentMessageContentDeltaEvent{}, chunks)
}

func (sink *ProposedPlanEventSink) AssistantText() string { return sink.parser.AssistantText() }
func (sink *ProposedPlanEventSink) PlanText() string      { return sink.parser.PlanText() }

func (sink *ProposedPlanEventSink) publishChunks(ctx context.Context, eventID protocol.EventID, source protocol.AgentMessageContentDeltaEvent, chunks []ProposedPlanChunk) error {
	for _, chunk := range chunks {
		if chunk.Assistant != "" {
			source.Delta = chunk.Assistant
			if err := sink.delegate.Publish(ctx, protocol.Event{ID: eventID, Msg: source}); err != nil {
				return err
			}
		}
		if chunk.Plan != "" {
			if !sink.started {
				sink.started = true
				now := timeNowUTC()
				if err := sink.delegate.Publish(ctx, protocol.Event{ID: eventID, Msg: protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: sink.planID, Kind: protocol.ItemPlan, Status: protocol.ItemInProgress, CreatedAt: now}}}); err != nil {
					return err
				}
			}
			if err := sink.delegate.Publish(ctx, protocol.Event{ID: eventID, Msg: protocol.PlanDeltaEvent{ItemID: sink.planID, Delta: chunk.Plan}}); err != nil {
				return fmt.Errorf("publish proposed plan delta: %w", err)
			}
		}
	}
	return nil
}

var timeNowUTC = func() time.Time { return time.Now().UTC() }
