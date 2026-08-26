package skill

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	defaultMaxSkills      = 64
	defaultMaxSkillBytes  = 128 << 10
	defaultMaxIndexBytes  = 128 << 10
	defaultMaxDescription = 512
)

type SkillCatalog struct {
	mutex       sync.RWMutex
	values      map[string]catalogEntry
	disabled    map[Source]map[string]bool
	userRoot    string
	projectRoot string
	options     LoadOptions
}

type catalogEntry struct {
	metadata SkillMetadata
}

type LoadOptions struct {
	MaxSkills      int
	MaxSkillBytes  int64
	MaxIndexBytes  int
	MaxDescription int
}

func DefaultLoadOptions() LoadOptions {
	return LoadOptions{MaxSkills: defaultMaxSkills, MaxSkillBytes: defaultMaxSkillBytes, MaxIndexBytes: defaultMaxIndexBytes, MaxDescription: defaultMaxDescription}
}

func (catalog *SkillCatalog) Lookup(name string) (SkillMetadata, bool) {
	if catalog == nil {
		return SkillMetadata{}, false
	}
	catalog.mutex.RLock()
	defer catalog.mutex.RUnlock()
	entry, ok := catalog.values[strings.TrimSpace(name)]
	if ok {
		entry.metadata.References = cloneResources(entry.metadata.References)
		entry.metadata.Scripts = cloneResources(entry.metadata.Scripts)
		entry.metadata.Assets = cloneResources(entry.metadata.Assets)
	}
	return entry.metadata, ok
}

func (catalog *SkillCatalog) LoadDocument(name string) (SkillDocument, error) {
	if catalog == nil {
		return SkillDocument{}, errors.New("skill catalog is nil")
	}
	catalog.mutex.RLock()
	entry, ok := catalog.values[strings.TrimSpace(name)]
	catalog.mutex.RUnlock()
	if !ok {
		return SkillDocument{}, fmt.Errorf("skill %q is not available", strings.TrimSpace(name))
	}
	if !entry.metadata.Enabled {
		return SkillDocument{}, fmt.Errorf("skill %q is disabled", entry.metadata.Name)
	}
	value, err := parse(entry.metadata.PathToSkillMD, entry.metadata.Source, filepath.Dir(entry.metadata.PathToSkillMD), catalog.options)
	if err != nil {
		return SkillDocument{}, fmt.Errorf("load Skill %q: %w", entry.metadata.Name, err)
	}
	if value.Name != entry.metadata.Name || value.Source != entry.metadata.Source || value.PathToSkillMD != entry.metadata.PathToSkillMD {
		return SkillDocument{}, fmt.Errorf("Skill %q metadata changed since catalog discovery", entry.metadata.Name)
	}
	value.Enabled = true
	return value, nil
}

func (catalog *SkillCatalog) Index() []SkillMetadata {
	if catalog == nil {
		return nil
	}
	catalog.mutex.RLock()
	defer catalog.mutex.RUnlock()
	entries := make([]SkillMetadata, 0, len(catalog.values))
	for _, entry := range catalog.values {
		value := entry.metadata
		value.References = cloneResources(value.References)
		value.Scripts = cloneResources(value.Scripts)
		value.Assets = cloneResources(value.Assets)
		entries = append(entries, value)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name < entries[right].Name })
	return entries
}

func (catalog *SkillCatalog) Len() int {
	if catalog == nil {
		return 0
	}
	catalog.mutex.RLock()
	defer catalog.mutex.RUnlock()
	return len(catalog.values)
}

func (catalog *SkillCatalog) SetEnabled(name string, enabled bool) error {
	if catalog == nil {
		return errors.New("skill catalog is nil")
	}
	name = strings.TrimSpace(name)
	catalog.mutex.Lock()
	defer catalog.mutex.Unlock()
	entry, ok := catalog.values[name]
	if !ok {
		return fmt.Errorf("skill %q is not available", name)
	}
	disabled := make(map[string]bool, len(catalog.disabled[entry.metadata.Source])+1)
	for existing, value := range catalog.disabled[entry.metadata.Source] {
		disabled[existing] = value
	}
	if enabled {
		delete(disabled, name)
	} else {
		disabled[name] = true
	}
	path := settingsPath(catalog.userRoot, catalog.projectRoot, entry.metadata.Source)
	names := sortedDisabled(disabled)
	if err := writeSettings(path, names); err != nil {
		return err
	}
	catalog.disabled[entry.metadata.Source] = disabled
	entry.metadata.Enabled = enabled
	catalog.values[name] = entry
	return nil
}

func (catalog *SkillCatalog) applyEnabledState() {
	for name, entry := range catalog.values {
		entry.metadata.Enabled = !catalog.disabled[entry.metadata.Source][name]
		catalog.values[name] = entry
	}
}
