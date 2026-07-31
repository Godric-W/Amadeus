package audit

import (
	"context"
	"errors"
	"sync"
)

type MemorySink struct {
	mutex   sync.Mutex
	records []Record
	err     error
}

func NewMemorySink() *MemorySink { return &MemorySink{} }

func (sink *MemorySink) Write(ctx context.Context, record Record) error {
	if sink == nil {
		return errors.New("audit memory sink is nil")
	}
	if ctx == nil {
		return errors.New("audit context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if sink.err != nil {
		return sink.err
	}
	sink.records = append(sink.records, record)
	return nil
}

func (sink *MemorySink) Snapshot() []Record {
	if sink == nil {
		return nil
	}
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return append([]Record(nil), sink.records...)
}

func (sink *MemorySink) SetError(err error) {
	if sink == nil {
		return
	}
	sink.mutex.Lock()
	sink.err = err
	sink.mutex.Unlock()
}

var _ Sink = (*MemorySink)(nil)
