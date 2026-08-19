package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/project"
	"go.yaml.in/yaml/v3"
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
	warnings := make([]error, 0)
	warnings = append(warnings, settingsWarnings...)
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

func parse(path string, source Source, root string, options LoadOptions) (SkillDocument, error) {
	info, err := os.Stat(path)
	if err != nil {
		return SkillDocument{}, err
	}
	if !info.Mode().IsRegular() {
		return SkillDocument{}, errors.New("SKILL.md is not a regular file")
	}
	if info.Size() > options.MaxSkillBytes {
		return SkillDocument{}, fmt.Errorf("SKILL.md exceeds maximum byte size %d", options.MaxSkillBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return SkillDocument{}, err
	}
	if !utf8.Valid(content) {
		return SkillDocument{}, errors.New("SKILL.md is not valid UTF-8")
	}
	frontmatter, body, err := splitFrontmatter(string(content))
	if err != nil {
		return SkillDocument{}, err
	}
	var header struct {
		Name             string `yaml:"name"`
		Description      string `yaml:"description"`
		ShortDescription string `yaml:"short_description"`
		AllowImplicit    *bool  `yaml:"allow_implicit_invocation"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(frontmatter))
	decoder.KnownFields(true)
	if err := decoder.Decode(&header); err != nil {
		return SkillDocument{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	header.Name = strings.TrimSpace(header.Name)
	header.Description = strings.TrimSpace(header.Description)
	header.ShortDescription = strings.TrimSpace(header.ShortDescription)
	if !validName(header.Name) {
		return SkillDocument{}, errors.New("frontmatter name must contain only lowercase letters, digits, and hyphens")
	}
	if header.Description == "" {
		return SkillDocument{}, errors.New("frontmatter description is empty")
	}
	if len([]rune(header.Description)) > options.MaxDescription {
		return SkillDocument{}, fmt.Errorf("frontmatter description exceeds %d runes", options.MaxDescription)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return SkillDocument{}, errors.New("SKILL.md body is empty")
	}
	path = filepath.Clean(path)
	resources, err := discoverResources(root)
	if err != nil {
		return SkillDocument{}, err
	}
	policy := Policy{AllowImplicitInvocation: true}
	if header.AllowImplicit != nil {
		policy.AllowImplicitInvocation = *header.AllowImplicit
	}
	metadata := SkillMetadata{
		Name: header.Name, Description: header.Description, ShortDescription: header.ShortDescription,
		PathToSkillMD: path, Source: source, Scope: scopeForSource(source), Enabled: true,
		Policy: policy, References: resources[ResourceReference], Scripts: resources[ResourceScript], Assets: resources[ResourceAsset], Size: info.Size(),
	}
	revisionInput := struct {
		Metadata SkillMetadata `json:"metadata"`
		Content  string        `json:"content"`
	}{Metadata: metadata, Content: body}
	encoded, err := json.Marshal(revisionInput)
	if err != nil {
		return SkillDocument{}, err
	}
	digest := sha256.Sum256(encoded)
	metadata.Revision = hex.EncodeToString(digest[:])
	return SkillDocument{SkillMetadata: metadata, Root: root, Content: body}, nil
}

func splitFrontmatter(content string) (string, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", errors.New("SKILL.md must start with YAML frontmatter")
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			return strings.Join(lines[1:index], "\n"), strings.Join(lines[index+1:], "\n"), nil
		}
	}
	return "", "", errors.New("SKILL.md frontmatter is not terminated")
}

func normalizeOptions(options LoadOptions) LoadOptions {
	if options.MaxSkills <= 0 {
		options.MaxSkills = defaultMaxSkills
	}
	if options.MaxSkillBytes <= 0 {
		options.MaxSkillBytes = defaultMaxSkillBytes
	}
	if options.MaxIndexBytes <= 0 {
		options.MaxIndexBytes = defaultMaxIndexBytes
	}
	if options.MaxDescription <= 0 {
		options.MaxDescription = defaultMaxDescription
	}
	return options
}

func validName(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func inside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func indexBytes(entries []SkillMetadata) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.Name) + len(entry.Description) + len(entry.Source) + len(entry.PathToSkillMD) + len(entry.Revision) + 8
	}
	return total
}

func scopeForSource(source Source) Scope {
	if source == SourceProject {
		return ScopeProject
	}
	return ScopeUser
}
