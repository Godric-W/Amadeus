package session

import (
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func validateSpawnIdentity(args SpawnArgs) error {
	if args.SessionID.IsZero() || args.ThreadID.IsZero() {
		return errors.New("session spawn identity is incomplete")
	}
	source := args.State.Configuration.Source
	if source.IsSubAgent() {
		if args.ParentThreadID == nil || args.ParentThreadID.IsZero() || *args.ParentThreadID != source.SubAgent.ParentThreadID {
			return errors.New("sub-agent spawn parent identity is inconsistent")
		}
		if args.ThreadID == *args.ParentThreadID {
			return errors.New("sub-agent thread cannot be its own parent")
		}
	} else {
		if args.ParentThreadID != nil {
			return errors.New("root session spawn has parent thread ID")
		}
		if args.SessionID != protocol.SessionIDFromThreadID(args.ThreadID) {
			return errors.New("root session ID does not match root thread ID")
		}
	}
	if args.History.Kind != thread.InitialHistoryResumed {
		return nil
	}
	meta, ok := args.History.Lines[0].Item.(rollout.SessionMetaItem)
	if !ok {
		return errors.New("resumed session history does not begin with session metadata")
	}
	if meta.SessionID != args.SessionID || meta.ID != args.ThreadID || !sameOptionalThreadID(meta.ParentThreadID, args.ParentThreadID) {
		return fmt.Errorf("resumed session identity does not match canonical session metadata")
	}
	return nil
}

func cloneOptionalThreadID(id *protocol.ThreadID) *protocol.ThreadID {
	if id == nil {
		return nil
	}
	cloned := *id
	return &cloned
}

func sameOptionalThreadID(left, right *protocol.ThreadID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
