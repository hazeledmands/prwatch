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
// a region of the main pane. The pixel-coord fields (startX/Y, endX/Y) are
// in the same frame as the mouse events; the rendering paths (ApplyHighlight,
// SelectedText) still iterate by screen coordinates and clip by screen X.
// scrollDir is +1 to auto-scroll the viewport down (drag past the bottom
// edge), -1 to scroll up, 0 when the drag end is inside the viewport.
//
// sel.Anchor and sel.Active hold the click and current-mouse positions
// translated into source space. Anchor is set once at Begin; Active is
// re-set every MoveEnd / Release. nil means the corresponding event landed
// vertically outside the content area (title row, status bar, borders);
// SelectedText uses Anchor's nil-ness to clamp the upper end of the
// selection to the first visible content row instead of pulling a screen
// row the user never saw into the copy.
//
// Pixel storage is still kept because rendering iterates rendered rows;
// see REFACTOR_IDEAS.md for the deferred pixel→Position migration in
// ApplyHighlight / SelectedText.
type dragSelection struct {
	startX, startY int
	endX, endY     int
	sel            Selection
	active         bool
	scrollDir      int
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
// source space. Returns nil iff (x, y) lands vertically outside the main
// pane's content rows — title row, status bar, top/bottom border. Clicks
// on the gutter or past the right edge still resolve to a Position
// (column clamped), since they anchor to a real source row. The nil
// distinction is what SelectedText uses in place of the old originStartY
// "anchor above content" check; horizontal placement is handled by
// existing column clamping further downstream.
func (g dragGeometry) sourcePositionAt(x, y int) *Position {
	topBorder := 1
	titleRow := 1
	contentStartY := g.statusRows + topBorder + titleRow
	contentEndY := g.screenH - 2
	if y < contentStartY || y > contentEndY {
		return nil
	}
	gutterOffset := g.sidebarW + 1 + g.pane.gutterWidth
	col := x - gutterOffset
	if col < 0 {
		col = 0
	}
	vpRelativeY := y - contentStartY
	return &Position{
		SourceLine: g.pane.sourceLineAtViewportOffset(g.pane.viewport.YOffset() + vpRelativeY),
		Column:     col,
	}
}

func newDragSelection() *dragSelection {
	return &dragSelection{startX: -1, startY: -1}
}

// Begin starts a drag at the given pixel position. anchor is the click
// translated into source-space — nil iff (x, y) lands outside the content
// area. The end is initialized to the same point so HasRange reports false
// until the mouse moves; Active mirrors Anchor at start.
func (d *dragSelection) Begin(x, y int, anchor *Position) {
	d.active = true
	d.scrollDir = 0
	d.startX, d.startY = x, y
	d.sel.Anchor = anchor
	d.sel.Active = anchor
	d.endX, d.endY = x, y
}

// MoveEnd updates the drag end position. active is the source-space
// translation of (x, y) — nil iff the mouse is currently vertically
// outside the content area (above or below). Caller should additionally
// call UpdateAutoScroll to manage scroll-direction state.
func (d *dragSelection) MoveEnd(x, y int, active *Position) {
	d.endX, d.endY = x, y
	d.sel.Active = active
}

// Release finalizes an active drag at the given pixel position. active is
// the source-space translation of (x, y), or nil if outside content.
// Returns true if the drag was active; the caller should then trigger a
// copy.
func (d *dragSelection) Release(x, y int, active *Position) bool {
	if !d.active {
		return false
	}
	d.active = false
	d.scrollDir = 0
	d.sel.Active = active
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
// drag-selected region. Constrains highlighting to the main pane content
// area only.
func (d *dragSelection) ApplyHighlight(content string, g dragGeometry) string {
	startY, endY := d.startY, d.endY
	startX, endX := d.startX, d.endX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	topBorder := 1
	titleRow := 1
	contentStartY := g.statusRows + topBorder + titleRow
	gutterOffset := g.sidebarW + 1 + g.pane.gutterWidth
	if startX < gutterOffset {
		startX = gutterOffset
	}
	if endX >= g.screenW {
		endX = g.screenW - 1
	}
	contentEndY := g.screenH - 2
	if startY < contentStartY {
		startY = contentStartY
		startX = gutterOffset
	}
	if endY > contentEndY {
		endY = contentEndY
	}

	lines := strings.Split(content, "\n")
	rightBorderCol := g.screenW - 1

	for y := startY; y <= endY && y < len(lines); y++ {
		fromCol := gutterOffset
		stripped := stripANSIForWidth(lines[y])
		mainContent := sliceByDisplayCol(stripped, gutterOffset, rightBorderCol)
		trimmed := strings.TrimRight(mainContent, " ")
		contentEndCol := gutterOffset + displayWidthOf(trimmed)
		toCol := contentEndCol
		if y == startY {
			fromCol = startX
		}
		if y == endY {
			toCol = min(endX+1, contentEndCol)
		}
		if fromCol >= toCol {
			continue
		}

		before, middle, after := splitAtDisplayCols(lines[y], fromCol, toCol)
		selected := stripANSIForWidth(middle)
		if selected == "" {
			continue
		}
		lines[y] = before + "\x1b[7m" + selected + "\x1b[27m" + after
	}
	return strings.Join(lines, "\n")
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

// SelectedText extracts the plain text from the current drag selection,
// stripping ANSI codes, gutter prefixes, and joining word-wrap
// continuations. Returns empty string if the drag start and end are the
// same point.
//
// The selection works against the viewport's full GetContent() (not just
// the visible View()). That matters when auto-scroll has moved the
// viewport during the drag: the original click line may now be off-screen
// above, but the user still expects it included in the copy.
//
// The body is decomposed into helpers that each own one concern:
// normalize → clamp-vertically → translate pixel-to-content-row →
// per-line extraction. Behavior is unchanged from the pre-split version;
// see REFACTOR_IDEAS.md for the deferred source-space rewrite that will
// replace the whole pipeline with Position-driven iteration.
func (d *dragSelection) SelectedText(g dragGeometry) string {
	if d.startX == d.endX && d.startY == d.endY {
		return ""
	}

	startY, endY, startX, endX, swapped := d.normalizedPixelBounds()
	startY, endY, startX, endX, ok := clampSelectionVertically(g, startY, endY, startX, endX)
	if !ok {
		return ""
	}

	contentStartY := mainPaneContentTop(g)
	contentStartX := mainPaneContentLeft(g)
	anchorAbove := d.originalAnchorAboveContent(swapped, contentStartY)
	absStartY, absEndY, absStartX, absEndX := pixelsToContentRows(
		g, startY, endY, startX, endX, contentStartY, contentStartX, anchorAbove,
	)

	return extractSelectionText(g, absStartY, absEndY, absStartX, absEndX)
}

// normalizedPixelBounds returns the drag's start/end pixel coordinates
// in canonical (start ≤ end) order. swapped reports whether the
// canonical start came from d.endX/Y rather than d.startX/Y — downstream
// code needs that to know which original end corresponded to the click.
func (d *dragSelection) normalizedPixelBounds() (startY, endY, startX, endX int, swapped bool) {
	startY, endY = d.startY, d.endY
	startX, endX = d.startX, d.endX
	swapped = startY > endY || (startY == endY && startX > endX)
	if swapped {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}
	return startY, endY, startX, endX, swapped
}

// clampSelectionVertically clamps the canonical (start ≤ end) pixel
// bounds to the main pane's content area. Returns ok=false when the
// whole selection is below content (nothing selectable). When the end
// is past content, it is pulled to the last content row and the right
// edge — matching the visual highlight, which extends to the far right
// when the user drags off the bottom.
func clampSelectionVertically(g dragGeometry, startY, endY, startX, endX int) (int, int, int, int, bool) {
	contentEndY := g.screenH - 2
	if endY > contentEndY {
		endY = contentEndY
		endX = g.screenW
	}
	if startY > contentEndY {
		return startY, endY, startX, endX, false
	}
	return startY, endY, startX, endX, true
}

// originalAnchorAboveContent reports whether the *original* click landed
// above the content area at Begin time — distinct from "the upper end of
// the swapped-canonical selection is above content," which is what a
// naive Y check would tell us.
//
// When swapped, the upper end of the canonical selection corresponds to
// d.endY (the live mouse position, never mutated by auto-scroll), so a
// direct pixel-Y check answers the question. When not swapped, the
// upper end corresponds to the original click — sel.Anchor records that
// click's source-space translation (nil iff outside content), and
// falling back to d.startY handles tests that bypass Begin.
func (d *dragSelection) originalAnchorAboveContent(swapped bool, contentStartY int) bool {
	if swapped {
		return d.endY < contentStartY
	}
	if d.sel.Anchor != nil {
		return false
	}
	return d.startY < contentStartY
}

// pixelsToContentRows translates clamped pixel coords into absolute
// (viewport-content-relative) row and column offsets, the units that
// viewport.GetContent() lines and per-line column slicing operate on.
// When anchorAbove, the top of the selection is clamped to the first
// visible row instead of translated to an absolute row the user never
// saw.
func pixelsToContentRows(
	g dragGeometry,
	startY, endY, startX, endX int,
	contentStartY, contentStartX int,
	anchorAbove bool,
) (absStartY, absEndY, absStartX, absEndX int) {
	vpOffset := g.pane.viewport.YOffset()
	absStartY = vpOffset + (startY - contentStartY)
	absEndY = vpOffset + (endY - contentStartY)
	absStartX = startX - contentStartX
	absEndX = endX - contentStartX

	if anchorAbove {
		absStartY = vpOffset
		absStartX = 0
	}
	if absStartY < 0 {
		absStartY = 0
		absStartX = 0
	}
	if absStartX < 0 {
		absStartX = 0
	}
	if absEndX < 0 {
		absEndX = 0
	}
	return absStartY, absEndY, absStartX, absEndX
}

// extractSelectionText iterates content rows in the pane's full rendered
// output (not just the visible window) and accumulates the selected
// text, stripping ANSI, trimming the gutter, and joining word-wrap
// continuations into a single logical line in the output.
func extractSelectionText(g dragGeometry, absStartY, absEndY, absStartX, absEndX int) string {
	contentLines := strings.Split(g.pane.viewport.GetContent(), "\n")
	gw := g.pane.gutterWidth
	contMap := g.pane.wrapContinuation

	var out strings.Builder
	for y := absStartY; y <= absEndY && y < len(contentLines); y++ {
		isCont := contMap != nil && y < len(contMap) && contMap[y]
		fromCol, toCol := selectionColumnsForRow(y, absStartY, absEndY, absStartX, absEndX, gw)
		out.WriteString(extractLineFragment(contentLines[y], fromCol, toCol, gw, isCont))
		if y < absEndY {
			nextAbsY := y + 1
			if contMap != nil && nextAbsY < len(contMap) && contMap[nextAbsY] {
				continue
			}
			out.WriteString("\n")
		}
	}
	return out.String()
}

// selectionColumnsForRow returns the [fromCol, toCol) post-gutter column
// range to extract from the row at y. Inner rows get the full line;
// the first and last rows clip on absStartX/absEndX. Columns are
// gutter-relative (0 == first character after the gutter).
func selectionColumnsForRow(y, absStartY, absEndY, absStartX, absEndX, gw int) (int, int) {
	const noUpperBound = -1 // sentinel; extractLineFragment clamps to line width
	fromCol := 0
	toCol := noUpperBound
	if y == absStartY {
		fromCol = max(0, absStartX-gw)
	}
	if y == absEndY {
		toCol = max(0, absEndX+1-gw)
	}
	return fromCol, toCol
}

// extractLineFragment pulls the [fromCol, toCol) gutter-relative slice
// out of a single rendered line, after stripping ANSI codes, trimming
// trailing whitespace, and removing the gutter prefix. A toCol of -1
// means "to end of line." Returns empty string when the range is empty
// or past the line's content.
func extractLineFragment(line string, fromCol, toCol, gw int, isContinuation bool) string {
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
	_ = isContinuation // reserved for future wrap-aware semantics; currently identical handling
	return sliceByDisplayCol(stripped, fromCol, toCol)
}

// mainPaneContentTop returns the screen-Y of the first row inside the
// main pane's content area (below status bar, top border, title row).
func mainPaneContentTop(g dragGeometry) int {
	const topBorder, titleRow = 1, 1
	return g.statusRows + topBorder + titleRow
}

// mainPaneContentLeft returns the screen-X of the first column inside
// the main pane's content area (past the sidebar and left border).
func mainPaneContentLeft(g dragGeometry) int {
	const mainLeftBorder = 1
	return g.sidebarW + mainLeftBorder
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
