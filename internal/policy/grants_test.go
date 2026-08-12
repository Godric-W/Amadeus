package policy

import (
	"testing"
)

func TestSessionApprovalStoreUsesExactCommandKey(t *testing.T) {
	store := NewSessionApprovalStore()
	key, ok := NewCommandApprovalKey("echo a\r\n", "/tmp")
	if !ok {
		t.Fatal("key rejected")
	}
	store.Approve(key)
	if store.Count() != 1 {
		t.Fatalf("unexpected approval count: %d", store.Count())
	}
	equivalent, _ := NewCommandApprovalKey("echo a\n", "/tmp")
	if !store.IsApproved(equivalent) {
		t.Fatal("CRLF normalization did not match")
	}
	different, _ := NewCommandApprovalKey("echo b\n", "/tmp")
	if store.IsApproved(different) {
		t.Fatal("different command reused approval")
	}
	store.Clear()
	if store.Count() != 0 {
		t.Fatalf("approval count was not cleared: %d", store.Count())
	}
}
