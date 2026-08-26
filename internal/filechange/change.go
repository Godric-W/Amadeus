package filechange

type Operation string

const (
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
)

type DiffStats struct {
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

type DiffHunk struct {
	Header string   `json:"header"`
	Lines  []string `json:"lines"`
}

type Preview struct {
	Path        string     `json:"path"`
	Operation   Operation  `json:"operation"`
	BeforeHash  string     `json:"before_hash,omitempty"`
	AfterHash   string     `json:"after_hash"`
	Stats       DiffStats  `json:"stats"`
	Hunks       []DiffHunk `json:"hunks"`
	BeforeBytes int        `json:"before_bytes"`
	AfterBytes  int        `json:"after_bytes"`
	UnifiedDiff string     `json:"unified_diff"`
}

func (preview Preview) Clone() Preview {
	preview.Hunks = append([]DiffHunk(nil), preview.Hunks...)
	for index := range preview.Hunks {
		preview.Hunks[index].Lines = append([]string(nil), preview.Hunks[index].Lines...)
	}
	return preview
}

type Result struct {
	Path         string    `json:"path"`
	Operation    Operation `json:"operation"`
	OriginalFile *string   `json:"original_file,omitempty"`
	UpdatedFile  *string   `json:"updated_file,omitempty"`
	Diff         *Preview  `json:"diff,omitempty"`
	UserModified bool      `json:"user_modified"`
}
