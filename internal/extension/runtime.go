package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
)

var explicitSkillPattern = regexp.MustCompile(`\$([A-Za-z0-9][A-Za-z0-9._-]{0,63})`)

type Options struct {
	SkillLoadOptions skill.LoadOptions
	MCPLoadOptions   mcp.LoadOptions
	MCPClientFactory mcp.ClientFactory
}

type Runtime struct {
	skills        *skill.Catalog
	skillWarnings []error
	skillRevision string
	mcp           *mcp.Manager
	mcpRevision   string

	closeOnce sync.Once
	closeErr  error
}

func New(userRoot string, root project.Root, options Options) (*Runtime, error) {
	skillOptions := options.SkillLoadOptions
	if skillOptions == (skill.LoadOptions{}) {
		skillOptions = skill.DefaultLoadOptions()
	}
	skills, warnings, err := skill.Load(userRoot, root, skillOptions)
	if err != nil {
		return nil, err
	}
	configured, err := mcp.Load(userRoot, root, options.MCPLoadOptions)
	if err != nil {
		return nil, err
	}
	manager, err := mcp.NewManager(configured, options.MCPClientFactory)
	if err != nil {
		return nil, err
	}
	skillRevision, err := catalogRevision(skills)
	if err != nil {
		_ = manager.Close()
		return nil, err
	}
	mcpRevision, err := revision(configured)
	if err != nil {
		_ = manager.Close()
		return nil, err
	}
	return &Runtime{
		skills: skills, skillWarnings: append([]error(nil), warnings...), skillRevision: skillRevision,
		mcp: manager, mcpRevision: mcpRevision,
	}, nil
}

func (runtime *Runtime) Skills() *skill.Catalog {
	if runtime == nil {
		return nil
	}
	return runtime.skills
}

func (runtime *Runtime) SetSkillEnabled(name string, enabled bool) error {
	if runtime == nil || runtime.skills == nil {
		return errors.New("Skill catalog is unavailable")
	}
	return runtime.skills.SetEnabled(name, enabled)
}

func (runtime *Runtime) SkillWarnings() []error {
	if runtime == nil {
		return nil
	}
	return append([]error(nil), runtime.skillWarnings...)
}

func (runtime *Runtime) SkillRevision() string {
	if runtime == nil {
		return ""
	}
	if runtime.skills != nil {
		if revision, err := runtime.skills.Revision(); err == nil {
			return revision
		}
	}
	return runtime.skillRevision
}

func (runtime *Runtime) MCP() *mcp.Manager {
	if runtime == nil {
		return nil
	}
	return runtime.mcp
}

func (runtime *Runtime) MCPRevision() string {
	if runtime == nil {
		return ""
	}
	binding := ""
	if runtime.mcp != nil {
		binding = runtime.mcp.BindingSnapshot().Revision
	}
	digest := sha256.Sum256([]byte(runtime.mcpRevision + "\x00" + binding))
	return hex.EncodeToString(digest[:])
}

func (runtime *Runtime) MCPBinding() mcp.BindingSnapshot {
	if runtime == nil || runtime.mcp == nil {
		return mcp.BindingSnapshot{}
	}
	return runtime.mcp.BindingSnapshot()
}

func (runtime *Runtime) ResolveSkillInjections(task string) ([]agentcontext.SkillInjection, error) {
	if runtime == nil || runtime.skills == nil {
		return nil, nil
	}
	matches := explicitSkillPattern.FindAllStringSubmatch(task, -1)
	result := make([]agentcontext.SkillInjection, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		if _, exists := seen[name]; exists {
			continue
		}
		value, err := runtime.skills.Load(name)
		if err != nil {
			return nil, fmt.Errorf("resolve explicit Skill %q: %w", name, err)
		}
		content := strings.TrimSpace(value.Content)
		digest := sha256.Sum256([]byte(content))
		result = append(result, agentcontext.SkillInjection{
			Name: value.Name, Content: content, ContentHash: hex.EncodeToString(digest[:]), Source: string(value.Source),
		})
		seen[name] = struct{}{}
	}
	return result, nil
}

func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	runtime.closeOnce.Do(func() {
		if runtime.mcp != nil {
			runtime.closeErr = runtime.mcp.Close()
		}
	})
	return runtime.closeErr
}

func catalogRevision(catalog *skill.Catalog) (string, error) {
	if catalog == nil {
		return "", errors.New("skill catalog is nil")
	}
	return catalog.Revision()
}

func revision(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
