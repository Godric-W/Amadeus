package skill

import (
	"errors"
	"path/filepath"
	"strings"
)

type ScriptInvocation struct {
	Skill   SkillMetadata
	Script  SkillResource
	Path    string
	Command string
}

func (catalog *SkillCatalog) FindScript(command, workingDirectory string) (ScriptInvocation, bool, error) {
	if catalog == nil {
		return ScriptInvocation{}, false, errors.New("skill catalog is nil")
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ScriptInvocation{}, false, nil
	}
	pathToken := scriptToken(fields)
	if pathToken == "" {
		return ScriptInvocation{}, false, nil
	}
	if strings.TrimSpace(workingDirectory) == "" {
		workingDirectory = "."
	}
	pathToken = strings.Trim(pathToken, "\"'")
	if !filepath.IsAbs(pathToken) {
		pathToken = filepath.Join(workingDirectory, pathToken)
	}
	absolute, err := filepath.Abs(pathToken)
	if err != nil {
		return ScriptInvocation{}, false, err
	}
	absolute = filepath.Clean(absolute)
	catalog.mutex.RLock()
	entries := make([]SkillMetadata, 0, len(catalog.values))
	for _, entry := range catalog.values {
		if entry.metadata.Enabled && entry.metadata.Policy.AllowImplicitInvocation {
			entries = append(entries, entry.metadata)
		}
	}
	catalog.mutex.RUnlock()
	for _, metadata := range entries {
		root := filepath.Dir(metadata.PathToSkillMD)
		scriptsRoot := filepath.Join(root, "scripts")
		if !inside(root, absolute) || !inside(scriptsRoot, absolute) {
			continue
		}
		realPath, evalErr := filepath.EvalSymlinks(absolute)
		if evalErr != nil || filepath.Clean(realPath) != absolute {
			continue
		}
		for _, script := range metadata.Scripts {
			candidate := filepath.Clean(filepath.Join(root, filepath.FromSlash(script.Path)))
			if candidate == absolute {
				return ScriptInvocation{Skill: metadata, Script: script, Path: absolute, Command: command}, true, nil
			}
		}
	}
	return ScriptInvocation{}, false, nil
}

func scriptToken(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	first := filepath.Base(fields[0])
	interpreters := map[string]bool{
		"python": true, "python3": true, "bash": true, "sh": true, "zsh": true,
		"node": true, "deno": true, "ruby": true, "perl": true, "pwsh": true,
	}
	if interpreters[first] {
		for _, field := range fields[1:] {
			field = strings.Trim(field, "\"'")
			if field == "" {
				continue
			}
			if strings.EqualFold(field, "-c") || strings.EqualFold(field, "--command") {
				return ""
			}
			if strings.HasPrefix(field, "-") {
				continue
			}
			return field
		}
		return ""
	}
	extension := strings.ToLower(filepath.Ext(fields[0]))
	switch extension {
	case ".py", ".sh", ".js", ".ts", ".rb", ".pl", ".ps1":
		return fields[0]
	default:
		return ""
	}
}
