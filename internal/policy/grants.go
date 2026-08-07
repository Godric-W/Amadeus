package policy

import (
	"path/filepath"
	"strings"
	"sync"

	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
)

type CommandApprovalKey struct {
	Shell         string
	Command       string
	CWD           string
	TTY           bool
	IsolationMode sandboxdomain.IsolationMode
}

func NewCommandApprovalKey(shell, command, cwd string, tty bool, isolation sandboxdomain.IsolationMode) (CommandApprovalKey, bool) {
	if strings.ContainsRune(command, '\x00') {
		return CommandApprovalKey{}, false
	}
	shell = filepath.Clean(strings.TrimSpace(shell))
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	command = strings.ReplaceAll(command, "\r\n", "\n")
	if shell == "." || cwd == "." || strings.TrimSpace(command) == "" || !filepath.IsAbs(shell) || !filepath.IsAbs(cwd) {
		return CommandApprovalKey{}, false
	}
	if isolation != sandboxdomain.IsolationSandboxed && isolation != sandboxdomain.IsolationUnsandboxed {
		return CommandApprovalKey{}, false
	}
	return CommandApprovalKey{Shell: shell, Command: command, CWD: cwd, TTY: tty, IsolationMode: isolation}, true
}

type SessionApprovalStore struct {
	mutex sync.RWMutex
	keys  map[CommandApprovalKey]struct{}
}

func NewSessionApprovalStore() *SessionApprovalStore {
	return &SessionApprovalStore{keys: make(map[CommandApprovalKey]struct{})}
}

func (store *SessionApprovalStore) IsApproved(key CommandApprovalKey) bool {
	if store == nil {
		return false
	}
	store.mutex.RLock()
	_, ok := store.keys[key]
	store.mutex.RUnlock()
	return ok
}

func (store *SessionApprovalStore) Approve(key CommandApprovalKey) {
	if store == nil {
		return
	}
	store.mutex.Lock()
	store.keys[key] = struct{}{}
	store.mutex.Unlock()
}

func (store *SessionApprovalStore) Clear() {
	if store == nil {
		return
	}
	store.mutex.Lock()
	store.keys = make(map[CommandApprovalKey]struct{})
	store.mutex.Unlock()
}

func (store *SessionApprovalStore) Count() int {
	if store == nil {
		return 0
	}
	store.mutex.RLock()
	count := len(store.keys)
	store.mutex.RUnlock()
	return count
}
