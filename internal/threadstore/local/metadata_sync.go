package local

import (
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

// metadataSync keeps the SQLite read model current from the typed lines that
// were just appended. Full rollout projection remains reserved for startup,
// resume and explicit index rebuild.
type metadataSync struct {
	persisted   threadstore.StoredThread
	pending     threadstore.StoredThread
	previewSeen bool
}

func newMetadataSync(current threadstore.StoredThread) *metadataSync {
	return &metadataSync{persisted: current, pending: current, previewSeen: strings.TrimSpace(current.Preview) != ""}
}

func (syncState *metadataSync) observe(lines []rollout.Line) error {
	if syncState == nil {
		return errors.New("metadata sync is nil")
	}
	next := syncState.pending
	next.Source = next.Source.Clone()
	previewSeen := syncState.previewSeen
	for _, line := range lines {
		if line.Timestamp.After(next.UpdatedAt) {
			next.UpdatedAt = line.Timestamp.UTC()
		}
		switch item := line.Item.(type) {
		case rollout.SessionMetaItem:
			if item.ID != next.ID {
				continue
			}
			next.Source = item.Source.Clone()
			next.CWD = item.CWD
			next.Title = item.Title
			next.ModelProvider = item.ModelProvider
			next.Model = item.Model
			next.CreatedAt = item.CreatedAt.UTC()
			next.GitSHA = item.GitSHA
			next.GitBranch = item.GitBranch
			next.GitOriginURL = item.GitOriginURL
			next.Archived = item.Archived
		case rollout.ResponseItem:
			if !previewSeen {
				if preview := responsePreview(item); preview != "" {
					next.Preview = preview
					previewSeen = true
				}
			}
		case rollout.EventMsgItem:
			switch event := item.Msg.(type) {
			case protocol.ThreadNameUpdatedEvent:
				if title := strings.TrimSpace(event.Name); title != "" {
					next.Title = title
				}
			case protocol.ThreadArchivedEvent:
				next.Archived = event.Archived
			case protocol.TokenCountEvent:
				if event.Info != nil {
					next.TokensUsed = event.Info.TotalTokenUsage.TotalTokens
				}
			}
		case rollout.AgentSpawnEdgeItem:
			if next.Source.IsSubAgent() && item.AgentID == next.ID {
				next.AgentEdgeState = item.State
			}
		}
	}
	if next.UpdatedAt.IsZero() {
		next.UpdatedAt = time.Now().UTC()
	}
	if err := next.Validate(); err != nil {
		return err
	}
	syncState.pending = next
	syncState.previewSeen = previewSeen
	return nil
}

func (syncState *metadataSync) durableSnapshot() *threadstore.StoredThread {
	if syncState == nil {
		return nil
	}
	next := syncState.pending
	next.Source = next.Source.Clone()
	return &next
}

func (syncState *metadataSync) markPersisted() {
	if syncState == nil {
		return
	}
	syncState.persisted = syncState.pending
}
