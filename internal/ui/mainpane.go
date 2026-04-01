package ui

import (
	"fmt"
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
	wordWrap    bool // whether to wrap long lines
	lineNumbers bool // whether to show line numbers (plain content only)
}

func newMainPane() *mainPane {
	vp := viewport.New()
	return &mainPane{viewport: vp, wordWrap: true, lineNumbers: true}
}

// SetWordWrap enables or disables word wrapping.
func (m *mainPane) SetWordWrap(on bool) {
	m.wordWrap = on
	m.refreshViewport()
}

// SetLineNumbers enables or disables line numbers for plain content.
func (m *mainPane) SetLineNumbers(on bool) {
	m.lineNumbers = on
	m.refreshViewport()
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
	} else if m.lineNumbers {
		content = addLineNumbers(content)
	}
	if m.searchQuery != "" {
		content = highlightSearch(content, m.searchQuery)
	}
	if m.width > 0 {
		if m.wordWrap {
			content = wrapLines(content, m.width)
		} else {
			content = truncateLines(content, m.width)
		}
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

// addLineNumbers prepends line numbers to each line of content.
func addLineNumbers(content string) string {
	lines := strings.Split(content, "\n")
	width := len(fmt.Sprintf("%d", len(lines)))
	for i, line := range lines {
		lines[i] = fmt.Sprintf("%*d  %s", width, i+1, line)
	}
	return strings.Join(lines, "\n")
}

// ansiAwareIterate calls fn for each rune in line, passing the rune and its
// display width (0 for characters inside ANSI escape sequences, 1 for normal
// printable characters, and the tab width for '\t').
// It returns the total display width.
func ansiAwareIterate(line string, fn func(r rune, displayW int)) int {
	totalW := 0
	inEscape := false
	for _, r := range line {
		if inEscape {
			fn(r, 0)
			// SGR sequences end with a letter; OSC 8 sequences end with ST (\x1b\\)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			fn(r, 0)
			inEscape = true
			continue
		}
		w := 1
		if r == '\t' {
			w = 8 - (totalW % 8) // tab stop every 8 columns
		}
		fn(r, w)
		totalW += w
	}
	return totalW
}

// wrapLines wraps each line at the given width, respecting ANSI escape codes.
func wrapLines(content string, width int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	var result []string
	for _, line := range lines {
		// Check if line fits
		lineW := ansiAwareIterate(line, func(r rune, w int) {})
		if lineW <= width {
			result = append(result, line)
			continue
		}
		// Wrap at width boundaries
		var current strings.Builder
		w := 0
		ansiAwareIterate(line, func(r rune, dw int) {
			if dw > 0 && w+dw > width {
				result = append(result, current.String())
				current.Reset()
				w = 0
			}
			current.WriteRune(r)
			w += dw
		})
		if current.Len() > 0 {
			result = append(result, current.String())
		}
	}
	return strings.Join(result, "\n")
}

// truncateLines cuts each line at the given width, respecting ANSI codes.
// Lines shorter than width are left as-is.
func truncateLines(content string, width int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineW := ansiAwareIterate(line, func(r rune, w int) {})
		if lineW <= width {
			continue
		}
		var b strings.Builder
		w := 0
		ansiAwareIterate(line, func(r rune, dw int) {
			if dw > 0 && w+dw > width {
				return
			}
			b.WriteRune(r)
			w += dw
		})
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
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
