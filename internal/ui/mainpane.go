package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// diffLineKind describes how a source line relates to a diff.
type diffLineKind int

const (
	diffLineUnchanged diffLineKind = iota
	diffLineAdded
	diffLineRemoved // a removed line (not present in the file, shown inline when Shift+D is on)
)

// diffAnnotation maps a file line number (1-indexed) to its change kind.
type diffAnnotation struct {
	kind         diffLineKind
	removedLines []string // removed lines before this line (for Shift+D display)
}

type mainPane struct {
	viewport        viewport.Model
	content         string
	isDiff          bool // whether content was set via SetContent (diff coloring)
	searchQuery     string
	width           int
	height          int
	wordWrap        bool                   // whether to wrap long lines
	lineNumbers     bool                   // whether to show line numbers (plain content only)
	diffAnnotations map[int]diffAnnotation // line number -> annotation (for file-view gutter)
	showRemoved     bool                   // Shift+D: show removed lines inline
}

func newMainPane() *mainPane {
	vp := viewport.New()
	return &mainPane{viewport: vp, wordWrap: true, lineNumbers: true, showRemoved: true}
}

// SetDiffAnnotations sets diff annotations for file-view mode gutter rendering.
func (m *mainPane) SetDiffAnnotations(annotations map[int]diffAnnotation) {
	m.diffAnnotations = annotations
	m.refreshViewport()
}

// ClearDiffAnnotations removes diff annotations.
func (m *mainPane) ClearDiffAnnotations() {
	m.diffAnnotations = nil
	m.refreshViewport()
}

// ToggleShowRemoved toggles display of removed lines.
func (m *mainPane) ToggleShowRemoved() {
	m.showRemoved = !m.showRemoved
	m.refreshViewport()
}

// DiffLineNumbers returns the sorted list of file line numbers that have diff annotations.
func (m *mainPane) DiffLineNumbers() []int {
	if len(m.diffAnnotations) == 0 {
		return nil
	}
	var lines []int
	for lineNo, ann := range m.diffAnnotations {
		if ann.kind == diffLineAdded {
			lines = append(lines, lineNo)
		}
	}
	// Sort
	for i := 0; i < len(lines); i++ {
		for j := i + 1; j < len(lines); j++ {
			if lines[j] < lines[i] {
				lines[i], lines[j] = lines[j], lines[i]
			}
		}
	}
	return lines
}

// parseDiffAnnotations parses a unified diff and returns annotations keyed by
// new-file line number. Removed lines are attached to the next added/context line.
func parseDiffAnnotations(unifiedDiff string) map[int]diffAnnotation {
	annotations := make(map[int]diffAnnotation)
	if unifiedDiff == "" {
		return annotations
	}

	lines := strings.Split(unifiedDiff, "\n")
	var pendingRemoved []string

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			// Parse hunk header: @@ -old,count +new,count @@
			newStart := parseHunkNewStart(line)
			if newStart > 0 {
				// Flush any pending removed lines to the start of this hunk
				if len(pendingRemoved) > 0 {
					ann := annotations[newStart]
					ann.removedLines = append(ann.removedLines, pendingRemoved...)
					annotations[newStart] = ann
					pendingRemoved = nil
				}
			}
			continue
		}
		if strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		// We need to track the current new-file line number
		// This simplified parser re-scans from hunk headers
	}

	// Better approach: iterate through hunks tracking line numbers
	annotations = make(map[int]diffAnnotation)
	newLineNo := 0
	pendingRemoved = nil

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			newLineNo = parseHunkNewStart(line)
			if newLineNo < 1 {
				newLineNo = 1
			}
			// Attach pending removed to the first line of the new hunk
			if len(pendingRemoved) > 0 {
				ann := annotations[newLineNo]
				ann.removedLines = append(ann.removedLines, pendingRemoved...)
				annotations[newLineNo] = ann
				pendingRemoved = nil
			}
			continue
		}
		if newLineNo == 0 {
			continue // before first hunk
		}
		if strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") ||
			strings.HasPrefix(line, "\\") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			ann := annotations[newLineNo]
			ann.kind = diffLineAdded
			if len(pendingRemoved) > 0 {
				ann.removedLines = append(ann.removedLines, pendingRemoved...)
				pendingRemoved = nil
			}
			annotations[newLineNo] = ann
			newLineNo++
		} else if strings.HasPrefix(line, "-") {
			pendingRemoved = append(pendingRemoved, line[1:]) // strip the "-"
		} else {
			// Context line
			if len(pendingRemoved) > 0 {
				ann := annotations[newLineNo]
				ann.removedLines = append(ann.removedLines, pendingRemoved...)
				annotations[newLineNo] = ann
				pendingRemoved = nil
			}
			newLineNo++
		}
	}

	return annotations
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
	} else {
		content = m.applyFileViewFormatting(content)
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

// applyFileViewFormatting adds line numbers and diff gutter to plain content.
func (m *mainPane) applyFileViewFormatting(content string) string {
	lines := strings.Split(content, "\n")
	numWidth := len(fmt.Sprintf("%d", len(lines)))

	var result []string
	for i, line := range lines {
		lineNo := i + 1
		var prefix string

		if m.lineNumbers {
			prefix = fmt.Sprintf("%*d", numWidth, lineNo)
		}

		ann, hasAnn := m.diffAnnotations[lineNo]
		if hasAnn && m.showRemoved && len(ann.removedLines) > 0 {
			// Insert removed lines before this line
			for _, removed := range ann.removedLines {
				gutterMark := " - "
				if m.lineNumbers {
					gutterMark = strings.Repeat(" ", numWidth) + " - "
				}
				result = append(result, diffRemoveStyle.Render(gutterMark+removed))
			}
		}

		if hasAnn && ann.kind == diffLineAdded {
			gutter := " + "
			if m.lineNumbers {
				formatted := prefix + gutter + line
				result = append(result, diffAddStyle.Render(formatted))
			} else {
				result = append(result, diffAddStyle.Render(gutter+line))
			}
		} else {
			gutter := "   "
			if m.lineNumbers {
				result = append(result, prefix+gutter+line)
			} else {
				result = append(result, gutter+line)
			}
		}
	}
	return strings.Join(result, "\n")
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
