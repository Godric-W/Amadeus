package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/tool/builtin"
)

func (runner *agentController) compactInteractiveSession(ctx context.Context, invocation agentInvocation) (string, error) {
	active, _, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return "", err
	}
	projection, err := agentcontext.ProjectRolloutMessages(active.History())
	if err != nil {
		return "", err
	}
	if len(projection.Messages) == 0 {
		return "", errors.New("There is no conversation to compact")
	}
	if err := active.Submit(ctx, protocol.CompactOp{}); err != nil {
		return "", err
	}
	if err := runner.waitTurn(ctx, active, invocation.EventSink, invocation.Approvals); err != nil {
		return "", err
	}
	return "Conversation compacted", nil
}

func (runner *agentController) currentCapabilities(ctx context.Context, invocation agentInvocation) (agentsession.CapabilityView, error) {
	active, _, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return nil, err
	}
	capabilities, ok := active.CapabilityView()
	if !ok {
		return nil, errors.New("active thread capabilities are unavailable")
	}
	return capabilities, nil
}

func (runner *agentController) writeInteractiveSkills(ctx context.Context, invocation agentInvocation, writer io.Writer) error {
	capabilities, err := runner.currentCapabilities(ctx, invocation)
	if err != nil {
		return err
	}
	entries := capabilities.Skills()
	if len(entries) == 0 {
		_, err = fmt.Fprintln(writer, "No skills available.")
		return err
	}
	for _, entry := range entries {
		state := "enabled"
		if !entry.Enabled {
			state = "disabled"
		}
		if _, err := fmt.Fprintf(writer, "%s  %s  %s  %s\n", entry.Name, entry.Source, state, entry.Description); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (runner *agentController) writeInteractiveMCP(ctx context.Context, invocation agentInvocation, writer io.Writer, verbose bool) error {
	capabilities, err := runner.currentCapabilities(ctx, invocation)
	if err != nil {
		return err
	}
	servers := capabilities.MCPServers()
	sort.Strings(servers)
	if len(servers) == 0 {
		_, err = fmt.Fprintln(writer, "No MCP servers configured.")
		return err
	}
	bindings := capabilities.MCPBindings()
	byName := make(map[string]bool, len(bindings.Servers))
	for _, binding := range bindings.Servers {
		byName[binding.Name] = binding.ToolsLoaded
	}
	for _, server := range servers {
		if !verbose {
			state := "not started"
			if byName[server] {
				state = "tools loaded"
			}
			if _, err := fmt.Fprintf(writer, "%s  %s\n", server, state); err != nil {
				return err
			}
			continue
		}
		catalog, listErr := capabilities.MCPTools(ctx, server)
		if listErr != nil {
			if _, err := fmt.Fprintf(writer, "%s  error: %v\n", server, listErr); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(writer, "%s  %d tools\n", server, len(catalog.Tools)); err != nil {
			return err
		}
		for _, remote := range catalog.Tools {
			if _, err := fmt.Fprintf(writer, "  %s  %s\n", remote.Name, remote.Description); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeInteractiveTools(writer io.Writer) error {
	for _, spec := range builtin.CoreSpecs() {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", spec.Name, spec.SideEffect); err != nil {
			return err
		}
	}
	return nil
}
