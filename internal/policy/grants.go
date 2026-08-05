package policy

import (
	"encoding/json"
	"errors"
	"strings"
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
	key := grantKey(request)
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
		cache.session[grantKey(request)] = decision
	}
	return nil
}

func grantKey(request ApprovalRequest) string {
	switch request.ToolName {
	case "mcp_call":
		return mcpGrantKey(request, "name")
	case "mcp_list_tools", "mcp_list_resources":
		return mcpGrantKey(request)
	case "mcp_read_resource":
		return mcpGrantKey(request, "uri")
	default:
		return request.ToolName
	}
}

func mcpGrantKey(request ApprovalRequest, targetFields ...string) string {
	var arguments map[string]any
	if err := json.Unmarshal(request.Arguments, &arguments); err != nil {
		return request.ToolName + "\x00" + request.ArgumentsSHA256
	}
	parts := []string{request.ToolName, stringArgument(arguments, "server")}
	for _, field := range targetFields {
		parts = append(parts, stringArgument(arguments, field))
	}
	return strings.Join(parts, "\x00")
}

func stringArgument(arguments map[string]any, name string) string {
	value, _ := arguments[name].(string)
	return strings.TrimSpace(value)
}

func (cache *GrantCache) ClearSession() {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	cache.session = make(map[string]ApprovalDecision)
	cache.mutex.Unlock()
}
