package patch

type Version string

const Version1 Version = "v1"

type OperationKind string

const (
	OperationAdd    OperationKind = "add"
	OperationUpdate OperationKind = "update"
	OperationDelete OperationKind = "delete"
	OperationMove   OperationKind = "move"
)

type LineKind string

const (
	LineContext LineKind = "context"
	LineAdd     LineKind = "add"
	LineDelete  LineKind = "delete"
)

type Document struct {
	Version    Version
	Operations []Operation
	Bytes      int
}

type Operation struct {
	Kind     OperationKind
	Path     string
	MovePath string
	AddLines []string
	Hunks    []Hunk
	Line     int
}

type Hunk struct {
	Header    string
	Lines     []Line
	Line      int
	EndOfFile bool
}

type Line struct {
	Kind    LineKind
	Content string
	Line    int
}

func (document Document) Clone() Document {
	cloned := document
	cloned.Operations = make([]Operation, len(document.Operations))
	for index, operation := range document.Operations {
		cloned.Operations[index] = operation.clone()
	}
	return cloned
}

func (operation Operation) clone() Operation {
	cloned := operation
	cloned.AddLines = append([]string(nil), operation.AddLines...)
	cloned.Hunks = make([]Hunk, len(operation.Hunks))
	for index, hunk := range operation.Hunks {
		cloned.Hunks[index] = hunk.clone()
	}
	return cloned
}

func (hunk Hunk) clone() Hunk {
	cloned := hunk
	cloned.Lines = append([]Line(nil), hunk.Lines...)
	return cloned
}
