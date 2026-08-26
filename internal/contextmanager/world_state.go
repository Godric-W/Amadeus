package contextmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type ContextualUserFragment struct {
	Key  UpdateKey
	Body string
}

func NewContextualUserFragment(key UpdateKey, body string) (ContextualUserFragment, error) {
	if !validUpdateKey(key) {
		return ContextualUserFragment{}, fmt.Errorf("unknown world state section %q", key)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return ContextualUserFragment{}, nil
	}
	return ContextualUserFragment{Key: key, Body: body}, nil
}

func (fragment ContextualUserFragment) Render() string {
	if fragment.Key == "" || strings.TrimSpace(fragment.Body) == "" {
		return ""
	}
	open, close := fragmentMarkers(fragment.Key)
	return open + "\n" + fragment.Body + "\n" + close
}

func fragmentMarkers(key UpdateKey) (string, string) {
	switch key {
	case UpdateCollaborationMode:
		return "<collaboration_mode>", "</collaboration_mode>"
	case UpdateAgents:
		return "<user_instructions>", "</user_instructions>"
	case UpdateEnvironment:
		return "<environment_context>", "</environment_context>"
	case UpdatePermissionMode:
		return "<permission_context>", "</permission_context>"
	case UpdateSkills:
		return "<skills_instructions>", "</skills_instructions>"
	case UpdateMCP:
		return "<tools>", "</tools>"
	default:
		return "<amadeus_context>", "</amadeus_context>"
	}
}

func (fragment ContextualUserFragment) Revision() string {
	hash := sha256.Sum256([]byte(string(fragment.Key) + "\x00" + fragment.Render()))
	return hex.EncodeToString(hash[:])
}

type WorldState struct {
	fragments map[UpdateKey]ContextualUserFragment
}

func NewWorldState() *WorldState {
	return &WorldState{fragments: make(map[UpdateKey]ContextualUserFragment)}
}

func (state *WorldState) Set(key UpdateKey, body string) error {
	if state == nil {
		return fmt.Errorf("world state is nil")
	}
	fragment, err := NewContextualUserFragment(key, body)
	if err != nil {
		return err
	}
	if fragment.Render() == "" {
		delete(state.fragments, key)
		return nil
	}
	state.fragments[key] = fragment
	return nil
}

func (state *WorldState) Fragments() []ContextualUserFragment {
	if state == nil {
		return nil
	}
	keys := make([]string, 0, len(state.fragments))
	for key := range state.fragments {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	result := make([]ContextualUserFragment, 0, len(keys))
	for _, key := range keys {
		result = append(result, state.fragments[UpdateKey(key)])
	}
	return result
}

func (state *WorldState) Fragment(key UpdateKey) ContextualUserFragment {
	if state == nil {
		return ContextualUserFragment{}
	}
	return state.fragments[key]
}

func (state *WorldState) Revision() string {
	hash := sha256.New()
	for _, fragment := range state.Fragments() {
		_, _ = hash.Write([]byte(string(fragment.Key)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(fragment.Revision()))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
