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
// a region of the main pane. Source-space iteration is the canonical path:
// sel.Anchor / sel.Active hold the click and live-mouse positions in
// source coordinates (line + column); anchorVpRow / activeVpRow carry the
// click's viewport row for wrap-row disambiguation; the pixel fields are
// retained only for HasRange, AdvanceAutoScroll's anchor re-pin, and
// resolveSelectionEnds' active-direction signal — see REFACTOR_IDEAS.md
// for the deferred pixel-storage cleanup.
//
// scrollDir is +1 to auto-scroll the viewport down (drag past the bottom
// edge), -1 to scroll up, 0 when the drag end is inside the viewport.
//
// Anchor/Active nil means the corresponding event landed outside the
// content area (title row, status bar, borders) or past the last source
// row (in pane padding); resolveSelectionEnds clamps these to the
// visible source range with column 0 (above) or maxColumn (below).
type dragSelection struct {
	startX, startY int
	endX, endY     int
	sel            Selection
	// anchorVpRow / activeVpRow are the viewport rows the click /
	// last-move landed on. They disambiguate Position.Column at wrap-row
	// boundaries: a click past wrap row K's right edge has Column at
	// row K+1's start in source-space, but the user clicked on row K.
	// Clip logic constrains each endpoint's effect to its click vpRow's
	// wrap row. -1 means "no row info" (endpoint is nil or test set
	// fields without going through Begin/MoveEnd/Release).
	anchorVpRow int
	activeVpRow int
	active      bool
	scrollDir   int
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

// sourcePositionAt translates a screen pixel coordinate to a Position in
// source space. Returns nil iff (x, y) lands somewhere with no
// underlying source row:
//   - vertically outside the main pane's content rows (title row, status
//     bar, top/bottom border, i.e. y < contentTop or y > screenH-2), OR
//   - inside the pane content area but past the last rendered content
//     row (drag below the source's last line, into pane padding).
//
// The nil signal lets resolveSelectionEnds clamp the active end to the
// visible source range instead of pinning it to the last source line's
// column 0 (which would shrink the selection on the bottom row when the
// user dragged past content).
//
// Clicks on the gutter or past the right edge still resolve to a
// Position (column clamped) since they anchor to a real source row.
// Position.Column is source-absolute via absoluteColumnFromDisplay so
// clicks on wrap-continuation rows clip the right source columns.
// sourcePositionAt's vpRow return value is the absolute viewport row
// the click landed on. Always ≥ 0 when the returned *Position is
// non-nil; -1 when nil. Source-space clipping (extractSourceRange /
// buildHighlightClips) uses the click vpRow to confine each endpoint's
// effect to its clicked wrap row — necessary because source-absolute
// Column is ambiguous at wrap-row boundaries (col=N on row K's right
// edge equals col=N at row K+1's left edge in source space). Without
// the vpRow disambiguation, drags past a wrap row's right edge spill
// one char into the next wrap row.
func (g dragGeometry) sourcePositionAt(x, y int) (*Position, int) {
	if y < mainPaneContentTop(g) || y > g.screenH-2 {
		return nil, -1
	}
	vpRow := g.pane.viewport.YOffset() + (y - mainPaneContentTop(g))
	if vpRow >= viewportContentRowCount(g.pane) {
		return nil, -1
	}
	gutterOffset := g.sidebarW + 1 + g.pane.gutterWidth
	displayCol := x - gutterOffset
	if displayCol < 0 {
		displayCol = 0
	}
	return &Position{
		SourceLine: g.pane.sourceLineAtViewportOffset(vpRow),
		Column:     g.pane.absoluteColumnFromDisplay(vpRow, displayCol),
	}, vpRow
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
	return &dragSelection{startX: -1, startY: -1, anchorVpRow: -1, activeVpRow: -1}
}

// Begin starts a drag at the given pixel position. anchor + anchorRow
// come from g.sourcePositionAt(x, y); anchor is nil and anchorRow is -1
// when (x, y) lands outside the content area. The end is initialized to
// the same point so HasRange reports false until the mouse moves; Active
// mirrors Anchor at start.
func (d *dragSelection) Begin(x, y int, anchor *Position, anchorRow int) {
	d.active = true
	d.scrollDir = 0
	d.startX, d.startY = x, y
	d.sel.Anchor = anchor
	d.sel.Active = anchor
	d.anchorVpRow = anchorRow
	d.activeVpRow = anchorRow
	d.endX, d.endY = x, y
}

// MoveEnd updates the drag end position. active + activeRow come from
// g.sourcePositionAt(x, y); active is nil and activeRow is -1 when
// the mouse is currently outside content (or past the last source row).
// Caller should additionally call UpdateAutoScroll to manage
// scroll-direction state.
func (d *dragSelection) MoveEnd(x, y int, active *Position, activeRow int) {
	d.endX, d.endY = x, y
	d.sel.Active = active
	d.activeVpRow = activeRow
}

// Release finalizes an active drag. active + activeRow come from
// g.sourcePositionAt(x, y); see MoveEnd. Returns true if the drag was
// active; the caller should then trigger a copy.
func (d *dragSelection) Release(x, y int, active *Position, activeRow int) bool {
	if !d.active {
		return false
	}
	d.active = false
	d.scrollDir = 0
	d.sel.Active = active
	d.activeVpRow = activeRow
	d.endX, d.endY = x, y
	return true
}

// Cancel ends a drag without setting an end point — used when a click
// lands in the status bar or sidebar.
func (d *dragSelection) Cancel() {
	d.active = false
	d.scrollDir = 0
}

// IsActive reports whether a drag is in progress.
func (d *dragSelection) IsActive() bool { return d.active }

// HasRange reports whether the start and end positions differ, i.e.
// whether there is any text to highlight or copy.
func (d *dragSelection) HasRange() bool {
	return d.startX != d.endX || d.startY != d.endY
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

	lines := strings.Split(content, "\n")
	contentStartY := mainPaneContentTop(g)
	contentEndY := g.screenH - 2
	vpOffset := g.pane.viewport.YOffset()
	gutterOffset := g.sidebarW + 1 + g.pane.gutterWidth
	rightBorderCol := g.screenW - 1

	for _, clip := range buildHighlightClips(g.pane, upper, lower) {
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
		mainContent := sliceByDisplayCol(stripped, gutterOffset, rightBorderCol)
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
	var clips []highlightClip
	for sl := upper.SourceLine; sl <= lower.SourceLine; sl++ {
		if _, mapped := pane.sourceToFormatLine[sl]; !mapped {
			continue
		}
		firstVpRow, rowCount := selectionRowRange(pane, sl, upper, lower)
		for k := 0; k < rowCount; k++ {
			vpRow := firstVpRow + k
			rowSrcStart, rowSrcEnd := pane.wrapRowSourceColRange(vpRow)

			selStart := rowSrcStart
			selEnd := rowSrcEnd
			if sl == upper.SourceLine && upper.VpRow == vpRow && upper.Column > selStart {
				selStart = upper.Column
			}
			if sl == lower.SourceLine && lower.VpRow == vpRow && lower.Column < selEnd {
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

// AdvanceAutoScroll scrolls the main pane viewport one line in scrollDir
// and re-anchors startY so the original click stays attached to the same
// content row. Returns the next tick command unless the viewport has hit
// the corresponding edge.
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
	delta := g.pane.viewport.YOffset() - beforeOffset
	if delta == 0 {
		d.scrollDir = 0
		return nil
	}
	d.startY -= delta
	return scheduleDragScrollTick()
}

func scheduleDragScrollTick() tea.Cmd {
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg {
		return dragScrollTickMsg{}
	})
}

// SelectedText extracts the plain text from the current drag selection.
// Operates on Selection's source-space endpoints — no pixel coordinates
// in the body. Iterates source lines in [upper, lower] via
// pane.sourceToFormatLine, pulling formatted (pre-wrap) content per
// source line, clipping by source-absolute column on the first and last
// source lines. Wrapped lines emit one logical line of output (joined
// in source space), not one per wrap row.
func (d *dragSelection) SelectedText(g dragGeometry) string {
	if !d.HasRange() {
		return ""
	}
	upper, lower, ok := d.resolveSelectionEnds(g)
	if !ok {
		return ""
	}
	return extractSourceRange(g.pane, upper, lower)
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

// resolveSelectionEnds returns the Selection's two endpoints in document
// order (upper ≤ lower by SourceLine then Column), substituting visible-
// range clamps for any end that lands outside the content area.
//
// Classification of outside-content ends:
//   - Anchor nil → click was outside at Begin time. In practice the only
//     reachable case is "above content" (clicks below content land on
//     borders that don't initiate drags via Begin), so anchor-nil clamps
//     the upper end to (visible.Start, col 0).
//   - Active nil → mouse is currently above or below content. The
//     d.endY pixel-Y signal disambiguates: < contentStartY = above
//     (upper-clamp), > contentEndY = below (lower-clamp at end-of-line).
//     This is the last transitional use of pixel storage in the
//     rendering path; pixel fields go away in slice 5 once a richer
//     Endpoint type (or visual-mode's source-native input) replaces the
//     direction signal.
//
// Anchors that scrolled off-screen (auto-scroll moved the viewport past
// the original click) are intentionally NOT clamped — the source line
// is stable across viewport scrolls in source-space, so the original
// click line is preserved in the copy even when no longer visible. This
// matches the old pixel behavior of decrementing startY in
// AdvanceAutoScroll to keep the anchor pinned to its content row.
func (d *dragSelection) resolveSelectionEnds(g dragGeometry) (upper, lower orderedEnd, ok bool) {
	visible := g.pane.visibleRange()
	contentStartY := mainPaneContentTop(g)
	contentEndY := g.screenH - 2

	activeDir := 0 // -1 above, +1 below; 0 when active is inside content
	if d.sel.Active == nil {
		// Active is nil when (a) mouse is above content, (b) mouse is
		// below the pane, or (c) mouse is inside the pane but past the
		// last source row (in pane padding). Cases (b) and (c) both
		// clamp the lower end to visible.End. Pixel-Y disambiguates (a)
		// from (b)+(c); the vpRow check picks up (c).
		vpRow := g.pane.viewport.YOffset() + (d.endY - contentStartY)
		switch {
		case d.endY < contentStartY:
			activeDir = -1
		case d.endY > contentEndY || vpRow >= viewportContentRowCount(g.pane):
			activeDir = +1
		default:
			// Active is nil but pixel-Y is inside content and on a real
			// source row — happens in tests that set pixel fields
			// without populating sel. Treat as no selectable range.
			return orderedEnd{}, orderedEnd{}, false
		}
	}

	anchor := orderedEnd{VpRow: d.anchorVpRow}
	if d.sel.Anchor != nil {
		anchor.Position = *d.sel.Anchor
	}
	active := orderedEnd{VpRow: d.activeVpRow}
	if d.sel.Active != nil {
		active.Position = *d.sel.Active
	}

	// Synthesized endpoints (anchor/active outside content) pin to the
	// first/last visible wrap row, not just the source line. That matters
	// when the synthesized source line wraps: without a VpRow, the
	// iteration would pull in wrap rows that scrolled off-screen above
	// (or extend into rows below the active mouse).
	vpOffset := g.pane.viewport.YOffset()
	visibleTopRow := vpOffset
	visibleBottomRow := vpOffset + g.pane.viewport.Height() - 1
	if last := viewportContentRowCount(g.pane) - 1; last >= 0 && visibleBottomRow > last {
		visibleBottomRow = last
	}

	switch {
	case d.sel.Anchor == nil && d.sel.Active == nil:
		return orderedEnd{}, orderedEnd{}, false
	case d.sel.Anchor == nil:
		upper = orderedEnd{Position: Position{SourceLine: visible.Start.SourceLine, Column: 0}, VpRow: visibleTopRow}
		lower = active
	case d.sel.Active == nil:
		if activeDir < 0 {
			upper = orderedEnd{Position: Position{SourceLine: visible.Start.SourceLine, Column: 0}, VpRow: visibleTopRow}
			lower = anchor
		} else {
			upper = anchor
			lower = orderedEnd{Position: Position{SourceLine: visible.End.SourceLine, Column: maxColumn}, VpRow: visibleBottomRow}
		}
	default:
		if positionLess(anchor.Position, active.Position) {
			upper, lower = anchor, active
		} else {
			upper, lower = active, anchor
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
// break copies one continuous string); chars dropped at word-boundary
// breaks stay dropped, since they're not visible.
//
// upper.VpRow / lower.VpRow narrow the wrap-row iteration on the
// endpoint source lines (see selectionRowRange), disambiguating clicks
// at wrap-row boundaries.
func extractSourceRange(pane *mainPane, upper, lower orderedEnd) string {
	vpLines := strings.Split(pane.viewport.GetContent(), "\n")
	var out strings.Builder
	wroteFirst := false
	for sl := upper.SourceLine; sl <= lower.SourceLine; sl++ {
		if _, mapped := pane.sourceToFormatLine[sl]; !mapped {
			continue
		}
		firstVpRow, rowCount := selectionRowRange(pane, sl, upper, lower)

		var srcOut strings.Builder
		for k := 0; k < rowCount; k++ {
			vpRow := firstVpRow + k
			if vpRow < 0 || vpRow >= len(vpLines) {
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
				continue
			}
			fromCol := selStart - rowStart
			toCol := selEnd - rowStart + 1
			srcOut.WriteString(extractLineFragment(vpLines[vpRow], fromCol, toCol, pane.gutterWidth))
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
// out of a single rendered line, after stripping ANSI codes, trimming
// trailing whitespace, and removing the gutter prefix. A toCol of -1
// means "to end of line." Returns empty string when the range is empty
// or past the line's content.
func extractLineFragment(line string, fromCol, toCol, gw int) string {
	stripped := stripANSIForWidth(line)
	stripped = strings.TrimRight(stripped, " ")
	if gw > 0 && len(stripped) > gw {
		stripped = stripped[gw:]
	}
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
	return sliceByDisplayCol(stripped, fromCol, toCol)
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
	text := m.drag.SelectedText(m.dragGeom())
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
