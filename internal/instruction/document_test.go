package instruction

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstructionDocumentRecordsStableProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	document, err := NewInstructionDocument(SourceUser, path, UserScope(), "Use focused tests.\n")
	if err != nil {
		t.Fatalf("create instruction document: %v", err)
	}
	if document.Source != SourceUser || document.Path != path || document.Scope != UserScope() || len(document.SHA256) != 64 || document.Content != "Use focused tests.\n" {
		t.Fatalf("unexpected instruction document: %#v", document)
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("validate instruction document: %v", err)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal instruction document: %v", err)
	}
	for _, field := range []string{`"source"`, `"path"`, `"scope"`, `"sha256"`, `"content"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("instruction JSON omitted %s: %s", field, encoded)
		}
	}
}

func TestInstructionDocumentValidationRejectsInvalidState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	valid, err := NewInstructionDocument(SourceProject, path, ProjectScope(), "Project rules")
	if err != nil {
		t.Fatalf("create valid instruction document: %v", err)
	}
	directoryScope, err := NewDirectoryScope("pkg")
	if err != nil {
		t.Fatalf("create directory scope: %v", err)
	}
	tests := []struct {
		name     string
		mutate   func(InstructionDocument) InstructionDocument
		contains string
	}{
		{name: "invalid source", mutate: func(document InstructionDocument) InstructionDocument { document.Source = "memory"; return document }, contains: "source"},
		{name: "relative path", mutate: func(document InstructionDocument) InstructionDocument { document.Path = "AGENTS.md"; return document }, contains: "must be absolute"},
		{name: "invalid scope", mutate: func(document InstructionDocument) InstructionDocument {
			document.Scope = Scope{Kind: "tree"}
			return document
		}, contains: "scope kind"},
		{name: "user source project scope", mutate: func(document InstructionDocument) InstructionDocument { document.Source = SourceUser; return document }, contains: "requires user scope"},
		{name: "project source user scope", mutate: func(document InstructionDocument) InstructionDocument { document.Scope = UserScope(); return document }, contains: "cannot use user scope"},
		{name: "empty content", mutate: func(document InstructionDocument) InstructionDocument { document.Content = " \n"; return document }, contains: "content is empty"},
		{name: "invalid UTF-8", mutate: func(document InstructionDocument) InstructionDocument {
			document.Content = string([]byte{0xff})
			return document
		}, contains: "valid UTF-8"},
		{name: "short hash", mutate: func(document InstructionDocument) InstructionDocument { document.SHA256 = "abc"; return document }, contains: "64 hexadecimal"},
		{name: "non-hex hash", mutate: func(document InstructionDocument) InstructionDocument {
			document.SHA256 = strings.Repeat("z", 64)
			return document
		}, contains: "not hexadecimal"},
		{name: "mismatched hash", mutate: func(document InstructionDocument) InstructionDocument { document.Content = "changed"; return document }, contains: "does not match"},
		{name: "user source directory scope", mutate: func(document InstructionDocument) InstructionDocument {
			document.Source = SourceUser
			document.Scope = directoryScope
			return document
		}, contains: "requires user scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := test.mutate(valid)
			if err := document.Validate(); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected document validation error: %v", err)
			}
		})
	}
}

func TestInstructionScopesApplyByProjectDirectory(t *testing.T) {
	directory, err := NewDirectoryScope("pkg/service")
	if err != nil {
		t.Fatalf("create directory scope: %v", err)
	}
	tests := []struct {
		name   string
		scope  Scope
		target string
		want   bool
	}{
		{name: "user covers root", scope: UserScope(), target: ".", want: true},
		{name: "project covers nested", scope: ProjectScope(), target: "pkg/file.go", want: true},
		{name: "directory covers itself", scope: directory, target: "pkg/service", want: true},
		{name: "directory covers descendant", scope: directory, target: "pkg/service/file.go", want: true},
		{name: "directory excludes sibling", scope: directory, target: "pkg/other/file.go"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.scope.AppliesTo(test.target)
			if err != nil || got != test.want {
				t.Fatalf("unexpected scope application: got %v err=%v, want %v", got, err, test.want)
			}
		})
	}
	if directory.Specificity() <= ProjectScope().Specificity() || ProjectScope().Specificity() <= UserScope().Specificity() {
		t.Fatalf("unexpected instruction scope specificity: user=%d project=%d directory=%d", UserScope().Specificity(), ProjectScope().Specificity(), directory.Specificity())
	}
}

func TestInstructionScopesRejectInvalidPaths(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../outside", "/absolute", `pkg\\service`} {
		if _, err := NewDirectoryScope(path); err == nil {
			t.Fatalf("expected invalid directory scope %q to fail", path)
		}
	}
	tests := []Scope{
		{Kind: ScopeUser, Path: "."},
		{Kind: ScopeProject},
		{Kind: ScopeDirectory, Path: "pkg/../service"},
	}
	for _, scope := range tests {
		if err := scope.Validate(); err == nil {
			t.Fatalf("expected invalid scope to fail: %#v", scope)
		}
	}
	normalized, err := NewDirectoryScope("./pkg/../service")
	if err != nil || !reflect.DeepEqual(normalized, Scope{Kind: ScopeDirectory, Path: "service"}) {
		t.Fatalf("unexpected normalized directory scope: %#v err=%v", normalized, err)
	}
}
