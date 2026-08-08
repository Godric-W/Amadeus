package tui

import "strings"

type selectionItem struct {
	Name           string
	Description    string
	Disabled       bool
	DisabledReason string
}

type selectionOverlay struct {
	Title    string
	Subtitle string
	Items    []selectionItem
	Selected int
	Input    bool
	Search   bool
	Value    string
	Hint     string
}

func (overlay *selectionOverlay) move(delta int) {
	indices := overlay.filteredIndices()
	if len(indices) == 0 {
		return
	}
	for attempts := 0; attempts < len(indices); attempts++ {
		overlay.Selected = (overlay.Selected + delta + len(indices)) % len(indices)
		if !overlay.Items[indices[overlay.Selected]].Disabled {
			return
		}
	}
}

func (overlay *selectionOverlay) selectedIndex() (int, bool) {
	indices := overlay.filteredIndices()
	if overlay == nil || overlay.Selected < 0 || overlay.Selected >= len(indices) {
		return 0, false
	}
	index := indices[overlay.Selected]
	if overlay.Items[index].Disabled {
		return 0, false
	}
	return index, true
}

func (overlay *selectionOverlay) selectedItem() (selectionItem, bool) {
	index, ok := overlay.selectedIndex()
	if !ok {
		return selectionItem{}, false
	}
	return overlay.Items[index], true
}

func (overlay *selectionOverlay) filteredIndices() []int {
	if overlay == nil {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(overlay.Value))
	indices := make([]int, 0, len(overlay.Items))
	for index, item := range overlay.Items {
		if query != "" && !strings.Contains(strings.ToLower(item.Name+" "+item.Description), query) {
			continue
		}
		indices = append(indices, index)
	}
	if overlay.Selected >= len(indices) {
		overlay.Selected = 0
	}
	return indices
}
