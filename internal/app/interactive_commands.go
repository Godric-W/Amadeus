package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (application *InteractiveApplication) SubmitUser(ctx context.Context, content, clientUserMessageID string, overrides protocol.ThreadSettingsOverrides) (protocol.UserMessageAdmission, error) {
	if content == "" {
		return protocol.UserMessageAdmission{}, errors.New("interactive user message is empty")
	}
	if application.maxUserMessageBytes > 0 && len(content) > application.maxUserMessageBytes {
		return protocol.UserMessageAdmission{}, fmt.Errorf("interactive user message exceeds %d bytes", application.maxUserMessageBytes)
	}
	active, _, err := application.current()
	if err != nil {
		return protocol.UserMessageAdmission{}, err
	}
	return active.SubmitUserInputAndWaitForAdmission(ctx, protocol.UserInputOp{
		Content: content, ClientUserMessageID: strings.TrimSpace(clientUserMessageID), ThreadSettings: overrides,
	})
}

func (application *InteractiveApplication) SubmitCompact(ctx context.Context) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	application.mu.Lock()
	application.phase = "compacting"
	application.mu.Unlock()
	if err := active.Submit(ctx, protocol.CompactOp{}); err != nil {
		application.mu.Lock()
		application.phase = "idle"
		application.mu.Unlock()
		return err
	}
	return nil
}

func (application *InteractiveApplication) SetMode(ctx context.Context, mode protocol.ModeKind) error {
	if mode != protocol.ModeKindDefault && mode != protocol.ModeKindPlan {
		return fmt.Errorf("collaboration mode %q is invalid", mode)
	}
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.ThreadSettingsOp{Mode: mode})
}

func (application *InteractiveApplication) Interrupt(ctx context.Context) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.InterruptOp{})
}

func (application *InteractiveApplication) ResolveApproval(ctx context.Context, requestID string, decision policy.ApprovalDecision) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.ApprovalDecisionOp{
		RequestID: protocol.RequestID(requestID), OptionID: decision.OptionID, Outcome: string(decision.Outcome),
		Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason,
	})
}

func (application *InteractiveApplication) ResolveUserInput(ctx context.Context, requestID protocol.RequestID, response protocol.RequestUserInputResponse) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.UserInputAnswerOp{RequestID: requestID, Response: response})
}
