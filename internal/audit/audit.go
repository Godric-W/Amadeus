package audit

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Outcome string

const (
	OutcomeAllow Outcome = "allow"
	OutcomeDeny  Outcome = "deny"
	OutcomeError Outcome = "error"
)

type Record struct {
	Timestamp       time.Time `json:"timestamp"`
	SessionID       string    `json:"session_id,omitempty"`
	RequestID       string    `json:"request_id"`
	ToolName        string    `json:"tool_name"`
	ArgumentsSHA256 string    `json:"arguments_sha256,omitempty"`
	Risk            string    `json:"risk,omitempty"`
	Outcome         Outcome   `json:"outcome"`
	Scope           string    `json:"scope,omitempty"`
	Source          string    `json:"source"`
	Reason          string    `json:"reason"`
	DurationMS      int64     `json:"duration_ms"`
}

func (record Record) Validate() error {
	if record.Timestamp.IsZero() {
		return errors.New("audit timestamp is zero")
	}
	if strings.TrimSpace(record.RequestID) == "" {
		return errors.New("audit request ID is empty")
	}
	if strings.TrimSpace(record.ToolName) == "" {
		return errors.New("audit tool name is empty")
	}
	if record.ArgumentsSHA256 != "" {
		decoded, err := hex.DecodeString(record.ArgumentsSHA256)
		if err != nil || len(decoded) != 32 || record.ArgumentsSHA256 != strings.ToLower(record.ArgumentsSHA256) {
			return errors.New("audit arguments SHA-256 is invalid")
		}
	}
	switch record.Outcome {
	case OutcomeAllow, OutcomeDeny, OutcomeError:
	default:
		return fmt.Errorf("audit outcome %q is invalid", record.Outcome)
	}
	if strings.TrimSpace(record.Source) == "" {
		return errors.New("audit source is empty")
	}
	if strings.TrimSpace(record.Reason) == "" {
		return errors.New("audit reason is empty")
	}
	if record.DurationMS < 0 {
		return errors.New("audit duration cannot be negative")
	}
	return nil
}

type Sink interface {
	Write(context.Context, Record) error
}
