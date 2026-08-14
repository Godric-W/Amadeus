package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/project"
	internalprompt "github.com/Godric-W/Amadeus/internal/prompt"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type contextPreparationOptions struct {
	Task             string
	TurnContext      *turn.Context
	Host             Host
	Agent            *agentruntime.Agent
	Extensions       *extensionruntime.Runtime
	FileSystemPolicy *project.FileSystemPolicy
	Instructions     *instruction.WorkspaceResolver
	ApprovalCount    func() int
}

func prepareTurnContext(ctx context.Context, options contextPreparationOptions) error {
	host, ok := options.Host.(ContextHost)
	if !ok || host.Context() == nil {
		return nil
	}
	tools := options.Agent.AvailableTools()
	mode := "execute"
	if options.TurnContext.InitialPermissionMode == turn.PermissionModePlan {
		mode = "plan"
		tools = planModeTools(tools)
	}
	developer, err := internalprompt.DeveloperInstructions(mode, promptToolNames(tools))
	if err != nil {
		return err
	}
	contextManager := host.Context()
	var contextItems []rollout.Item
	setUpdate := func(key agentcontext.UpdateKey, content string) error {
		content = strings.TrimSpace(content)
		if contextManager.Update(key) == content {
			return nil
		}
		item, err := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{Key: string(key), Content: content})
		if err != nil {
			return err
		}
		contextItems = append(contextItems, item)
		return nil
	}
	if err := setUpdate(agentcontext.UpdateDeveloperInstructions, developer); err != nil {
		return err
	}

	request, resolved, err := options.Instructions.ResolveTarget(ctx, options.TurnContext.CWD, instruction.TargetCommandCWD)
	if err != nil {
		return err
	}
	var agents strings.Builder
	for _, document := range resolved.Documents {
		if content := strings.TrimSpace(document.Content); content != "" {
			if agents.Len() > 0 {
				agents.WriteString("\n\n")
			}
			agents.WriteString("Instructions from ")
			agents.WriteString(document.Path)
			agents.WriteString(":\n")
			agents.WriteString(content)
		}
	}
	agentsText := "## Persistent Instructions\n\n{\"type\":\"amadeus.instructions.v1\"}"
	if content := strings.TrimSpace(agents.String()); content != "" {
		agentsText += "\n\n" + content
	}
	if err := setUpdate(agentcontext.UpdateAgents, agentsText); err != nil {
		return err
	}
	if err := setUpdate(agentcontext.UpdateEnvironment, fmt.Sprintf("## Workspace Context\n\nCurrent working directory: %s\nInstruction target: %s", options.TurnContext.CWD, request.TargetPath)); err != nil {
		return err
	}

	effective := options.FileSystemPolicy.EffectiveProfile()
	approvalCount := 0
	if options.ApprovalCount != nil {
		approvalCount = options.ApprovalCount()
	}
	permissionPayload := struct {
		ReadHost       bool     `json:"read_host"`
		WorkspaceRoots []string `json:"workspace_roots"`
		TemporaryRoots []string `json:"temporary_roots"`
		ReadOnlyRoots  []string `json:"read_only_roots"`
		DeniedRoots    []string `json:"denied_roots"`
		ApprovalCount  int      `json:"session_command_approval_count"`
	}{
		ReadHost: effective.ReadHost, WorkspaceRoots: effective.WorkspaceRoots,
		TemporaryRoots: effective.TemporaryRoots, ReadOnlyRoots: effective.ReadOnlyRoots, DeniedRoots: effective.DeniedRoots,
		ApprovalCount: approvalCount,
	}
	encodedPermission, err := json.Marshal(permissionPayload)
	if err != nil {
		return err
	}
	if err := setUpdate(agentcontext.UpdatePermissionMode, "## Permission And Isolation Context\n\nPermission context (enforced by runtime, not by this text): "+string(encodedPermission)); err != nil {
		return err
	}

	skillParts := make([]string, 0)
	if options.Extensions != nil {
		skillInjections, err := options.Extensions.ResolveSkillInjections(options.Task)
		if err != nil {
			return err
		}
		for _, injection := range skillInjections {
			skillParts = append(skillParts, "{\"type\":\"amadeus.skill_injection.v1\",\"name\":\""+injection.Name+"\"}\n"+injection.Content)
		}
		if err := setUpdate(agentcontext.UpdateMCP, "MCP tools are available only through their exposed Tool Specs and current bindings."); err != nil {
			return err
		}
	} else {
		if err := setUpdate(agentcontext.UpdateMCP, ""); err != nil {
			return err
		}
	}
	indexParts := make([]string, 0)
	for _, entry := range options.Agent.SkillIndex() {
		if !entry.Enabled {
			continue
		}
		indexParts = append(indexParts, fmt.Sprintf("- `%s`: %s", entry.Name, entry.Description))
	}
	if len(indexParts) > 0 {
		skillParts = append([]string{"## Skills And Extensions\n\n{\"type\":\"amadeus.skill_index.v1\",\"skills\":\n" + strings.Join(indexParts, "\n")}, skillParts...)
	} else if len(skillParts) > 0 {
		skillParts = append([]string{"## Skills And Extensions"}, skillParts...)
	} else {
		skillParts = []string{"## Skills And Extensions\n\nNo Skills are currently available."}
	}
	if err := setUpdate(agentcontext.UpdateSkills, strings.Join(skillParts, "\n\n")); err != nil {
		return err
	}
	if len(contextItems) > 0 {
		if err := options.Host.AppendItems(ctx, options.TurnContext.TurnID, contextItems...); err != nil {
			return fmt.Errorf("persist dynamic context updates: %w", err)
		}
	}
	return nil
}

func promptToolNames(specs []tool.ToolSpec) []string {
	result := make([]string, 0, len(specs))
	for _, spec := range specs {
		result = append(result, spec.Name)
	}
	return result
}

func planModeTools(specs []tool.ToolSpec) []tool.ToolSpec {
	allowedNetwork := map[string]struct{}{"web_search": {}, "web_fetch": {}, "mcp_list_tools": {}, "mcp_list_resources": {}, "mcp_read_resource": {}}
	result := make([]tool.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.Name == "update_plan" {
			continue
		}
		if spec.SideEffect == tool.SideEffectNone || spec.SideEffect == tool.SideEffectRead {
			result = append(result, spec.Clone())
			continue
		}
		if _, ok := allowedNetwork[spec.Name]; ok {
			result = append(result, spec.Clone())
		}
	}
	return result
}
