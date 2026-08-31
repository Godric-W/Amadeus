package contextmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Godric-W/Amadeus/internal/llm"
)

// promptSnapshotCache and activeTokenCache are derived accelerators only. They
// are invalidated whenever the canonical ContextManager state advances and do
// not own history, configuration, or token facts.
type promptSnapshotCache struct {
	historyVersion     uint64
	worldStateRevision string
	inputKey           string
	snapshot           PromptSnapshot
}

type activeTokenCache struct {
	historyVersion uint64
	tokenSequence  uint64
	modelKey       string
	active         int64
	estimated      bool
}

func derivedModelKey(model llm.ModelInfo) string {
	encoded, _ := json.Marshal(model.Normalized())
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func derivedPromptKey(model llm.ModelInfo, prompt llm.Prompt, worldStateRevision string) string {
	payload := struct {
		Model              llm.ModelInfo
		Prompt             llm.Prompt
		WorldStateRevision string
	}{Model: model.Normalized(), Prompt: prompt, WorldStateRevision: worldStateRevision}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func clonePromptSnapshot(snapshot PromptSnapshot) PromptSnapshot {
	snapshot.Items = cloneResponseItems(snapshot.Items)
	return snapshot
}
