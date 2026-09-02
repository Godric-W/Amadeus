package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type ToolRouteFilter func(ToolSpec) bool

type ToolRouter struct {
	routes   map[string]toolRoute
	ordered  []string
	revision string
	snapshot RequestSnapshot
}

func (registry *Registry) SnapshotRouterWithDefinitions(conditions map[string]bool, snapshot RequestSnapshot, include ToolRouteFilter, definitions []ToolDefinition) (ToolRouter, error) {
	if registry == nil {
		return ToolRouter{}, fmt.Errorf("tool registry is nil")
	}
	registry.mutex.RLock()
	entries := make(map[string]Entry, len(registry.tools)+len(definitions))
	for name, entry := range registry.tools {
		entries[name] = entry
	}
	registry.mutex.RUnlock()
	for index, definition := range definitions {
		if definition == nil || isNilDefinition(definition) {
			return ToolRouter{}, ErrNilTool
		}
		spec := definition.Spec().Clone()
		if err := validateSpec(spec); err != nil {
			return ToolRouter{}, err
		}
		if _, exists := entries[spec.Name]; exists {
			return ToolRouter{}, fmt.Errorf("%w: %s", ErrDuplicateTool, spec.Name)
		}
		entries[spec.Name] = Entry{Spec: spec, Tool: definition, Exposure: ExposureDirect, bindingID: math.MaxUint64 - uint64(index)}
	}
	routes := make(map[string]toolRoute, len(entries))
	ordered := make([]string, 0, len(entries))
	for name, entry := range entries {
		if !entryVisible(entry, conditions) {
			continue
		}
		spec := entry.Spec.Clone()
		if include != nil && !include(spec) {
			continue
		}
		routes[name] = toolRoute{spec: spec, definition: entry.Tool, parallel: entry.Tool.SupportsParallelToolCalls(), exposure: entry.Exposure, condition: entry.Condition, bindingID: entry.bindingID}
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	revision := toolRouterRevision(routes, ordered, snapshot)
	snapshot.ToolRouterRevision = revision
	return ToolRouter{routes: routes, ordered: ordered, revision: revision, snapshot: snapshot}, nil
}

type toolRoute struct {
	spec       ToolSpec
	definition ToolDefinition
	parallel   bool
	exposure   Exposure
	condition  string
	bindingID  uint64
}

func (registry *Registry) SnapshotRouter(conditions map[string]bool, snapshot RequestSnapshot, include ToolRouteFilter) ToolRouter {
	if registry == nil {
		return ToolRouter{}
	}
	registry.mutex.RLock()
	routes := make(map[string]toolRoute, len(registry.tools))
	ordered := make([]string, 0, len(registry.tools))
	for name, entry := range registry.tools {
		if !entryVisible(entry, conditions) {
			continue
		}
		spec := entry.Spec.Clone()
		if include != nil && !include(spec) {
			continue
		}
		routes[name] = toolRoute{
			spec:       spec,
			definition: entry.Tool,
			parallel:   entry.Tool.SupportsParallelToolCalls(),
			exposure:   entry.Exposure,
			condition:  entry.Condition,
			bindingID:  entry.bindingID,
		}
		ordered = append(ordered, name)
	}
	registry.mutex.RUnlock()
	sort.Strings(ordered)
	revision := toolRouterRevision(routes, ordered, snapshot)
	snapshot.ToolRouterRevision = revision
	return ToolRouter{routes: routes, ordered: ordered, revision: revision, snapshot: snapshot}
}

func (router ToolRouter) Specs() []ToolSpec {
	specs := make([]ToolSpec, 0, len(router.ordered))
	for _, name := range router.ordered {
		specs = append(specs, router.routes[name].spec.Clone())
	}
	return specs
}

func (router ToolRouter) Names() []string {
	return append([]string(nil), router.ordered...)
}

func (router ToolRouter) Revision() string {
	return router.revision
}

func (router ToolRouter) RequestSnapshot() RequestSnapshot {
	return router.snapshot
}

func (router ToolRouter) Contains(name string) bool {
	_, ok := router.routes[strings.TrimSpace(name)]
	return ok
}

func (router ToolRouter) SupportsParallelToolCalls(name string) bool {
	route, ok := router.routes[strings.TrimSpace(name)]
	return ok && route.parallel
}

func (router ToolRouter) isConfigured() bool {
	return router.routes != nil && router.revision != ""
}

func (router ToolRouter) resolve(name string) (toolRoute, bool) {
	route, ok := router.routes[strings.TrimSpace(name)]
	return route, ok
}

func toolRouterRevision(routes map[string]toolRoute, ordered []string, snapshot RequestSnapshot) string {
	type revisionRoute struct {
		Spec      ToolSpec `json:"spec"`
		Parallel  bool     `json:"parallel"`
		Exposure  Exposure `json:"exposure"`
		Condition string   `json:"condition,omitempty"`
		BindingID uint64   `json:"binding_id"`
	}
	payload := struct {
		Routes           []revisionRoute `json:"routes"`
		MCPRevision      string          `json:"mcp_revision,omitempty"`
		SkillRevision    string          `json:"skill_revision,omitempty"`
		AgentsMdRevision string          `json:"agents_md_revision,omitempty"`
	}{
		Routes:           make([]revisionRoute, 0, len(ordered)),
		MCPRevision:      snapshot.MCPBindingRevision,
		SkillRevision:    snapshot.SkillRevision,
		AgentsMdRevision: snapshot.AgentsMdRevision,
	}
	for _, name := range ordered {
		route := routes[name]
		payload.Routes = append(payload.Routes, revisionRoute{
			Spec: route.spec, Parallel: route.parallel, Exposure: route.exposure,
			Condition: route.condition, BindingID: route.bindingID,
		})
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
