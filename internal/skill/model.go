package skill

type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type ResourceKind string

const (
	ResourceReference ResourceKind = "reference"
	ResourceScript    ResourceKind = "script"
	ResourceAsset     ResourceKind = "asset"
)

type Policy struct {
	AllowImplicitInvocation bool `json:"allow_implicit_invocation"`
}

type SkillResource struct {
	Path     string       `json:"path"`
	Kind     ResourceKind `json:"kind"`
	Size     int64        `json:"size"`
	Revision string       `json:"revision"`
}

type SkillMetadata struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	ShortDescription string          `json:"short_description,omitempty"`
	PathToSkillMD    string          `json:"path_to_skills_md"`
	Source           Source          `json:"source"`
	Scope            Scope           `json:"scope"`
	Enabled          bool            `json:"enabled"`
	Policy           Policy          `json:"policy"`
	References       []SkillResource `json:"references,omitempty"`
	Scripts          []SkillResource `json:"scripts,omitempty"`
	Assets           []SkillResource `json:"assets,omitempty"`
	Size             int64           `json:"size"`
	Revision         string          `json:"revision"`
}

type SkillDocument struct {
	SkillMetadata
	Root    string
	Content string
}
