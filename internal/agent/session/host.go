package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func (session *Session) AppendItems(ctx context.Context, turnID turn.ID, items ...rollout.Item) error {
	return session.appendItems(ctx, turnID, false, items...)
}

func (session *Session) appendItemsDurable(ctx context.Context, turnID turn.ID, items ...rollout.Item) error {
	return session.appendItems(ctx, turnID, true, items...)
}

func (session *Session) appendItems(ctx context.Context, turnID turn.ID, durable bool, items ...rollout.Item) error {
	if session == nil {
		return errors.New("session is nil")
	}
	if ctx == nil {
		return errors.New("append rollout context is nil")
	}
	var result thread.AppendResult
	var err error
	if durable {
		result, err = session.services.LiveThread.AppendItems(ctx, turnID, items...)
	} else {
		result, err = session.services.LiveThread.AppendItemsBuffered(ctx, turnID, items...)
	}
	if err != nil {
		return err
	}
	session.appendHistory(result.Lines)
	session.contextMu.Lock()
	rebuildErr := session.state.Context.Rebuild(session.History())
	session.contextMu.Unlock()
	if rebuildErr != nil {
		return fmt.Errorf("rebuild context after rollout append: %w", rebuildErr)
	}
	if result.MetadataWarning != nil {
		session.publish(protocol.SessionEvent{ThreadID: session.threadID, TurnID: turnID, Message: protocol.Warning{Message: result.MetadataWarning.Error()}})
	}
	return nil
}

func (session *Session) UpdatePlan(ctx context.Context, turnID turn.ID, update plan.Update) (plan.Snapshot, error) {
	if session == nil || session.state.Plan == nil {
		return plan.Snapshot{}, errors.New("session plan state is unavailable")
	}
	return session.state.Plan.ApplyPersistent(update, session.services.Clock().UTC(), func(snapshot plan.Snapshot) error {
		item, err := rollout.NewItem(rollout.KindPlanUpdate, snapshot)
		if err != nil {
			return err
		}
		return session.appendItemsDurable(ctx, turnID, item)
	})
}

func (session *Session) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	if session == nil || session.state.Context == nil {
		return agentcontext.PromptSnapshot{}
	}
	return session.state.Context.Snapshot(model, prompt)
}

func (session *Session) ContextUpdate(key agentcontext.UpdateKey) string {
	if session == nil || session.state.Context == nil {
		return ""
	}
	return session.state.Context.Update(key)
}

func (session *Session) History() []rollout.Line {
	if session == nil {
		return nil
	}
	session.historyMu.RLock()
	defer session.historyMu.RUnlock()
	return cloneLines(session.state.History)
}

type CapabilityView interface {
	PermissionGrantCount() int
	SkillRevision() string
	MCPRevision() string
	Skills() []skill.IndexEntry
	SetSkillEnabled(string, bool) error
	MCPServers() []string
	MCPBindings() mcp.BindingSnapshot
	MCPTools(context.Context, string) ([]mcp.RemoteTool, error)
}

func (session *Session) CapabilityView() (CapabilityView, bool) {
	services := session.AgentServices()
	if services == nil {
		return nil, false
	}
	return services, true
}

func (session *Session) AgentServices() *engine.Services {
	if session == nil {
		return nil
	}
	return session.services.AgentServices
}

func (session *Session) appendHistory(lines []rollout.Line) {
	if len(lines) == 0 {
		return
	}
	session.historyMu.Lock()
	session.state.History = append(session.state.History, cloneLines(lines)...)
	session.historyMu.Unlock()
}

func latestPlanSnapshot(lines []rollout.Line) (plan.Snapshot, bool) {
	for index := len(lines) - 1; index >= 0; index-- {
		if lines[index].Item.Kind != rollout.KindPlanUpdate {
			continue
		}
		var snapshot plan.Snapshot
		if json.Unmarshal(lines[index].Item.Payload, &snapshot) == nil {
			return snapshot, true
		}
	}
	return plan.Snapshot{}, false
}

func (session *Session) Rename(ctx context.Context, title string, at time.Time) error {
	if session == nil {
		return errors.New("session is nil")
	}
	if at.IsZero() {
		return errors.New("session rename time is zero")
	}
	item, err := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{Title: strings.TrimSpace(title)})
	if err != nil {
		return err
	}
	return session.appendItemsDurable(ctx, "", item)
}
