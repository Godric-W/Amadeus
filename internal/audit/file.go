package audit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type JSONLFile struct {
	sink *JSONLSink
	file *os.File
}

func OpenJSONLFile(path string) (*JSONLFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("audit JSONL file path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve audit JSONL path: %w", err)
	}
	if info, err := os.Lstat(absolute); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("audit JSONL file cannot be a symlink: %q", absolute)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect audit JSONL file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, fmt.Errorf("create audit JSONL directory: %w", err)
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit JSONL file: %w", err)
	}
	closeOnError := func(cause error) (*JSONLFile, error) {
		_ = file.Close()
		return nil, cause
	}
	info, err := file.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("stat audit JSONL file: %w", err))
	}
	if !info.Mode().IsRegular() {
		return closeOnError(fmt.Errorf("audit JSONL path is not a regular file: %q", absolute))
	}
	if err := file.Chmod(0o600); err != nil {
		return closeOnError(fmt.Errorf("secure audit JSONL file permissions: %w", err))
	}
	sink, err := NewJSONLSink(file)
	if err != nil {
		return closeOnError(err)
	}
	return &JSONLFile{sink: sink, file: file}, nil
}

func (file *JSONLFile) Write(ctx context.Context, record Record) error {
	if file == nil || file.sink == nil {
		return errors.New("audit JSONL file is nil")
	}
	return file.sink.Write(ctx, record)
}

func (file *JSONLFile) Close() error {
	if file == nil || file.sink == nil || file.file == nil {
		return nil
	}
	file.sink.mutex.Lock()
	defer file.sink.mutex.Unlock()
	err := file.file.Close()
	file.file = nil
	file.sink.writer = closedAuditWriter{}
	return err
}

type closedAuditWriter struct{}

func (closedAuditWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

var _ Sink = (*JSONLFile)(nil)
