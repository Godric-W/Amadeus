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
	Spec Spec
	Tool Tool
}

type Registry struct {
	mutex sync.RWMutex
	tools map[string]Entry
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Entry)}
}

func (registry *Registry) Register(candidate Tool) error {
	if candidate == nil || isNilTool(candidate) {
		return ErrNilTool
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
	registry.tools[spec.Name] = Entry{Spec: spec, Tool: candidate}
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
		entries = append(entries, Entry{Spec: entry.Spec.Clone(), Tool: entry.Tool})
	}
	registry.mutex.RUnlock()
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Spec.Name < entries[right].Spec.Name
	})
	return entries
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
	if !spec.ResourceStrategy.Mode.Valid() {
		return fmt.Errorf("%w: resource mode %q is unsupported", ErrInvalidSpec, spec.ResourceStrategy.Mode)
	}
	if spec.ResourceStrategy.Mode == ResourceModeArguments && len(spec.ResourceStrategy.ArgumentPaths) == 0 {
		return fmt.Errorf("%w: argument resource strategy has no paths", ErrInvalidSpec)
	}
	return nil
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
