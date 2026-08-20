package state

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

var ErrNotFound = errors.New("thread metadata not found")

type StoredThread struct {
	ID            protocol.ThreadID `json:"id"`
	RolloutPath   string            `json:"rollout_path"`
	CWD           string            `json:"cwd"`
	Title         string            `json:"title"`
	Preview       string            `json:"preview,omitempty"`
	ModelProvider string            `json:"model_provider,omitempty"`
	Model         string            `json:"model,omitempty"`
	TokensUsed    int64             `json:"tokens_used"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	Archived      bool              `json:"archived"`
	GitSHA        string            `json:"git_sha,omitempty"`
	GitBranch     string            `json:"git_branch,omitempty"`
	GitOriginURL  string            `json:"git_origin_url,omitempty"`
}

func (thread StoredThread) Validate() error {
	if strings.TrimSpace(string(thread.ID)) == "" || strings.TrimSpace(thread.RolloutPath) == "" || strings.TrimSpace(thread.CWD) == "" || strings.TrimSpace(thread.Title) == "" {
		return errors.New("stored thread is incomplete")
	}
	if !filepath.IsAbs(thread.RolloutPath) || filepath.Clean(thread.RolloutPath) != thread.RolloutPath {
		return errors.New("stored thread rollout path must be clean and absolute")
	}
	if !filepath.IsAbs(thread.CWD) || filepath.Clean(thread.CWD) != thread.CWD {
		return errors.New("stored thread CWD must be clean and absolute")
	}
	if thread.TokensUsed < 0 {
		return errors.New("stored thread tokens_used is negative")
	}
	if thread.CreatedAt.IsZero() || thread.UpdatedAt.IsZero() || thread.UpdatedAt.Before(thread.CreatedAt) {
		return errors.New("stored thread timestamps are invalid")
	}
	return nil
}

type ListQuery struct {
	CWD             string
	IncludeArchived bool
	Limit           int
}

type DB interface {
	UpsertThread(context.Context, StoredThread) error
	GetThread(context.Context, protocol.ThreadID) (StoredThread, error)
	ListThreads(context.Context, ListQuery) ([]StoredThread, error)
	RenameThread(context.Context, protocol.ThreadID, string, time.Time) error
	ArchiveThread(context.Context, protocol.ThreadID, time.Time) error
	ReplaceThreads(context.Context, []StoredThread) error
	Close() error
}
