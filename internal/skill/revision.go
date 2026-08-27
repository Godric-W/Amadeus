package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func (catalog *SkillCatalog) Revision() (string, error) {
	_, revision, err := catalog.Snapshot()
	return revision, err
}

func (catalog *SkillCatalog) Snapshot() ([]SkillMetadata, string, error) {
	if catalog == nil {
		return nil, "", errors.New("skill catalog is nil")
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
	for index := range entries {
		entry, exists := catalog.values[entries[index].Name]
		if !exists {
			return nil, "", fmt.Errorf("skill %q disappeared from catalog", entries[index].Name)
		}
		value, err := parse(entry.metadata.PathToSkillMD, entry.metadata.Source, filepath.Dir(entry.metadata.PathToSkillMD), catalog.options)
		if err != nil {
			return nil, "", fmt.Errorf("refresh Skill %q revision: %w", entries[index].Name, err)
		}
		if value.Name != entry.metadata.Name || value.Source != entry.metadata.Source || value.PathToSkillMD != entry.metadata.PathToSkillMD {
			return nil, "", fmt.Errorf("Skill %q metadata changed since catalog discovery", entry.metadata.Name)
		}
		value.Enabled = entry.metadata.Enabled
		entries[index].Size = value.Size
		entries[index].Revision = value.Revision
		entries[index].References = cloneResources(value.References)
		entries[index].Scripts = cloneResources(value.Scripts)
		entries[index].Assets = cloneResources(value.Assets)
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return entries, hex.EncodeToString(digest[:]), nil
}

func (catalog *SkillCatalog) ValidateRevision(expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	current, err := catalog.Revision()
	if err != nil {
		return err
	}
	if current != expected {
		return &StaleRevisionError{Expected: expected, Current: current}
	}
	return nil
}
