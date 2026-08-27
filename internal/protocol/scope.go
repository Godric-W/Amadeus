package protocol

func ScopeEventMsg(message EventMsg, threadID ThreadID, turnID TurnID) EventMsg {
	switch value := message.(type) {
	case SessionConfiguredEvent:
		value.ThreadID = threadID
		return value
	case ThreadSettingsAppliedEvent:
		value.ThreadID = threadID
		return value
	case ThreadNameUpdatedEvent:
		value.ThreadID = threadID
		return value
	case ThreadArchivedEvent:
		value.ThreadID = threadID
		return value
	case ShutdownCompleteEvent:
		value.ThreadID = threadID
		return value
	case TurnStartedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ErrorEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TurnCompleteEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TurnAbortedEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case WarningEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case StreamErrorEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case ApprovalRequestEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case RequestUserInputEvent:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case SubagentNotificationEvent:
		value.ThreadID = threadID
		return value
	default:
		return ScopeItemEventMsg(message, threadID, turnID)
	}
}

func ThreadIDOf(message EventMsg) ThreadID {
	switch value := message.(type) {
	case SessionConfiguredEvent:
		return value.ThreadID
	case ThreadSettingsAppliedEvent:
		return value.ThreadID
	case ThreadNameUpdatedEvent:
		return value.ThreadID
	case ThreadArchivedEvent:
		return value.ThreadID
	case ShutdownCompleteEvent:
		return value.ThreadID
	case TurnStartedEvent:
		return value.ThreadID
	case ErrorEvent:
		return value.ThreadID
	case TurnCompleteEvent:
		return value.ThreadID
	case TurnAbortedEvent:
		return value.ThreadID
	case WarningEvent:
		return value.ThreadID
	case StreamErrorEvent:
		return value.ThreadID
	case ApprovalRequestEvent:
		return value.ThreadID
	case RequestUserInputEvent:
		return value.ThreadID
	case SubagentNotificationEvent:
		return value.ThreadID
	default:
		return ItemEventThreadID(message)
	}
}

func TurnIDOf(message EventMsg) TurnID {
	switch value := message.(type) {
	case TurnStartedEvent:
		return value.TurnID
	case ErrorEvent:
		return value.TurnID
	case TurnCompleteEvent:
		return value.TurnID
	case TurnAbortedEvent:
		return value.TurnID
	case WarningEvent:
		return value.TurnID
	case StreamErrorEvent:
		return value.TurnID
	case ApprovalRequestEvent:
		return value.TurnID
	case RequestUserInputEvent:
		return value.TurnID
	default:
		return ItemEventTurnID(message)
	}
}
