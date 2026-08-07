package policy

import (
	"testing"

	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
)

func TestSessionApprovalStoreUsesExactCommandKey(t *testing.T) {
	store := NewSessionApprovalStore()
	key, ok := NewCommandApprovalKey("/bin/sh", "echo a\r\n", "/tmp", false, sandboxdomain.IsolationUnsandboxed)
	if !ok {
		t.Fatal("key rejected")
	}
	store.Approve(key)
	if store.Count() != 1 {
		t.Fatalf("unexpected approval count: %d", store.Count())
	}
	equivalent, _ := NewCommandApprovalKey("/bin/sh", "echo a\n", "/tmp", false, sandboxdomain.IsolationUnsandboxed)
	if !store.IsApproved(equivalent) {
		t.Fatal("CRLF normalization did not match")
	}
	different, _ := NewCommandApprovalKey("/bin/sh", "echo a\n", "/tmp", true, sandboxdomain.IsolationUnsandboxed)
	if store.IsApproved(different) {
		t.Fatal("TTY difference reused approval")
	}
	store.Clear()
	if store.Count() != 0 {
		t.Fatalf("approval count was not cleared: %d", store.Count())
	}
}
