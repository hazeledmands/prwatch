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
// a region of the main pane. The fields are pixel coordinates in the same
// frame as the mouse events. scrollDir is +1 to auto-scroll the viewport
// down (drag past the bottom edge), -1 to scroll up, 0 when the drag end is
// inside the viewport.
//
// originStartY records the screen Y at the moment Begin was called. Unlike
// startY (which AdvanceAutoScroll decrements to re-anchor the click to its
// absolute content row when the viewport scrolls), originStartY never
// changes during the drag. SelectedText uses it to tell a click that
// originated outside the content area (status bar / borders / title) from
// a click that originated in content and was later scrolled above the
// viewport — only the latter should pull its original row into the copy.
type dragSelection struct {
	startX, startY int
	endX, endY     int
	originStartY   int
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

func newDragSelection() *dragSelection {
	return &dragSelection{startX: -1, startY: -1, originStartY: -1}
}

// Begin starts a drag at the given pixel position. The end is initialized
// to the same point so HasRange reports false until the mouse moves.
func (d *dragSelection) Begin(x, y int) {
	d.active = true
	d.scrollDir = 0
	d.startX, d.startY = x, y
	d.originStartY = y
	d.endX, d.endY = x, y
}

// MoveEnd updates the drag end position. Caller should additionally call
// UpdateAutoScroll to manage scroll-direction state.
func (d *dragSelection) MoveEnd(x, y int) {
	d.endX, d.endY = x, y
}

// Release finalizes an active drag at the given pixel position. Returns
// true if the drag was active; the caller should then trigger a copy.
func (d *dragSelection) Release(x, y int) bool {
	if !d.active {
		return false
	}
	d.active = false
	d.scrollDir = 0
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
func (d *dragSelection) SelectedText(g dragGeometry) string {
	if d.startX == d.endX && d.startY == d.endY {
		return ""
	}

	topBorder := 1
	titleRow := 1
	mainLeftBorder := 1
	contentStartY := g.statusRows + topBorder + titleRow
	contentStartX := g.sidebarW + mainLeftBorder

	viewportContent := g.pane.viewport.GetContent()
	contentLines := strings.Split(viewportContent, "\n")

	startY, endY := d.startY, d.endY
	startX, endX := d.startX, d.endX
	swapped := startY > endY || (startY == endY && startX > endX)
	if swapped {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	contentEndY := g.screenH - 2
	if endY > contentEndY {
		endY = contentEndY
		endX = g.screenW
	}
	if startY > contentEndY {
		return ""
	}

	// Detect a drag whose "earlier" end (the swapped-min Y) originated
	// outside the content area. originStartY records the un-adjusted screen
	// Y at Begin time; endY is mutated only by MoveEnd, which doesn't apply
	// the auto-scroll correction that distorts startY. We can therefore
	// check each side directly and only clamp the side that was actually
	// above content at the time of the drag.
	anchorAboveContent := false
	if swapped {
		// After swap, local startY corresponds to d.endY (the drag end).
		anchorAboveContent = d.endY < contentStartY
	} else {
		// No swap — local startY corresponds to the original click, whose
		// screen-coord origin is d.originStartY (or d.startY when Begin
		// hasn't been called; tests that bypass Begin leave originStartY at
		// the -1 sentinel, in which case d.startY itself is the truth).
		originY := d.originStartY
		if originY < 0 {
			originY = d.startY
		}
		anchorAboveContent = originY < contentStartY
	}

	vpOffset := g.pane.viewport.YOffset()
	startY = vpOffset + (startY - contentStartY)
	endY = vpOffset + (endY - contentStartY)
	startX -= contentStartX
	endX -= contentStartX

	if anchorAboveContent {
		// The user never saw any content above vpOffset; clamp to the first
		// visible row rather than translating to a (possibly off-screen)
		// absolute row. Matches ApplyHighlight's clamp to contentStartY.
		startY = vpOffset
		startX = 0
	}
	if startY < 0 {
		startY = 0
		startX = 0
	}
	if startX < 0 {
		startX = 0
	}
	if endX < 0 {
		endX = 0
	}

	gw := g.pane.gutterWidth
	contMap := g.pane.wrapContinuation
	var selected strings.Builder
	for y := startY; y <= endY && y < len(contentLines); y++ {
		line := stripANSIForWidth(contentLines[y])
		line = strings.TrimRight(line, " ")

		isCont := contMap != nil && y < len(contMap) && contMap[y]
		if isCont {
			if gw > 0 && len(line) > gw {
				line = line[gw:]
			}
		} else if gw > 0 && len(line) > gw {
			line = line[gw:]
		}

		lineWidth := displayWidthOf(line)
		fromCol := 0
		toCol := lineWidth
		if y == startY {
			fromCol = max(0, startX-gw)
		}
		if y == endY {
			toCol = max(0, endX+1-gw)
		}
		if fromCol > lineWidth {
			fromCol = lineWidth
		}
		if toCol > lineWidth {
			toCol = lineWidth
		}
		if fromCol < toCol {
			selected.WriteString(sliceByDisplayCol(line, fromCol, toCol))
		}
		if y < endY {
			nextAbsY := y + 1
			if contMap != nil && nextAbsY < len(contMap) && contMap[nextAbsY] {
				continue
			}
			selected.WriteString("\n")
		}
	}

	return selected.String()
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
		vr := m.visibleRange()
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
