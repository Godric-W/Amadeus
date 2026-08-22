package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type statusLineBranchUpdatedMsg struct {
	Generation uint64
	CurrentDir string
	Branch     string
}

func (model *fullscreenModel) resetStatusLineWorkspace() tea.Cmd {
	currentDir := strings.TrimSpace(model.session.Configuration.CWD)
	model.workspace = statusLineWorkspaceState{
		Generation: model.session.Generation,
		CurrentDir: currentDir,
		Pending:    currentDir != "",
	}
	model.refreshStatusLine()
	return model.statusLineBranchLookupCommand()
}

func (model fullscreenModel) statusLineBranchLookupCommand() tea.Cmd {
	state := model.workspace
	if !state.Pending || state.CurrentDir == "" {
		return nil
	}
	ctx := model.ctx
	return func() tea.Msg {
		return statusLineBranchUpdatedMsg{
			Generation: state.Generation,
			CurrentDir: state.CurrentDir,
			Branch:     ResolveWorkspaceBranch(ctx, state.CurrentDir),
		}
	}
}

func (model *fullscreenModel) applyStatusLineBranchUpdate(message statusLineBranchUpdatedMsg) {
	if message.Generation != model.session.Generation || message.Generation != model.workspace.Generation {
		return
	}
	if strings.TrimSpace(message.CurrentDir) != strings.TrimSpace(model.session.Configuration.CWD) || message.CurrentDir != model.workspace.CurrentDir {
		return
	}
	model.workspace.Branch = strings.TrimSpace(message.Branch)
	model.workspace.Pending = false
	model.refreshStatusLine()
}
