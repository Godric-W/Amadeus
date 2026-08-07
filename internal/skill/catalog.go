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

type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

type Skill struct {
	Name        string
	Description string
	Content     string
	Source      Source
	Root        string
	Path        string
	Size        int64
	Revision    string
}

type IndexEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      Source `json:"source"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Revision    string `json:"revision"`
}

type Catalog struct {
	values  map[string]catalogEntry
	options LoadOptions
}

type catalogEntry struct {
	metadata Skill
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

func Load(userRoot string, root project.Root, options LoadOptions) (*Catalog, []error, error) {
	if root.Path() == "" {
		return nil, nil, errors.New("skill project root is empty")
	}
	options = normalizeOptions(options)
	catalog := &Catalog{values: make(map[string]catalogEntry), options: options}
	warnings := make([]error, 0)
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
	if len(catalog.values) > options.MaxSkills {
		return nil, warnings, fmt.Errorf("skill catalog exceeds maximum skill count %d", options.MaxSkills)
	}
	if indexBytes(catalog.Index()) > options.MaxIndexBytes {
		return nil, warnings, fmt.Errorf("skill catalog index exceeds maximum byte size %d", options.MaxIndexBytes)
	}
	return catalog, warnings, nil
}

func (catalog *Catalog) Lookup(name string) (Skill, bool) {
	if catalog == nil {
		return Skill{}, false
	}
	entry, ok := catalog.values[strings.TrimSpace(name)]
	return entry.metadata, ok
}

func (catalog *Catalog) Load(name string) (Skill, error) {
	if catalog == nil {
		return Skill{}, errors.New("skill catalog is nil")
	}
	entry, ok := catalog.values[strings.TrimSpace(name)]
	if !ok {
		return Skill{}, fmt.Errorf("skill %q is not available", strings.TrimSpace(name))
	}
	value, err := parse(entry.metadata.Path, entry.metadata.Source, entry.metadata.Root, catalog.options)
	if err != nil {
		return Skill{}, fmt.Errorf("load Skill %q: %w", entry.metadata.Name, err)
	}
	if value.Name != entry.metadata.Name || value.Source != entry.metadata.Source || value.Root != entry.metadata.Root {
		return Skill{}, fmt.Errorf("Skill %q metadata changed since catalog discovery", entry.metadata.Name)
	}
	return value, nil
}

func (catalog *Catalog) Index() []IndexEntry {
	if catalog == nil {
		return nil
	}
	entries := make([]IndexEntry, 0, len(catalog.values))
	for _, entry := range catalog.values {
		value := entry.metadata
		entries = append(entries, IndexEntry{Name: value.Name, Description: value.Description, Source: value.Source, Path: value.Path, Size: value.Size, Revision: value.Revision})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name < entries[right].Name })
	return entries
}

func (catalog *Catalog) Revision() (string, error) {
	if catalog == nil {
		return "", errors.New("skill catalog is nil")
	}
	entries := catalog.Index()
	for index := range entries {
		value, err := catalog.Load(entries[index].Name)
		if err != nil {
			return "", err
		}
		entries[index].Size = value.Size
		entries[index].Revision = value.Revision
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (catalog *Catalog) Len() int {
	if catalog == nil {
		return 0
	}
	return len(catalog.values)
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
		values[value.Name] = catalogEntry{metadata: value}
	}
	return values, warnings
}

func parse(path string, source Source, root string, options LoadOptions) (Skill, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, err
	}
	if !info.Mode().IsRegular() {
		return Skill{}, errors.New("SKILL.md is not a regular file")
	}
	if info.Size() > options.MaxSkillBytes {
		return Skill{}, fmt.Errorf("SKILL.md exceeds maximum byte size %d", options.MaxSkillBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	if !utf8.Valid(content) {
		return Skill{}, errors.New("SKILL.md is not valid UTF-8")
	}
	metadata, body, err := splitFrontmatter(string(content))
	if err != nil {
		return Skill{}, err
	}
	var header struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(metadata))
	decoder.KnownFields(true)
	if err := decoder.Decode(&header); err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	header.Name = strings.TrimSpace(header.Name)
	header.Description = strings.TrimSpace(header.Description)
	if !validName(header.Name) {
		return Skill{}, errors.New("frontmatter name must contain only lowercase letters, digits, and hyphens")
	}
	if header.Description == "" {
		return Skill{}, errors.New("frontmatter description is empty")
	}
	if len([]rune(header.Description)) > options.MaxDescription {
		return Skill{}, fmt.Errorf("frontmatter description exceeds %d runes", options.MaxDescription)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Skill{}, errors.New("SKILL.md body is empty")
	}
	digest := sha256.Sum256([]byte(body))
	return Skill{
		Name: header.Name, Description: header.Description, Content: body, Source: source, Root: root,
		Path: filepath.Clean(path), Size: info.Size(), Revision: hex.EncodeToString(digest[:]),
	}, nil
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

func indexBytes(entries []IndexEntry) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.Name) + len(entry.Description) + len(entry.Source) + len(entry.Path) + len(entry.Revision) + 8
	}
	return total
}
