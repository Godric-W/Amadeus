package contextmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type PreviousSectionKind string

const (
	PreviousSectionAbsent  PreviousSectionKind = "absent"
	PreviousSectionUnknown PreviousSectionKind = "unknown"
	PreviousSectionKnown   PreviousSectionKind = "known"
)

type PreviousSectionState struct {
	Kind     PreviousSectionKind
	Snapshot json.RawMessage
}

type ContextFragment struct {
	Kind     string
	Role     llm.Role
	Content  string
	Separate bool
}

func (fragment ContextFragment) ResponseItem() (llm.ResponseItem, error) {
	if strings.TrimSpace(fragment.Kind) == "" || (fragment.Role != llm.RoleDeveloper && fragment.Role != llm.RoleUser) || strings.TrimSpace(fragment.Content) == "" {
		return llm.ResponseItem{}, errors.New("context fragment is invalid")
	}
	return llm.ResponseItem{Role: fragment.Role, Content: fragment.Content}, nil
}

type WorldStateSection interface {
	ID() string
	Snapshot() (json.RawMessage, error)
	RenderDiff(PreviousSectionState) (*ContextFragment, error)
}

type TextSectionOptions struct {
	ID                string
	Kind              string
	Role              llm.Role
	Text              string
	OpenMarker        string
	CloseMarker       string
	ReplacementNotice string
	RemovalNotice     string
	Separate          bool
}

type textSection struct{ options TextSectionOptions }

type snapshotSection struct {
	id    string
	value any
}

func NewSnapshotSection(id string, value any) (WorldStateSection, error) {
	id = strings.TrimSpace(id)
	if id == "" || value == nil {
		return nil, errors.New("snapshot world state section is invalid")
	}
	return snapshotSection{id: id, value: value}, nil
}

func (section snapshotSection) ID() string { return section.id }

func (section snapshotSection) Snapshot() (json.RawMessage, error) {
	return json.Marshal(section.value)
}

func (section snapshotSection) RenderDiff(PreviousSectionState) (*ContextFragment, error) {
	return nil, nil
}

func NewTextSection(options TextSectionOptions) (WorldStateSection, error) {
	options.ID = strings.TrimSpace(options.ID)
	options.Kind = strings.TrimSpace(options.Kind)
	options.Text = strings.TrimSpace(normalizeLineEndings(options.Text))
	if options.ID == "" || options.Kind == "" || (options.Role != llm.RoleDeveloper && options.Role != llm.RoleUser) {
		return nil, errors.New("text world state section is invalid")
	}
	return textSection{options: options}, nil
}

func normalizeLineEndings(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func (section textSection) ID() string { return section.options.ID }

func (section textSection) Snapshot() (json.RawMessage, error) {
	return json.Marshal(struct {
		Text string `json:"text"`
	}{Text: section.options.Text})
}

func (section textSection) RenderDiff(previous PreviousSectionState) (*ContextFragment, error) {
	previousText := ""
	switch previous.Kind {
	case PreviousSectionAbsent:
	case PreviousSectionUnknown:
		previousText = "unknown"
	case PreviousSectionKnown:
		var snapshot struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(previous.Snapshot, &snapshot); err != nil {
			return nil, fmt.Errorf("decode previous world state section %q: %w", section.ID(), err)
		}
		previousText = snapshot.Text
	default:
		return nil, errors.New("previous world state section kind is invalid")
	}
	current := section.options.Text
	if previous.Kind == PreviousSectionKnown && current == previousText {
		return nil, nil
	}
	if current == "" {
		if previous.Kind == PreviousSectionAbsent || strings.TrimSpace(section.options.RemovalNotice) == "" {
			return nil, nil
		}
		return section.fragment(strings.TrimSpace(section.options.RemovalNotice)), nil
	}
	if previous.Kind != PreviousSectionAbsent && strings.TrimSpace(section.options.ReplacementNotice) != "" {
		current = strings.TrimSpace(section.options.ReplacementNotice) + "\n\n" + current
	}
	return section.fragment(current), nil
}

func (section textSection) fragment(body string) *ContextFragment {
	content := strings.TrimSpace(body)
	if section.options.OpenMarker != "" {
		content = section.options.OpenMarker + "\n" + content + "\n" + section.options.CloseMarker
	}
	return &ContextFragment{Kind: section.options.Kind, Role: section.options.Role, Content: content, Separate: section.options.Separate}
}

type WorldState struct {
	sections []WorldStateSection
	ids      map[string]struct{}
}

func NewWorldState() *WorldState { return &WorldState{ids: make(map[string]struct{})} }

func (state *WorldState) Add(section WorldStateSection) error {
	if state == nil || section == nil {
		return errors.New("world state section is nil")
	}
	id := strings.TrimSpace(section.ID())
	if id == "" {
		return errors.New("world state section ID is empty")
	}
	if _, exists := state.ids[id]; exists {
		return fmt.Errorf("world state section %q is duplicated", id)
	}
	state.ids[id] = struct{}{}
	state.sections = append(state.sections, section)
	return nil
}

type WorldStateSnapshot map[string]json.RawMessage

func (snapshot WorldStateSnapshot) Clone() WorldStateSnapshot {
	cloned := make(WorldStateSnapshot, len(snapshot))
	for id, value := range snapshot {
		cloned[id] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

func (snapshot WorldStateSnapshot) Revision() string {
	ids := make([]string, 0, len(snapshot))
	for id := range snapshot {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	hash := sha256.New()
	for _, id := range ids {
		hash.Write([]byte(id))
		hash.Write([]byte{0})
		hash.Write(snapshot[id])
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (state *WorldState) Render(previous WorldStateSnapshot, missingKind PreviousSectionKind) ([]ContextFragment, WorldStateSnapshot, error) {
	if state == nil {
		return nil, nil, errors.New("world state is nil")
	}
	if missingKind != PreviousSectionAbsent && missingKind != PreviousSectionUnknown && missingKind != PreviousSectionKnown {
		return nil, nil, errors.New("previous world state kind is invalid")
	}
	current := make(WorldStateSnapshot, len(state.sections))
	fragments := make([]ContextFragment, 0, len(state.sections))
	for _, section := range state.sections {
		snapshot, err := section.Snapshot()
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot world state section %q: %w", section.ID(), err)
		}
		current[section.ID()] = append(json.RawMessage(nil), snapshot...)
		previousState := PreviousSectionState{Kind: missingKind}
		if missingKind == PreviousSectionKnown {
			previousState.Kind = PreviousSectionAbsent
		}
		if value, exists := previous[section.ID()]; exists && missingKind == PreviousSectionKnown {
			previousState = PreviousSectionState{Kind: PreviousSectionKnown, Snapshot: value}
		}
		fragment, err := section.RenderDiff(previousState)
		if err != nil {
			return nil, nil, err
		}
		if fragment != nil {
			fragments = append(fragments, *fragment)
		}
	}
	return fragments, current, nil
}

func WorldStatePatch(previous, current WorldStateSnapshot) map[string]json.RawMessage {
	patch := make(map[string]json.RawMessage)
	for id := range previous {
		if _, exists := current[id]; !exists {
			patch[id] = json.RawMessage("null")
		}
	}
	for id, value := range current {
		if previousValue, exists := previous[id]; !exists || string(previousValue) != string(value) {
			patch[id] = append(json.RawMessage(nil), value...)
		}
	}
	return patch
}

func ApplyWorldStatePatch(base WorldStateSnapshot, full bool, sections map[string]json.RawMessage) (WorldStateSnapshot, error) {
	result := make(WorldStateSnapshot)
	if !full {
		result = base.Clone()
	}
	for id, value := range sections {
		if strings.TrimSpace(id) == "" || len(value) == 0 || !json.Valid(value) {
			return nil, errors.New("world state patch is invalid")
		}
		if string(value) == "null" {
			delete(result, id)
			continue
		}
		result[id] = append(json.RawMessage(nil), value...)
	}
	return result, nil
}
