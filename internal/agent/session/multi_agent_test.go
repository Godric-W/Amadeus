package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/testutil"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestSubagentToolFilterIsExact(t *testing.T) {
	filter := composeToolFilters(nil, protocol.NewSubAgentSessionSource(testutil.ThreadID(1), 1, "atlas", "explorer"))
	for _, name := range []string{"read", "glob", "grep", "read_skill", "web_search"} {
		if !filter(tool.ToolSpec{Name: name}) {
			t.Fatalf("tool %q was hidden", name)
		}
	}
	for _, name := range []string{"edit", "write", "execute_command", "request_user_input", "spawn_agent", "mcp_call"} {
		if filter(tool.ToolSpec{Name: name}) {
			t.Fatalf("tool %q was exposed", name)
		}
	}
}

func TestSubagentBudgetUsesFrozenMultiAgentLimits(t *testing.T) {
	configured := config.Default()
	configured.Agent.MultiAgent.ChildMaxSamples = 7
	configured.Agent.MultiAgent.ChildMaxToolCalls = 13
	configured.Agent.MultiAgent.ChildMaxDuration = 2 * time.Minute
	budget := configuredTurnBudget(Configuration{Runtime: configured, Source: protocol.NewSubAgentSessionSource(testutil.ThreadID(1), 1, "atlas", "explorer")})
	if budget.MaxSamples != 7 || budget.MaxToolCalls != 13 || budget.MaxDuration != 2*time.Minute {
		t.Fatalf("child budget = %#v", budget)
	}
}

func TestRenderSubagentsUsesStableCodexShape(t *testing.T) {
	content := renderSubagents([]multiagent.AgentRecord{{
		Metadata: protocol.AgentMetadata{ThreadID: testutil.ThreadID(2), ParentThreadID: testutil.ThreadID(1), Depth: 1, AgentNickname: "atlas", AgentRole: "explorer"},
		Status:   protocol.AgentStatus{Kind: protocol.AgentStatusRunning},
	}})
	for _, fragment := range []string{"<subagents>", testutil.ThreadID(2).String() + ": atlas [explorer] running", "</subagents>"} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("subagents context missing %q: %s", fragment, content)
		}
	}
}

func TestSubagentApprovalPortAlwaysDeniesWithoutInteraction(t *testing.T) {
	decision, err := (denySubagentApprovalPort{}).Decide(context.Background(), policy.ApprovalRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed() || decision.Source != policy.ApprovalSourcePolicy || decision.Scope != policy.ApprovalOnce {
		t.Fatalf("subagent approval decision = %#v", decision)
	}
	if !strings.Contains(decision.Reason, "cannot request interactive approval") {
		t.Fatalf("subagent denial reason = %q", decision.Reason)
	}
}
