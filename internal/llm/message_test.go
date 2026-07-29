package llm

import "testing"

func TestMessageConstructors(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		role    Role
		content string
	}{
		{name: "system", message: SystemMessage("system prompt"), role: RoleSystem, content: "system prompt"},
		{name: "developer", message: DeveloperMessage("developer prompt"), role: RoleDeveloper, content: "developer prompt"},
		{name: "user", message: UserMessage("hello"), role: RoleUser, content: "hello"},
		{name: "assistant", message: AssistantMessage("hi"), role: RoleAssistant, content: "hi"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.message.Role != test.role {
				t.Fatalf("unexpected role: got %q, want %q", test.message.Role, test.role)
			}
			if test.message.Content != test.content {
				t.Fatalf("unexpected content: got %q, want %q", test.message.Content, test.content)
			}
			if test.message.Reasoning != "" {
				t.Fatalf("constructor set reasoning: got %q", test.message.Reasoning)
			}
		})
	}
}

func TestRoleValidation(t *testing.T) {
	for _, role := range []Role{RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool} {
		if !role.Valid() {
			t.Fatalf("known role is invalid: %q", role)
		}
	}
	if Role("observer").Valid() {
		t.Fatal("unknown role is valid")
	}
}
