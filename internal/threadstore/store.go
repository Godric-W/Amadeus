package threadstore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/protocol/identity"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type CreateInput struct {
	SessionID        identity.SessionID
	ID               identity.ThreadID
	Source           protocol.SessionSource
	CWD              string
	Title            string
	ModelProvider    string
	Model            string
	BaseInstructions llm.BaseInstructions
	GitSHA           string
	GitBranch        string
	GitOriginURL     string
	CreatedAt        time.Time
}

func (input CreateInput) Validate() error {
	if input.SessionID.IsZero() || input.ID.IsZero() || strings.TrimSpace(input.Title) == "" {
		return errors.New("thread create input is incomplete")
	}
	if !filepath.IsAbs(input.CWD) || filepath.Clean(input.CWD) != input.CWD {
		return errors.New("thread create CWD must be clean and absolute")
	}
	if input.CreatedAt.IsZero() {
		return errors.New("thread create time is zero")
	}
	if err := input.BaseInstructions.ValidatePersisted(); err != nil {
		return err
	}
	if input.Source.Kind == "" {
		input.Source = protocol.RootSessionSource()
	}
	if err := input.Source.Validate(); err != nil {
		return err
	}
	if input.Source.IsSubAgent() {
		if input.Source.SubAgent.ParentThreadID == input.ID {
			return errors.New("child thread cannot be its own parent")
		}
	} else if input.SessionID != protocol.SessionIDFromThreadID(input.ID) {
		return errors.New("root session ID does not match root thread ID")
	}
	return nil
}

type InitialHistoryKind string

const (
	InitialHistoryNew     InitialHistoryKind = "new"
	InitialHistoryResumed InitialHistoryKind = "resumed"
)

type InitialHistory struct {
	Kind  InitialHistoryKind
	Lines []rollout.Line
}

func (history InitialHistory) Validate(id identity.ThreadID) error {
	if id.IsZero() {
		return errors.New("initial history thread ID is empty")
	}
	switch history.Kind {
	case InitialHistoryNew:
		if len(history.Lines) != 0 {
			return errors.New("new initial history must be empty")
		}
	case InitialHistoryResumed:
		if len(history.Lines) == 0 {
			return errors.New("resumed initial history must begin with session_meta")
		}
		if _, ok := history.Lines[0].Item.(rollout.SessionMetaItem); !ok {
			return errors.New("resumed initial history must begin with session_meta")
		}
		for index, line := range history.Lines {
			if err := line.Validate(id, uint64(index+1)); err != nil {
				return err
			}
		}
	default:
		return errors.New("initial history kind is invalid")
	}
	return nil
}

type AppendResult struct {
	FirstSequence   uint64
	Count           int
	MetadataWarning error
}

type ThreadStore interface {
	Materialize(context.Context, CreateInput) (AppendResult, error)
	OpenWriter(context.Context, identity.ThreadID) (InitialHistory, error)
	AppendItems(context.Context, identity.ThreadID, identity.TurnID, ...rollout.RolloutItem) (AppendResult, error)
	Flush(context.Context, identity.ThreadID) error
	CloseWriter(context.Context, identity.ThreadID) error
	DiscardWriter(context.Context, identity.ThreadID) error
	LoadHistory(context.Context, identity.ThreadID) (InitialHistory, error)
	GetThread(context.Context, identity.ThreadID) (StoredThread, error)
	ListThreads(context.Context, ListQuery) ([]StoredThread, error)
	ListOpenChildren(context.Context, identity.ThreadID) ([]StoredThread, error)
	RenameThread(context.Context, identity.ThreadID, string, time.Time) error
	DeleteThread(context.Context, identity.ThreadID) error
	RebuildIndex(context.Context) error
	Close() error
}
