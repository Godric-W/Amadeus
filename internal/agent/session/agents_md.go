package session

import (
	"context"
	"fmt"

	contextmanager "github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/protocol"
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
	if session.ContextUpdate(contextmanager.UpdateAgents) == content {
		return nil
	}
	item, err := rollout.NewEventMsgItem(protocol.ContextUpdateEvent{
		Key: string(contextmanager.UpdateAgents), Content: content, Revision: loaded.Revision,
	})
	if err != nil {
		return err
	}
	if err := session.AppendItems(ctx, turnID, item); err != nil {
		return fmt.Errorf("persist AGENTS.md context: %w", err)
	}
	return nil
}

func (session *Session) captureStep(ctx context.Context, services *SessionServices, turnContext TurnContext) (StepContext, error) {
	if err := session.refreshAgentsMd(ctx, services, turnContext.TurnID, turnContext.CWD); err != nil {
		return StepContext{}, err
	}
	return services.CaptureStep(session.Snapshot, turnContext)
}
