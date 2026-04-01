package ui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type mainPane struct {
	viewport    viewport.Model
	content     string
	isDiff      bool // whether content was set via SetContent (diff coloring)
	searchQuery string
	width       int
	height      int
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
	m.isDiff = true
	m.refreshViewport()
}

func (m *mainPane) SetPlainContent(content string) {
	m.content = content
	m.isDiff = false
	m.refreshViewport()
}

// SetSearchQuery updates the search highlighting in the viewport.
func (m *mainPane) SetSearchQuery(query string) {
	m.searchQuery = query
	m.refreshViewport()
}

func (m *mainPane) refreshViewport() {
	content := m.content
	if m.isDiff {
		content = colorDiff(content)
	}
	if m.searchQuery != "" {
		content = highlightSearch(content, m.searchQuery)
	}
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
	// lipgloss v2: Width/Height set the outer dimensions (includes borders).
	// Add 2 for border characters on each axis.
	return style.Width(m.width + 2).Height(m.height + 2).Render(m.viewport.View())
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

// FindMatches returns line indices where query appears (case-insensitive).
// Searches all content, not just the visible viewport.
func (m *mainPane) FindMatches(query string) []int {
	if query == "" {
		return nil
	}
	lines := strings.Split(m.content, "\n")
	q := strings.ToLower(query)
	var matches []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), q) {
			matches = append(matches, i)
		}
	}
	return matches
}

// ScrollToLine scrolls the viewport to show the given line.
func (m *mainPane) ScrollToLine(line int) {
	m.viewport.SetYOffset(line)
}

// highlightSearch applies a contrasting background to matching text in each line.
func highlightSearch(content, query string) string {
	if query == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	q := strings.ToLower(query)
	for i, line := range lines {
		stripped := stripANSIForWidth(line)
		if strings.Contains(strings.ToLower(stripped), q) {
			lines[i] = highlightMatchInLine(line, query)
		}
	}
	return strings.Join(lines, "\n")
}

// highlightMatchInLine wraps matching substrings with the search highlight style.
// Works on text that may already contain ANSI escape codes.
func highlightMatchInLine(line, query string) string {
	q := strings.ToLower(query)
	lower := strings.ToLower(line)
	var result strings.Builder
	pos := 0
	for {
		idx := strings.Index(strings.ToLower(line[pos:]), q)
		if idx < 0 {
			result.WriteString(line[pos:])
			break
		}
		result.WriteString(line[pos : pos+idx])
		matchEnd := pos + idx + len(query)
		// Find the actual matched text (preserving original case)
		matchText := line[pos+idx : matchEnd]
		result.WriteString(searchHighlightStyle.Render(matchText))
		pos = matchEnd
	}
	_ = lower // used in ToLower above
	return result.String()
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
