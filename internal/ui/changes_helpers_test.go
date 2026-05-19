package ui

import gitpkg "github.com/hazeledmands/prwatch/internal/git"

// putChanges adds one or more paths to m.changes, all in the same section
// and with the same class. Useful for the common case where a test just
// wants "these files are committed" or "these files are untracked." For
// renames or mixed sets, build the ChangedFile struct(s) and call addChange.
func putChanges(m *Model, section gitpkg.Section, class gitpkg.Class, paths ...string) {
	if m.changes == nil {
		m.changes = gitpkg.NewChangedFiles()
	}
	for _, p := range paths {
		m.changes.Add(gitpkg.ChangedFile{Path: p, Section: section, Class: class})
	}
}

// addChange records one ChangedFile (typically used for renames where extra
// fields beyond Path/Section/Class matter).
func addChange(m *Model, f gitpkg.ChangedFile) {
	if m.changes == nil {
		m.changes = gitpkg.NewChangedFiles()
	}
	m.changes.Add(f)
}

// changesFromSlices builds a ChangedFiles from the legacy slice-shape inputs.
// Used in test migrations where the old code carried six parallel []string
// fields; new tests can build *ChangedFiles directly.
func changesFromSlices(committed, uncommitted, staged, deleted, added []string, renamed []gitpkg.Rename) *gitpkg.ChangedFiles {
	return gitpkg.ChangedFilesResult{
		Committed:   committed,
		Uncommitted: uncommitted,
		Staged:      staged,
		Deleted:     deleted,
		Added:       added,
		Renamed:     renamed,
	}.ToChangedFiles()
}
