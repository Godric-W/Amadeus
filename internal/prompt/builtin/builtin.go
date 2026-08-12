package builtin

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

type ID string

const (
	AgentBase           ID = "templates/agent/base.md"
	AgentExecution      ID = "templates/agent/execution.md"
	AgentHandoff        ID = "templates/agent/handoff.md"
	ModeExecute         ID = "templates/modes/execute.md"
	ModePlan            ID = "templates/modes/plan.md"
	RuntimeWorkspace    ID = "templates/runtime/workspace.md"
	RuntimePermission   ID = "templates/runtime/permissions.md"
	RuntimeInstructions ID = "templates/runtime/instructions.md"
	RuntimeSkills       ID = "templates/runtime/skills.md"
	ToolsGeneral        ID = "templates/tools/general.md"
	ToolUpdatePlan      ID = "templates/tools/update_plan.md"
	ToolExecuteCommand  ID = "templates/tools/execute_command.md"
	ContextCompaction   ID = "templates/context/compaction.md"
)

var agentSystemLayers = []ID{AgentBase, AgentExecution, AgentHandoff}

var all = []ID{
	AgentBase,
	AgentExecution,
	AgentHandoff,
	ModeExecute,
	ModePlan,
	RuntimeWorkspace,
	RuntimePermission,
	RuntimeInstructions,
	RuntimeSkills,
	ToolsGeneral,
	ToolUpdatePlan,
	ToolExecuteCommand,
	ContextCompaction,
}

var known = func() map[ID]struct{} {
	result := make(map[ID]struct{}, len(all))
	for _, id := range all {
		result[id] = struct{}{}
	}
	return result
}()

//go:embed templates/**/*.md
var embedded embed.FS

func AgentSystemLayers() []ID { return append([]ID(nil), agentSystemLayers...) }
func All() []ID               { return append([]ID(nil), all...) }

func DeveloperLayers(mode string, toolNames []string) ([]ID, error) {
	var modeLayer ID
	switch strings.TrimSpace(mode) {
	case "execute":
		modeLayer = ModeExecute
	case "plan":
		modeLayer = ModePlan
	default:
		return nil, fmt.Errorf("unknown Prompt run mode %q", mode)
	}
	visible := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("Prompt Tool name is empty")
		}
		visible[name] = struct{}{}
	}
	layers := []ID{modeLayer, RuntimeWorkspace}
	if modeLayer == ModePlan {
		return append(layers, RuntimeInstructions, RuntimeSkills), nil
	}
	layers = append(layers, RuntimePermission, RuntimeInstructions, RuntimeSkills, ToolsGeneral)
	if _, ok := visible["update_plan"]; ok {
		layers = append(layers, ToolUpdatePlan)
	}
	if _, ok := visible["execute_command"]; ok {
		layers = append(layers, ToolExecuteCommand)
	}
	return layers, nil
}

func Paths(ids []ID) []string {
	result := make([]string, len(ids))
	for index, id := range ids {
		result[index] = string(id)
	}
	return result
}

func Read(id ID) (string, error) {
	if _, ok := known[id]; !ok {
		return "", fmt.Errorf("unknown built-in Prompt %q", id)
	}
	content, err := embedded.ReadFile(string(id))
	if err != nil {
		return "", fmt.Errorf("read built-in Prompt %q: %w", id, err)
	}
	contentValue := strings.TrimSpace(string(content))
	if contentValue == "" {
		return "", fmt.Errorf("built-in Prompt %q is empty", id)
	}
	return contentValue, nil
}

func Embedded() fs.FS { return embedded }

func Validate() error {
	if len(agentSystemLayers) == 0 {
		return errors.New("built-in Agent Prompt layers are empty")
	}
	seen := make(map[ID]struct{}, len(all))
	for _, id := range all {
		if _, ok := seen[id]; ok {
			return fmt.Errorf("built-in Prompt %q is duplicated", id)
		}
		seen[id] = struct{}{}
		if _, err := Read(id); err != nil {
			return err
		}
	}
	return nil
}
