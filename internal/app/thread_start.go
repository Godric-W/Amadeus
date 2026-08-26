package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/protocol"
	threadmanager "github.com/Godric-W/Amadeus/internal/threadmanager"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

type ThreadTargetKind string

const (
	ThreadTargetNew    ThreadTargetKind = "new"
	ThreadTargetLatest ThreadTargetKind = "latest"
	ThreadTargetResume ThreadTargetKind = "resume"
)

type ThreadTarget struct {
	Kind     ThreadTargetKind
	ThreadID protocol.ThreadID
}

func (target ThreadTarget) Validate() error {
	switch target.Kind {
	case "", ThreadTargetNew, ThreadTargetLatest:
		if !target.ThreadID.IsZero() {
			return fmt.Errorf("thread target %q must not include a thread ID", target.Kind)
		}
		return nil
	case ThreadTargetResume:
		if target.ThreadID.IsZero() {
			return errors.New("resume thread target has no thread ID")
		}
		return nil
	default:
		return fmt.Errorf("unsupported thread target %q", target.Kind)
	}
}

type ThreadStartResult struct {
	Active   *threadmanager.AmadeusThread
	Metadata *threadstore.StoredThread
	Draft    bool
}

// PrepareStart resolves an invocation's persisted-thread target. A new target
// remains a draft until the owning interface calls EnsureCurrent.
func (workspace *ThreadWorkspace) PrepareStart(
	ctx context.Context,
	target ThreadTarget,
	configuration agentsession.Configuration,
) (ThreadStartResult, error) {
	if workspace == nil {
		return ThreadStartResult{}, errors.New("thread workspace is nil")
	}
	if ctx == nil {
		return ThreadStartResult{}, errors.New("thread start context is nil")
	}
	if err := target.Validate(); err != nil {
		return ThreadStartResult{}, err
	}

	switch target.Kind {
	case "", ThreadTargetNew:
		return ThreadStartResult{Draft: true}, nil
	case ThreadTargetLatest:
		threads, err := workspace.List(ctx, threadstore.ListQuery{CWD: configuration.CWD, Limit: 1})
		if err != nil {
			return ThreadStartResult{}, err
		}
		if len(threads) == 0 {
			return ThreadStartResult{Draft: true}, nil
		}
		active, err := workspace.Resume(ctx, threads[0].ID, configuration)
		if err != nil {
			return ThreadStartResult{}, err
		}
		metadata := threads[0]
		return ThreadStartResult{Active: active, Metadata: &metadata}, nil
	case ThreadTargetResume:
		metadata, err := workspace.threadMetadata(ctx, target.ThreadID, configuration.CWD)
		if err != nil && !errors.Is(err, threadstore.ErrNotFound) {
			return ThreadStartResult{}, err
		}
		active, err := workspace.Resume(ctx, target.ThreadID, configuration)
		if err != nil {
			return ThreadStartResult{}, err
		}
		result := ThreadStartResult{Active: active}
		if !metadata.ID.IsZero() {
			result.Metadata = &metadata
		}
		return result, nil
	default:
		return ThreadStartResult{}, fmt.Errorf("unsupported thread target %q", target.Kind)
	}
}

func (workspace *ThreadWorkspace) threadMetadata(ctx context.Context, id protocol.ThreadID, cwd string) (threadstore.StoredThread, error) {
	threads, err := workspace.List(ctx, threadstore.ListQuery{CWD: strings.TrimSpace(cwd), IncludeArchived: true})
	if err != nil {
		return threadstore.StoredThread{}, err
	}
	for _, candidate := range threads {
		if candidate.ID == id {
			return candidate, nil
		}
	}
	return threadstore.StoredThread{}, threadstore.ErrNotFound
}
