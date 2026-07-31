package policy

import (
	"errors"
	"sync"
)

type approvalGrantKey struct {
	toolName        string
	argumentsSHA256 string
	risk            CommandRisk
}

type GrantCache struct {
	mutex      sync.RWMutex
	session    map[approvalGrantKey]ApprovalDecision
	persistent map[approvalGrantKey]ApprovalDecision
}

func NewGrantCache() *GrantCache {
	return &GrantCache{
		session:    make(map[approvalGrantKey]ApprovalDecision),
		persistent: make(map[approvalGrantKey]ApprovalDecision),
	}
}

func (cache *GrantCache) Lookup(request ApprovalRequest) (ApprovalDecision, bool) {
	if cache == nil {
		return ApprovalDecision{}, false
	}
	key := grantKey(request)
	cache.mutex.RLock()
	decision, ok := cache.session[key]
	if !ok {
		decision, ok = cache.persistent[key]
	}
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
	key := grantKey(request)
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	switch decision.Scope {
	case ApprovalOnce:
		return nil
	case ApprovalSession:
		cache.session[key] = decision
	case ApprovalAlways:
		cache.persistent[key] = decision
	}
	return nil
}

func (cache *GrantCache) ClearSession() {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	cache.session = make(map[approvalGrantKey]ApprovalDecision)
	cache.mutex.Unlock()
}

func grantKey(request ApprovalRequest) approvalGrantKey {
	return approvalGrantKey{toolName: request.ToolName, argumentsSHA256: request.ArgumentsSHA256, risk: request.Risk}
}
