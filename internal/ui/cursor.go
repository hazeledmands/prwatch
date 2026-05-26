package ui

import (
	"strings"
)

// cursor is the active pointing position in the main pane. Exists
// outside any selection mode — visual mode, PR comments, and LSP all
// need "where am I pointing" without a modal opt-in.
//
// The cursor's invariant: the cursor is always inside the viewport.
// Cursor-driven motion (j/k/h/l/click/hunk-nav) scrolls the viewport
// only when cursor would otherwise leave. Viewport-driven motion (mouse
// wheel, space/b page) drags the cursor along the edge when scrolling
// would push it off-screen. Maintaining this invariant is the
// responsibility of the model: after any cursor or viewport change, call
// EnsureVisible.
//
// Position is canonical source-space. desiredCol is the sticky display
// column for vim-style j/k vertical motion: pressing j onto a row whose
// content doesn't extend to the cursor's column clamps Pos.Column but
// preserves desiredCol so the next j onto a longer row restores the
// column. Set fresh on h/l/click; preserved by j/k.
type cursor struct {
	pos        Position
	desiredCol int
}

func newCursor() *cursor {
	return &cursor{pos: Position{SourceLine: 1, Column: 0}}
}

// Pos returns the cursor's source-space position.
func (c *cursor) Pos() Position { return c.pos }

// SetPosition places the cursor at pos and resets desiredCol from
// pos's display column. Used for initial placement and click. Returns
// false if pane state can't translate pos to a display row (e.g. empty
// pane).
func (c *cursor) SetPosition(pane *mainPane, pos Position) {
	c.pos = pos
	_, c.desiredCol = pane.positionToDisplay(pos)
}

// MoveDown advances cursor to the next visual (wrap) row, preserving
// desiredCol. Returns false at the last viewport row. Matches vim's
// gj semantics: wrapped lines step through each wrap row.
func (c *cursor) MoveDown(pane *mainPane) bool {
	vp, _ := pane.positionToDisplay(c.pos)
	nextVp := vp + 1
	if nextVp >= viewportContentRowCount(pane) {
		return false
	}
	c.placeOnRow(pane, nextVp)
	return true
}

// MoveUp moves cursor to the previous visual row, preserving desiredCol.
// Returns false at the first viewport row.
func (c *cursor) MoveUp(pane *mainPane) bool {
	vp, _ := pane.positionToDisplay(c.pos)
	if vp <= 0 {
		return false
	}
	c.placeOnRow(pane, vp-1)
	return true
}

// MoveLeft moves cursor one source column left. At Column=0, no-op.
// Updates desiredCol from the new display position. May change vp row
// when crossing a wrap-row boundary.
func (c *cursor) MoveLeft(pane *mainPane) bool {
	if c.pos.Column <= 0 {
		return false
	}
	c.pos.Column--
	_, c.desiredCol = pane.positionToDisplay(c.pos)
	return true
}

// MoveRight moves cursor one source column right. Clamps to the source
// line's last content column (no movement past EOL). Updates desiredCol.
func (c *cursor) MoveRight(pane *mainPane) bool {
	tryPos := Position{SourceLine: c.pos.SourceLine, Column: c.pos.Column + 1}
	vp, dc := pane.positionToDisplay(tryPos)
	start, end := pane.wrapRowSourceColRange(vp)
	rowW := end - start + 1
	if rowW <= 0 || dc >= rowW {
		// Past row content: that means past EOL of the source line.
		return false
	}
	// If the new vp row's source line differs from the current source
	// line, we've fallen off the end. (Can happen when an empty source
	// line follows a line whose content ends exactly on a wrap-row
	// boundary.) Refuse the move.
	if pane.sourceLineAtViewportOffset(vp) != c.pos.SourceLine {
		return false
	}
	c.pos = tryPos
	c.desiredCol = dc
	return true
}

// placeOnRow positions the cursor on vpRow at desiredCol (clamped to
// the row's content width). Pos.SourceLine and Pos.Column are updated;
// desiredCol is preserved (sticky).
func (c *cursor) placeOnRow(pane *mainPane, vpRow int) {
	sl := pane.sourceLineAtViewportOffset(vpRow)
	start, end := pane.wrapRowSourceColRange(vpRow)
	rowW := end - start + 1
	if rowW <= 0 {
		// Empty row: pin to start.
		c.pos = Position{SourceLine: sl, Column: start}
		return
	}
	dc := max(c.desiredCol, 0)
	if dc >= rowW {
		dc = rowW - 1
	}
	col := pane.absoluteColumnFromDisplay(vpRow, dc)
	c.pos = Position{SourceLine: sl, Column: col}
}

// SetFromClick places cursor at the source-space position corresponding
// to a click at (vpRow, displayCol). Updates desiredCol from the
// clicked display column (so subsequent j/k preserves the click's
// visual column).
func (c *cursor) SetFromClick(pane *mainPane, vpRow, displayCol int) {
	sl := pane.sourceLineAtViewportOffset(vpRow)
	col := pane.absoluteColumnFromDisplay(vpRow, displayCol)
	c.pos = Position{SourceLine: sl, Column: col}
	c.desiredCol = displayCol
}

// EnsureVisible scrolls the viewport minimally so the cursor's row lies
// inside the visible window. Called after cursor-driven motion to
// maintain the always-visible invariant.
func (c *cursor) EnsureVisible(pane *mainPane) {
	if c.pos.SourceLine <= 0 {
		return
	}
	vp, _ := pane.positionToDisplay(c.pos)
	vpOffset := pane.viewport.YOffset()
	vpHeight := pane.viewport.Height()
	if vpHeight <= 0 {
		return
	}
	if vp < vpOffset {
		pane.viewport.SetYOffset(vp)
	} else if vp >= vpOffset+vpHeight {
		pane.viewport.SetYOffset(vp - vpHeight + 1)
	}
}

// DragAlongScroll keeps the cursor visible after a viewport-driven
// scroll (mouse wheel, space/b, g/G). If the cursor's row is no longer
// inside the viewport window, moves the cursor to the nearest visible
// row at desiredCol — "cursor dragged along the edge."
func (c *cursor) DragAlongScroll(pane *mainPane) {
	if c.pos.SourceLine <= 0 {
		return
	}
	vp, _ := pane.positionToDisplay(c.pos)
	vpOffset := pane.viewport.YOffset()
	vpHeight := pane.viewport.Height()
	if vpHeight <= 0 {
		return
	}
	if vp < vpOffset {
		c.placeOnRow(pane, vpOffset)
	} else if vp >= vpOffset+vpHeight {
		c.placeOnRow(pane, vpOffset+vpHeight-1)
	}
}

// ApplyHighlight paints a single reverse-video cell at the cursor's
// display position. Returns content unchanged when the cursor hasn't
// been positioned (Pos.SourceLine == 0) or when the cursor's row is
// outside the visible viewport area (model is expected to maintain the
// always-visible invariant, but defensive).
//
// On rows where the cursor lands past actual content (empty line, past
// EOL of a short line), paints on the padding cell — the rendered line
// is padded with spaces to the pane width, so there's always a cell to
// reverse-video. When even the padding doesn't reach (cell beyond the
// pane's right border), or splitAtDisplayCols can't extract a cell,
// fabricates a space and inserts it so the cursor remains visible.
func (c *cursor) ApplyHighlight(content string, g dragGeometry) string {
	if c.pos.SourceLine <= 0 {
		return content
	}
	pane := g.pane
	if pane == nil {
		return content
	}
	vp, dc := pane.positionToDisplay(c.pos)
	contentStartY := mainPaneContentTop(g)
	contentEndY := g.screenH - 2
	vpOffset := pane.viewport.YOffset()
	screenY := contentStartY + (vp - vpOffset)
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
