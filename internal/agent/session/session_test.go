package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func TestPreviousTurnSettingsUsesLatestCanonicalTurnContext(t *testing.T) {
	first := turn.Context{
		ThreadID: "thread-1", TurnID: "turn-1", Provider: "openai", Model: "first", CWD: "/workspace",
		InitialPermissionMode: turn.PermissionModeDefault,
	}
	second := turn.Context{
		ThreadID: "thread-1", TurnID: "turn-2", Provider: "openai", Model: "second", CWD: "/workspace",
		CurrentDate: "2026-08-11", Timezone: "Asia/Shanghai", InitialPermissionMode: turn.PermissionModePlan,
		ToolNames: []string{"read", "edit"}, OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	firstItem, err := rollout.NewItem(rollout.KindTurnContext, first)
	if err != nil {
		t.Fatal(err)
	}
	secondItem, err := rollout.NewItem(rollout.KindTurnContext, second)
	if err != nil {
		t.Fatal(err)
	}
	lines := []rollout.Line{
		{Version: rollout.CurrentVersion, Sequence: 1, Timestamp: time.Now().UTC(), ThreadID: "thread-1", TurnID: "turn-1", Item: firstItem},
		{Version: rollout.CurrentVersion, Sequence: 2, Timestamp: time.Now().UTC(), ThreadID: "thread-1", TurnID: "turn-2", Item: secondItem},
	}
	settings := previousTurnSettings(lines)
	if settings == nil || settings.Model != "second" || settings.PermissionMode != turn.PermissionModePlan || len(settings.ToolNames) != 2 {
		t.Fatalf("settings = %#v", settings)
	}
	second.ToolNames[0] = "changed"
	second.OutputSchema[0] = 'x'
	if settings.ToolNames[0] != "read" || string(settings.OutputSchema) != `{"type":"object"}` {
		t.Fatalf("settings alias source data: %#v", settings)
	}
}
