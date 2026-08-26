package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

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
