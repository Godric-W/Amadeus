package session

import (
	"context"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

type runtimeToolsAgentHost struct{}

func (runtimeToolsAgentHost) SpawnChild(context.Context, *multiagent.Control, multiagent.SpawnChildRequest) (multiagent.AgentRuntime, error) {
	panic("unexpected child spawn")
}

func (runtimeToolsAgentHost) ResumeChild(context.Context, *multiagent.Control, protocol.ThreadID) (multiagent.AgentRuntime, error) {
	panic("unexpected child resume")
}

func (runtimeToolsAgentHost) RecordSpawnEdge(context.Context, protocol.ThreadID, protocol.ThreadID, protocol.AgentSpawnEdgeState) error {
	panic("unexpected agent edge mutation")
}

func (runtimeToolsAgentHost) NotifyParent(context.Context, protocol.ThreadID, multiagent.Notification) error {
	panic("unexpected parent notification")
}

func TestBuildToolRuntimeScopesMultiAgentToolsToEnabledRoot(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: root.Path(), Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{root.Path()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mcpRuntime, err := mcp.NewMCPRuntime(mcp.Config{Servers: map[string]mcp.ServerConfig{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	configured := config.Default()
	control, err := multiagent.NewControl(testutil.SessionID(1), testutil.ThreadID(1), runtimeToolsAgentHost{}, multiagent.Options{
		MaxAgents: configured.Agent.MultiAgent.MaxAgents,
		MaxDepth:  configured.Agent.MultiAgent.MaxDepth,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close(context.Background())

	tests := []struct {
		name        string
		enabled     bool
		source      protocol.SessionSource
		control     *multiagent.Control
		wantPresent bool
	}{
		{name: "enabled root", enabled: true, source: protocol.RootSessionSource(), control: control, wantPresent: true},
		{name: "enabled subagent", enabled: true, source: protocol.NewSubAgentSessionSource(testutil.ThreadID(1), 1, "atlas", "explorer"), control: control},
		{name: "disabled root", enabled: false, source: protocol.RootSessionSource()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := configured
			current.Agent.MultiAgent.Enabled = test.enabled
			runtime, buildErr := BuildToolRuntime(ToolRuntimeOptions{
				Config: current, Project: root, Events: protocol.NewMemorySink(),
				FileSystemPolicy: filesystem, AgentControl: test.control, SessionSource: test.source, MCP: mcpRuntime,
			})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			defer runtime.Processes.Close()
			for _, name := range []string{"spawn_agent", "send_input", "wait_agent", "close_agent"} {
				_, present := runtime.Registry.Lookup(name)
				if present != test.wantPresent {
					t.Fatalf("tool %q present = %v, want %v", name, present, test.wantPresent)
				}
			}
		})
	}
}
