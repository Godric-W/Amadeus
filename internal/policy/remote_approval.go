package policy

import (
	"strings"
	"sync"
)

const ApprovalPurposeExternal ApprovalPurpose = "external_operation"

type SessionRuleStore struct {
	mu    sync.RWMutex
	rules map[string]struct{}
}

func NewSessionRuleStore() *SessionRuleStore {
	return &SessionRuleStore{rules: make(map[string]struct{})}
}

func (store *SessionRuleStore) Allows(key string) bool {
	if store == nil {
		return false
	}
	key = strings.TrimSpace(key)
	store.mu.RLock()
	_, ok := store.rules[key]
	store.mu.RUnlock()
	return key != "" && ok
}

func (store *SessionRuleStore) Approve(key string) {
	if store == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	store.mu.Lock()
	store.rules[key] = struct{}{}
	store.mu.Unlock()
}

func (store *SessionRuleStore) Clear() {
	if store == nil {
		return
	}
	store.mu.Lock()
	store.rules = make(map[string]struct{})
	store.mu.Unlock()
}

func (store *SessionRuleStore) Count() int {
	if store == nil {
		return 0
	}
	store.mu.RLock()
	count := len(store.rules)
	store.mu.RUnlock()
	return count
}

func MCPApprovalKey(server, name string) string {
	return "mcp:" + strings.TrimSpace(server) + "/" + strings.TrimSpace(name)
}

func WebHostApprovalKey(host string) string {
	return "web:" + strings.ToLower(strings.TrimSpace(host))
}
