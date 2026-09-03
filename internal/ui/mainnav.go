package ui

import (
	tea "charm.land/bubbletea/v2"
)

// mainNav is the single seam through which Model-level code moves the main
// pane's scroll position, moves the cursor, or changes the row↔source
// mapping underneath either of them.
//
// It exists because three things must move together and were previously
// maintained independently at each call site:
//
//  1. The cursor is always inside the viewport (PLAN.md step 5). A
//     cursor-driven motion scrolls to follow the cursor; a viewport-driven
//     scroll drags the cursor along.
//  2. In visual mode the selection's active end is the cursor. Every path
//     that moves the cursor must update it.
//  3. A change to the row↔source mapping (content refresh, w/n/D toggle,
//     resize) invalidates the cursor's canonical vpRow, so it has to be
//     re-derived from the source-space Position it was pointing at.
//
// Every method here restores all three. There is deliberately no
// Model-level "just scroll" or "just move the cursor" entry point —
// TestSeam_MainPaneNavigationGoesThroughNav enforces that no non-test file
// outside this one touches m.cursor or the pane's scroll primitives.
//
// Scope: the seam owns the *vertical* axis only. Horizontal scroll
// (mainPane.ScrollLeft / ScrollRight / xOffset) is deliberately outside the
// contract and is still mutated directly from model.go's key and mouse arms.
// That is sound today because none of the three invariants above is expressed
// in x: the cursor's canonical state is a viewport *row*, visibility is a row
// range, and the row↔source mapping does not depend on xOffset (horizontal
// scroll only re-truncates rows, and only when word wrap is off). Note it
// still changes what a *display column* means, so if the cursor ever gains a
// canonical column — or the guard test grows an xOffset pattern — the x-axis
// has to be pulled in here rather than bolted onto the call sites.
type mainNav struct {
	m *Model
}

// nav returns the navigation seam for this model.
func (m *Model) nav() mainNav { return mainNav{m} }

// ---------------------------------------------------------------------------
// Cursor-driven motion: move the cursor, scroll only if it would leave.
// ---------------------------------------------------------------------------

// CursorUp/CursorDown/CursorLeft/CursorRight move by one viewport row or
// display column. Report whether the cursor actually moved.
func (n mainNav) CursorUp() bool    { return n.cursorMove((*cursor).MoveUp) }
func (n mainNav) CursorDown() bool  { return n.cursorMove((*cursor).MoveDown) }
func (n mainNav) CursorLeft() bool  { return n.cursorMove((*cursor).MoveLeft) }
func (n mainNav) CursorRight() bool { return n.cursorMove((*cursor).MoveRight) }

func (n mainNav) cursorMove(move func(*cursor, *mainPane) bool) bool {
	moved := move(n.m.cursor, n.m.mainPane)
	n.afterCursorMotion()
	return moved
}

// PlaceCursorFromClick places the cursor at a viewport row / display column
// (mouse click or release). Past-EOL columns clamp to end-of-line.
func (n mainNav) PlaceCursorFromClick(vpRow, displayCol int) {
	n.m.cursor.SetFromClick(n.m.mainPane, vpRow, displayCol)
	n.afterCursorMotion()
}

// PlaceCursorAt places the cursor at a source-space position and scrolls
// only as far as needed to bring it into view. Used by the per-item cursor
// defaults in updateMainContent.
func (n mainNav) PlaceCursorAt(pos Position) {
	n.m.cursor.SetPosition(n.m.mainPane, pos)
	n.afterCursorMotion()
}

// ---------------------------------------------------------------------------
// Viewport-driven motion: scroll, then drag the cursor along the edge.
// ---------------------------------------------------------------------------

// GoToTop / GoToBottom are the g / G main-pane motions.
func (n mainNav) GoToTop()    { n.scroll((*mainPane).GoToTop) }
func (n mainNav) GoToBottom() { n.scroll((*mainPane).GoToBottom) }

func (n mainNav) scroll(do func(*mainPane)) {
	do(n.m.mainPane)
	n.afterViewportScroll()
}

// ForwardKey hands an unhandled key to the viewport (space/b/PgUp/PgDn,
// Ctrl-D/U, …) and restores the invariants afterwards.
func (n mainNav) ForwardKey(msg tea.Msg) tea.Cmd {
	cmd := n.m.mainPane.Update(msg)
	n.afterViewportScroll()
	return cmd
}

// ScrollToSourceLine scrolls so sourceLine sits at the viewport top and
// places the cursor on it. This is what search navigation means by "go to
// the match": the match becomes both visible and the thing pointed at.
func (n mainNav) ScrollToSourceLine(sourceLine int) {
	n.m.mainPane.ScrollToSourceLine(sourceLine)
	n.m.cursor.SetPosition(n.m.mainPane, Position{SourceLine: sourceLine})
	n.afterCursorMotion()
}

// JumpToHunkStart scrolls so the hunk's start line sits hunkNavMargin rows
// down from the viewport top (Vim-style centering) and puts the cursor on
// it. Hunk nav is cursor-driven per PLAN.md, so the cursor lands on the
// target rather than being dragged to the viewport edge.
func (n mainNav) JumpToHunkStart(sourceLine int) {
	n.m.mainPane.scrollToHunkStart(sourceLine)
	n.m.cursor.SetPosition(n.m.mainPane, Position{SourceLine: sourceLine})
	n.afterCursorMotion()
}

// HunkNavAnchor returns the source line hunk navigation treats as "where I
// am now". That is the cursor's line: JumpToHunkStart puts the cursor
// exactly on the target hunk's StartLine, so the anchor is the hunk itself
// and each J/K advances by exactly one hunk. It lives on the seam because
// its two inputs are the cursor and the pane's row↔source mapping, and the
// seam owns every read of the cursor by Model-level code.
//
// The anchor must not be inferred back from the scroll position
// (YOffset + hunkNavMargin): the margin subtraction clamps at 0 and at the
// end-of-file maximum, and a clamped YOffset puts the inferred line below
// the target hunk (top clamp: J skips hunks) or above it (EOF clamp: J
// re-finds the current hunk and stalls).
//
// The cursor is always placed when this runs, so there is no unplaced
// fallback: opening a file places it (jumpToFirstDiff, or scroll-memory
// restore via ScrollToSourceLine), a content refresh or a w/n/D toggle
// re-derives it through Reflow, and a viewport scroll drags it along via
// DragAlongScroll — no path leaves vpRow negative.
//
// Rows without a source line of their own — removed-line decoration rows,
// wrap continuation rows — resolve to a *preceding* source line, so a
// cursor parked on the removals above hunk K anchors just before K's
// start: J goes to K (no skip) and K's predecessor is reachable with one
// backward press. TestHunkNavAnchor_OnDecorationRow pins that down.
func (n mainNav) HunkNavAnchor() int {
	return n.m.cursor.SourceLine(n.m.mainPane)
}

// BeginVisualStream / BeginVisualLine anchor a visual-mode selection at the
// cursor. They live here because the anchor is a cursor endpoint: the seam
// owns every read of the cursor's position by selection code.
func (n mainNav) BeginVisualStream() { n.m.selection.BeginStream(n.m.cursor.Endpoint(n.m.mainPane)) }
func (n mainNav) BeginVisualLine()   { n.m.selection.BeginLine(n.m.cursor.Endpoint(n.m.mainPane)) }

// AdvanceDragAutoScroll steps the mouse-drag auto-scroll. The drag's own
// scrolling is a viewport-driven motion like any other, so the cursor is
// dragged along after it.
func (n mainNav) AdvanceDragAutoScroll(g dragGeometry) tea.Cmd {
	cmd := n.m.drag.AdvanceAutoScroll(g)
	n.afterViewportScroll()
	return cmd
}

// ---------------------------------------------------------------------------
// searchView: the seam handed to searchOverlay. Routing search through
// mainNav rather than the raw pane is what makes search's scrolls paired.
// ---------------------------------------------------------------------------

func (n mainNav) FindMatches(query string) []int { return n.m.mainPane.FindMatches(query) }

// SetSearchQuery changes the rendered content (highlight spans), which can
// change wrapping, so it goes through Reflow.
func (n mainNav) SetSearchQuery(query string) {
	n.Reflow(func(p *mainPane) { p.SetSearchQuery(query) })
}

// searchView compile-time check: mainNav is what Model hands to the search
// overlay, so the overlay physically cannot reach the unpaired pane methods.
var _ searchView = mainNav{}

// ---------------------------------------------------------------------------
// Reflow: mutations that change the row↔source mapping.
// ---------------------------------------------------------------------------

// Reflow runs a mutation that changes the mapping between viewport rows and
// source lines — new content, a wrap/line-number/removed-line toggle, or a
// resize — and re-derives the cursor across it.
//
// The cursor's canonical state is a viewport row, so any such mutation
// silently moves it to a different source line (or off the end of shrunken
// content, where MoveDown goes dead and ApplyHighlight paints into the
// pane's padding). Reflow snapshots the source-space Position first and
// restores it afterwards, which is also the resize-invariance property
// PLAN.md's step-5 test list asks for.
//
// When the mutation itself placed the cursor explicitly (setItem's
// scroll-memory / first-hunk / top-of-file defaults), that placement wins:
// cursor.seq changes and Reflow does not restore the pre-mutation position.
func (n mainNav) Reflow(mutate func(*mainPane)) {
	pane := n.m.mainPane
	c := n.m.cursor
	placed := c.IsPlaced()
	var pos Position
	if placed {
		pos = c.Pos(pane)
	}
	seqBefore := c.seq

	mutate(pane)

	if placed && c.seq == seqBefore {
		c.SetPosition(pane, pos)
	}
	c.ClampToContent(pane)
	// The selection's endpoints are viewport rows too; re-derive the anchor
	// against the new mapping so the highlight stays on the same text.
	n.m.selection.Reflow(pane)
	n.afterCursorMotion()
}

// ---------------------------------------------------------------------------
// Shared fixups
// ---------------------------------------------------------------------------

// afterCursorMotion restores the pair for a cursor-driven change: scroll so
// the cursor is visible, then sync the visual-mode selection.
func (n mainNav) afterCursorMotion() {
	n.m.cursor.EnsureVisible(n.m.mainPane)
	n.syncSelection()
}

// afterViewportScroll restores the pair for a viewport-driven change: drag
// the cursor to the nearest visible row, then sync the visual-mode selection.
func (n mainNav) afterViewportScroll() {
	n.m.cursor.DragAlongScroll(n.m.mainPane)
	n.syncSelection()
}

// syncSelection keeps visual mode's active end on the cursor. Without this
// at a single choke point, each key arm had to remember it — and g/G, the
// forwarded page keys and the wheel didn't.
func (n mainNav) syncSelection() {
	if n.m.selection.IsActive() {
		n.m.selection.SetActive(n.m.cursor.Endpoint(n.m.mainPane))
	}
}
