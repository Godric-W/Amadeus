package session

import (
	"context"
	"errors"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func (session *Session) prepareInitialTurnInput(ctx context.Context, task *regularTask, turnContext TurnContext) error {
	if session == nil || task == nil || task.runtime == nil || task.modelSession == nil {
		return errors.New("initial user input preparation is incomplete")
	}
	for {
		step, err := session.captureStep(ctx, task.runtime, turnContext)
		if err != nil {
			return err
		}
		prompt := session.promptSnapshot(step)
		status, err := session.refreshContextWindowStatus(ctx, turnContext.TurnID, step, prompt, task.events)
		if err != nil {
			return err
		}
		if !status.TokenLimitReached {
			break
		}
		if len(session.ContextProjection().Messages) == 0 {
			return errors.New("context limit reached before initial input with no compactable history")
		}
		compacted, err := session.runCompaction(ctx, task.runtime, task.modelSession, turnContext, &step, &prompt, task.events, compactionInvocation{
			Trigger: protocol.CompactionTriggerAuto, Reason: protocol.CompactionReasonContextLimit, Phase: protocol.CompactionPhasePreTurn,
		})
		if err != nil {
			return err
		}
		if !compacted {
			break
		}
	}
	if _, err := session.captureStep(ctx, task.runtime, turnContext); err != nil {
		return err
	}
	switch input := task.initialInput.(type) {
	case UserTurnInput:
		responseItem, err := rollout.NewResponseItem(rollout.ResponseItem{Type: rollout.ResponseUserMessage, Role: string(llm.RoleUser), Content: input.Content})
		if err != nil {
			return err
		}
		createdAt := task.startedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		userItem := completedUserMessageItem(protocol.ItemID(session.services.NextID("item")), input.Content, input.ClientID, createdAt)
		items := []rollout.RolloutItem{turnContextItem(turnContext), responseItem, rollout.EventMsgItem{Msg: protocol.ItemCompletedEvent{Item: userItem}}}
		skillItems, err := explicitSkillItems(task.runtime, input.Content)
		if err != nil {
			return err
		}
		items = append(items, skillItems...)
		if err := session.appendItemsDurable(ctx, turnContext.TurnID, items...); err != nil {
			return err
		}
		return task.events.Publish(ctx, protocol.Event{Msg: protocol.ItemCompletedEvent{Item: userItem}})
	case ResponseItemTurnInput:
		return session.appendItemsDurable(ctx, turnContext.TurnID, turnContextItem(turnContext), input.Item)
	default:
		return errors.New("regular task initial input kind is unsupported")
	}
}
