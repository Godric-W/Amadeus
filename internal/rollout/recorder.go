package rollout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Clock func() time.Time

type Recorder struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	threadID ThreadID
	next     uint64
	clock    Clock
	closed   bool
}

func Create(path string, threadID ThreadID, clock Clock) (*Recorder, error) {
	if err := validateID("thread", string(threadID)); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create rollout directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create rollout: %w", err)
	}
	return &Recorder{file: file, path: path, threadID: threadID, next: 1, clock: clock}, nil
}

func Open(path string, threadID ThreadID, clock Clock) (*Recorder, []Line, error) {
	if err := validateID("thread", string(threadID)); err != nil {
		return nil, nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open rollout: %w", err)
	}
	lines, validSize, err := readLines(file, threadID)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("stat rollout: %w", err)
	}
	repaired := false
	if validSize < info.Size() {
		if err := file.Truncate(validSize); err != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("truncate damaged rollout tail: %w", err)
		}
		repaired = true
	}
	if validSize > 0 {
		var tail [1]byte
		if _, err := file.ReadAt(tail[:], validSize-1); err != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("read rollout tail: %w", err)
		}
		if tail[0] != '\n' {
			if _, err := file.WriteAt([]byte{'\n'}, validSize); err != nil {
				_ = file.Close()
				return nil, nil, fmt.Errorf("repair rollout line terminator: %w", err)
			}
			validSize++
			repaired = true
		}
	}
	if repaired {
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("flush repaired rollout: %w", err)
		}
	}
	if _, err := file.Seek(0, 2); err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("seek rollout end: %w", err)
	}
	return &Recorder{file: file, path: path, threadID: threadID, next: uint64(len(lines)) + 1, clock: clock}, lines, nil
}

func Read(path string, threadID ThreadID) ([]Line, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rollout: %w", err)
	}
	defer file.Close()
	lines, _, err := readLines(file, threadID)
	return lines, err
}

func (recorder *Recorder) Append(ctx context.Context, turnID TurnID, items ...Item) ([]Line, error) {
	if recorder == nil {
		return nil, errors.New("rollout recorder is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return nil, errors.New("rollout recorder is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lines := make([]Line, len(items))
	var encoded bytes.Buffer
	for index, item := range items {
		line := Line{
			Version: CurrentVersion, Sequence: recorder.next + uint64(index), Timestamp: recorder.clock().UTC(),
			ThreadID: recorder.threadID, TurnID: turnID, Item: item,
		}
		if err := line.Validate(recorder.threadID, line.Sequence); err != nil {
			return nil, err
		}
		content, err := json.Marshal(line)
		if err != nil {
			return nil, fmt.Errorf("encode rollout line: %w", err)
		}
		encoded.Write(content)
		encoded.WriteByte('\n')
		lines[index] = line
	}
	if len(lines) == 0 {
		return lines, nil
	}
	offset, err := recorder.file.Seek(0, 2)
	if err != nil {
		return nil, fmt.Errorf("seek rollout end: %w", err)
	}
	if err := writeAll(recorder.file, encoded.Bytes()); err != nil {
		_ = recorder.file.Truncate(offset)
		_, _ = recorder.file.Seek(0, 2)
		return nil, fmt.Errorf("append rollout: %w", err)
	}
	recorder.next += uint64(len(lines))
	return lines, nil
}

func (recorder *Recorder) Flush(ctx context.Context) error {
	if recorder == nil {
		return errors.New("rollout recorder is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return errors.New("rollout recorder is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := recorder.file.Sync(); err != nil {
		return fmt.Errorf("flush rollout: %w", err)
	}
	return nil
}

func (recorder *Recorder) Close(ctx context.Context) error {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return nil
	}
	contextErr := ctx.Err()
	flushErr := recorder.file.Sync()
	closeErr := recorder.file.Close()
	recorder.closed = true
	if flushErr != nil {
		flushErr = fmt.Errorf("flush rollout before close: %w", flushErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close rollout: %w", closeErr)
	}
	return errors.Join(contextErr, flushErr, closeErr)
}

func (recorder *Recorder) Path() string {
	if recorder == nil {
		return ""
	}
	return recorder.path
}

func readLines(file *os.File, threadID ThreadID) ([]Line, int64, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return nil, 0, fmt.Errorf("seek rollout start: %w", err)
	}
	content, err := os.ReadFile(file.Name())
	if err != nil {
		return nil, 0, fmt.Errorf("read rollout: %w", err)
	}
	parts := bytes.Split(content, []byte{'\n'})
	endsWithNewline := len(content) == 0 || content[len(content)-1] == '\n'
	lines := make([]Line, 0, len(parts))
	var validSize int64
	for index, part := range parts {
		if len(part) == 0 {
			if index == len(parts)-1 {
				continue
			}
			return nil, 0, fmt.Errorf("rollout line %d is empty", index+1)
		}
		var line Line
		if err := json.Unmarshal(part, &line); err != nil {
			if index == len(parts)-1 && !endsWithNewline {
				return lines, validSize, nil
			}
			return nil, 0, fmt.Errorf("decode rollout line %d: %w", index+1, err)
		}
		if err := line.Validate(threadID, uint64(len(lines))+1); err != nil {
			return nil, 0, fmt.Errorf("validate rollout line %d: %w", index+1, err)
		}
		lines = append(lines, line)
		validSize += int64(len(part))
		if index < len(parts)-1 || endsWithNewline {
			validSize++
		}
	}
	return lines, validSize, nil
}

func writeAll(file *os.File, content []byte) error {
	for len(content) > 0 {
		written, err := file.Write(content)
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("short rollout write")
		}
		content = content[written:]
	}
	return nil
}
