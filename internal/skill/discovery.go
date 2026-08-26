package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

func Load(userRoot string, root project.Root, options LoadOptions) (*SkillCatalog, []error, error) {
	if root.Path() == "" {
		return nil, nil, errors.New("skill project root is empty")
	}
	options = normalizeOptions(options)
	disabled, settingsWarnings := loadSettings(userRoot, root.Path())
	catalog := &SkillCatalog{
		values: make(map[string]catalogEntry), disabled: disabled, userRoot: strings.TrimSpace(userRoot),
		projectRoot: root.Path(), options: options,
	}
	warnings := append([]error(nil), settingsWarnings...)
	if strings.TrimSpace(userRoot) != "" {
		values, sourceWarnings := scan(filepath.Join(userRoot, "skills"), SourceUser, options)
		warnings = append(warnings, sourceWarnings...)
		for name, value := range values {
			catalog.values[name] = value
		}
	}
	values, sourceWarnings := scan(filepath.Join(root.Path(), ".amadeus", "skills"), SourceProject, options)
	warnings = append(warnings, sourceWarnings...)
	for name, value := range values {
		catalog.values[name] = value
	}
	catalog.applyEnabledState()
	if len(catalog.values) > options.MaxSkills {
		return nil, warnings, fmt.Errorf("skill catalog exceeds maximum skill count %d", options.MaxSkills)
	}
	if indexBytes(catalog.Index()) > options.MaxIndexBytes {
		return nil, warnings, fmt.Errorf("skill catalog index exceeds maximum byte size %d", options.MaxIndexBytes)
	}
	return catalog, warnings, nil
}

func scan(root string, source Source, options LoadOptions) (map[string]catalogEntry, []error) {
	values := make(map[string]catalogEntry)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return values, []error{fmt.Errorf("read %s skills directory: %w", source, err)}
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return values, []error{fmt.Errorf("resolve %s skills directory: %w", source, err)}
	}
	warnings := make([]error, 0)
	for _, entry := range entries {
		if len(values) >= options.MaxSkills {
			warnings = append(warnings, fmt.Errorf("%s skill limit %d reached", source, options.MaxSkills))
			break
		}
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
			warnings = append(warnings, fmt.Errorf("ignore %s skill entry %q: expected non-symlink directory", source, entry.Name()))
			continue
		}
		directory := filepath.Join(absRoot, entry.Name())
		realDirectory, err := filepath.EvalSymlinks(directory)
		if err != nil || !inside(absRoot, realDirectory) {
			warnings = append(warnings, fmt.Errorf("ignore %s skill %q: directory escapes skill root", source, entry.Name()))
			continue
		}
		value, err := parse(filepath.Join(realDirectory, "SKILL.md"), source, realDirectory, options)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("ignore %s skill %q: %w", source, entry.Name(), err))
			continue
		}
		if _, exists := values[value.Name]; exists {
			warnings = append(warnings, fmt.Errorf("ignore duplicate %s skill name %q", source, value.Name))
			continue
		}
		value.Content = ""
		values[value.Name] = catalogEntry{metadata: value.SkillMetadata}
	}
	return values, warnings
}
