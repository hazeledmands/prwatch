package ui

import (
	"strings"
)

// cursor is the active pointing position in the main pane. Exists
// outside any selection mode — visual mode, PR comments, and LSP all
// need "where am I pointing" without a modal opt-in.
//
// The cursor's identity is (viewport row, display column). Source-space
// Position is derived on demand via Pos. Using vpRow as canonical is
// important for decoration rows (e.g. red removed-line rows in a diff)
// — multiple decoration rows can map to the same source line (the
// "most recent at or before"), so navigating by source line gets stuck.
// vpRow uniquely identifies a displayed row, so j/k/click can step
// through decoration rows naturally.
//
// Invariant maintained by the model: the cursor is always inside the
// viewport. Cursor-driven motion (j/k/h/l/click/hunk-nav) scrolls the
// viewport only when cursor would otherwise leave. Viewport-driven
// motion (mouse wheel, space/b page) drags the cursor along the edge
// when scrolling would push it off-screen.
//
// desiredCol is the sticky display column for vim-style j/k vertical
// motion: pressing j onto a row whose content doesn't extend to the
// cursor's column clamps the rendered column but preserves desiredCol
// so the next j onto a longer row restores the column.
type cursor struct {
	vpRow      int // -1 = unplaced
	desiredCol int

	// seq counts explicit placements (SetPosition / SetFromClick). It lets
	// mainNav.Reflow tell "the mutation re-placed the cursor deliberately"
	// (e.g. setItem's scroll-memory restore) from "the mutation moved the
	// cursor's row out from under it", so only the latter is undone.
	seq int
}

func newCursor() *cursor {
	// Default to (0, 0) so j/k work immediately even when the cursor
	// hasn't been explicitly placed by setItem (e.g. mid-test before
	// SetPlainContent fires). Real apps will overwrite via SetPosition
	// when the file loads.
	return &cursor{vpRow: 0, desiredCol: 0}
}

// IsPlaced reports whether the cursor has a valid position.
func (c *cursor) IsPlaced() bool { return c.vpRow >= 0 }

// Pos returns the cursor's source-space position derived from
// (vpRow, displayed column). For decoration rows (no source-line
// mapping) SourceLine is the most-recent-before source line — best
// effort, the caller should also consult VpRow when the distinction
// matters (e.g. selection rendering does, via Endpoint).
func (c *cursor) Pos(pane *mainPane) Position {
	if c.vpRow < 0 {
		return Position{}
	}
	sl := pane.sourceLineAtViewportOffset(c.vpRow)
	col := pane.absoluteColumnFromDisplay(c.vpRow, c.displayCol(pane))
	return Position{SourceLine: sl, Column: col}
}

// Endpoint returns the cursor as a selection endpoint (Pos + VpRow),
// the shape both keyboard and mouse selection consume. VpRow lets
// downstream rendering disambiguate decoration rows.
func (c *cursor) Endpoint(pane *mainPane) endpoint {
	if c.vpRow < 0 {
		return endpoint{OutsideDir: +1}
	}
	return endpoint{Pos: c.Pos(pane), VpRow: c.vpRow}
}

// SetPosition places the cursor at the given source-space position
// (e.g. on file switch / scroll-memory restore). Translates pos to
// (vpRow, displayCol) via positionToDisplay.
func (c *cursor) SetPosition(pane *mainPane, pos Position) {
	vp, dc := pane.positionToDisplay(pos)
	c.vpRow = vp
	c.desiredCol = dc
	c.seq++
	c.ClampToContent(pane)
}

// ClampToContent bounds vpRow to the rows the pane currently renders.
// Content that shrank under the cursor (a refresh, a `D` toggle hiding
// removed-line rows, a narrower terminal) would otherwise leave vpRow past
// the end, where MoveDown is a permanent no-op and ApplyHighlight paints
// into the pane's padding.
func (c *cursor) ClampToContent(pane *mainPane) {
	if c.vpRow < 0 {
		return
	}
	rows := viewportContentRowCount(pane)
	if rows <= 0 {
		c.vpRow = 0
		return
	}
	if c.vpRow >= rows {
		c.vpRow = rows - 1
	}
}

// SetFromClick places cursor at viewport (vpRow, displayCol). Clamps
// displayCol to the row's content width so past-EOL clicks land at
// end-of-line rather than in the padding area.
func (c *cursor) SetFromClick(pane *mainPane, vpRow, displayCol int) {
	c.vpRow = vpRow
	c.desiredCol = clampDisplayCol(pane, vpRow, displayCol)
	c.seq++
	c.ClampToContent(pane)
}

// MoveDown advances cursor to the next viewport row. Returns false at
// the last content row. Uses vpRow directly so decoration rows (red
// removed lines etc.) are traversed correctly.
func (c *cursor) MoveDown(pane *mainPane) bool {
	if c.vpRow < 0 {
		return false
	}
	nextVp := c.vpRow + 1
	if nextVp >= viewportContentRowCount(pane) {
		return false
	}
	c.vpRow = nextVp
	return true
}

// MoveUp moves cursor to the previous viewport row.
func (c *cursor) MoveUp(pane *mainPane) bool {
	if c.vpRow <= 0 {
		return false
	}
	c.vpRow--
	return true
}

// MoveLeft moves cursor one display column left within the current
// row. Stops at displayCol 0 (vim default; no whichwrap).
func (c *cursor) MoveLeft(pane *mainPane) bool {
	if c.vpRow < 0 {
		return false
	}
	dc := c.displayCol(pane)
	if dc <= 0 {
		return false
	}
	c.desiredCol = dc - 1
	return true
}

// MoveRight moves cursor one display column right, clamping to the
// row's content end.
func (c *cursor) MoveRight(pane *mainPane) bool {
	if c.vpRow < 0 {
		return false
	}
	rowW := rowContentWidth(pane, c.vpRow)
	dc := c.displayCol(pane)
	if dc >= rowW-1 {
		return false
	}
	c.desiredCol = dc + 1
	return true
}

// EnsureVisible scrolls the viewport minimally so the cursor's row
// lies inside the visible window.
func (c *cursor) EnsureVisible(pane *mainPane) {
	if c.vpRow < 0 {
		return
	}
	vpOffset := pane.viewport.YOffset()
	vpHeight := pane.viewport.Height()
	if vpHeight <= 0 {
		return
	}
	if c.vpRow < vpOffset {
		pane.viewport.SetYOffset(c.vpRow)
	} else if c.vpRow >= vpOffset+vpHeight {
		pane.viewport.SetYOffset(c.vpRow - vpHeight + 1)
	}
}

// DragAlongScroll keeps the cursor visible after a viewport-driven
// scroll. If the cursor's row left the viewport, snaps it to the
// nearest visible row.
func (c *cursor) DragAlongScroll(pane *mainPane) {
	if c.vpRow < 0 {
		return
	}
	vpOffset := pane.viewport.YOffset()
	vpHeight := pane.viewport.Height()
	if vpHeight <= 0 {
		return
	}
	if c.vpRow < vpOffset {
		c.vpRow = vpOffset
	} else if c.vpRow >= vpOffset+vpHeight {
		c.vpRow = vpOffset + vpHeight - 1
	}
}

// displayCol returns the cursor's clamped display column on the
// current row (desiredCol bounded to row width).
func (c *cursor) displayCol(pane *mainPane) int {
	if c.vpRow < 0 {
		return 0
	}
	rowW := rowContentWidth(pane, c.vpRow)
	dc := max(c.desiredCol, 0)
	if rowW <= 0 {
		return 0
	}
	if dc >= rowW {
		return rowW - 1
	}
	return dc
}

// rowContentWidth returns the number of source columns of content on
// vpRow (post-gutter, no trailing spaces). 0 for empty rows.
func rowContentWidth(pane *mainPane, vpRow int) int {
	start, end := pane.wrapRowSourceColRange(vpRow)
	w := end - start + 1
	if w < 0 {
		return 0
	}
	return w
}

// clampDisplayCol bounds a click's display column to the row's
// content. Used by SetFromClick so past-EOL clicks land at end-of-line.
func clampDisplayCol(pane *mainPane, vpRow, displayCol int) int {
	if displayCol < 0 {
		return 0
	}
	rowW := rowContentWidth(pane, vpRow)
	if rowW <= 0 {
		return 0
	}
	if displayCol >= rowW {
		return rowW - 1
	}
	return displayCol
}

// ApplyHighlight paints a single reverse-video cell at the cursor's
// display position. When the cursor lands past the row's content
// (empty line, or desiredCol clamped to a short row), paints on the
// padding cell or fabricates a space.
func (c *cursor) ApplyHighlight(content string, g dragGeometry) string {
	if c.vpRow < 0 {
		return content
	}
	pane := g.pane
	if pane == nil {
		return content
	}
	dc := c.displayCol(pane)
	contentStartY := mainPaneContentTop(g)
	contentEndY := g.screenH - 2
	vpOffset := pane.viewport.YOffset()
	screenY := contentStartY + (c.vpRow - vpOffset)
	if screenY < contentStartY || screenY > contentEndY {
		return content
	}
	lines := strings.Split(content, "\n")
	if screenY >= len(lines) {
		return content
	}
	gutterOffset := g.sidebarW + 1 + pane.gutterWidth
	rightBorderCol := g.screenW - 1
	fromCol := gutterOffset + dc
	toCol := fromCol + 1
	if toCol > rightBorderCol+1 {
		return content
	}
	before, middle, after := splitAtDisplayCols(lines[screenY], fromCol, toCol)
	cell := stripANSIForWidth(middle)
	if cell == "" {
		// Past the rendered row's width — fabricate a space so the
		// cursor remains visible on empty/short lines whose padding
		// doesn't reach this column.
		cell = " "
	}
	lines[screenY] = before + "\x1b[7m" + cell + "\x1b[27m" + after
	return strings.Join(lines, "\n")
}
