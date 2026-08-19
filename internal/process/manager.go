package process

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type ID string

type State string

const (
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateCancelled State = "cancelled"
	StateTimedOut  State = "timed_out"
	StateFailed    State = "failed"
)

var ErrNotFound = errors.New("process not found")
var ErrOwnerMismatch = errors.New("process owner mismatch")

type Command struct {
	OriginCallID   string
	Attribution    Attribution
	Shell          string
	Command        string
	Executable     string
	Arguments      []string
	Directory      string
	Timeout        time.Duration
	TTY            bool
	MaxOutputBytes int
}

type Attribution struct {
	Name     string
	Resource string
	Revision string
}

type Snapshot struct {
	ID               ID
	OriginCallID     string
	Attribution      Attribution
	Owner            string
	State            State
	Output           string
	ExitCode         int
	StartedAt        time.Time
	FinishedAt       time.Time
	TotalOutputBytes int64
	OutputTruncated  bool
	Error            string
}

type Manager struct {
	mutex     sync.RWMutex
	processes map[ID]*managed
}

type managed struct {
	mutex        sync.Mutex
	ioMutex      sync.Mutex
	id           ID
	originCallID string
	attribution  Attribution
	owner        string
	command      *exec.Cmd
	stdin        io.WriteCloser
	cancel       context.CancelFunc
	output       *transcript
	state        State
	exitCode     int
	startedAt    time.Time
	finishedAt   time.Time
	err          error
	done         chan struct{}
}

func NewManager() *Manager {
	return &Manager{processes: make(map[ID]*managed)}
}

func (manager *Manager) Start(owner string, command Command, configure func(*exec.Cmd)) (ID, error) {
	if manager == nil {
		return "", errors.New("process manager is nil")
	}
	if command.Directory == "" || command.Timeout <= 0 || command.MaxOutputBytes <= 0 || (command.Executable == "" && (command.Shell == "" || command.Command == "")) {
		return "", errors.New("process command is invalid")
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	processCtx, cancel := context.WithTimeout(context.Background(), command.Timeout)
	executable := command.Executable
	arguments := append([]string(nil), command.Arguments...)
	if executable == "" {
		executable = command.Shell
		arguments = []string{"-c", command.Command}
	}
	cmd := exec.CommandContext(processCtx, executable, arguments...)
	cmd.Dir = command.Directory
	configureManagedCommand(cmd, command.TTY)
	if configure != nil {
		configure(cmd)
	}
	output := newTranscript(command.MaxOutputBytes)
	stdin, err := startProcess(cmd, command.TTY, output)
	if err != nil {
		cancel()
		return "", err
	}
	value := &managed{
		id: id, originCallID: command.OriginCallID, attribution: command.Attribution, owner: owner, command: cmd, stdin: stdin, cancel: cancel, output: output,
		state: StateRunning, exitCode: -1, startedAt: time.Now(), done: make(chan struct{}),
	}
	manager.mutex.Lock()
	manager.processes[id] = value
	manager.mutex.Unlock()
	go value.wait(processCtx)
	return id, nil
}

func (manager *Manager) Snapshot(id ID, owner string, wait time.Duration) (Snapshot, error) {
	return manager.SnapshotContext(context.Background(), id, owner, wait)
}

func (manager *Manager) SnapshotContext(ctx context.Context, id ID, owner string, wait time.Duration) (Snapshot, error) {
	value, err := manager.lookup(id, owner)
	if err != nil {
		return Snapshot{}, err
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-value.done:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return Snapshot{}, ctx.Err()
		}
	}
	return value.snapshot(), nil
}

func (manager *Manager) Write(id ID, owner, chars string, eof bool, wait time.Duration) (Snapshot, error) {
	return manager.WriteContext(context.Background(), id, owner, chars, eof, wait)
}

func (manager *Manager) WriteContext(ctx context.Context, id ID, owner, chars string, eof bool, wait time.Duration) (Snapshot, error) {
	value, err := manager.lookup(id, owner)
	if err != nil {
		return Snapshot{}, err
	}
	value.ioMutex.Lock()
	defer value.ioMutex.Unlock()
	value.mutex.Lock()
	if value.state != StateRunning {
		value.mutex.Unlock()
		return value.snapshot(), nil
	}
	stdin := value.stdin
	value.mutex.Unlock()
	if chars != "" {
		if _, err := io.WriteString(stdin, chars); err != nil {
			return Snapshot{}, fmt.Errorf("write process stdin: %w", err)
		}
	}
	if eof {
		if err := stdin.Close(); err != nil {
			return Snapshot{}, fmt.Errorf("close process stdin: %w", err)
		}
	}
	return manager.SnapshotContext(ctx, id, owner, wait)
}

func (manager *Manager) Cancel(id ID, owner string) error {
	value, err := manager.lookup(id, owner)
	if err != nil {
		return err
	}
	value.cancel()
	return nil
}

func (manager *Manager) CloseOwner(owner string) {
	if manager == nil {
		return
	}
	manager.mutex.RLock()
	values := make([]*managed, 0)
	for _, value := range manager.processes {
		if value.owner == owner {
			values = append(values, value)
		}
	}
	manager.mutex.RUnlock()
	for _, value := range values {
		value.cancel()
	}
}

func (manager *Manager) Close() {
	if manager == nil {
		return
	}
	manager.mutex.RLock()
	values := make([]*managed, 0, len(manager.processes))
	for _, value := range manager.processes {
		values = append(values, value)
	}
	manager.mutex.RUnlock()
	for _, value := range values {
		value.cancel()
	}
}

func (manager *Manager) lookup(id ID, owner string) (*managed, error) {
	manager.mutex.RLock()
	value, exists := manager.processes[id]
	manager.mutex.RUnlock()
	if !exists {
		return nil, ErrNotFound
	}
	if value.owner != owner {
		return nil, ErrOwnerMismatch
	}
	return value, nil
}

func (value *managed) wait(processCtx context.Context) {
	err := value.command.Wait()
	contextErr := processCtx.Err()
	value.cancel()
	value.mutex.Lock()
	defer value.mutex.Unlock()
	value.finishedAt = time.Now()
	value.err = err
	_ = value.stdin.Close()
	if value.command.ProcessState != nil {
		value.exitCode = value.command.ProcessState.ExitCode()
	}
	switch {
	case errors.Is(contextErr, context.DeadlineExceeded):
		value.state = StateTimedOut
	case errors.Is(contextErr, context.Canceled) && err != nil:
		value.state = StateCancelled
	case err != nil:
		value.state = StateFailed
	default:
		value.state = StateCompleted
	}
	close(value.done)
}

func (value *managed) snapshot() Snapshot {
	value.mutex.Lock()
	defer value.mutex.Unlock()
	output, total, truncated := value.output.snapshot()
	result := Snapshot{
		ID: value.id, OriginCallID: value.originCallID, Attribution: value.attribution, Owner: value.owner, State: value.state, Output: output, ExitCode: value.exitCode,
		StartedAt: value.startedAt, FinishedAt: value.finishedAt, TotalOutputBytes: total, OutputTruncated: truncated,
	}
	if value.err != nil {
		result.Error = value.err.Error()
	}
	return result
}

func newID() (ID, error) {
	content := make([]byte, 8)
	if _, err := rand.Read(content); err != nil {
		return "", fmt.Errorf("generate process ID: %w", err)
	}
	return ID("proc_" + hex.EncodeToString(content)), nil
}

type transcript struct {
	mutex sync.Mutex
	head  []byte
	tail  []byte
	limit int
	total int64
}

func newTranscript(limit int) *transcript { return &transcript{limit: limit} }

func (output *transcript) Write(content []byte) (int, error) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	originalLength := len(content)
	output.total += int64(originalLength)
	headLimit := output.limit / 2
	tailLimit := output.limit - headLimit
	if len(output.head) < headLimit {
		count := min(headLimit-len(output.head), len(content))
		output.head = append(output.head, content[:count]...)
		content = content[count:]
	}
	if len(content) > 0 {
		output.tail = append(output.tail, content...)
		if len(output.tail) > tailLimit {
			output.tail = append([]byte(nil), output.tail[len(output.tail)-tailLimit:]...)
		}
	}
	return originalLength, nil
}

func (output *transcript) snapshot() (string, int64, bool) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	truncated := output.total > int64(len(output.head)+len(output.tail))
	if !truncated {
		return string(append(append([]byte(nil), output.head...), output.tail...)), output.total, false
	}
	var buffer bytes.Buffer
	buffer.Write(output.head)
	buffer.WriteString("\n… output truncated …\n")
	buffer.Write(output.tail)
	return buffer.String(), output.total, true
}
