package session

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestPermissionWorldStateConsumesCurrentModelMessages(t *testing.T) {
	content, err := renderPermissionContext(StepContext{}, llm.ModelMessages{
		Permissions: llm.PermissionMessages{WorkspaceWrite: "model workspace guidance"},
		Approvals:   llm.ApprovalMessages{OnRequest: "model approval guidance"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"model workspace guidance", "model approval guidance", "enforced by the runtime"} {
		if !strings.Contains(content, required) {
			t.Fatalf("permission WorldState omitted %q: %s", required, content)
		}
	}
}
