package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	runewidth "github.com/mattn/go-runewidth"
)

// diffLineKind describes how a source line relates to a diff.
type diffLineKind int

const (
	diffLineUnchanged diffLineKind = iota
	diffLineAdded
	diffLineRemoved // a removed line (not present in the file, shown inline when Shift+D is on)
	diffLineChanged // a modified line (consecutive -/+ in diff)
)

// diffAnnotation maps a file line number (1-indexed) to its change kind.
type diffAnnotation struct {
	kind         diffLineKind
	removedLines []string // removed lines before this line (for Shift+D display)
}

type mainPane struct {
	viewport           viewport.Model
	content            string
	isDiff             bool // whether content was set via SetContent (diff coloring)
	searchQuery        string
	width              int
	height             int
	wordWrap           bool                   // whether to wrap long lines
	lineNumbers        bool                   // whether to show line numbers (plain content only)
	diffAnnotations    map[int]diffAnnotation // line number -> annotation (for files mode gutter)
	showRemoved        bool                   // Shift+D: show removed lines inline
	xOffset            int                    // horizontal scroll offset (when word wrap is off)
	formattedContent   string                 // content after formatting but before wrapping
	gutterWidth        int                    // gutter width from last formatting
	sourceToFormatLine map[int]int            // source line number (1-indexed) → formatted line index (0-indexed)
	wrapContinuation   []bool                 // per viewport line: true if this line is a word-wrap continuation
}

func newMainPane() *mainPane {
	vp := viewport.New()
	return &mainPane{viewport: vp, wordWrap: true, lineNumbers: true, showRemoved: true}
}

// SetDiffAnnotations sets diff annotations for files mode gutter rendering.
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
		if ann.kind == diffLineAdded || ann.kind == diffLineChanged {
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
			if len(pendingRemoved) > 0 {
				// Removed lines followed by added = changed line
				ann.kind = diffLineChanged
				ann.removedLines = append(ann.removedLines, pendingRemoved...)
				pendingRemoved = nil
			} else {
				ann.kind = diffLineAdded
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
	gutterWidth := 0
	if m.isDiff {
		content = colorDiff(content)
	} else {
		content, gutterWidth = m.applyFileViewFormatting(content)
	}

	// Store the pre-wrap formatted content for line mapping
	m.formattedContent = content
	m.gutterWidth = gutterWidth

	if m.searchQuery != "" {
		content = highlightSearch(content, m.searchQuery)
	}
	m.wrapContinuation = nil
	if m.width > 0 {
		if m.wordWrap {
			var contMap []bool
			content, contMap = wrapLinesWithContinuationMap(content, m.width, gutterWidth)
			m.wrapContinuation = contMap
		} else {
			content = truncateLinesWithOffset(content, m.width, m.xOffset, gutterWidth)
		}
	}
	m.viewport.SetContent(content)
}

// inlineDiffSize returns the total display width of the changed characters
// between old and new text (characters that differ).
func inlineDiffSize(oldText, newText string) int {
	oldRunes := []rune(oldText)
	newRunes := []rune(newText)

	// Find common prefix length
	prefixLen := 0
	for prefixLen < len(oldRunes) && prefixLen < len(newRunes) && oldRunes[prefixLen] == newRunes[prefixLen] {
		prefixLen++
	}

	// Find common suffix length
	suffixLen := 0
	for suffixLen < len(oldRunes)-prefixLen && suffixLen < len(newRunes)-prefixLen &&
		oldRunes[len(oldRunes)-1-suffixLen] == newRunes[len(newRunes)-1-suffixLen] {
		suffixLen++
	}

	// The diff size is the changed portion in both old and new
	oldDiffLen := len(oldRunes) - prefixLen - suffixLen
	newDiffLen := len(newRunes) - prefixLen - suffixLen
	if oldDiffLen < 0 {
		oldDiffLen = 0
	}
	if newDiffLen < 0 {
		newDiffLen = 0
	}
	return oldDiffLen + newDiffLen
}

// renderInlineDiff renders a changed line with inline diff coloring.
// Retained text is yellow, deleted text is red, new text is green.
func renderInlineDiff(oldText, newText string) string {
	oldRunes := []rune(oldText)
	newRunes := []rune(newText)

	// Find common prefix
	prefixLen := 0
	for prefixLen < len(oldRunes) && prefixLen < len(newRunes) && oldRunes[prefixLen] == newRunes[prefixLen] {
		prefixLen++
	}

	// Find common suffix
	suffixLen := 0
	for suffixLen < len(oldRunes)-prefixLen && suffixLen < len(newRunes)-prefixLen &&
		oldRunes[len(oldRunes)-1-suffixLen] == newRunes[len(newRunes)-1-suffixLen] {
		suffixLen++
	}

	var b strings.Builder

	// Common prefix (yellow — retained)
	if prefixLen > 0 {
		b.WriteString(diffRetainedStyle.Render(string(newRunes[:prefixLen])))
	}

	// Deleted middle (red)
	oldMidEnd := len(oldRunes) - suffixLen
	if oldMidEnd < prefixLen {
		oldMidEnd = prefixLen
	}
	if oldMidEnd > prefixLen {
		b.WriteString(diffRemoveStyle.Render(string(oldRunes[prefixLen:oldMidEnd])))
	}

	// Added middle (green)
	newMidEnd := len(newRunes) - suffixLen
	if newMidEnd < prefixLen {
		newMidEnd = prefixLen
	}
	if newMidEnd > prefixLen {
		b.WriteString(diffAddStyle.Render(string(newRunes[prefixLen:newMidEnd])))
	}

	// Common suffix (yellow — retained)
	if suffixLen > 0 {
		b.WriteString(diffRetainedStyle.Render(string(newRunes[len(newRunes)-suffixLen:])))
	}

	return b.String()
}

// applyFileViewFormatting adds line numbers and diff gutter to plain content.
// Returns the formatted content and the gutter width (for wrapping indentation).
func (m *mainPane) applyFileViewFormatting(content string) (string, int) {
	lines := strings.Split(content, "\n")
	numWidth := len(fmt.Sprintf("%d", len(lines)))
	gutterWidth := 3 // " + " or "   "
	if m.lineNumbers {
		gutterWidth = numWidth + 3 // "  N + " or "  N   "
	}

	m.sourceToFormatLine = make(map[int]int)
	var result []string
	for i, line := range lines {
		lineNo := i + 1
		var prefix string
		// Track which formatted line index this source line starts at
		m.sourceToFormatLine[lineNo] = len(result)

		if m.lineNumbers {
			prefix = fmt.Sprintf("%*d", numWidth, lineNo)
		}

		ann, hasAnn := m.diffAnnotations[lineNo]
		if hasAnn && m.showRemoved && len(ann.removedLines) > 0 && ann.kind != diffLineChanged {
			// Insert removed lines before this line (for non-changed lines only;
			// changed lines handle their own removed content via inline/split rendering)
			for _, removed := range ann.removedLines {
				gutterMark := " - "
				if m.lineNumbers {
					gutterMark = strings.Repeat(" ", numWidth) + " - "
				}
				result = append(result, diffRemoveStyle.Render(gutterMark+removed))
			}
		}

		if hasAnn && ann.kind == diffLineChanged && len(ann.removedLines) > 0 {
			gutter := " ~ "

			// When multiple lines were removed and replaced by one new line,
			// show all removed lines except the last as pure deletions, then
			// use the last for the inline/split diff comparison.
			if len(ann.removedLines) > 1 {
				gutterMarkDel := " - "
				if m.lineNumbers {
					gutterMarkDel = strings.Repeat(" ", numWidth) + " - "
				}
				for _, extra := range ann.removedLines[:len(ann.removedLines)-1] {
					result = append(result, diffRemoveStyle.Render(gutterMarkDel+extra))
				}
			}

			oldLine := ann.removedLines[len(ann.removedLines)-1]
			contentWidth := m.width - gutterWidth
			if contentWidth <= 0 {
				contentWidth = m.width
			}
			diffSize := inlineDiffSize(oldLine, line)
			if diffSize <= contentWidth/4 {
				// Small diff: show inline with old (red) + new (green) on same line
				rendered := renderInlineDiff(oldLine, line)
				if m.lineNumbers {
					result = append(result, prefix+gutter+rendered)
				} else {
					result = append(result, gutter+rendered)
				}
			} else {
				// Large diff: show deleted version (red) on top, new version (green) on bottom
				gutterMarkDel := " ~ "
				if m.lineNumbers {
					gutterMarkDel = strings.Repeat(" ", numWidth) + " ~ "
				}
				result = append(result, diffRemoveStyle.Render(gutterMarkDel+oldLine))
				if m.lineNumbers {
					result = append(result, diffAddStyle.Render(prefix+gutter+line))
				} else {
					result = append(result, diffAddStyle.Render(gutter+line))
				}
			}
		} else if hasAnn && ann.kind == diffLineRemoved {
			// Completely deleted file — mark every line with "-"
			gutter := " - "
			if m.lineNumbers {
				result = append(result, diffRemoveStyle.Render(prefix+gutter+line))
			} else {
				result = append(result, diffRemoveStyle.Render(gutter+line))
			}
		} else if hasAnn && (ann.kind == diffLineAdded || ann.kind == diffLineChanged) {
			var gutter string
			var style lipgloss.Style
			if ann.kind == diffLineChanged {
				gutter = " ~ "
				style = diffChangeStyle
			} else {
				gutter = " + "
				style = diffAddStyle
			}
			if m.lineNumbers {
				result = append(result, style.Render(prefix+gutter+line))
			} else {
				result = append(result, style.Render(gutter+line))
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
	return strings.Join(result, "\n"), gutterWidth
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

// ScrollToSourceLine scrolls the viewport to show the given source file line number.
// Unlike ScrollToLine, this accounts for formatting (gutter, removed lines) and
// word wrapping that may change the viewport line count.
func (m *mainPane) ScrollToSourceLine(sourceLine int) {
	// Use the source-to-formatted-line mapping if available
	if m.sourceToFormatLine != nil {
		if formattedIdx, ok := m.sourceToFormatLine[sourceLine]; ok {
			if !m.wordWrap || m.width <= 0 {
				m.viewport.SetYOffset(formattedIdx)
				return
			}
			// Account for wrapping: count viewport lines for formatted lines 0..formattedIdx-1
			formattedLines := strings.Split(m.formattedContent, "\n")
			viewportLine := 0
			for i := 0; i < formattedIdx && i < len(formattedLines); i++ {
				lineW := ansiAwareIterate(formattedLines[i], func(r rune, w int) {})
				if lineW > m.width {
					viewportLine += (lineW + m.width - 1) / m.width
				} else {
					viewportLine++
				}
			}
			m.viewport.SetYOffset(viewportLine)
			return
		}
	}
	// Fallback: direct line mapping
	m.viewport.SetYOffset(sourceLine - 1)
}

// ViewportToSourceLine converts a viewport scroll offset to the closest source
// file line number. This reverses the formatting/wrapping transformation so that
// hunk navigation can compare viewport position against source line numbers.
func (m *mainPane) ViewportToSourceLine() int {
	vpOffset := m.viewport.YOffset()
	if m.sourceToFormatLine == nil || len(m.sourceToFormatLine) == 0 {
		return vpOffset + 1
	}
	// Build reverse map: formatted line -> source line
	reverseMap := make(map[int]int, len(m.sourceToFormatLine))
	for src, fmt := range m.sourceToFormatLine {
		reverseMap[fmt] = src
	}

	if !m.wordWrap || m.width <= 0 {
		// Without wrapping, viewport line = formatted line
		if src, ok := reverseMap[vpOffset]; ok {
			return src
		}
		// Find the closest formatted line <= vpOffset
		best := 1
		for formattedIdx, srcLine := range reverseMap {
			if formattedIdx <= vpOffset && srcLine > best {
				best = srcLine
			}
		}
		return best
	}

	// With wrapping: walk through formatted lines, counting viewport lines
	formattedLines := strings.Split(m.formattedContent, "\n")
	viewportLine := 0
	for i, line := range formattedLines {
		lineW := ansiAwareIterate(line, func(r rune, w int) {})
		var linesUsed int
		if lineW > m.width {
			linesUsed = (lineW + m.width - 1) / m.width
		} else {
			linesUsed = 1
		}
		if viewportLine+linesUsed > vpOffset {
			// This formatted line contains the viewport offset
			if src, ok := reverseMap[i]; ok {
				return src
			}
			return i + 1
		}
		viewportLine += linesUsed
	}
	return len(formattedLines)
}

// ViewportBottomSourceLine returns the source line number at the bottom of the
// visible viewport.
func (m *mainPane) ViewportBottomSourceLine() int {
	bottomOffset := m.viewport.YOffset() + m.height - 1
	if bottomOffset < 0 {
		bottomOffset = 0
	}
	if m.sourceToFormatLine == nil || len(m.sourceToFormatLine) == 0 {
		return bottomOffset + 1
	}
	reverseMap := make(map[int]int, len(m.sourceToFormatLine))
	for src, fmt := range m.sourceToFormatLine {
		reverseMap[fmt] = src
	}

	if !m.wordWrap || m.width <= 0 {
		if src, ok := reverseMap[bottomOffset]; ok {
			return src
		}
		best := 1
		for formattedIdx, srcLine := range reverseMap {
			if formattedIdx <= bottomOffset && srcLine > best {
				best = srcLine
			}
		}
		return best
	}

	formattedLines := strings.Split(m.formattedContent, "\n")
	viewportLine := 0
	lastSrc := 1
	for i, line := range formattedLines {
		lineW := ansiAwareIterate(line, func(r rune, w int) {})
		var linesUsed int
		if lineW > m.width {
			linesUsed = (lineW + m.width - 1) / m.width
		} else {
			linesUsed = 1
		}
		if src, ok := reverseMap[i]; ok {
			lastSrc = src
		}
		if viewportLine+linesUsed > bottomOffset {
			return lastSrc
		}
		viewportLine += linesUsed
	}
	return lastSrc
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
		var w int
		if r == '\t' {
			w = 8 - (totalW % 8) // tab stop every 8 columns
		} else {
			w = runewidth.RuneWidth(r)
		}
		fn(r, w)
		totalW += w
	}
	return totalW
}

// wrapLines wraps each line at the given width, respecting ANSI escape codes.
// Spec: "word-wrap should break at word boundaries, except words longer than
// 1/8 of the screen width should be broken mid-word."
func wrapLines(content string, width int) string {
	return wrapLinesWordBoundary(content, width, 0)
}

// wrapLinesWordBoundary wraps lines at word boundaries with optional indent
// for continuation lines.
func wrapLinesWordBoundary(content string, width, indent int) string {
	if width <= 0 {
		return content
	}
	maxWordWidth := max(10, width/8)
	lines := strings.Split(content, "\n")
	var result []string
	indentStr := ""
	if indent > 0 {
		indentStr = strings.Repeat(" ", indent)
	}

	for _, line := range lines {
		lineW := ansiAwareIterate(line, func(r rune, w int) {})
		if lineW <= width {
			result = append(result, line)
			continue
		}

		// Build a list of "tokens" from the line: each token is either a
		// word (sequence of non-space runes) or whitespace (sequence of space runes).
		// ANSI escapes are attached to whichever token they precede/follow.
		type token struct {
			text     string
			displayW int
			isSpace  bool
		}
		var tokens []token
		var cur strings.Builder
		curW := 0
		curIsSpace := false
		inEscape := false

		for _, r := range line {
			if inEscape {
				cur.WriteRune(r)
				if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
					inEscape = false
				}
				continue
			}
			if r == '\x1b' {
				cur.WriteRune(r)
				inEscape = true
				continue
			}
			isSpace := r == ' ' || r == '\t'
			if cur.Len() > 0 && isSpace != curIsSpace {
				tokens = append(tokens, token{text: cur.String(), displayW: curW, isSpace: curIsSpace})
				cur.Reset()
				curW = 0
			}
			curIsSpace = isSpace
			cur.WriteRune(r)
			if r == '\t' {
				curW += 8 - (curW % 8)
			} else {
				curW += runewidth.RuneWidth(r)
			}
		}
		if cur.Len() > 0 {
			tokens = append(tokens, token{text: cur.String(), displayW: curW, isSpace: curIsSpace})
		}

		// Now greedily fill lines from tokens
		var curLine strings.Builder
		lineWidth := 0
		first := true

		flush := func() {
			result = append(result, curLine.String())
			curLine.Reset()
			if indent > 0 {
				curLine.WriteString(indentStr)
				lineWidth = indent
			} else {
				lineWidth = 0
			}
			first = false
		}

		currentMax := width
		for _, tok := range tokens {
			if tok.isSpace {
				if lineWidth+tok.displayW <= currentMax {
					curLine.WriteString(tok.text)
					lineWidth += tok.displayW
				} else {
					// Space at end of line — flush without the trailing space
					flush()
					currentMax = width
				}
				continue
			}

			// Word token
			if tok.displayW > maxWordWidth {
				// Long word — break mid-word at width boundary
				for _, r := range tok.text {
					if r == '\x1b' || inEscape {
						curLine.WriteRune(r)
						if inEscape && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
							inEscape = false
						} else if r == '\x1b' {
							inEscape = true
						}
						continue
					}
					rw := runewidth.RuneWidth(r)
					if lineWidth+rw > currentMax {
						flush()
						currentMax = width
					}
					curLine.WriteRune(r)
					lineWidth += rw
				}
			} else {
				// Normal word — break before it if it doesn't fit
				if lineWidth+tok.displayW > currentMax {
					flush()
					currentMax = width
				}
				curLine.WriteString(tok.text)
				lineWidth += tok.displayW
			}
			_ = first
		}
		if curLine.Len() > 0 {
			result = append(result, curLine.String())
		}
	}
	return strings.Join(result, "\n")
}

// wrapLinesWithIndent wraps lines like wrapLines but indents continuation lines
// by the given indent width (for gutter alignment).
func wrapLinesWithIndent(content string, width, indent int) string {
	if indent <= 0 {
		return wrapLinesWordBoundary(content, width, 0)
	}
	if width <= indent {
		return wrapLinesWordBoundary(content, width, 0)
	}
	return wrapLinesWordBoundary(content, width, indent)
}

// wrapLinesWithContinuationMap wraps content and returns a boolean slice where
// each entry corresponds to a viewport line. true means the line is a continuation
// of the previous source line (due to word wrapping).
func wrapLinesWithContinuationMap(content string, width, indent int) (string, []bool) {
	if width <= 0 {
		lines := strings.Split(content, "\n")
		cont := make([]bool, len(lines))
		return content, cont
	}
	effectiveIndent := indent
	if indent <= 0 || width <= indent {
		effectiveIndent = 0
	}

	maxWordWidth := max(10, width/8)
	lines := strings.Split(content, "\n")
	var result []string
	var contMap []bool
	indentStr := ""
	if effectiveIndent > 0 {
		indentStr = strings.Repeat(" ", effectiveIndent)
	}

	for _, line := range lines {
		lineW := ansiAwareIterate(line, func(r rune, w int) {})
		if lineW <= width {
			result = append(result, line)
			contMap = append(contMap, false)
			continue
		}

		type token struct {
			text     string
			displayW int
			isSpace  bool
		}
		var tokens []token
		var cur strings.Builder
		curW := 0
		curIsSpace := false
		inEscape := false

		for _, r := range line {
			if inEscape {
				cur.WriteRune(r)
				if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
					inEscape = false
				}
				continue
			}
			if r == '\x1b' {
				cur.WriteRune(r)
				inEscape = true
				continue
			}
			isSpace := r == ' ' || r == '\t'
			if cur.Len() > 0 && isSpace != curIsSpace {
				tokens = append(tokens, token{text: cur.String(), displayW: curW, isSpace: curIsSpace})
				cur.Reset()
				curW = 0
			}
			curIsSpace = isSpace
			cur.WriteRune(r)
			if r == '\t' {
				curW += 8 - (curW % 8)
			} else {
				curW += runewidth.RuneWidth(r)
			}
		}
		if cur.Len() > 0 {
			tokens = append(tokens, token{text: cur.String(), displayW: curW, isSpace: curIsSpace})
		}

		var curLine strings.Builder
		lineWidth := 0
		first := true

		flush := func() {
			result = append(result, curLine.String())
			contMap = append(contMap, !first) // first line of source is not a continuation
			curLine.Reset()
			if effectiveIndent > 0 {
				curLine.WriteString(indentStr)
				lineWidth = effectiveIndent
			} else {
				lineWidth = 0
			}
			first = false
		}

		currentMax := width
		for _, tok := range tokens {
			if tok.isSpace {
				if lineWidth+tok.displayW <= currentMax {
					curLine.WriteString(tok.text)
					lineWidth += tok.displayW
				} else {
					flush()
					currentMax = width
				}
				continue
			}

			if tok.displayW > maxWordWidth {
				for _, r := range tok.text {
					if r == '\x1b' || inEscape {
						curLine.WriteRune(r)
						if inEscape && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
							inEscape = false
						} else if r == '\x1b' {
							inEscape = true
						}
						continue
					}
					rw := runewidth.RuneWidth(r)
					if lineWidth+rw > currentMax {
						flush()
						currentMax = width
					}
					curLine.WriteRune(r)
					lineWidth += rw
				}
			} else {
				if lineWidth+tok.displayW > currentMax {
					flush()
					currentMax = width
				}
				curLine.WriteString(tok.text)
				lineWidth += tok.displayW
			}
		}
		if curLine.Len() > 0 {
			result = append(result, curLine.String())
			contMap = append(contMap, !first)
		}
	}
	return strings.Join(result, "\n"), contMap
}

// ScrollLeft scrolls the viewport left by n columns.
func (m *mainPane) ScrollLeft(n int) {
	m.xOffset = max(0, m.xOffset-n)
	m.refreshViewport()
}

// ScrollRight scrolls the viewport right by n columns.
// Caps at the max content width minus viewport width.
func (m *mainPane) ScrollRight(n int) {
	m.xOffset += n
	// Cap at max content width minus gutter (gutter is always shown)
	maxWidth := m.maxContentWidth() - m.gutterWidth
	availableWidth := m.width - m.gutterWidth
	if maxWidth > availableWidth && m.xOffset > maxWidth-availableWidth {
		m.xOffset = maxWidth - availableWidth
	} else if maxWidth <= availableWidth {
		m.xOffset = 0
	}
	m.refreshViewport()
}

// maxContentWidth returns the display width of the widest line in content.
func (m *mainPane) maxContentWidth() int {
	maxW := 0
	for _, line := range strings.Split(m.content, "\n") {
		w := runewidth.StringWidth(stripANSIForWidth(line))
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

// truncateLinesWithOffset applies a horizontal scroll offset, then truncates.
// The stickyPrefix parameter specifies how many display columns at the start
// of each line are "sticky" (always shown, not affected by horizontal scroll).
// This is used to keep the gutter visible when scrolling.
func truncateLinesWithOffset(content string, width, offset, stickyPrefix int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		var b strings.Builder
		pos := 0   // display position in the full line
		taken := 0 // display width taken in the output
		ansiAwareIterate(line, func(r rune, dw int) {
			if dw == 0 {
				// ANSI escape character — always emit to preserve styling
				b.WriteRune(r)
				return
			}
			if pos < stickyPrefix {
				// Sticky prefix — always show
				b.WriteRune(r)
				taken += dw
			} else if pos-stickyPrefix >= offset && taken+dw <= width {
				b.WriteRune(r)
				taken += dw
			}
			pos += dw
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
