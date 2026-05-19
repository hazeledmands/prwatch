package git

import "sort"

// Section is the slot a changed file occupies in the user's workflow.
// Sections are mutually exclusive — each changed file is in exactly one.
type Section int

const (
	SectionUncommitted Section = iota // unstaged or untracked changes
	SectionStaged                     // staged in the index, not yet committed
	SectionCommitted                  // landed in a commit in base..HEAD
)

// Class describes what kind of change happened to the file. Sections and
// classes are orthogonal — a file can be (Uncommitted, Modified), (Committed,
// Renamed), and so on.
type Class int

const (
	ClassModified Class = iota // tracked file's content changed
	ClassAdded                 // entirely new file (untracked or pure-add in commit/index)
	ClassDeleted               // file was deleted (currently only set for committed deletions)
	ClassRenamed               // file moved from OldPath; may also have content edits
)

// ChangedFile is a single entry in the unified change set. One file → one
// ChangedFile, regardless of how many classifications apply to it. Rename
// fields are zero-valued unless Class == ClassRenamed.
type ChangedFile struct {
	Path       string
	Section    Section
	Class      Class
	OldPath    string // populated iff Class == ClassRenamed
	PureRename bool   // populated iff Class == ClassRenamed; similarity == 100%
}

// ChangedFiles is the collection of changed files keyed by path, with stable
// alphabetic iteration order. Use NewChangedFiles to construct; Add records a
// file (replacing any prior entry for the same path).
type ChangedFiles struct {
	byPath map[string]ChangedFile
}

// NewChangedFiles returns an empty collection.
func NewChangedFiles() *ChangedFiles {
	return &ChangedFiles{byPath: make(map[string]ChangedFile)}
}

// Add inserts (or replaces) the entry for f.Path.
func (c *ChangedFiles) Add(f ChangedFile) {
	c.byPath[f.Path] = f
}

// Get returns the ChangedFile for path, or (zero, false) if absent.
func (c *ChangedFiles) Get(path string) (ChangedFile, bool) {
	f, ok := c.byPath[path]
	return f, ok
}

// Len returns the total number of changed files.
func (c *ChangedFiles) Len() int {
	return len(c.byPath)
}

// All returns every changed file in alphabetic order by path.
func (c *ChangedFiles) All() []ChangedFile {
	out := make([]ChangedFile, 0, len(c.byPath))
	for _, f := range c.byPath {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// InSection returns the changed files in s, alphabetic by path.
func (c *ChangedFiles) InSection(s Section) []ChangedFile {
	var out []ChangedFile
	for _, f := range c.byPath {
		if f.Section == s {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Renames returns every file whose Class is ClassRenamed, alphabetic by new
// path.
func (c *ChangedFiles) Renames() []ChangedFile {
	var out []ChangedFile
	for _, f := range c.byPath {
		if f.Class == ClassRenamed {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// HasRenameOldPath reports whether any rename's OldPath equals path. Used by
// the UI to suppress rename old paths from the "all files" listing when the
// underlying filesystem still shows them (which it shouldn't, but the model
// can be defensive).
func (c *ChangedFiles) HasRenameOldPath(path string) bool {
	for _, f := range c.byPath {
		if f.Class == ClassRenamed && f.OldPath == path {
			return true
		}
	}
	return false
}
