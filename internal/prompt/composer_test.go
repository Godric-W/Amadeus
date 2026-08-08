package prompt

import (
	"reflect"
	"strings"
	"testing"
)

func TestComposeDeveloperExecuteTracksExposureAndRuntimeFacts(t *testing.T) {
	assembler := newBuiltinAssembler(t)
	facts := testRuntimeFacts()
	bundles, err := ComposeDeveloper(assembler, "execute", []string{"read_file", "update_plan", "apply_patch", "execute_command"}, facts)
	if err != nil {
		t.Fatalf("compose execute Developer Prompts: %v", err)
	}
	wantIDs := []string{DeveloperModeBundle, DeveloperWorkspaceBundle, DeveloperPermissionBundle, DeveloperExtensionsBundle, DeveloperToolGuidanceBundle}
	if got := namedBundleIDs(bundles); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("unexpected Developer bundle order: got %v, want %v", got, wantIDs)
	}
	combined := namedBundleContent(bundles)
	for _, required := range []string{"## Execute Mode", "`/work/repo`", `["/tmp"]`, "request_permissions", "## `update_plan`", "multiple files or components", "## `apply_patch`", "## `execute_command`"} {
		if !strings.Contains(combined, required) {
			t.Fatalf("execute Developer Prompt omitted %q:\n%s", required, combined)
		}
	}

	facts.RunWritableRoots = []string{"/outside/run"}
	changed, err := ComposeDeveloper(assembler, "execute", []string{"read_file", "update_plan", "apply_patch", "execute_command"}, facts)
	if err != nil {
		t.Fatalf("recompose execute Developer Prompts: %v", err)
	}
	if namedBundleByID(t, bundles, DeveloperPermissionBundle).Bundle.SHA256 == namedBundleByID(t, changed, DeveloperPermissionBundle).Bundle.SHA256 {
		t.Fatal("dynamic Permission Prompt hash did not change after effective permissions changed")
	}
	if !strings.Contains(namedBundleContent(changed), `["/outside/run"]`) {
		t.Fatalf("dynamic Run permission missing from Developer Prompt: %s", namedBundleContent(changed))
	}
}

func TestComposeDeveloperPlanOmitsMutationAndPermissionGuidance(t *testing.T) {
	bundles, err := ComposeDeveloper(newBuiltinAssembler(t), "plan", []string{"read_file", "list_dir"}, testRuntimeFacts())
	if err != nil {
		t.Fatalf("compose Plan Developer Prompts: %v", err)
	}
	wantIDs := []string{DeveloperModeBundle, DeveloperWorkspaceBundle, DeveloperExtensionsBundle}
	if got := namedBundleIDs(bundles); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("unexpected Plan Developer bundle order: got %v, want %v", got, wantIDs)
	}
	combined := namedBundleContent(bundles)
	if !strings.Contains(combined, "## Plan Mode") {
		t.Fatalf("Plan Mode guidance missing: %s", combined)
	}
	for _, forbidden := range []string{"## `apply_patch`", "## `execute_command`", "request_permissions", "update_plan", "Additional writable roots"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("Plan Developer Prompt contains forbidden guidance %q: %s", forbidden, combined)
		}
	}
}

func TestComposeDeveloperInjectsToolSpecificGuidanceOnlyWhenExposed(t *testing.T) {
	assembler := newBuiltinAssembler(t)
	without, err := ComposeDeveloper(assembler, "execute", []string{"read_file"}, testRuntimeFacts())
	if err != nil {
		t.Fatalf("compose Developer Prompts without complex tools: %v", err)
	}
	withoutContent := namedBundleContent(without)
	for _, forbidden := range []string{"## `update_plan`", "## `apply_patch`", "## `execute_command`", "requested_permissions.writable_roots"} {
		if strings.Contains(withoutContent, forbidden) {
			t.Fatalf("unexposed Tool guidance %q was injected: %s", forbidden, withoutContent)
		}
	}
	withPatch, err := ComposeDeveloper(assembler, "execute", []string{"read_file", "apply_patch"}, testRuntimeFacts())
	if err != nil {
		t.Fatalf("compose Developer Prompts with apply_patch: %v", err)
	}
	if !strings.Contains(namedBundleContent(withPatch), "## `apply_patch`") || strings.Contains(namedBundleContent(withPatch), "## `execute_command`") {
		t.Fatalf("Tool-specific guidance did not follow exposure: %s", namedBundleContent(withPatch))
	}
	withPlan, err := ComposeDeveloper(assembler, "execute", []string{"read_file", "update_plan"}, testRuntimeFacts())
	if err != nil {
		t.Fatalf("compose Developer Prompts with update_plan: %v", err)
	}
	if !strings.Contains(namedBundleContent(withPlan), "## `update_plan`") || strings.Contains(namedBundleContent(withPlan), "## `apply_patch`") {
		t.Fatalf("update_plan guidance did not follow exposure: %s", namedBundleContent(withPlan))
	}
}

func TestComposeDeveloperRejectsInvalidFacts(t *testing.T) {
	facts := testRuntimeFacts()
	facts.CWD = " "
	if _, err := ComposeDeveloper(newBuiltinAssembler(t), "execute", nil, facts); err == nil || !strings.Contains(err.Error(), "cwd is empty") {
		t.Fatalf("unexpected empty cwd error: %v", err)
	}
	facts = testRuntimeFacts()
	facts.SessionCommandApprovalCount = -1
	if _, err := ComposeDeveloper(newBuiltinAssembler(t), "execute", nil, facts); err == nil || !strings.Contains(err.Error(), "approval count is negative") {
		t.Fatalf("unexpected approval count error: %v", err)
	}
}

func newBuiltinAssembler(t *testing.T) *Assembler {
	t.Helper()
	repository, err := NewBuiltinRepository()
	if err != nil {
		t.Fatalf("create built-in Prompt repository: %v", err)
	}
	assembler, err := NewAssembler(repository)
	if err != nil {
		t.Fatalf("create built-in Prompt assembler: %v", err)
	}
	return assembler
}

func testRuntimeFacts() RuntimeFacts {
	return RuntimeFacts{
		CWD: "/work/repo", WorkspaceRoots: []string{"/work/repo"}, TemporaryRoots: []string{"/tmp"},
		ReadOnlyRoots: []string{"/work/repo/vendor"}, DeniedRoots: []string{"/root/.ssh"}, ReadHost: true,
		IsolationMode: "sandboxed", RunWritableRoots: []string{}, SessionWritableRoots: []string{"/work/shared"},
		SessionCommandApprovalCount: 2,
	}
}

func namedBundleIDs(bundles []NamedBundle) []string {
	ids := make([]string, len(bundles))
	for index, bundle := range bundles {
		ids[index] = bundle.ID
	}
	return ids
}

func namedBundleContent(bundles []NamedBundle) string {
	parts := make([]string, len(bundles))
	for index, bundle := range bundles {
		parts[index] = bundle.Bundle.Content
	}
	return strings.Join(parts, "\n\n")
}

func namedBundleByID(t *testing.T, bundles []NamedBundle, id string) NamedBundle {
	t.Helper()
	for _, bundle := range bundles {
		if bundle.ID == id {
			return bundle
		}
	}
	t.Fatalf("Developer Prompt bundle %q not found", id)
	return NamedBundle{}
}
