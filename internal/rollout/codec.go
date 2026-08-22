package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

const CurrentVersion = 3

const (
	itemTypeSessionMeta = "session_meta"
	itemTypeResponse    = "response_item"
	itemTypeCompacted   = "compacted"
	itemTypeTurnContext = "turn_context"
	itemTypeEventMsg    = "event_msg"
)

type Line struct {
	Version   int
	Sequence  uint64
	Timestamp time.Time
	Item      RolloutItem
}

type lineWire struct {
	Version   int             `json:"version"`
	Sequence  uint64          `json:"sequence"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func (line Line) MarshalJSON() ([]byte, error) {
	if err := line.Validate(protocol.ThreadID{}, line.Sequence); err != nil {
		return nil, err
	}
	kind, payload, err := encodeItem(line.Item)
	if err != nil {
		return nil, err
	}
	return json.Marshal(lineWire{Version: line.Version, Sequence: line.Sequence, Timestamp: line.Timestamp, Type: kind, Payload: payload})
}

func (line *Line) UnmarshalJSON(content []byte) error {
	if line == nil {
		return errors.New("rollout line target is nil")
	}
	var wire lineWire
	if err := json.Unmarshal(content, &wire); err != nil {
		return fmt.Errorf("decode rollout line: %w", err)
	}
	if wire.Version != CurrentVersion {
		return fmt.Errorf("unsupported rollout format version %d", wire.Version)
	}
	if wire.Type == "" || len(wire.Payload) == 0 || !json.Valid(wire.Payload) {
		return errors.New("unsupported rollout item format")
	}
	item, err := decodeItem(wire.Type, wire.Payload)
	if err != nil {
		return err
	}
	*line = Line{Version: wire.Version, Sequence: wire.Sequence, Timestamp: wire.Timestamp, Item: item}
	return nil
}

func (line Line) Validate(expectedThreadID protocol.ThreadID, expectedSequence uint64) error {
	if line.Version != CurrentVersion {
		return fmt.Errorf("unsupported rollout format version %d", line.Version)
	}
	if line.Sequence != expectedSequence {
		return fmt.Errorf("rollout sequence is %d, expected %d", line.Sequence, expectedSequence)
	}
	if line.Timestamp.IsZero() {
		return errors.New("rollout timestamp is zero")
	}
	if line.Item == nil {
		return errors.New("rollout item is nil")
	}
	if err := line.Item.Validate(); err != nil {
		return err
	}
	threadID := ThreadIDOf(line.Item)
	if !expectedThreadID.IsZero() && threadID != expectedThreadID {
		return fmt.Errorf("rollout thread is %q, expected %q", threadID, expectedThreadID)
	}
	return nil
}

func CloneLines(source []Line) []Line {
	lines := make([]Line, len(source))
	for index, line := range source {
		line.Item = CloneItem(line.Item)
		lines[index] = line
	}
	return lines
}

func encodeItem(item RolloutItem) (string, json.RawMessage, error) {
	var kind string
	var payload any
	switch value := item.(type) {
	case SessionMetaItem:
		kind, payload = itemTypeSessionMeta, value
	case ResponseItem:
		kind, payload = itemTypeResponse, value
	case CompactedItem:
		kind, payload = itemTypeCompacted, value
	case TurnContextItem:
		kind, payload = itemTypeTurnContext, value
	case EventMsgItem:
		encoded, err := protocol.EncodeEventMsg(value.Msg)
		if err != nil {
			return "", nil, err
		}
		kind, payload = itemTypeEventMsg, encoded
	default:
		return "", nil, fmt.Errorf("unsupported rollout item %T", item)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("encode rollout item %q: %w", kind, err)
	}
	return kind, encoded, nil
}

func decodeItem(kind string, payload json.RawMessage) (RolloutItem, error) {
	switch kind {
	case itemTypeSessionMeta:
		return decodeTypedItem[SessionMetaItem](kind, payload)
	case itemTypeResponse:
		return decodeTypedItem[ResponseItem](kind, payload)
	case itemTypeCompacted:
		return decodeTypedItem[CompactedItem](kind, payload)
	case itemTypeTurnContext:
		return decodeTypedItem[TurnContextItem](kind, payload)
	case itemTypeEventMsg:
		var encoded protocol.EncodedEventMsg
		if err := json.Unmarshal(payload, &encoded); err != nil {
			return nil, fmt.Errorf("decode event_msg rollout item: %w", err)
		}
		message, err := protocol.DecodeEventMsg(encoded)
		if err != nil {
			return nil, err
		}
		return EventMsgItem{Msg: message}, nil
	default:
		return nil, fmt.Errorf("unsupported rollout item type %q", kind)
	}
}

func decodeTypedItem[T RolloutItem](kind string, payload json.RawMessage) (RolloutItem, error) {
	var item T
	if err := json.Unmarshal(payload, &item); err != nil {
		return nil, fmt.Errorf("decode rollout item %q: %w", kind, err)
	}
	if err := item.Validate(); err != nil {
		return nil, fmt.Errorf("validate rollout item %q: %w", kind, err)
	}
	return item, nil
}
