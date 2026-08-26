package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/policy"
)

func TestTUIApprovalPermissionChoicesUseOnceThenSession(t *testing.T) {
	request, err := policy.NewApprovalRequestForPurpose(
		"permission-1", "execute_command", json.RawMessage(`{"command":"touch /outside/file"}`),
		policy.ApprovalPurposePermission, policy.CommandRiskHigh, policy.ApprovalCause{Kind: policy.ApprovalCauseFilesystemRead, Code: "outside_workspace"},
	)
	if err != nil {
		t.Fatal(err)
	}
	choices := approvalChoices(request)
	if len(choices) != 3 {
		t.Fatalf("unexpected permission choices: %#v", choices)
	}
	if choices[0].decision.Outcome != policy.ApprovalAllow || choices[0].decision.Scope != policy.ApprovalOnce || !strings.Contains(choices[0].description, "once") {
		t.Fatalf("unexpected once choice: %#v", choices[0])
	}
	if choices[1].decision.Outcome != policy.ApprovalAllow || choices[1].decision.Scope != policy.ApprovalSession || !strings.Contains(choices[1].label, "this session") {
		t.Fatalf("unexpected session choice: %#v", choices[1])
	}
	if choices[2].decision.Outcome != policy.ApprovalDeny || choices[2].decision.Scope != policy.ApprovalOnce {
		t.Fatalf("unexpected deny choice: %#v", choices[2])
	}
}
