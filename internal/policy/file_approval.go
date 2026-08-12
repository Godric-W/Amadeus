package policy

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileApprovalStore holds the in-memory Claude-style "accept edits" grant.
// A grant is scoped to a canonical directory and never survives Resume.
type FileApprovalStore struct {
	mu          sync.RWMutex
	acceptEdits []string
}

func NewFileApprovalStore() *FileApprovalStore { return &FileApprovalStore{} }

func (store *FileApprovalStore) Allows(path string) bool {
	if store == nil {
		return false
	}
	canonical := filepath.Clean(path)
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, root := range store.acceptEdits {
		if canonical == root || strings.HasPrefix(canonical, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (store *FileApprovalStore) ApproveDirectory(path string) {
	if store == nil || strings.TrimSpace(path) == "" {
		return
	}
	canonical := filepath.Clean(path)
	store.mu.Lock()
	for _, existing := range store.acceptEdits {
		if canonical == existing || strings.HasPrefix(canonical, existing+string(filepath.Separator)) {
			store.mu.Unlock()
			return
		}
	}
	filtered := store.acceptEdits[:0]
	for _, existing := range store.acceptEdits {
		if !strings.HasPrefix(existing, canonical+string(filepath.Separator)) {
			filtered = append(filtered, existing)
		}
	}
	store.acceptEdits = append(filtered, canonical)
	sort.Slice(store.acceptEdits, func(i, j int) bool { return len(store.acceptEdits[i]) > len(store.acceptEdits[j]) })
	store.mu.Unlock()
}

func (store *FileApprovalStore) Clear() {
	if store == nil {
		return
	}
	store.mu.Lock()
	store.acceptEdits = nil
	store.mu.Unlock()
}

func (store *FileApprovalStore) Count() int {
	if store == nil {
		return 0
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.acceptEdits)
}
