package ui

// The commits-mode sidebar carries two pseudo-entries alongside the real
// commits. Their labels are their identity — sidebar selection, main-item
// scroll-memory keys, and the main-pane dispatch all match on these strings —
// so they live here rather than being spelled out at each site.
const (
	pseudoNewChangesLabel = "new changes"
	pseudoStagedLabel     = "staged changes"
)

// pseudoEntryContent is what a pseudo-entry renders: the main-pane body, how to
// install it, and the title bar's right half.
type pseudoEntryContent struct {
	body string
	// asDiff selects SetContent (colorized unified diff) over
	// SetPlainContent (used for the empty state and the binary placeholder,
	// neither of which is a diff).
	asDiff     bool
	titleRight string
}

// buildPseudoEntryContent turns a pseudo-entry's own diff into what the main
// pane should show. Pure — the caller fetches the diff and applies the result,
// so each entry's shortstat is derived from that entry's diff and can't be
// shared with the other one.
func buildPseudoEntryContent(label, diff string) pseudoEntryContent {
	switch {
	case diff == "":
		return pseudoEntryContent{body: emptyPseudoEntryText(label)}
	case isBinaryContent(diff):
		// PROMPT.md: binary content is never shown. git's own diff output
		// already elides it per-file, so this only fires if the whole body
		// somehow came back binary.
		return pseudoEntryContent{body: "[binary content]"}
	}
	return pseudoEntryContent{body: diff, asDiff: true, titleRight: shortstatFromDiff(diff)}
}

// pseudoDiffCache memoizes each pseudo-entry's rendered content for one
// git-load cycle.
//
// Without it, the diffs were re-fetched on every updateMainContent — which
// fires on each sidebar move onto the entry and on every message that
// refreshes the pane (git load, PR refresh, RWX logs, PR tick). "new changes"
// costs `git diff` + `ls-files` + one `--no-index` invocation *per untracked
// file*, so a tree with hundreds of untracked files spawned hundreds of
// subprocesses on the Update goroutine, repeatedly, while the user did nothing
// but hold a cursor still.
//
// A git load is the cycle that refreshes the sidebar's own file counts, so it
// is also the point where these bodies can have changed; invalidating there
// keeps the body consistent with the header counting it. Steady-state
// selection costs zero subprocess spawns.
type pseudoDiffCache struct {
	entries map[string]pseudoDiffEntry
}

type pseudoDiffEntry struct {
	content pseudoEntryContent
	err     error
}

// Invalidate drops every memoized body. Called when a git load lands.
func (c *pseudoDiffCache) Invalidate() { c.entries = nil }

// Get returns label's content, calling fetch at most once per label per cycle.
// A failed fetch is cached too: a persistent git failure should not re-spawn a
// subprocess on every keystroke.
func (c *pseudoDiffCache) Get(label string, fetch func() (string, error)) (pseudoEntryContent, error) {
	if e, ok := c.entries[label]; ok {
		return e.content, e.err
	}
	diff, err := fetch()
	e := pseudoDiffEntry{err: err}
	if err == nil {
		e.content = buildPseudoEntryContent(label, diff)
	}
	if c.entries == nil {
		c.entries = make(map[string]pseudoDiffEntry, 2)
	}
	c.entries[label] = e
	return e.content, e.err
}

// emptyPseudoEntryText is the quiet line shown when a pseudo-entry is still in
// the sidebar but its diff is empty — e.g. the index was reset between the git
// load and this render.
func emptyPseudoEntryText(label string) string {
	if label == pseudoStagedLabel {
		return "no staged changes"
	}
	return "no new changes"
}
