package plan

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type ItemStatus string

const (
	ItemPending    ItemStatus = "pending"
	ItemInProgress ItemStatus = "in_progress"
	ItemCompleted  ItemStatus = "completed"
)

func (status ItemStatus) Valid() bool {
	switch status {
	case ItemPending, ItemInProgress, ItemCompleted:
		return true
	default:
		return false
	}
}

type Item struct {
	Step   string     `json:"step"`
	Status ItemStatus `json:"status"`
}

type Update struct {
	Explanation string `json:"explanation,omitempty"`
	Items       []Item `json:"items"`
}

type Snapshot struct {
	Explanation string    `json:"explanation,omitempty"`
	Items       []Item    `json:"items,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	Revision    int64     `json:"revision"`
}

type State struct {
	mutex       sync.RWMutex
	explanation string
	items       []Item
	updatedAt   time.Time
	revision    int64
}

func NewState() *State { return &State{} }

func (state *State) Apply(update Update, now time.Time) (Snapshot, error) {
	if state == nil {
		return Snapshot{}, errors.New("plan state is nil")
	}
	items := append([]Item(nil), update.Items...)
	if err := validateItems(items); err != nil {
		return Snapshot{}, err
	}
	state.mutex.Lock()
	state.explanation = strings.TrimSpace(update.Explanation)
	state.items = items
	state.updatedAt = now.UTC()
	state.revision++
	snapshot := state.snapshotLocked()
	state.mutex.Unlock()
	return snapshot, nil
}

func (state *State) Snapshot() Snapshot {
	if state == nil {
		return Snapshot{}
	}
	state.mutex.RLock()
	defer state.mutex.RUnlock()
	return state.snapshotLocked()
}

func (state *State) snapshotLocked() Snapshot {
	return Snapshot{
		Explanation: state.explanation,
		Items:       append([]Item(nil), state.items...),
		UpdatedAt:   state.updatedAt,
		Revision:    state.revision,
	}
}

func validateItems(items []Item) error {
	if len(items) == 0 {
		return errors.New("plan update requires at least one item")
	}
	inProgress := 0
	for index := range items {
		items[index].Step = strings.TrimSpace(items[index].Step)
		if items[index].Step == "" {
			return fmt.Errorf("plan item %d step is empty", index)
		}
		if !items[index].Status.Valid() {
			return fmt.Errorf("plan item %d status %q is invalid", index, items[index].Status)
		}
		if items[index].Status == ItemInProgress {
			inProgress++
		}
	}
	if inProgress > 1 {
		return errors.New("plan update can contain at most one in-progress item")
	}
	return nil
}
