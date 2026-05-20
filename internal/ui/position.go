package ui

// Position names a specific point in the displayed text of the main pane:
// a 1-indexed source line and 0-indexed column. Side (add/remove/context)
// is added in step 6 when deep-links and PR comments need it; see PLAN.md.
//
// Position is line-and-column only; file/document identity is paired with
// it at the call site rather than embedded, following the convention used
// by LSP, VS Code, tree-sitter, and other editor APIs. Range is the pair,
// naming spans like the visible viewport window, hunk extents, and PR
// comment ranges.
//
// Column is the rendered-column offset *past the gutter* — i.e., 0 means
// the first character of the line's content, not the first character of
// the rendered row. Drag selection populates it from screen-x at click
// time; visual-mode (step 5) populates it from cursor motion.
type Position struct {
	SourceLine int
	Column     int
}

// Range names a span between two Positions. By convention Start ≤ End, but
// the field order alone does not enforce it; features that depend on the
// invariant should normalize at the boundary (drag selection swaps reversed
// Y when computing the highlight, for example).
type Range struct {
	Start, End Position
}
