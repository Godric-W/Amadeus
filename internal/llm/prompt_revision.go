package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (spec ToolSpec) RevisionID() string {
	encoded, _ := json.Marshal(spec)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (prompt Prompt) RevisionID() string {
	encoded, _ := json.Marshal(prompt)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
