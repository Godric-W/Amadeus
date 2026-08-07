package tool

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNilTool       = errors.New("tool is nil")
	ErrInvalidSpec   = errors.New("tool spec is invalid")
	ErrDuplicateTool = errors.New("tool is already registered")
)

type Entry struct {
	Spec      Spec
	Tool      Tool
	Exposure  Exposure
	Condition string
}

type Registry struct {
	mutex  sync.RWMutex
	tools  map[string]Entry
	groups map[string]map[string]struct{}
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Entry), groups: make(map[string]map[string]struct{})}
}

func (registry *Registry) ReplaceGroup(group string, candidates []Tool) error {
	return registry.ReplaceGroupWithRegistration(group, candidates, DirectRegistration())
}

func (registry *Registry) ReplaceGroupWithRegistration(group string, candidates []Tool, registration Registration) error {
	group = strings.TrimSpace(group)
	if group == "" {
		return errors.New("tool registry group is empty")
	}
	registration, err := normalizeRegistration(registration)
	if err != nil {
		return err
	}
	entries := make([]Entry, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || isNilTool(candidate) {
			return ErrNilTool
		}
		spec := candidate.Spec().Clone()
		if err := validateSpec(spec); err != nil {
			return err
		}
		if _, exists := seen[spec.Name]; exists {
			return fmt.Errorf("%w: %s", ErrDuplicateTool, spec.Name)
		}
		seen[spec.Name] = struct{}{}
		entries = append(entries, Entry{Spec: spec, Tool: candidate, Exposure: registration.Exposure, Condition: registration.Condition})
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	for _, entry := range entries {
		if _, exists := registry.tools[entry.Spec.Name]; exists {
			if _, owned := registry.groups[group][entry.Spec.Name]; !owned {
				return fmt.Errorf("%w: %s", ErrDuplicateTool, entry.Spec.Name)
			}
		}
	}
	for name := range registry.groups[group] {
		delete(registry.tools, name)
	}
	owned := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		registry.tools[entry.Spec.Name] = entry
		owned[entry.Spec.Name] = struct{}{}
	}
	registry.groups[group] = owned
	return nil
}

func (registry *Registry) Register(candidate Tool) error {
	return registry.RegisterWithRegistration(candidate, DirectRegistration())
}

func (registry *Registry) RegisterWithRegistration(candidate Tool, registration Registration) error {
	if candidate == nil || isNilTool(candidate) {
		return ErrNilTool
	}
	registration, err := normalizeRegistration(registration)
	if err != nil {
		return err
	}
	spec := candidate.Spec().Clone()
	if err := validateSpec(spec); err != nil {
		return err
	}

	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	if _, exists := registry.tools[spec.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, spec.Name)
	}
	registry.tools[spec.Name] = Entry{Spec: spec, Tool: candidate, Exposure: registration.Exposure, Condition: registration.Condition}
	return nil
}

func (registry *Registry) Lookup(name string) (Tool, bool) {
	registry.mutex.RLock()
	entry, exists := registry.tools[strings.TrimSpace(name)]
	registry.mutex.RUnlock()
	return entry.Tool, exists
}

func (registry *Registry) Snapshot() []Entry {
	registry.mutex.RLock()
	entries := make([]Entry, 0, len(registry.tools))
	for _, entry := range registry.tools {
		entries = append(entries, Entry{Spec: entry.Spec.Clone(), Tool: entry.Tool, Exposure: entry.Exposure, Condition: entry.Condition})
	}
	registry.mutex.RUnlock()
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Spec.Name < entries[right].Spec.Name
	})
	return entries
}

func (registry *Registry) VisibleSnapshot(conditions map[string]bool) []Entry {
	entries := registry.Snapshot()
	visible := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		switch entry.Exposure {
		case ExposureDirect:
			visible = append(visible, entry)
		case ExposureConditional:
			if conditions != nil && conditions[entry.Condition] {
				visible = append(visible, entry)
			}
		case ExposureDeferred:
			if conditions != nil && conditions[entry.Condition] {
				visible = append(visible, entry)
			}
		case ExposureHidden:
		}
	}
	return visible
}

func normalizeRegistration(registration Registration) (Registration, error) {
	if registration.Exposure == "" {
		registration.Exposure = ExposureDirect
	}
	if !registration.Exposure.Valid() {
		return Registration{}, fmt.Errorf("tool registration exposure %q is unsupported", registration.Exposure)
	}
	registration.Condition = strings.TrimSpace(registration.Condition)
	if registration.Exposure == ExposureConditional && registration.Condition == "" {
		return Registration{}, errors.New("conditional tool registration has no condition")
	}
	return registration, nil
}

func (registry *Registry) Len() int {
	registry.mutex.RLock()
	length := len(registry.tools)
	registry.mutex.RUnlock()
	return length
}

func validateSpec(spec Spec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidSpec)
	}
	if spec.Name != strings.TrimSpace(spec.Name) || strings.ContainsAny(spec.Name, " \t\r\n") {
		return fmt.Errorf("%w: name contains whitespace", ErrInvalidSpec)
	}
	if strings.TrimSpace(spec.Description) == "" {
		return fmt.Errorf("%w: description is empty", ErrInvalidSpec)
	}
	if !spec.SideEffect.Valid() {
		return fmt.Errorf("%w: side effect %q is unsupported", ErrInvalidSpec, spec.SideEffect)
	}
	if !spec.Concurrency.Valid() {
		return fmt.Errorf("%w: concurrency %q is unsupported", ErrInvalidSpec, spec.Concurrency)
	}
	return nil
}

func ValidateSpec(spec Spec) error {
	return validateSpec(spec)
}

func isNilTool(candidate Tool) bool {
	value := reflect.ValueOf(candidate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
