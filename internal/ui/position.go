package ui

// Position names a specific point in the displayed text of the main pane:
// which 1-indexed source line. As features land it will grow additional
// fields — Column for stream-mode visual selection, Side for deep-links
// and PR comments — see PLAN.md for the staged rollout.
//
// Position is line-and-column only; file/document identity is paired with
// it at the call site rather than embedded, following the convention used
// by LSP, VS Code, tree-sitter, and other editor APIs. Range is the pair,
// naming spans like the visible viewport window, selections, hunk extents,
// and PR comment ranges.
type Position struct {
	SourceLine int
}

// Range names a span between two Positions. By convention Start ≤ End, but
// the field order alone does not enforce it; features that depend on the
// invariant should normalize at the boundary (drag selection swaps reversed
// Y when computing the highlight, for example).
type Range struct {
	Start, End Position
}
