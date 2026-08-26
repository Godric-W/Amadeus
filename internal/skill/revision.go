package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func (catalog *SkillCatalog) Revision() (string, error) {
	if catalog == nil {
		return "", errors.New("skill catalog is nil")
	}
	entries := catalog.Index()
	for index := range entries {
		catalog.mutex.RLock()
		entry, exists := catalog.values[entries[index].Name]
		catalog.mutex.RUnlock()
		if !exists {
			return "", fmt.Errorf("skill %q disappeared from catalog", entries[index].Name)
		}
		value, err := parse(entry.metadata.PathToSkillMD, entry.metadata.Source, filepath.Dir(entry.metadata.PathToSkillMD), catalog.options)
		if err != nil {
			return "", fmt.Errorf("refresh Skill %q revision: %w", entries[index].Name, err)
		}
		if value.Name != entry.metadata.Name || value.Source != entry.metadata.Source || value.PathToSkillMD != entry.metadata.PathToSkillMD {
			return "", fmt.Errorf("Skill %q metadata changed since catalog discovery", entry.metadata.Name)
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
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
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
