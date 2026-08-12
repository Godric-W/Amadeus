package policy

import (
	"path/filepath"
	"strings"
	"sync"
)

// CommandApprovalKey is the smallest stable identity for a Claude-style
// "don't ask again" command grant. Shell, TTY and isolation are execution
// details, not part of the user's command rule.
type CommandApprovalKey struct {
	CWD     string
	Command string
}

func NewCommandApprovalKey(command, cwd string) (CommandApprovalKey, bool) {
	if strings.ContainsRune(command, '\x00') {
		return CommandApprovalKey{}, false
	}
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	command = strings.ReplaceAll(command, "\r\n", "\n")
	if cwd == "." || strings.TrimSpace(command) == "" || !filepath.IsAbs(cwd) {
		return CommandApprovalKey{}, false
	}
	return CommandApprovalKey{CWD: cwd, Command: command}, true
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
