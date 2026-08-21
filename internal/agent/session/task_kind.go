package session

type TaskKind string

const (
	TaskKindRegular TaskKind = "regular"
	TaskKindCompact TaskKind = "compact"
)

func (kind TaskKind) Valid() bool {
	return kind == TaskKindRegular || kind == TaskKindCompact
}
