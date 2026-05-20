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

// Selection names a directed pair of Positions backing the drag (and,
// eventually, the keyboard-driven visual mode in step 5) selection. Unlike
// Range it's directed: Anchor is where the selection started (the click,
// or where `v` was pressed); Active is where it currently extends to. Both
// can be nil to signal "outside the content area" — for the anchor, that
// means the click landed on title row / status bar / borders; for the
// active end, that means the mouse is currently above or below content
// (which triggers auto-scroll downstream rather than placing the active
// end on a specific source row).
//
// In step 5 Selection grows a Mode field (stream / line) to share
// machinery with vim-style visual mode.
type Selection struct {
	Anchor *Position
	Active *Position
}
