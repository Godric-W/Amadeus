package tui

const slashPopupMaxVisible = commandListMaxVisible

type slashCommandPopup struct {
	items     []SlashCommand
	selected  int
	dismissed string
	filter    string
	rows      int
}

func (popup *slashCommandPopup) sync(input string, running bool) {
	if popup.dismissed == input {
		popup.items = nil
		popup.selected = 0
		popup.rows = 0
		return
	}
	filter := slashCommandFilter(input)
	if filter != popup.filter {
		popup.selected = 0
		popup.filter = filter
	}
	popup.items = FilterSlashCommands(input, running)
	if len(popup.items) == 0 {
		popup.rows = 0
	} else if visible := minInt(len(popup.items), slashPopupMaxVisible); visible > popup.rows {
		popup.rows = visible
	}
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

func (popup *slashCommandPopup) selectedItem() (SlashCommand, bool) {
	if popup.selected < 0 || popup.selected >= len(popup.items) {
		return "", false
	}
	return popup.items[popup.selected], true
}

func (popup *slashCommandPopup) visibleItems() ([]SlashCommand, int) {
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
	popup.rows = 0
}

func (popup *slashCommandPopup) resetDismissal(input string) {
	if popup.dismissed != "" && popup.dismissed != input {
		popup.dismissed = ""
	}
}
