package protocol

import (
	"encoding/json"
	"fmt"
)

type EncodedEventMsg struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func EncodeEventMsg(message EventMsg) (EncodedEventMsg, error) {
	message = eventMsgValue(message)
	if tokenCount, ok := message.(TokenCountEvent); ok {
		if err := tokenCount.Validate(); err != nil {
			return EncodedEventMsg{}, err
		}
	}
	kind, err := eventMsgType(message)
	if err != nil {
		return EncodedEventMsg{}, err
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return EncodedEventMsg{}, fmt.Errorf("encode event message %q: %w", kind, err)
	}
	return EncodedEventMsg{Type: kind, Payload: payload}, nil
}

func DecodeEventMsg(encoded EncodedEventMsg) (EventMsg, error) {
	var message EventMsg
	switch encoded.Type {
	case "session_configured":
		message = &SessionConfiguredEvent{}
	case "thread_settings_applied":
		message = &ThreadSettingsAppliedEvent{}
	case "thread_name_updated":
		message = &ThreadNameUpdatedEvent{}
	case "thread_archived":
		message = &ThreadArchivedEvent{}
	case "shutdown_complete":
		message = &ShutdownCompleteEvent{}
	case "turn_started":
		message = &TurnStartedEvent{}
	case "error":
		message = &ErrorEvent{}
	case "turn_complete":
		message = &TurnCompleteEvent{}
	case "turn_aborted":
		message = &TurnAbortedEvent{}
	case "warning":
		message = &WarningEvent{}
	case "stream_error":
		message = &StreamErrorEvent{}
	case "approval_request":
		message = &ApprovalRequestEvent{}
	case "request_user_input":
		message = &RequestUserInputEvent{}
	case "item_started":
		message = &ItemStartedEvent{}
	case "item_completed":
		message = &ItemCompletedEvent{}
	case "agent_message_content_delta":
		message = &AgentMessageContentDeltaEvent{}
	case "reasoning_content_delta":
		message = &ReasoningContentDeltaEvent{}
	case "command_output_delta":
		message = &CommandOutputDeltaEvent{}
	case "plan_update":
		message = &PlanUpdateEvent{}
	case "plan_delta":
		message = &PlanDeltaEvent{}
	case "token_count":
		message = &TokenCountEvent{}
	case "context_update":
		message = &ContextUpdateEvent{}
	case "subagent_notification":
		message = &SubagentNotificationEvent{}
	default:
		return nil, fmt.Errorf("unsupported event message type %q", encoded.Type)
	}
	if len(encoded.Payload) == 0 || !json.Valid(encoded.Payload) {
		return nil, fmt.Errorf("event message %q payload is invalid", encoded.Type)
	}
	if err := json.Unmarshal(encoded.Payload, message); err != nil {
		return nil, fmt.Errorf("decode event message %q: %w", encoded.Type, err)
	}
	message = eventMsgValue(message)
	if tokenCount, ok := message.(TokenCountEvent); ok {
		if err := tokenCount.Validate(); err != nil {
			return nil, fmt.Errorf("decode event message %q: %w", encoded.Type, err)
		}
	}
	return message, nil
}

func eventMsgType(message EventMsg) (string, error) {
	switch message.(type) {
	case SessionConfiguredEvent:
		return "session_configured", nil
	case ThreadSettingsAppliedEvent:
		return "thread_settings_applied", nil
	case ThreadNameUpdatedEvent:
		return "thread_name_updated", nil
	case ThreadArchivedEvent:
		return "thread_archived", nil
	case ShutdownCompleteEvent:
		return "shutdown_complete", nil
	case TurnStartedEvent:
		return "turn_started", nil
	case ErrorEvent:
		return "error", nil
	case TurnCompleteEvent:
		return "turn_complete", nil
	case TurnAbortedEvent:
		return "turn_aborted", nil
	case WarningEvent:
		return "warning", nil
	case StreamErrorEvent:
		return "stream_error", nil
	case ApprovalRequestEvent:
		return "approval_request", nil
	case RequestUserInputEvent:
		return "request_user_input", nil
	case ItemStartedEvent:
		return "item_started", nil
	case ItemCompletedEvent:
		return "item_completed", nil
	case AgentMessageContentDeltaEvent:
		return "agent_message_content_delta", nil
	case ReasoningContentDeltaEvent:
		return "reasoning_content_delta", nil
	case CommandOutputDeltaEvent:
		return "command_output_delta", nil
	case PlanUpdateEvent:
		return "plan_update", nil
	case PlanDeltaEvent:
		return "plan_delta", nil
	case TokenCountEvent:
		return "token_count", nil
	case ContextUpdateEvent:
		return "context_update", nil
	case SubagentNotificationEvent:
		return "subagent_notification", nil
	default:
		return "", fmt.Errorf("unsupported event message %T", message)
	}
}

func eventMsgValue(message EventMsg) EventMsg {
	switch value := message.(type) {
	case *SessionConfiguredEvent:
		return *value
	case *ThreadSettingsAppliedEvent:
		return *value
	case *ThreadNameUpdatedEvent:
		return *value
	case *ThreadArchivedEvent:
		return *value
	case *ShutdownCompleteEvent:
		return *value
	case *TurnStartedEvent:
		return *value
	case *ErrorEvent:
		return *value
	case *TurnCompleteEvent:
		return *value
	case *TurnAbortedEvent:
		return *value
	case *WarningEvent:
		return *value
	case *StreamErrorEvent:
		return *value
	case *ApprovalRequestEvent:
		return *value
	case *RequestUserInputEvent:
		return *value
	case *ItemStartedEvent:
		return *value
	case *ItemCompletedEvent:
		return *value
	case *AgentMessageContentDeltaEvent:
		return *value
	case *ReasoningContentDeltaEvent:
		return *value
	case *CommandOutputDeltaEvent:
		return *value
	case *PlanUpdateEvent:
		return *value
	case *PlanDeltaEvent:
		return *value
	case *TokenCountEvent:
		return *value
	case *ContextUpdateEvent:
		return *value
	case *SubagentNotificationEvent:
		return *value
	default:
		return message
	}
}
