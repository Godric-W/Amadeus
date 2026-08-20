package protocol

type ModeKind string

const (
	ModeKindDefault ModeKind = "default"
	ModeKindPlan    ModeKind = "plan"
)

func (mode ModeKind) Valid() bool { return mode == ModeKindDefault || mode == ModeKindPlan }

type CollaborationMode struct {
	Mode ModeKind `json:"mode"`
}

type ThreadSettingsOverrides struct {
	CollaborationMode *CollaborationMode `json:"collaboration_mode,omitempty"`
}

func (overrides ThreadSettingsOverrides) CollaborationModeValue() (ModeKind, bool) {
	if overrides.CollaborationMode == nil || !overrides.CollaborationMode.Mode.Valid() {
		return "", false
	}
	return overrides.CollaborationMode.Mode, true
}
