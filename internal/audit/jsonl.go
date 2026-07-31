package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const maxAuditTextRunes = 512

var (
	sensitiveAssignment = regexp.MustCompile(`(?i)(authorization|proxy-authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|password|client[_-]?secret|cookie|set-cookie)([[:space:]]*[:=][[:space:]]*)([^[:space:],;}]+)`)
	bearerCredential    = regexp.MustCompile(`(?i)bearer[[:space:]]+[A-Za-z0-9._~+/=-]+`)
)

type JSONLSink struct {
	writer io.Writer
	mutex  sync.Mutex
}

func NewJSONLSink(writer io.Writer) (*JSONLSink, error) {
	if writer == nil {
		return nil, errors.New("audit JSONL writer is nil")
	}
	return &JSONLSink{writer: writer}, nil
}

func (sink *JSONLSink) Write(ctx context.Context, record Record) error {
	if sink == nil {
		return errors.New("audit JSONL sink is nil")
	}
	if ctx == nil {
		return errors.New("audit context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record = redactRecord(record)
	if err := record.Validate(); err != nil {
		return err
	}
	var line bytes.Buffer
	encoder := json.NewEncoder(&line)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("encode audit JSONL record: %w", err)
	}

	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	written, err := sink.writer.Write(line.Bytes())
	if err != nil {
		return fmt.Errorf("write audit JSONL record: %w", err)
	}
	if written != line.Len() {
		return io.ErrShortWrite
	}
	return nil
}

func redactRecord(record Record) Record {
	record.SessionID = redactAuditText(record.SessionID)
	record.RequestID = redactAuditText(record.RequestID)
	record.ToolName = redactAuditText(record.ToolName)
	record.Risk = redactAuditText(record.Risk)
	record.Scope = redactAuditText(record.Scope)
	record.Source = redactAuditText(record.Source)
	record.Reason = redactAuditText(record.Reason)
	return record
}

func redactAuditText(value string) string {
	value = bearerCredential.ReplaceAllString(value, "Bearer [REDACTED]")
	value = sensitiveAssignment.ReplaceAllString(value, "$1$2[REDACTED]")
	var sanitized strings.Builder
	runes := 0
	for len(value) > 0 && runes < maxAuditTextRunes {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		if r == utf8.RuneError && size == 1 {
			r = '?'
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		sanitized.WriteRune(r)
		runes++
	}
	if value != "" {
		sanitized.WriteString("…")
	}
	return sanitized.String()
}

var _ Sink = (*JSONLSink)(nil)
