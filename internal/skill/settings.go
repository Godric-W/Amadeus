package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type settingsDocument struct {
	Disabled []string `yaml:"disabled,omitempty"`
}

func loadSettings(userRoot, projectRoot string) (map[Source]map[string]bool, []error) {
	result := map[Source]map[string]bool{SourceUser: {}, SourceProject: {}}
	warnings := make([]error, 0, 2)
	for _, source := range []Source{SourceUser, SourceProject} {
		path := settingsPath(userRoot, projectRoot, source)
		if path == "" {
			continue
		}
		document, err := readSettings(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, fmt.Errorf("read %s Skill settings: %w", source, err))
			continue
		}
		for _, name := range document.Disabled {
			name = strings.TrimSpace(name)
			if validName(name) {
				result[source][name] = true
			}
		}
	}
	return result, warnings
}

func settingsPath(userRoot, projectRoot string, source Source) string {
	switch source {
	case SourceUser:
		if strings.TrimSpace(userRoot) == "" {
			return ""
		}
		return filepath.Join(userRoot, "skills.yaml")
	case SourceProject:
		if strings.TrimSpace(projectRoot) == "" {
			return ""
		}
		return filepath.Join(projectRoot, ".amadeus", "skills.yaml")
	default:
		return ""
	}
}

func readSettings(path string) (settingsDocument, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return settingsDocument{}, err
	}
	var document settingsDocument
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil {
		return settingsDocument{}, err
	}
	return document, nil
}

func writeSettings(path string, disabled []string) error {
	if path == "" {
		return errors.New("Skill settings path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Skill settings directory: %w", err)
	}
	content, err := yaml.Marshal(settingsDocument{Disabled: disabled})
	if err != nil {
		return fmt.Errorf("encode Skill settings: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".skills-*.yaml")
	if err != nil {
		return fmt.Errorf("create Skill settings temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Skill settings: %w", err)
	}
	return nil
}

func sortedDisabled(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for name, disabled := range values {
		if disabled {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
