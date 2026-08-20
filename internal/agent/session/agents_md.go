package session

import (
	"context"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) refreshAgentsMd(ctx context.Context, services *SessionServices, turnID protocol.TurnID, cwd string) error {
	if services == nil || services.agentsMd == nil {
		return nil
	}
	loaded, _, err := services.agentsMd.Refresh(ctx, cwd)
	if err != nil {
		return fmt.Errorf("refresh AGENTS.md: %w", err)
	}
	content := loaded.Render()
	if session.ContextUpdate(agentcontext.UpdateAgents) == content {
		return nil
	}
	item, err := rollout.NewEventMsgItem(protocol.ContextUpdateEvent{
		Key: string(agentcontext.UpdateAgents), Content: content, Revision: loaded.Revision,
	})
	if err != nil {
		return err
	}
	if err := session.AppendItems(ctx, turnID, item); err != nil {
		return fmt.Errorf("persist AGENTS.md context: %w", err)
	}
	return nil
}

func (session *Session) captureStep(ctx context.Context, services *SessionServices, turnContext turn.TurnContext) (engine.StepContext, error) {
	if err := session.refreshAgentsMd(ctx, services, turnContext.TurnID, turnContext.CWD); err != nil {
		return engine.StepContext{}, err
	}
	return services.CaptureStep(session.Snapshot, turnContext)
}
