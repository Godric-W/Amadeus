package thread

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
)

type ID = rollout.ThreadID

type TurnID = rollout.TurnID

type CreateInput struct {
	ID            ID
	CWD           string
	Title         string
	ModelProvider string
	Model         string
	GitSHA        string
	GitBranch     string
	GitOriginURL  string
	CreatedAt     time.Time
}

func (input CreateInput) Validate() error {
	if strings.TrimSpace(string(input.ID)) == "" || strings.TrimSpace(input.Title) == "" {
		return errors.New("thread create input is incomplete")
	}
	if !filepath.IsAbs(input.CWD) || filepath.Clean(input.CWD) != input.CWD {
		return errors.New("thread create CWD must be clean and absolute")
	}
	if input.CreatedAt.IsZero() {
		return errors.New("thread create time is zero")
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

func (history InitialHistory) Validate(id ID) error {
	if id == "" {
		return errors.New("initial history thread ID is empty")
	}
	switch history.Kind {
	case InitialHistoryNew:
		if len(history.Lines) != 0 {
			return errors.New("new initial history must be empty")
		}
	case InitialHistoryResumed:
		if len(history.Lines) == 0 || history.Lines[0].Item.Kind != rollout.KindSessionMeta {
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
	Lines           []rollout.Line
	MetadataWarning error
}

type ThreadStore interface {
	Materialize(context.Context, CreateInput) (AppendResult, error)
	OpenWriter(context.Context, ID) (InitialHistory, error)
	AppendItems(context.Context, ID, TurnID, ...rollout.Item) (AppendResult, error)
	Flush(context.Context, ID) error
	CloseWriter(context.Context, ID) error
	LoadHistory(context.Context, ID) (InitialHistory, error)
	GetThread(context.Context, ID) (state.StoredThread, error)
	ListThreads(context.Context, state.ListQuery) ([]state.StoredThread, error)
	RenameThread(context.Context, ID, string, time.Time) error
	DeleteThread(context.Context, ID, time.Time) error
	RebuildIndex(context.Context) error
	Close() error
}
