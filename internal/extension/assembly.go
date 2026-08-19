package extension

import (
	"errors"
	"strings"
	"sync"

	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
)

type Options struct {
	SkillLoadOptions skill.LoadOptions
	MCPLoadOptions   mcp.LoadOptions
	MCPClientFactory mcp.ClientFactory
}

type Assembly struct {
	skills        *skill.SkillCatalog
	skillWarnings []error
	mcp           *mcp.MCPRuntime

	closeOnce sync.Once
	closeErr  error
}

func Assemble(userRoot string, root project.Root, options Options) (*Assembly, error) {
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
	manager, err := mcp.NewMCPRuntime(configured, options.MCPClientFactory)
	if err != nil {
		return nil, err
	}
	return &Assembly{
		skills: skills, skillWarnings: append([]error(nil), warnings...),
		mcp: manager,
	}, nil
}

func (assembly *Assembly) SkillCatalog() *skill.SkillCatalog {
	if assembly == nil {
		return nil
	}
	return assembly.skills
}

func (assembly *Assembly) SetSkillEnabled(name string, enabled bool) error {
	if assembly == nil || assembly.skills == nil {
		return errors.New("Skill catalog is unavailable")
	}
	return assembly.skills.SetEnabled(name, enabled)
}

func (assembly *Assembly) SkillWarnings() []error {
	if assembly == nil {
		return nil
	}
	return append([]error(nil), assembly.skillWarnings...)
}

func (assembly *Assembly) SkillRevision() string {
	if assembly == nil {
		return ""
	}
	if assembly.skills != nil {
		if revision, err := assembly.skills.Revision(); err == nil {
			return revision
		}
	}
	return ""
}

func (assembly *Assembly) MCPRuntime() *mcp.MCPRuntime {
	if assembly == nil {
		return nil
	}
	return assembly.mcp
}

func (assembly *Assembly) MCPRevision() string {
	if assembly == nil {
		return ""
	}
	if assembly.mcp == nil {
		return ""
	}
	return assembly.mcp.Revision()
}

func (assembly *Assembly) MCPBinding() mcp.MCPBinding {
	if assembly == nil || assembly.mcp == nil {
		return mcp.MCPBinding{}
	}
	return assembly.mcp.Binding()
}

func (assembly *Assembly) MCPConfiguration() mcp.Config {
	if assembly == nil || assembly.mcp == nil {
		return mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	}
	return assembly.mcp.Configuration()
}

func (assembly *Assembly) ResolveSkillInjections(task string) ([]agentcontext.SkillInjection, error) {
	if assembly == nil || assembly.skills == nil {
		return nil, nil
	}
	documents, err := assembly.skills.ResolveExplicit(task)
	if err != nil {
		return nil, err
	}
	result := make([]agentcontext.SkillInjection, 0, len(documents))
	for _, value := range documents {
		content := strings.TrimSpace(value.Content)
		result = append(result, agentcontext.SkillInjection{
			Name: value.Name, Path: value.PathToSkillMD, Revision: value.Revision, Content: content, Source: string(value.Source),
		})
	}
	return result, nil
}

func (assembly *Assembly) Close() error {
	if assembly == nil {
		return nil
	}
	assembly.closeOnce.Do(func() {
		if assembly.mcp != nil {
			assembly.closeErr = assembly.mcp.Close()
		}
	})
	return assembly.closeErr
}
