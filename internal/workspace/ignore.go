package workspace

import (
	"bufio"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
)

type ignoreRule struct {
	pattern   string
	negated   bool
	directory bool
	anchored  bool
}

type IgnoreMatcher struct {
	rules []ignoreRule
}

func LoadIgnoreMatcher(root project.Root) (*IgnoreMatcher, error) {
	if root.Path() == "" {
		return nil, errors.New("ignore matcher project root is empty")
	}
	matcher := &IgnoreMatcher{}
	for _, name := range []string{".gitignore", ".ignore"} {
		file, err := os.Open(filepath.Join(root.Path(), name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			matcher.add(scanner.Text())
		}
		closeErr := file.Close()
		if scanner.Err() != nil {
			return nil, scanner.Err()
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return matcher, nil
}

func (matcher *IgnoreMatcher) add(value string) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "#") {
		return
	}
	rule := ignoreRule{}
	if strings.HasPrefix(value, "!") {
		rule.negated = true
		value = strings.TrimPrefix(value, "!")
	}
	rule.directory = strings.HasSuffix(value, "/")
	rule.anchored = strings.HasPrefix(value, "/")
	value = strings.Trim(value, "/")
	if value == "" {
		return
	}
	rule.pattern = filepath.ToSlash(value)
	matcher.rules = append(matcher.rules, rule)
}

func (matcher *IgnoreMatcher) Ignored(relative string, directory bool) bool {
	if matcher == nil {
		return false
	}
	relative = strings.Trim(filepath.ToSlash(relative), "/")
	ignored := false
	for _, rule := range matcher.rules {
		if rule.directory && !directory && !strings.Contains(relative, rule.pattern+"/") {
			continue
		}
		matched := matchIgnoreRule(rule, relative)
		if matched {
			ignored = !rule.negated
		}
	}
	return ignored
}

func matchIgnoreRule(rule ignoreRule, relative string) bool {
	if rule.anchored || strings.Contains(rule.pattern, "/") {
		matched, _ := MatchGlob(rule.pattern, relative)
		if matched {
			return true
		}
		return relative == rule.pattern || strings.HasPrefix(relative, rule.pattern+"/")
	}
	for _, segment := range strings.Split(relative, "/") {
		matched, _ := path.Match(rule.pattern, segment)
		if matched {
			return true
		}
	}
	return false
}
