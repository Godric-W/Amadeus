package policy

import (
	"errors"
	"sync"
)

type GrantCache struct {
	mutex   sync.RWMutex
	session map[string]ApprovalDecision
}

func NewGrantCache() *GrantCache {
	return &GrantCache{
		session: make(map[string]ApprovalDecision),
	}
}

func (cache *GrantCache) Lookup(request ApprovalRequest) (ApprovalDecision, bool) {
	if cache == nil {
		return ApprovalDecision{}, false
	}
	key := request.ToolName
	cache.mutex.RLock()
	decision, ok := cache.session[key]
	cache.mutex.RUnlock()
	if !ok {
		return ApprovalDecision{}, false
	}
	decision.Source = ApprovalSourceGrant
	return decision, true
}

func (cache *GrantCache) Remember(request ApprovalRequest, decision ApprovalDecision) error {
	if cache == nil {
		return errors.New("approval grant cache is nil")
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if err := decision.Validate(); err != nil {
		return err
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	switch decision.Scope {
	case ApprovalOnce:
		return nil
	case ApprovalSession:
		cache.session[request.ToolName] = decision
	}
	return nil
}

func (cache *GrantCache) ClearSession() {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	cache.session = make(map[string]ApprovalDecision)
	cache.mutex.Unlock()
}
