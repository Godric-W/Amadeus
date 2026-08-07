package policy

import "testing"

func TestCommandGuardOnlyBlocksMinimalCatastrophicSet(t *testing.T) {
	guard := NewCommandGuard()
	for _, command := range []string{"go test ./...", "cat ../README.md", "bash -lc 'echo ok'"} {
		assessment, err := guard.Assess(command)
		if err != nil || assessment.Disposition != CommandRequireApproval {
			t.Fatalf("%q: %#v %v", command, assessment, err)
		}
	}
	assessment, err := guard.Assess("rm -rf /")
	if err != nil || assessment.Disposition != CommandDeny || assessment.Risk != CommandRiskBlocked {
		t.Fatalf("catastrophic command: %#v %v", assessment, err)
	}
}
