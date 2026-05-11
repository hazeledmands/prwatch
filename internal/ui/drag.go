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
// m.dragging is true and m.dragScrollDir != 0.
type dragScrollTickMsg struct{}

const dragScrollInterval = 60 * time.Millisecond

// applyDragHighlight applies reverse-video highlighting to the drag-selected
// region. Constrains highlighting to the main pane content area only.
func (m *Model) applyDragHighlight(content string) string {
	startY, endY := m.dragStartY, m.dragEndY
	startX, endX := m.dragStartX, m.dragEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	statusRows := m.statusBarLines()
	topBorder := 1
	titleRow := 1
	contentStartY := statusRows + topBorder + titleRow
	gutterOffset := sidebarW + 1 + m.mainPane.gutterWidth
	if startX < gutterOffset {
		startX = gutterOffset
	}
	if endX >= m.width {
		endX = m.width - 1
	}
	contentEndY := m.height - 2
	if startY < contentStartY {
		startY = contentStartY
		startX = gutterOffset
	}
	if endY > contentEndY {
		endY = contentEndY
	}

	lines := strings.Split(content, "\n")
	rightBorderCol := m.width - 1

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

func (m *Model) dragMainPaneBounds() (top, bottom int) {
	return mainPaneContentRows(m.statusBarLines(), m.height)
}

// updateDragAutoScroll inspects the current drag-end Y in screen coords and
// sets m.dragScrollDir accordingly: -1 if the user has dragged above the
// viewport top, +1 if below the bottom, 0 if back inside. Returns a tick
// command when scrolling needs to (re)start, nil otherwise.
func (m *Model) updateDragAutoScroll(y int) tea.Cmd {
	top, bottom := m.dragMainPaneBounds()
	prev := m.dragScrollDir
	switch {
	case y < top:
		m.dragScrollDir = -1
	case y > bottom:
		m.dragScrollDir = +1
	default:
		m.dragScrollDir = 0
	}
	if m.dragScrollDir != 0 && prev == 0 {
		return scheduleDragScrollTick()
	}
	return nil
}

// advanceDragAutoScroll scrolls the main pane viewport one line in the
// current drag direction and re-anchors the drag start so the original
// click stays attached to the same content row. Returns the next tick
// command unless the viewport has hit the corresponding edge.
func (m *Model) advanceDragAutoScroll() tea.Cmd {
	if m.dragScrollDir == 0 {
		return nil
	}
	beforeOffset := m.mainPane.viewport.YOffset()
	if m.dragScrollDir > 0 {
		m.mainPane.viewport.ScrollDown(1)
	} else {
		m.mainPane.viewport.ScrollUp(1)
	}
	delta := m.mainPane.viewport.YOffset() - beforeOffset
	if delta == 0 {
		m.dragScrollDir = 0
		return nil
	}
	m.dragStartY -= delta
	return scheduleDragScrollTick()
}

func scheduleDragScrollTick() tea.Cmd {
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg {
		return dragScrollTickMsg{}
	})
}

// selectedText extracts the plain text from the current drag selection,
// stripping ANSI codes, gutter prefixes, and joining word-wrap continuations.
// Returns empty string if the drag start and end are the same point.
//
// The selection works against the viewport's full GetContent() (not just
// the visible View()). That matters when auto-scroll has moved the
// viewport during the drag: the original click line may now be off-screen
// above, but the user still expects it included in the copy.
func (m *Model) selectedText() string {
	if m.dragStartX == m.dragEndX && m.dragStartY == m.dragEndY {
		return ""
	}

	statusRows := m.statusBarLines()
	topBorder := 1
	titleRow := 1
	sidebarW := 0
	if !m.sidebarHidden {
		sidebarW = m.sidebarPixelWidth()
	}
	mainLeftBorder := 1
	contentStartY := statusRows + topBorder + titleRow
	contentStartX := sidebarW + mainLeftBorder

	viewportContent := m.mainPane.viewport.GetContent()
	contentLines := strings.Split(viewportContent, "\n")

	startY, endY := m.dragStartY, m.dragEndY
	startX, endX := m.dragStartX, m.dragEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}

	contentEndY := m.height - 2
	if endY > contentEndY {
		endY = contentEndY
		endX = m.width
	}
	if startY > contentEndY {
		return ""
	}

	vpOffset := m.mainPane.viewport.YOffset()
	startY = vpOffset + (startY - contentStartY)
	endY = vpOffset + (endY - contentStartY)
	startX -= contentStartX
	endX -= contentStartX

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

	gw := m.mainPane.gutterWidth
	contMap := m.mainPane.wrapContinuation
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
		topLine := m.mainPane.ViewportToSourceLine()
		bottomLine := m.mainPane.ViewportBottomSourceLine()
		if topLine == bottomLine {
			text = fmt.Sprintf("%s:%d", file, topLine)
		} else {
			text = fmt.Sprintf("%s:%d-%d", file, topLine, bottomLine)
		}
	}
	m.copyToClipboard(text)
	m.notification = "copied " + text
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return notificationExpiredMsg{}
	})
}

func (m *Model) copySelection() tea.Cmd {
	text := m.selectedText()
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
