package tui

const slashPopupMaxVisible = commandListMaxVisible

type slashCommandPopup struct {
	items     []SlashCommandSpec
	selected  int
	dismissed string
}

func (popup *slashCommandPopup) sync(input string, running bool) {
	if popup.dismissed == input {
		popup.items = nil
		popup.selected = 0
		return
	}
	popup.items = FilterSlashCommands(input, running)
	if popup.selected >= len(popup.items) {
		popup.selected = 0
	}
}

func (popup *slashCommandPopup) active() bool { return len(popup.items) > 0 }

func (popup *slashCommandPopup) move(delta int) {
	if len(popup.items) == 0 {
		return
	}
	popup.selected = (popup.selected + delta + len(popup.items)) % len(popup.items)
}

func (popup *slashCommandPopup) selectedItem() (SlashCommandSpec, bool) {
	if popup.selected < 0 || popup.selected >= len(popup.items) {
		return SlashCommandSpec{}, false
	}
	return popup.items[popup.selected], true
}

func (popup *slashCommandPopup) visibleItems() ([]SlashCommandSpec, int) {
	if popup == nil || len(popup.items) <= slashPopupMaxVisible {
		return popup.items, 0
	}
	start := popup.selected - slashPopupMaxVisible/2
	if start < 0 {
		start = 0
	}
	if maximum := len(popup.items) - slashPopupMaxVisible; start > maximum {
		start = maximum
	}
	return popup.items[start : start+slashPopupMaxVisible], start
}

func (popup *slashCommandPopup) dismiss(input string) {
	popup.dismissed = input
	popup.items = nil
	popup.selected = 0
}

func (popup *slashCommandPopup) resetDismissal(input string) {
	if popup.dismissed != "" && popup.dismissed != input {
		popup.dismissed = ""
	}
}
