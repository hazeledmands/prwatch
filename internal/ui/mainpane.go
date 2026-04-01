package ui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type mainPane struct {
	viewport viewport.Model
	content  string
	width    int
	height   int
}

func newMainPane() *mainPane {
	vp := viewport.New()
	return &mainPane{viewport: vp}
}

func (m *mainPane) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(h)
}

func (m *mainPane) SetContent(content string) {
	m.content = content
	m.viewport.SetContent(colorDiff(content))
}

func (m *mainPane) SetPlainContent(content string) {
	m.content = content
	m.viewport.SetContent(content)
}

func (m *mainPane) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return cmd
}

func (m *mainPane) View(focused bool) string {
	style := mainPaneStyle
	if focused {
		style = mainPaneFocusedStyle
	}
	return style.Width(m.width).Height(m.height).Render(m.viewport.View())
}

// ScrollTop returns the line number at the top of the viewport.
func (m *mainPane) ScrollTop() int {
	return m.viewport.YOffset()
}

// GoToTop scrolls the viewport to the very top.
func (m *mainPane) GoToTop() {
	m.viewport.GotoTop()
}

// GoToBottom scrolls the viewport to the very bottom.
func (m *mainPane) GoToBottom() {
	m.viewport.GotoBottom()
}

// SearchAndHighlight scrolls to the first occurrence of query in the visible content.
func (m *mainPane) SearchAndHighlight(query string) {
	lines := strings.Split(m.content, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
			m.viewport.SetYOffset(i)
			return
		}
	}
}

// colorDiff applies syntax coloring to unified diff output.
func colorDiff(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			lines[i] = diffHeaderStyle.Render(line)
		case strings.HasPrefix(line, "diff "):
			lines[i] = diffHeaderStyle.Render(line)
		case strings.HasPrefix(line, "@@"):
			lines[i] = diffHunkStyle.Render(line)
		case strings.HasPrefix(line, "+"):
			lines[i] = diffAddStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			lines[i] = diffRemoveStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
