package skills

// Skill represents a single discovered skill with its metadata.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`  // "global" (home) or "project" (workspace/workDir); scope, wire-compat
	Managed     bool   `json:"managed"` // true = in .agents/skills (writable region, UI may modify); false = external read-only (.claude/.hotplex)
	FilePath    string `json:"-"`       // absolute SKILL.md path (CRUD detail/delete use; never sent on wire)
}

const (
	SourceGlobal  = "global"
	SourceProject = "project"
)
