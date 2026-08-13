package policy

import (
	"testing"
)

func TestSessionPermissionContextUsesExactCommandKey(t *testing.T) {
	store := NewSessionPermissionContext()
	key, ok := NewCommandApprovalKey("echo a\r\n", "/tmp")
	if !ok {
		t.Fatal("key rejected")
	}
	store.ApplyCommandGrant(key)
	if store.GrantCount() != 1 {
		t.Fatalf("unexpected approval count: %d", store.GrantCount())
	}
	equivalent, _ := NewCommandApprovalKey("echo a\n", "/tmp")
	if !store.MatchCommand(equivalent) {
		t.Fatal("CRLF normalization did not match")
	}
	different, _ := NewCommandApprovalKey("echo b\n", "/tmp")
	if store.MatchCommand(different) {
		t.Fatal("different command reused approval")
	}
	store.Clear()
	if store.GrantCount() != 0 {
		t.Fatalf("approval count was not cleared: %d", store.GrantCount())
	}
}

func TestSessionPermissionContextUnifiedGrantAPI(t *testing.T) {
	context := NewSessionPermissionContext()
	command, ok := NewCommandApprovalKey("go test ./...", "/workspace")
	if !ok {
		t.Fatal("command key rejected")
	}
	grants := []PermissionGrant{
		FileDirectoryGrant("/workspace/pkg"),
		CommandGrant(command),
		ExternalGrant("web:example.com"),
	}
	for _, grant := range grants {
		if !grant.Valid() {
			t.Fatalf("grant is invalid: %#v", grant)
		}
		context.ApplyGrant(grant)
		if !context.Match(grant) {
			t.Fatalf("grant did not match after apply: %#v", grant)
		}
	}
	if context.Match(FileDirectoryGrant("/workspace/other")) {
		t.Fatal("file grant escaped its directory")
	}
	context.Clear()
	for _, grant := range grants {
		if context.Match(grant) {
			t.Fatalf("grant survived clear: %#v", grant)
		}
	}
}
