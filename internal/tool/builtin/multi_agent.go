package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/multiagent"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type MultiAgentTools struct {
	control *multiagent.Control
}

func NewMultiAgentTools(control *multiagent.Control) ([]tool.ToolDefinition, error) {
	if control == nil {
		return nil, errors.New("multi-agent control is nil")
	}
	owner := &MultiAgentTools{control: control}
	return []tool.ToolDefinition{
		multiAgentTool{owner: owner, kind: "spawn_agent"},
		multiAgentTool{owner: owner, kind: "send_input"},
		multiAgentTool{owner: owner, kind: "wait_agent"},
		multiAgentTool{owner: owner, kind: "close_agent"},
	}, nil
}

type multiAgentTool struct {
	owner *MultiAgentTools
	kind  string
}

func (definition multiAgentTool) Spec() tool.ToolSpec { return multiAgentSpec(definition.kind) }
func (definition multiAgentTool) SupportsParallelToolCalls() bool {
	return definition.kind == "spawn_agent"
}

func (definition multiAgentTool) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	_, err := definition.decode(invocation)
	return err
}

func (definition multiAgentTool) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	arguments, err := definition.decode(invocation)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}

func (definition multiAgentTool) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	if definition.owner == nil || definition.owner.control == nil {
		return tool.ToolResult{}, errors.New("multi-agent control is unavailable")
	}
	var value any
	switch definition.kind {
	case "spawn_agent":
		arguments, ok := prepared.State.(spawnAgentArguments)
		if !ok {
			return tool.ToolResult{}, errors.New("spawn_agent preparation state is invalid")
		}
		result, err := definition.owner.control.Spawn(toolContext.Context, prepared.Invocation.ThreadID, arguments.Message)
		if err != nil {
			return tool.ToolResult{}, err
		}
		value = result
	case "send_input":
		arguments, ok := prepared.State.(sendInputArguments)
		if !ok {
			return tool.ToolResult{}, errors.New("send_input preparation state is invalid")
		}
		if err := definition.owner.control.SendInput(toolContext.Context, arguments.ID, arguments.Message, arguments.Interrupt); err != nil {
			return tool.ToolResult{}, err
		}
		snapshot := definition.owner.control.Snapshot(arguments.ID)
		value = map[string]any{"agent_id": arguments.ID, "nickname": snapshot.Nickname, "accepted": true}
	case "wait_agent":
		arguments, ok := prepared.State.(waitAgentArguments)
		if !ok {
			return tool.ToolResult{}, errors.New("wait_agent preparation state is invalid")
		}
		ids := arguments.IDs
		timeout := 30 * time.Second
		if arguments.TimeoutMS != nil {
			timeout = time.Duration(*arguments.TimeoutMS) * time.Millisecond
		}
		waited, err := definition.owner.control.Wait(toolContext.Context, ids, timeout)
		if err != nil {
			return tool.ToolResult{}, err
		}
		value = waited
	case "close_agent":
		arguments, ok := prepared.State.(closeAgentArguments)
		if !ok {
			return tool.ToolResult{}, errors.New("close_agent preparation state is invalid")
		}
		snapshot := definition.owner.control.Snapshot(arguments.ID)
		previous, err := definition.owner.control.CloseAgent(toolContext.Context, arguments.ID)
		if err != nil {
			return tool.ToolResult{}, err
		}
		value = map[string]any{"agent_id": arguments.ID, "nickname": snapshot.Nickname, "previous_status": previous, "previous_last_turn": snapshot.LastTurn}
	default:
		return tool.ToolResult{}, fmt.Errorf("unknown multi-agent tool %q", definition.kind)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return tool.ToolResult{}, err
	}
	return tool.ToolResult{ToolName: definition.kind, Text: string(encoded), Data: value}, nil
}

type spawnAgentArguments struct {
	Message string `json:"message"`
}
type sendInputArguments struct {
	ID        protocol.ThreadID `json:"id"`
	Message   string            `json:"message"`
	Interrupt bool              `json:"interrupt"`
}
type waitAgentArguments struct {
	IDs       []protocol.ThreadID `json:"ids"`
	TimeoutMS *int                `json:"timeout_ms"`
}
type closeAgentArguments struct {
	ID protocol.ThreadID `json:"id"`
}

func (definition multiAgentTool) decode(invocation tool.Invocation) (any, error) {
	switch definition.kind {
	case "spawn_agent":
		var value spawnAgentArguments
		if err := decodeArguments(invocation.Call.Payload, &value); err != nil {
			return nil, err
		}
		if strings.TrimSpace(value.Message) == "" {
			return nil, errors.New("spawn_agent message is empty")
		}
		return value, nil
	case "send_input":
		var value sendInputArguments
		if err := decodeArguments(invocation.Call.Payload, &value); err != nil {
			return nil, err
		}
		if value.ID.IsZero() || strings.TrimSpace(value.Message) == "" {
			return nil, errors.New("send_input arguments are incomplete")
		}
		return value, nil
	case "wait_agent":
		var value waitAgentArguments
		if err := decodeArguments(invocation.Call.Payload, &value); err != nil {
			return nil, err
		}
		if len(value.IDs) == 0 {
			return nil, errors.New("wait_agent IDs are empty")
		}
		for _, id := range value.IDs {
			if id.IsZero() {
				return nil, errors.New("wait_agent contains an empty ID")
			}
		}
		if value.TimeoutMS != nil && (*value.TimeoutMS < 10_000 || *value.TimeoutMS > 3_600_000) {
			return nil, errors.New("wait_agent timeout_ms must be between 10000 and 3600000")
		}
		return value, nil
	case "close_agent":
		var value closeAgentArguments
		if err := decodeArguments(invocation.Call.Payload, &value); err != nil {
			return nil, err
		}
		if value.ID.IsZero() {
			return nil, errors.New("close_agent ID is empty")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unknown multi-agent tool %q", definition.kind)
	}
}

func multiAgentSpec(name string) tool.ToolSpec {
	const spawnGuidance = `Spawn a read-only explorer sub-agent for one concrete, bounded, self-contained side task. First identify the critical path and delegate only independent exploration that materially advances the main task. Do not delegate work needed for your immediate next action, duplicate already delegated work, or spawn repeatedly for one unresolved task. Continue useful non-overlapping local work after spawning. A successfully spawned child belongs to the root thread tree and continues if your current turn ends. Spawn multiple independent investigations in parallel when appropriate, and use wait_agent only when a result blocks the next critical-path step. The child cannot modify files, execute commands, request user input, or spawn agents. Do not guess why an agent stopped; use its typed status, last_turn outcome, and reason.`
	specs := map[string]tool.ToolSpec{
		"spawn_agent": {Name: "spawn_agent", Description: spawnGuidance, InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","minLength":1,"description":"Complete delegated brief including objective, relevant context, scope, and expected evidence."}},"required":["message"],"additionalProperties":false}`), SideEffect: tool.SideEffectNone},
		"send_input":  {Name: "send_input", Description: "Send follow-up input to an existing sub-agent. Reuse an agent when the follow-up depends on its existing context; set interrupt=true only to redirect active work immediately.", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","minLength":1,"description":"Agent thread ID returned by spawn_agent."},"message":{"type":"string","minLength":1,"description":"Follow-up task or clarification."},"interrupt":{"type":"boolean","description":"Interrupt the active child turn before delivering this input. Defaults to false."}},"required":["id","message"],"additionalProperties":false}`), SideEffect: tool.SideEffectNone},
		"wait_agent":  {Name: "wait_agent", Description: "Wait until any target sub-agent reaches a final status. Returns every target already final at that moment, including its authoritative final message and last_turn outcome/reason. Interrupted is not final because the child can receive more input. Use sparingly: completed agents also send a bounded parent notification. A timeout returns timed_out=true with no fabricated final statuses and does not close agents.", InputSchema: json.RawMessage(`{"type":"object","properties":{"ids":{"type":"array","minItems":1,"description":"Agent thread IDs to observe.","items":{"type":"string","minLength":1}},"timeout_ms":{"type":"integer","minimum":10000,"maximum":3600000,"description":"Wait timeout in milliseconds. Defaults to 30000."}},"required":["ids"],"additionalProperties":false}`), SideEffect: tool.SideEffectNone},
		"close_agent": {Name: "close_agent", Description: "Close an agent and any open descendants when no longer needed, returning the target status observed before shutdown. Closed agents release their concurrency slot.", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","minLength":1,"description":"Agent thread ID returned by spawn_agent."}},"required":["id"],"additionalProperties":false}`), SideEffect: tool.SideEffectNone},
	}
	return specs[name]
}

var _ tool.ToolDefinition = multiAgentTool{}
