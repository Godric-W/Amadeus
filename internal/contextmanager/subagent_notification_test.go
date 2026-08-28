package contextmanager

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func TestSubagentNotificationOwnsTypedContextFragmentRendering(t *testing.T) {
	message := "verified result"
	lastTurn := &protocol.AgentTurnResult{TurnID: "turn-1", Outcome: protocol.TurnOutcomeCompleted, LastAgentMessage: &message}
	fragment, err := (SubagentNotification{
		AgentID: testutil.ThreadID(2), Nickname: "atlas",
		Status: protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, Message: message}, LastTurn: lastTurn,
	}).Fragment()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"<subagent_notification>", `"nickname":"atlas"`, `"turn_id":"turn-1"`, `"last_agent_message":"verified result"`, "</subagent_notification>"} {
		if !strings.Contains(fragment.Content, expected) {
			t.Fatalf("notification fragment missing %q: %s", expected, fragment.Content)
		}
	}
}
