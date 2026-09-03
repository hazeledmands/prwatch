package ui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/command"
)

// dragScrollTickMsg drives auto-scroll while a drag selection is held past
// the top or bottom edge of the main pane viewport. Only delivered while
// the drag is active and scrollDir != 0.
type dragScrollTickMsg struct{}

const dragScrollInterval = 60 * time.Millisecond

// dragSelection owns the click-drag-release state used to highlight and copy
// a region of the main pane. State is source-space throughout:
//
//   - anchor / active hold the click and live-mouse positions as
//     endpoint values (source Position + viewport row + outside
//     direction). Both are value types so the struct is comparable; that
//     drives HasRange.
//   - inProgress flags whether a drag is currently being tracked
//     (between Begin and Release/Cancel).
//   - scrollDir is +1 to auto-scroll the viewport down (drag past the
//     bottom edge), -1 to scroll up, 0 when the drag end is inside.
//
// No pixel coordinates are kept on the struct — pixel-to-source
// translation happens once at the geometry boundary (dragGeometry.clickAt)
// and the result is consumed only as a source-space endpoint.
type dragSelection struct {
	anchor     endpoint
	active     endpoint
	inProgress bool
	scrollDir  int
}

// endpoint locates one end of a drag selection. When OutsideDir is 0,
// Pos and VpRow describe where the click landed: Pos is the source
// line + column, VpRow is the absolute viewport row (so wrap-row-
// boundary ambiguity can be disambiguated). When OutsideDir is non-zero
// the click landed outside the main pane's content rows — -1 above
// (title row, status bar, or above the pane), +1 below (past the last
// source row, or below the pane). Pos and VpRow are then meaningless;
// resolveSelectionEnds substitutes a visible-range clamp.
//
// Value-typed (no pointers) so the parent struct is comparable —
// HasRange is just d.anchor != d.active.
type endpoint struct {
	Pos        Position
	VpRow      int
	OutsideDir int
	// DisplayCol is the click's column relative to the start of the main
	// pane's text, past the sidebar and gutter (clamped at 0). Cursor
	// placement needs the *displayed* column, not Pos.Column's source-absolute
	// one; carrying it here keeps that translation inside clickAt rather than
	// re-derived at each call site, per the "layout geometry comes from one
	// function" rule.
	DisplayCol int
	// OutsideSidebar reports that x landed over the sidebar proper — not over
	// the main pane's gutter, which belongs to the pane. A click and a release
	// on the gutter both clamp to column 0; only the sidebar declines cursor
	// placement. Drag selection ignores this field entirely.
	OutsideSidebar bool
}

// dragGeometry is the screen-layout snapshot dragSelection needs to map
// pixel coords onto rendered content. Built fresh by Model at each call
// site via Model.dragGeom().
type dragGeometry struct {
	statusRows int
	sidebarW   int // 0 when sidebar hidden
	screenW    int
	screenH    int
	pane       *mainPane
}

// screenRegion names the interactive region that owns a screen coordinate.
type screenRegion int

const (
	// regionStatusBar is the status bar at the top of the screen. It is
	// clickable but not scrollable.
	regionStatusBar screenRegion = iota
	// regionSidebar is the sidebar column, including its borders. Never
	// returned when the sidebar is hidden.
	regionSidebar
	// regionMainPane is everything else: the main pane's content rows plus
	// the box around them (top border, title row, bottom border). The pane
	// owns its whole box — clickAt is what narrows a coordinate inside it
	// down to content rows.
	regionMainPane
)

// regionAt resolves a screen coordinate to the region that owns it. It is
// the single authority every mouse path consults — click, drag, hover and
// wheel — so no handler re-derives "is this the sidebar?" or "is this the
// status bar?" from widths and row counts of its own. See CLAUDE.md,
// "Layout geometry comes from one function".
//
// A hidden sidebar is encoded as sidebarW == 0 (see Model.dragGeom), which
// makes the main pane the owner of the leftmost columns automatically.
func (g dragGeometry) regionAt(x, y int) screenRegion {
	if y < g.statusRows {
		return regionStatusBar
	}
	if g.sidebarW > 0 && x < g.sidebarW {
		return regionSidebar
	}
	return regionMainPane
}

// clickAt translates a screen pixel coordinate to a source-space
// endpoint. Returns an OutsideDir endpoint when the click lands:
//   - vertically outside the main pane's content rows (title row, status
//     bar, top/bottom border): OutsideDir = -1 (above) or +1 (below).
//   - inside the pane content area but past the last rendered content
//     row (drag below the source's last line, into pane padding):
//     OutsideDir = +1.
//
// Otherwise returns an inside endpoint: Pos is the source line + column
// (Column source-absolute, wrap-aware via absoluteColumnFromDisplay) and
// VpRow is the absolute viewport row the click landed on. VpRow is
// needed to disambiguate source-absolute Column at wrap-row boundaries
// (col=N on row K's right edge equals col=N at row K+1's left edge in
// source space).
func (g dragGeometry) clickAt(x, y int) endpoint {
	if y < mainPaneContentTop(g) {
		return endpoint{OutsideDir: -1}
	}
	if y > g.screenH-2 {
		return endpoint{OutsideDir: +1}
	}
	vpRow := g.pane.viewport.YOffset() + (y - mainPaneContentTop(g))
	if vpRow >= viewportContentRowCount(g.pane) {
		return endpoint{OutsideDir: +1}
	}
	gutterOffset := g.sidebarW + 1 + g.pane.gutterWidth
	displayCol := x - gutterOffset
	// Only the sidebar proper is "outside": the gutter sits inside the pane,
	// so a gutter x clamps to column 0 (matching what a gutter *click* has
	// always done) rather than being treated as a miss.
	outsideSidebar := g.sidebarW > 0 && x < g.sidebarW
	if displayCol < 0 {
		displayCol = 0
	}
	return endpoint{
		Pos: Position{
			SourceLine: g.pane.sourceLineAtViewportOffset(vpRow),
			Column:     g.pane.absoluteColumnFromDisplay(vpRow, displayCol),
		},
		VpRow:          vpRow,
		DisplayCol:     displayCol,
		OutsideSidebar: outsideSidebar,
	}
}

// viewportContentRowCount returns the number of rows of actual content
// the viewport holds (wrap-extended, post-truncation). Used to detect
// "past content" clicks that land inside the pane but below the source
// — the viewport widget pads with blanks past this count.
func viewportContentRowCount(pane *mainPane) int {
	content := pane.viewport.GetContent()
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func newDragSelection() *dragSelection {
	return &dragSelection{}
}

// Begin starts a drag at the given endpoint (typically from
// g.clickAt(x, y)). The active end mirrors the anchor at start so
// HasRange reports false until the mouse moves.
func (d *dragSelection) Begin(e endpoint) {
	d.inProgress = true
	d.scrollDir = 0
	d.anchor = e
	d.active = e
}

// MoveEnd updates the drag's active end (typically from
// g.clickAt(msg.X, msg.Y)). Caller should additionally call
// UpdateAutoScroll to manage scroll-direction state when the mouse
// crosses the pane edges.
func (d *dragSelection) MoveEnd(e endpoint) {
	d.active = e
}

// Release finalizes the drag at the given endpoint. Returns true if the
// drag was active; the caller should then trigger a copy.
func (d *dragSelection) Release(e endpoint) bool {
	if !d.inProgress {
		return false
	}
	d.inProgress = false
	d.scrollDir = 0
	d.active = e
	return true
}

// Cancel ends a drag without setting an end point — used when a click
// lands in the status bar or sidebar.
func (d *dragSelection) Cancel() {
	d.inProgress = false
	d.scrollDir = 0
}

// IsActive reports whether a drag is in progress.
func (d *dragSelection) IsActive() bool { return d.inProgress }

// HasRange reports whether the anchor and the active end differ, i.e.
// whether there is any text to highlight or copy.
func (d *dragSelection) HasRange() bool {
	return d.anchor != d.active
}

// ScrollDir reports the current auto-scroll direction. Exposed for tests.
func (d *dragSelection) ScrollDir() int { return d.scrollDir }

// ApplyHighlight returns content with reverse-video applied to the
// drag-selected region. Operates on the Selection (source-space ends)
// resolved through resolveSelectionEnds — wraps and gutter are handled
// by source-space column math, not screen-coord clipping. Lines outside
// the main pane content area (status bar, borders, title row) are never
// touched.
func (d *dragSelection) ApplyHighlight(content string, g dragGeometry) string {
	if !d.HasRange() {
		return content
	}
	upper, lower, ok := d.resolveSelectionEnds(g)
	if !ok {
		return content
	}
	return paintHighlightClips(content, g, buildHighlightClips(g.pane, upper, lower))
}

// paintHighlightClips applies reverse-video to the given clips on the
// rendered content. Shared by dragSelection.ApplyHighlight and
// selection.ApplyHighlight (and any future highlight consumer) so the
// rendering math lives in one place.
func paintHighlightClips(content string, g dragGeometry, clips []highlightClip) string {
	if len(clips) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	contentStartY := mainPaneContentTop(g)
	contentEndY := g.screenH - 2
	vpOffset := g.pane.viewport.YOffset()
	gutterOffset := g.sidebarW + 1 + g.pane.gutterWidth
	rightBorderCol := g.screenW - 1

	for _, clip := range clips {
		screenY := contentStartY + (clip.vpRow - vpOffset)
		if screenY < contentStartY || screenY > contentEndY || screenY >= len(lines) {
			continue
		}
		fromCol := gutterOffset + clip.fromDisplayCol
		toCol := gutterOffset + clip.toDisplayCol + 1
		if toCol > rightBorderCol+1 {
			toCol = rightBorderCol + 1
		}
		stripped := stripANSIForWidth(lines[screenY])
		// Clipping the rendered row to the pane, not selecting: a glyph
		// straddling the right border must be dropped, since taking it whole
		// would make the measured content end a cell past the border.
		mainContent := sliceByDisplayCol(stripped, gutterOffset, rightBorderCol, roundInward)
		trimmed := strings.TrimRight(mainContent, " ")
		contentEndCol := gutterOffset + displayWidthOf(trimmed)
		if toCol > contentEndCol {
			toCol = contentEndCol
		}
		if fromCol >= toCol {
			continue
		}
		before, middle, after := splitAtDisplayCols(lines[screenY], fromCol, toCol)
		selected := stripANSIForWidth(middle)
		if selected == "" {
			continue
		}
		lines[screenY] = before + "\x1b[7m" + selected + "\x1b[27m" + after
	}
	return strings.Join(lines, "\n")
}

// highlightClip names one screen-row's slice of the highlight region in
// gutter-relative display columns. Built by buildHighlightClips and
// consumed by ApplyHighlight.
type highlightClip struct {
	vpRow                        int
	fromDisplayCol, toDisplayCol int
}

// buildHighlightClips walks source lines [upper.SourceLine,
// lower.SourceLine], emitting one or more clips per source line — one
// per wrap row in wrap mode, one per source line in no-wrap mode. The
// clips' display columns are gutter-relative.
//
// Source-space → screen-space conversion is done here in one place; the
// rendering loop in ApplyHighlight then just applies the clips. Wrap-
// row column boundaries come from wrapRowSourceColRange (uses
// absoluteColumnFromDisplay for source-absolute starts).
//
// upper.VpRow / lower.VpRow constrain the wrap-row range on the
// endpoint source lines (see selectionRowRange). This is the
// disambiguation for wrap-row-boundary clicks: source-absolute
// upper.Column on row K+1's left edge equals lower.Column on row K's
// right edge, but the click was on a specific row.
func buildHighlightClips(pane *mainPane, upper, lower orderedEnd) []highlightClip {
	if lower.VpRow < upper.VpRow {
		return nil
	}
	var clips []highlightClip
	for vpRow := upper.VpRow; vpRow <= lower.VpRow; vpRow++ {
		rowSrcStart, rowSrcEnd := pane.wrapRowSourceColRange(vpRow)
		selStart := rowSrcStart
		selEnd := rowSrcEnd
		if vpRow == upper.VpRow && upper.Column > selStart {
			selStart = upper.Column
		}
		if vpRow == lower.VpRow && lower.Column < selEnd {
			selEnd = lower.Column
		}
		if selStart > selEnd {
			continue
		}
		clips = append(clips, highlightClip{
			vpRow:          vpRow,
			fromDisplayCol: selStart - rowSrcStart,
			toDisplayCol:   selEnd - rowSrcStart,
		})
	}
	return clips
}

// selectionRowRange returns (firstVpRow, count) — the wrap rows of
// source line sl that participate in the selection. By default this is
// all of sl's wrap rows. The endpoint VpRows narrow it on the endpoint
// source lines: an upper click on row K means iterate from K (no earlier
// rows); a lower click on row K means iterate up to and including K (no
// later rows).
func selectionRowRange(pane *mainPane, sl int, upper, lower orderedEnd) (firstVpRow, count int) {
	firstVpRow = pane.sourceLineToViewportOffset(sl)
	count = pane.wrapRowCountAtVpRow(firstVpRow)
	if sl == upper.SourceLine && upper.VpRow >= firstVpRow && upper.VpRow < firstVpRow+count {
		skip := upper.VpRow - firstVpRow
		firstVpRow = upper.VpRow
		count -= skip
	}
	if sl == lower.SourceLine && lower.VpRow >= firstVpRow && lower.VpRow < firstVpRow+count {
		count = lower.VpRow - firstVpRow + 1
	}
	return firstVpRow, count
}

// UpdateAutoScroll inspects the drag-end Y in screen coords and updates
// scrollDir: -1 if the user has dragged above the viewport top, +1 if
// below the bottom, 0 if back inside. Returns a tick command when
// scrolling needs to (re)start, nil otherwise.
func (d *dragSelection) UpdateAutoScroll(y int, g dragGeometry) tea.Cmd {
	top, bottom := mainPaneContentRows(g.statusRows, g.screenH)
	prev := d.scrollDir
	switch {
	case y < top:
		d.scrollDir = -1
	case y > bottom:
		d.scrollDir = +1
	default:
		d.scrollDir = 0
	}
	if d.scrollDir != 0 && prev == 0 {
		return scheduleDragScrollTick()
	}
	return nil
}

// AdvanceAutoScroll scrolls the main pane viewport one line in
// scrollDir. The anchor's source line/column/vpRow stay stable across
// scrolls — no re-anchoring needed because the anchor is in source
// space, not screen space. Returns the next tick command unless the
// viewport has hit the corresponding edge.
func (d *dragSelection) AdvanceAutoScroll(g dragGeometry) tea.Cmd {
	if d.scrollDir == 0 {
		return nil
	}
	beforeOffset := g.pane.viewport.YOffset()
	if d.scrollDir > 0 {
		g.pane.viewport.ScrollDown(1)
	} else {
		g.pane.viewport.ScrollUp(1)
	}
	if g.pane.viewport.YOffset() == beforeOffset {
		d.scrollDir = 0
		return nil
	}
	return scheduleDragScrollTick()
}

func scheduleDragScrollTick() tea.Cmd {
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg {
		return dragScrollTickMsg{}
	})
}

// SelectedText extracts the plain text from the current drag selection.
// Operates on the anchor / active endpoints in source space. Iterates
// source lines in [upper, lower] via pane.sourceToFormatLine, pulling
// formatted (pre-wrap) content per source line, clipping by source-
// absolute column on the first and last source lines. Wrapped lines
// emit one logical line of output (joined in source space), not one
// per wrap row.
func (d *dragSelection) SelectedText(g dragGeometry) string {
	if !d.HasRange() {
		return ""
	}
	upper, lower, ok := d.resolveSelectionEnds(g)
	if !ok {
		return ""
	}
	// A drag is cell-wise: what the highlight covers, trailing render
	// padding excluded (PROMPT.md, mouse behavior).
	return extractSourceRange(g.pane, upper, lower, false)
}

// orderedEnd is a Position paired with the click's viewport row, used
// to disambiguate source-absolute Column at wrap-row boundaries. VpRow
// is -1 when the endpoint was synthesized (e.g., clamped to
// visible.Start when the click was outside content) — meaning "no row
// constraint, iterate all wrap rows of this source line."
type orderedEnd struct {
	Position
	VpRow int
}

// resolveSelectionEnds returns the drag's two endpoints in document
// order (upper ≤ lower by SourceLine then Column), substituting visible-
// range clamps for any endpoint that landed outside the content area.
//
// Each end is classified by its OutsideDir:
//   - OutsideDir == -1: click was above the content area (title row,
//     status bar, or above the pane) → clamps the upper end to
//     (visible.Start, col 0).
//   - OutsideDir == +1: click was below the pane or past the last source
//     row → clamps the lower end to (visible.End, maxColumn).
//   - OutsideDir == 0: click was inside content; use Pos and VpRow.
//
// Anchors that scrolled off-screen (auto-scroll moved the viewport past
// the original click) are intentionally NOT clamped — the anchor's
// source line is stable across viewport scrolls, so the original click
// line is preserved in the copy even when no longer visible.
//
// Synthesized endpoints (from OutsideDir != 0) carry visibleTopRow /
// visibleBottomRow as their VpRow so the wrap-row clip logic doesn't
// pull in rows that scrolled off-screen above or extend past the active
// mouse.
func (d *dragSelection) resolveSelectionEnds(g dragGeometry) (upper, lower orderedEnd, ok bool) {
	visible := g.pane.visibleRange()

	vpOffset := g.pane.viewport.YOffset()
	visibleTopRow := vpOffset
	visibleBottomRow := vpOffset + g.pane.viewport.Height() - 1
	if last := viewportContentRowCount(g.pane) - 1; last >= 0 && visibleBottomRow > last {
		visibleBottomRow = last
	}

	switch {
	case d.anchor.OutsideDir != 0 && d.active.OutsideDir != 0:
		return orderedEnd{}, orderedEnd{}, false
	case d.anchor.OutsideDir != 0:
		upper = orderedEnd{Position: Position{SourceLine: visible.Start.SourceLine, Column: 0}, VpRow: visibleTopRow}
		lower = orderedEnd{Position: d.active.Pos, VpRow: d.active.VpRow}
	case d.active.OutsideDir != 0:
		if d.active.OutsideDir < 0 {
			upper = orderedEnd{Position: Position{SourceLine: visible.Start.SourceLine, Column: 0}, VpRow: visibleTopRow}
			lower = orderedEnd{Position: d.anchor.Pos, VpRow: d.anchor.VpRow}
		} else {
			upper = orderedEnd{Position: d.anchor.Pos, VpRow: d.anchor.VpRow}
			lower = orderedEnd{Position: Position{SourceLine: visible.End.SourceLine, Column: maxColumn}, VpRow: visibleBottomRow}
		}
	default:
		a := orderedEnd{Position: d.anchor.Pos, VpRow: d.anchor.VpRow}
		b := orderedEnd{Position: d.active.Pos, VpRow: d.active.VpRow}
		if positionLess(a.Position, b.Position) {
			upper, lower = a, b
		} else {
			upper, lower = b, a
		}
	}
	return upper, lower, true
}

// positionLess returns true iff a precedes b in document order
// (SourceLine, then Column).
func positionLess(a, b Position) bool {
	if a.SourceLine != b.SourceLine {
		return a.SourceLine < b.SourceLine
	}
	return a.Column < b.Column
}

// maxColumn is the sentinel "to end of line" column. extractLineFragment
// clamps any toCol > line width to line width, so column values up to
// math.MaxInt32 are safe.
const maxColumn = (1 << 31) - 1

// extractSourceRange walks source lines [upper.SourceLine,
// lower.SourceLine], pulling each line's visible content out of the
// viewport's wrap-extended / truncation-applied output and clipping by
// upper.Column / lower.Column on the first and last lines. Lines with
// no source mapping (rendered-only rows like inline-removed
// annotations) are skipped.
//
// One logical line of output per source line. In no-wrap mode each
// source line has exactly one wrap row in viewport.GetContent() (post
// horizontal-truncation), so the copy preserves only visible chars —
// chars truncated off the right of the pane are not in the copy. In
// wrap mode the source line's wrap rows are joined into one logical
// line (matching the user's expectation that selecting across a wrap
// break copies one continuous string), and the spaces the wrap break ate
// are put back from pane.breakSpacesBefore so the join is byte-identical
// to the source line (PROMPT.md:365 — "copied text should be the same as
// the text from the file").
//
// upper.VpRow / lower.VpRow narrow the wrap-row iteration on the
// endpoint source lines (see selectionRowRange), disambiguating clicks
// at wrap-row boundaries.
//
// lineWise selects the copy semantics PROMPT.md's `### visual mode`
// adjudicates. A line-wise (`V`) selection is a *source-text* operation:
// each selected line is reproduced exactly, so the line's own trailing
// whitespace — which stripGutterText trims off every rendered row — is
// re-appended from pane.trailingSpacesAfter. A cell-wise selection (`v`,
// mouse drag) is a *screen* operation: it copies what the highlight
// covers, and trailing render padding stays excluded. The two mechanisms
// are disjoint by construction: breakSpacesBefore covers a wrapped line's
// interior breaks, trailingSpacesAfter only its final row.
func extractSourceRange(pane *mainPane, upper, lower orderedEnd, lineWise bool) string {
	vpLines := strings.Split(pane.viewport.GetContent(), "\n")
	var out strings.Builder
	wroteFirst := false
	for sl := upper.SourceLine; sl <= lower.SourceLine; sl++ {
		if _, mapped := pane.sourceToFormatLine[sl]; !mapped {
			continue
		}
		firstVpRow, rowCount := selectionRowRange(pane, sl, upper, lower)
		lineFirstRow := pane.sourceLineToViewportOffset(sl)
		lineLastRow := lineFirstRow + pane.wrapRowCountAtVpRow(lineFirstRow) - 1

		var srcOut strings.Builder
		// Whether the previous wrap row's fragment ran all the way to that
		// row's last cell — the left half of the "selection spans this wrap
		// break" test below.
		prevRanToRowEnd := false
		for k := 0; k < rowCount; k++ {
			vpRow := firstVpRow + k
			if vpRow < 0 || vpRow >= len(vpLines) {
				prevRanToRowEnd = false
				continue
			}
			rowStart, rowEnd := pane.wrapRowSourceColRange(vpRow)

			selStart := rowStart
			selEnd := rowEnd
			if sl == upper.SourceLine && upper.VpRow == vpRow && upper.Column > selStart {
				selStart = upper.Column
			}
			if sl == lower.SourceLine && lower.VpRow == vpRow && lower.Column < selEnd {
				selEnd = lower.Column
			}
			if selStart > selEnd {
				prevRanToRowEnd = false
				continue
			}
			// Re-insert the spaces the wrap break above this row consumed.
			// They occupy no cell, so the selection can't name them
			// directly; the rule is that they're in the copy exactly when
			// the selection spans the break — it covers the last cell of
			// the row above and the first cell of this one. That keeps a
			// whole-line (or whole-row) yank byte-identical to the source,
			// and keeps a selection that merely stops at a row edge from
			// picking up a phantom leading/trailing space.
			if k > 0 && prevRanToRowEnd && selStart == rowStart {
				if n := pane.breakSpacesBefore(vpRow); n > 0 {
					srcOut.WriteString(strings.Repeat(" ", n))
				}
			}
			fromCol := selStart - rowStart
			toCol := selEnd - rowStart + 1
			srcOut.WriteString(extractLineFragment(vpLines[vpRow], fromCol, toCol, pane.gutterWidth))
			prevRanToRowEnd = selEnd >= rowEnd
		}

		// Re-append the source line's own trailing whitespace for a line-wise
		// selection. Guarded on the iteration having actually reached the end
		// of the line — selection.resolveEnds extends a `V` selection to the
		// last wrap row's final column, so this holds for every line in a real
		// V selection, but the guard keeps a partially-clipped range (a
		// synthesized endpoint, an out-of-range row) from growing spaces it
		// never covered.
		if lineWise && prevRanToRowEnd && firstVpRow+rowCount-1 == lineLastRow {
			if n := pane.trailingSpacesAfter(lineLastRow); n > 0 {
				srcOut.WriteString(strings.Repeat(" ", n))
			}
		}

		if wroteFirst {
			out.WriteString("\n")
		}
		out.WriteString(srcOut.String())
		wroteFirst = true
	}
	return out.String()
}

// extractLineFragment pulls the [fromCol, toCol) gutter-relative slice
// out of a single rendered line, after stripping ANSI codes, removing
// the gutter prefix, and trimming trailing whitespace (in that order —
// see stripGutterText). A toCol of -1 means "to end of line." Returns
// empty string when the range is empty or past the line's content.
func extractLineFragment(line string, fromCol, toCol, gw int) string {
	stripped := stripGutterText(line, gw)
	lineWidth := displayWidthOf(stripped)
	if toCol < 0 || toCol > lineWidth {
		toCol = lineWidth
	}
	if fromCol > lineWidth {
		fromCol = lineWidth
	}
	if fromCol >= toCol {
		return ""
	}
	// Selection: round outward at both edges so an endpoint landing on either
	// cell of a wide glyph takes the whole glyph, symmetrically (PROMPT.md,
	// mouse behavior). Rounding the start outward and letting the end straddle
	// — the old behavior — made the two edges disagree.
	return sliceByDisplayCol(stripped, fromCol, toCol, roundOutward)
}

// mainPaneContentTop returns the screen-Y of the first row inside the
// main pane's content area (below status bar, top border, title row).
func mainPaneContentTop(g dragGeometry) int {
	const topBorder, titleRow = 1, 1
	return g.statusRows + topBorder + titleRow
}

// yankPath copies the current file path to the clipboard. Sidebar focused:
// copies the relative path of the selected file. Main pane focused: copies
// path:startLine-endLine for the visible range.
func (m *Model) yankPath() tea.Cmd {
	file := m.sidebar.SelectedItem()
	if file == "" || m.sidebar.SelectedIsDir() {
		return nil
	}
	var text string
	if m.focus == SidebarFocus {
		text = file
	} else {
		vr := m.mainPane.visibleRange()
		if vr.Start.SourceLine == vr.End.SourceLine {
			text = fmt.Sprintf("%s:%d", file, vr.Start.SourceLine)
		} else {
			text = fmt.Sprintf("%s:%d-%d", file, vr.Start.SourceLine, vr.End.SourceLine)
		}
	}
	m.copyToClipboard(text)
	m.notification = "copied " + text
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return notificationExpiredMsg{}
	})
}

func (m *Model) copySelection() tea.Cmd {
	return m.copyAndNotify(m.drag.SelectedText(m.dragGeom()))
}

// copyVisualSelection copies the current visual-mode selection's text
// to the clipboard. Returns nil cmd when no selection is active or the
// selected text is empty.
func (m *Model) copyVisualSelection() tea.Cmd {
	return m.copyAndNotify(m.selection.SelectedText(m.dragGeom()))
}

// copyAndNotify pastes text to the clipboard and shows the
// "copied selection (N lines, M bytes)" notification. Shared by drag
// and visual-mode yank paths.
func (m *Model) copyAndNotify(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	m.copyToClipboard(text)
	lines := strings.Count(text, "\n") + 1
	lineWord := "lines"
	if lines == 1 {
		lineWord = "line"
	}
	bytes := len(text)
	byteWord := "bytes"
	if bytes == 1 {
		byteWord = "byte"
	}
	m.notification = fmt.Sprintf("copied selection (%d %s, %d %s)", lines, lineWord, bytes, byteWord)
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return notificationExpiredMsg{}
	})
}

// copyToClipboard copies the given text to the system clipboard using
// platform-specific tools (pbcopy on macOS, xclip on Linux).
func (m *Model) copyToClipboard(text string) {
	var cmd command.Command
	switch runtime.GOOS {
	case "darwin":
		cmd = m.cmdFactory("pbcopy")
	case "linux":
		cmd = m.cmdFactory("xclip", "-selection", "clipboard")
	default:
		return
	}
	cmd.SetStdin(strings.NewReader(text))
	cmd.Run()
}
