package builtin

import (
	"reflect"
	"strings"
	"testing"
)

func TestCatalogIsCompleteReadableAndImmutable(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("validate built-in Prompts: %v", err)
	}
	ids := All()
	if len(ids) == 0 {
		t.Fatal("built-in Prompt catalog is empty")
	}
	seen := make(map[ID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate built-in Prompt ID %q", id)
		}
		seen[id] = struct{}{}
		content, err := Read(id)
		if err != nil {
			t.Fatalf("read built-in Prompt %q: %v", id, err)
		}
		if strings.TrimSpace(content) == "" {
			t.Fatalf("built-in Prompt %q is empty", id)
		}
	}
	ids[0] = ContextCompaction
	if All()[0] != AgentBase {
		t.Fatal("built-in Prompt catalog shares mutable storage")
	}
	layers := AgentSystemLayers()
	layers[0] = ContextCompaction
	if AgentSystemLayers()[0] != AgentBase {
		t.Fatal("Agent System layer catalog shares mutable storage")
	}
}

func TestAgentSystemLayerOrderIsStable(t *testing.T) {
	want := []ID{AgentBase, AgentExecution, AgentHandoff}
	if got := AgentSystemLayers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Agent System layer order: got %v, want %v", got, want)
	}
}

func TestDeveloperLayersRespectModeAndToolExposure(t *testing.T) {
	execute, err := DeveloperLayers("execute", []string{"read", "update_plan", "edit", "write", "glob", "grep", "execute_command"})
	if err != nil {
		t.Fatalf("compose execute layers: %v", err)
	}
	wantExecute := []ID{ModeExecute, RuntimeWorkspace, RuntimePermission, RuntimeInstructions, RuntimeSkills, ToolsGeneral, ToolUpdatePlan, ToolExecuteCommand}
	if !reflect.DeepEqual(execute, wantExecute) {
		t.Fatalf("unexpected execute layers: got %v, want %v", execute, wantExecute)
	}
	plan, err := DeveloperLayers("plan", []string{"read"})
	if err != nil {
		t.Fatalf("compose plan layers: %v", err)
	}
	wantPlan := []ID{ModePlan, RuntimeWorkspace, RuntimeInstructions, RuntimeSkills}
	if !reflect.DeepEqual(plan, wantPlan) {
		t.Fatalf("unexpected plan layers: got %v, want %v", plan, wantPlan)
	}
	withoutSpecialTools, err := DeveloperLayers("execute", []string{"read"})
	if err != nil {
		t.Fatalf("compose read-only execute layers: %v", err)
	}
	for _, id := range withoutSpecialTools {
		if id == ToolUpdatePlan || id == ToolExecuteCommand {
			t.Fatalf("unexposed Tool guidance was selected: %v", withoutSpecialTools)
		}
	}
	if _, err := DeveloperLayers("adaptive", nil); err == nil {
		t.Fatal("unknown Prompt mode was accepted")
	}
}

func TestBuiltinsDoNotReintroduceRemovedAgentArchitectures(t *testing.T) {
	for _, id := range All() {
		content, err := Read(id)
		if err != nil {
			t.Fatalf("read built-in Prompt %q: %v", id, err)
		}
		for _, forbidden := range []string{"Replanner", "PlanReviewer", "Final Synthesizer", "Verification verdict", "Reflection verdict"} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("built-in Prompt %q contains removed architecture term %q", id, forbidden)
			}
		}
	}
}
