package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Godric-W/Amadeus/internal/prompt/builtin"
)

const (
	DeveloperModeBundle         = "developer.mode"
	DeveloperWorkspaceBundle    = "developer.workspace"
	DeveloperPermissionBundle   = "developer.permission"
	DeveloperExtensionsBundle   = "developer.extensions"
	DeveloperToolGuidanceBundle = "developer.tools"
)

type RuntimeFacts struct {
	CWD                         string
	WorkspaceRoots              []string
	TemporaryRoots              []string
	ReadOnlyRoots               []string
	DeniedRoots                 []string
	ReadHost                    bool
	IsolationMode               string
	RunWritableRoots            []string
	SessionWritableRoots        []string
	SessionCommandApprovalCount int
}

func ComposeDeveloper(assembler *Assembler, mode string, toolNames []string, facts RuntimeFacts) ([]NamedBundle, error) {
	if assembler == nil {
		return nil, errors.New("Developer Prompt assembler is nil")
	}
	layers, err := builtin.DeveloperLayers(mode, toolNames)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(facts.CWD) == "" {
		return nil, errors.New("Developer Prompt cwd is empty")
	}
	variables, err := runtimeVariables(facts)
	if err != nil {
		return nil, err
	}

	type layerGroup struct {
		id     string
		layers []builtin.ID
	}
	groups := []layerGroup{
		{id: DeveloperModeBundle, layers: []builtin.ID{layers[0]}},
		{id: DeveloperWorkspaceBundle, layers: []builtin.ID{builtin.RuntimeWorkspace}},
	}
	if strings.TrimSpace(mode) == "execute" {
		groups = append(groups, layerGroup{id: DeveloperPermissionBundle, layers: []builtin.ID{builtin.RuntimePermission}})
	}
	groups = append(groups, layerGroup{id: DeveloperExtensionsBundle, layers: []builtin.ID{builtin.RuntimeInstructions, builtin.RuntimeSkills}})
	if strings.TrimSpace(mode) == "execute" {
		toolLayers := []builtin.ID{builtin.ToolsGeneral}
		for _, layer := range layers {
			if layer == builtin.ToolApplyPatch || layer == builtin.ToolExecuteCommand {
				toolLayers = append(toolLayers, layer)
			}
		}
		groups = append(groups, layerGroup{id: DeveloperToolGuidanceBundle, layers: toolLayers})
	}

	result := make([]NamedBundle, 0, len(groups))
	for _, group := range groups {
		required, requiredErr := requiredVariablesFor(group.layers)
		if requiredErr != nil {
			return nil, requiredErr
		}
		provided := make(map[string]string, len(required))
		for _, name := range required {
			provided[name] = variables[name]
		}
		bundle, assembleErr := assembler.Assemble(AssembleInput{Layers: builtin.Paths(group.layers), Variables: provided})
		if assembleErr != nil {
			return nil, fmt.Errorf("assemble %s: %w", group.id, assembleErr)
		}
		named := NamedBundle{ID: group.id, Bundle: bundle}
		if validateErr := named.Validate(); validateErr != nil {
			return nil, validateErr
		}
		result = append(result, named)
	}
	return result, nil
}

func runtimeVariables(facts RuntimeFacts) (map[string]string, error) {
	if facts.SessionCommandApprovalCount < 0 {
		return nil, errors.New("Developer Prompt Session command approval count is negative")
	}
	encode := func(name string, values []string) (string, error) {
		if values == nil {
			values = []string{}
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return "", fmt.Errorf("encode Developer Prompt %s: %w", name, err)
		}
		return string(encoded), nil
	}
	workspaceRoots, err := encode("workspace roots", facts.WorkspaceRoots)
	if err != nil {
		return nil, err
	}
	temporaryRoots, err := encode("temporary roots", facts.TemporaryRoots)
	if err != nil {
		return nil, err
	}
	readOnlyRoots, err := encode("read-only roots", facts.ReadOnlyRoots)
	if err != nil {
		return nil, err
	}
	deniedRoots, err := encode("denied roots", facts.DeniedRoots)
	if err != nil {
		return nil, err
	}
	runWritableRoots, err := encode("Run writable roots", facts.RunWritableRoots)
	if err != nil {
		return nil, err
	}
	sessionWritableRoots, err := encode("Session writable roots", facts.SessionWritableRoots)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"cwd":                            facts.CWD,
		"workspace_roots":                workspaceRoots,
		"temporary_roots":                temporaryRoots,
		"read_only_roots":                readOnlyRoots,
		"denied_roots":                   deniedRoots,
		"read_host":                      strconv.FormatBool(facts.ReadHost),
		"isolation_mode":                 strings.TrimSpace(facts.IsolationMode),
		"run_writable_roots":             runWritableRoots,
		"session_writable_roots":         sessionWritableRoots,
		"session_command_approval_count": strconv.Itoa(facts.SessionCommandApprovalCount),
	}, nil
}

func requiredVariablesFor(layers []builtin.ID) ([]string, error) {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, layer := range layers {
		content, err := builtin.Read(layer)
		if err != nil {
			return nil, err
		}
		for _, match := range variablePattern.FindAllStringSubmatch(content, -1) {
			if _, ok := seen[match[1]]; ok {
				continue
			}
			seen[match[1]] = struct{}{}
			result = append(result, match[1])
		}
	}
	return result, nil
}
