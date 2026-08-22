package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ThreadID identifies one concrete conversation thread and its durable rollout.
type ThreadID struct {
	value uuid.UUID
}

func NewThreadID() (ThreadID, error) {
	value, err := uuid.NewV7()
	if err != nil {
		return ThreadID{}, fmt.Errorf("generate UUIDv7 thread ID: %w", err)
	}
	return ThreadID{value: value}, nil
}

func ParseThreadID(value string) (ThreadID, error) {
	parsed, err := parseUUID("thread ID", value)
	if err != nil {
		return ThreadID{}, err
	}
	return ThreadID{value: parsed}, nil
}

func (id ThreadID) String() string {
	if id.IsZero() {
		return ""
	}
	return id.value.String()
}

func (id ThreadID) IsZero() bool { return id.value == uuid.Nil }

func (id ThreadID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

func (id *ThreadID) UnmarshalText(text []byte) error {
	if id == nil {
		return errors.New("unmarshal thread ID into nil receiver")
	}
	parsed, err := ParseThreadID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func (id ThreadID) MarshalJSON() ([]byte, error) { return json.Marshal(id.String()) }

func (id *ThreadID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return errors.New("unmarshal thread ID into nil receiver")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*id = ThreadID{}
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode thread ID: %w", err)
	}
	return id.UnmarshalText([]byte(value))
}

func parseUUID(kind, value string) (uuid.UUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.Nil, fmt.Errorf("%s is empty", kind)
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse %s: %w", kind, err)
	}
	if parsed == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s is zero", kind)
	}
	return parsed, nil
}
