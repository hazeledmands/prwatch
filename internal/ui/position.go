package ui

import (
	"github.com/hazeledmands/prwatch/internal/git"
)

// Position names a specific point in the diff being displayed: which file,
// which 1-indexed source line in the displayed version. As features land it
// will grow additional fields — Column for stream-mode visual selection,
// Side for deep-links and PR comments, Ref for whole-scope diff — see
// PLAN.md for the staged rollout.
//
// Position is the singular point. Range is the pair, naming spans like the
// visible viewport window, selections, hunk extents, and PR comment ranges.
type Position struct {
	File       *git.ChangedFile
	SourceLine int
}

// Range names a span between two Positions. By convention Start ≤ End, but
// the field order alone does not enforce it; features that depend on the
// invariant should normalize at the boundary (drag selection swaps reversed
// Y when computing the highlight, for example).
type Range struct {
	Start, End Position
}
