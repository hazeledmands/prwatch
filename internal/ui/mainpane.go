package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	chroma "github.com/alecthomas/chroma/v2"
	chromaformatters "github.com/alecthomas/chroma/v2/formatters"
	chromalexers "github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// chromaStyle and chromaFormatter back files-mode syntax highlighting.
// catppuccin-mocha matches the existing diff palette (#A6E3A1 / #F38BA8 / etc.)
// and terminal16m emits truecolor — falls back gracefully on terminals that
// downgrade to 256-color via the user's terminfo.
var (
	chromaStyle     = chromastyles.Get("catppuccin-mocha")
	chromaFormatter = chromaformatters.Get("terminal16m")

	// Pre-computed open-sequences for the diff backgrounds, used to apply a
	// uniform bg tint to a line that contains chroma's per-token ANSI
	// resets. See applyDiffBg.
	diffAddBgOpen    = bgOpenSeq(diffAddBg)
	diffRemoveBgOpen = bgOpenSeq(diffRemoveBg)
	diffChangeBgOpen = bgOpenSeq(diffChangeBg)
)

// bgOpenSeq returns just the ANSI open-sequence for a background color, with
// no trailing reset. Used by applyDiffBg to re-establish the bg after each
// inner reset emitted by chroma.
func bgOpenSeq(c color.Color) string {
	rendered := lipgloss.NewStyle().Background(c).Render("")
	return strings.TrimSuffix(strings.TrimSuffix(rendered, "\x1b[0m"), "\x1b[m")
}

// applyDiffBg wraps content with a uniform background tint while preserving
// any per-token foreground styling already embedded in content (e.g. chroma
// syntax tokens or renderInlineDiff segments). The trick: every inner SGR
// reset is followed by a re-emission of the bg-open sequence, so the bg
// "sticks" across resets that would otherwise clear it.
func applyDiffBg(content, bgOpen string) string {
	if bgOpen == "" {
		return content
	}
	s := strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bgOpen)
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+bgOpen)
	return bgOpen + s + "\x1b[0m"
}

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

// diffHunk describes a single @@ chunk in a unified diff, mapped to the new
// file's line range. Hunks are 1-indexed and the range is inclusive.
type diffHunk struct {
	StartLine int // first new-file line covered by the hunk
	EndLine   int // last new-file line covered by the hunk
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
	titleLeft          string                 // left-aligned content of the sticky title bar
	titleRight         string                 // right-aligned content of the sticky title bar (when titleDynamic is false)
	titleDynamic       bool                   // when true, View renders the right side from current hunk position
	diffHunks          []diffHunk             // sorted by StartLine, used for sticky title position
	noHunkRight        string                 // shown as the right side in dynamic-title mode when there are no hunks; defaults to "no changes"
	diffPrefix         string                 // prepended to the dynamic right side (with " · " separator) when len(diffHunks) > 0
	filename           string                 // filename for chroma syntax highlighting; empty = no highlighting
	lexer              chroma.Lexer           // cached lexer for filename
	highlightedLines   []string               // per-source-line ANSI; nil when not highlighted (or content empty)
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

// SetDiffHunks installs the per-file hunk list used by the sticky title bar to
// describe the user's current scroll position.
func (m *mainPane) SetDiffHunks(hunks []diffHunk) {
	m.diffHunks = hunks
}

// ClearDiffHunks removes the hunk list.
func (m *mainPane) ClearDiffHunks() {
	m.diffHunks = nil
}

// hunkPosition describes where a source line sits relative to the diff hunks.
type hunkPosition struct {
	total     int // len(hunks); 0 means "no hunks"
	insideIdx int // 0-based hunk index when inside a hunk, else -1
	beforeIdx int // 0-based index of the next hunk when between/before, else -1
	afterIdx  int // 0-based index of the previous hunk when between/after, else -1
}

// hunkPositionForLine classifies sourceLine relative to the hunk list:
//   - inside a hunk → insideIdx is set
//   - before all hunks → beforeIdx == 0
//   - after all hunks → afterIdx == total-1
//   - between two hunks → both beforeIdx and afterIdx are set
//   - no hunks → total == 0
func hunkPositionForLine(hunks []diffHunk, sourceLine int) hunkPosition {
	pos := hunkPosition{total: len(hunks), insideIdx: -1, beforeIdx: -1, afterIdx: -1}
	if len(hunks) == 0 {
		return pos
	}
	for i, h := range hunks {
		if sourceLine >= h.StartLine && sourceLine <= h.EndLine {
			pos.insideIdx = i
			return pos
		}
		if sourceLine < h.StartLine {
			pos.beforeIdx = i
			if i > 0 {
				pos.afterIdx = i - 1
			}
			return pos
		}
	}
	pos.afterIdx = len(hunks) - 1
	return pos
}

// parseDiffHunks extracts hunks from a unified diff. Empty hunks (count == 0,
// e.g. pure deletions) are dropped.
func parseDiffHunks(unifiedDiff string) []diffHunk {
	if unifiedDiff == "" {
		return nil
	}
	var hunks []diffHunk
	for _, line := range strings.Split(unifiedDiff, "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		start, count := parseHunkHeader(line)
		if start <= 0 || count <= 0 {
			continue
		}
		hunks = append(hunks, diffHunk{
			StartLine: start,
			EndLine:   start + count - 1,
		})
	}
	return hunks
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
			switch {
			case len(pendingRemoved) == 1:
				// One-for-one swap → mark as changed and drive the
				// inline/split renderer.
				ann.kind = diffLineChanged
				ann.removedLines = append(ann.removedLines, pendingRemoved...)
				pendingRemoved = nil
			case len(pendingRemoved) > 1:
				// Multi-line block change. The 1-to-1 inline-diff pairing
				// breaks down here: the user expects N red lines followed
				// by M green lines, not a confused mix. Attach the
				// pendingRemoved as removedLines so the file view shows
				// them as `-` rows above; mark this line plainly added.
				ann.kind = diffLineAdded
				ann.removedLines = append(ann.removedLines, pendingRemoved...)
				pendingRemoved = nil
			default:
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
	m.viewport.SetHeight(viewportHeightFor(h))
}

// SetTitle sets the sticky title bar content. Left is shown left-aligned, right
// right-aligned. When the two would collide, left is truncated with an ellipsis
// before right is touched. Plain strings only — colors are applied at render
// time.
func (m *mainPane) SetTitle(left, right string) {
	m.titleLeft = left
	m.titleRight = right
	m.titleDynamic = false
}

// SetTitleWithHunks sets the title's left side and switches the pane into
// dynamic-title mode: each render computes the right side from the current
// scroll position against the installed hunk list.
func (m *mainPane) SetTitleWithHunks(left string) {
	m.titleLeft = left
	m.titleRight = ""
	m.titleDynamic = true
}

// SetNoHunkRight overrides the text shown on the right side of the sticky
// title when there are no hunks (e.g. an unchanged file). Pass "" to fall
// back to the default "no changes".
func (m *mainPane) SetNoHunkRight(right string) {
	m.noHunkRight = right
}

// SetDiffPrefix sets a string that is prepended (with " · " separator) to the
// dynamic right side when the file has hunks. Used in files mode to surface
// the file's most-recently-changed metadata next to the hunk position.
// Pass "" to clear.
func (m *mainPane) SetDiffPrefix(prefix string) {
	m.diffPrefix = prefix
}

// hunkTitleRight formats the right side of the title bar for files mode based
// on the hunks intersecting the visible viewport range.
//
// Format:
//   - 0 hunks visible:
//     "before hunk 1" / "between hunks (N–N+1)" / "after hunk M"
//     (classified from the top visible source line)
//   - 1 hunk visible:
//     "hunk N/M"
//   - 2+ hunks visible:
//     "viewing hunks N through M"
//   - no hunks at all in the file:
//     "no changes"
func (m *mainPane) hunkTitleRight() string {
	if len(m.diffHunks) == 0 {
		if m.noHunkRight != "" {
			return m.noHunkRight
		}
		return "no changes"
	}
	topLine := m.ViewportToSourceLine()
	bottomLine := m.ViewportBottomSourceLine()
	if bottomLine < topLine {
		bottomLine = topLine
	}
	first, last := visibleHunkRange(m.diffHunks, topLine, bottomLine)
	switch {
	case first < 0:
		// Nothing visible — classify off the top line.
		pos := hunkPositionForLine(m.diffHunks, topLine)
		switch {
		case pos.afterIdx < 0 && pos.beforeIdx == 0:
			return "before hunk 1"
		case pos.beforeIdx < 0 && pos.afterIdx == pos.total-1:
			return fmt.Sprintf("after hunk %d", pos.total)
		default:
			return fmt.Sprintf("between hunks (%d–%d)", pos.afterIdx+1, pos.beforeIdx+1)
		}
	case first == last:
		return fmt.Sprintf("hunk %d/%d", first+1, len(m.diffHunks))
	default:
		return fmt.Sprintf("viewing hunks %d through %d", first+1, last+1)
	}
}

// progressPercent returns the user's vertical position in the file as an
// integer percent in [0, 100], based on the bottom-most visible source line.
// Empty content, or content that fits entirely in the viewport, both report
// 100% (nothing left to scroll past). The trailing newline at end-of-file is
// not counted as a separate logical line.
func (m *mainPane) progressPercent() int {
	if m.content == "" {
		return 100
	}
	total := strings.Count(m.content, "\n")
	if !strings.HasSuffix(m.content, "\n") {
		total++
	}
	if total <= 0 {
		return 100
	}
	bottom := m.ViewportBottomSourceLine()
	if bottom >= total {
		return 100
	}
	if bottom < 0 {
		bottom = 0
	}
	return (bottom * 100) / total
}

// visibleHunkRange returns the inclusive [first, last] indices of hunks that
// intersect the visible source-line range [topLine, bottomLine]. Returns
// (-1, -1) when no hunks intersect.
func visibleHunkRange(hunks []diffHunk, topLine, bottomLine int) (int, int) {
	first, last := -1, -1
	for i, h := range hunks {
		if h.EndLine < topLine || h.StartLine > bottomLine {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	return first, last
}

// viewportHeightFor returns the viewport height for a given pane height,
// reserving one row for the sticky title bar.
func viewportHeightFor(paneHeight int) int {
	if paneHeight <= 1 {
		return 0
	}
	return paneHeight - 1
}

func (m *mainPane) SetContent(content string) {
	m.content = content
	m.isDiff = true
	m.refreshViewport()
}

func (m *mainPane) SetPlainContent(content string) {
	m.content = content
	m.isDiff = false
	m.recomputeHighlightedLines()
	m.refreshViewport()
}

// SetFilename installs (or clears) the filename used to pick a chroma lexer for
// files-mode syntax highlighting. Pass "" to disable highlighting.
func (m *mainPane) SetFilename(name string) {
	if m.filename == name {
		return
	}
	m.filename = name
	m.lexer = nil
	if name != "" {
		if lx := chromalexers.Match(name); lx != nil {
			m.lexer = chroma.Coalesce(lx)
		}
	}
	m.recomputeHighlightedLines()
	m.refreshViewport()
}

// recomputeHighlightedLines re-runs chroma over m.content into per-line ANSI
// strings. The result is cached and only invalidated when content or filename
// changes.
func (m *mainPane) recomputeHighlightedLines() {
	m.highlightedLines = nil
	if m.lexer == nil || chromaStyle == nil || chromaFormatter == nil || m.content == "" {
		return
	}
	iter, err := m.lexer.Tokenise(nil, m.content)
	if err != nil {
		return
	}
	var b strings.Builder
	if err := chromaFormatter.Format(&b, chromaStyle, iter); err != nil {
		return
	}
	m.highlightedLines = strings.Split(b.String(), "\n")
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

// intraLineDiffs computes a character-level diff between old and new, then runs
// semantic cleanup so the segments correspond to humanly meaningful chunks
// rather than fragmenting on incidental shared characters.
func intraLineDiffs(oldText, newText string) []diffmatchpatch.Diff {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(oldText, newText, false)
	return dmp.DiffCleanupSemantic(diffs)
}

// inlineDiffSize returns the total display width of the changed characters
// between old and new text (characters that differ).
func inlineDiffSize(oldText, newText string) int {
	size := 0
	for _, d := range intraLineDiffs(oldText, newText) {
		if d.Type == diffmatchpatch.DiffEqual {
			continue
		}
		size += runewidth.StringWidth(d.Text)
	}
	return size
}

// renderInlineDiff renders a changed line with inline diff coloring.
// Retained text is yellow, deleted text is red, new text is green. Deletes are
// emitted before inserts at the same position to keep the visual order
// "old then new".
func renderInlineDiff(oldText, newText string) string {
	diffs := intraLineDiffs(oldText, newText)
	var b strings.Builder
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			b.WriteString(diffRetainedStyle.Render(d.Text))
		case diffmatchpatch.DiffDelete:
			b.WriteString(diffRemoveStyle.Render(d.Text))
		case diffmatchpatch.DiffInsert:
			b.WriteString(diffAddStyle.Render(d.Text))
		}
	}
	return b.String()
}

// renderInlineDiffWithBg is the files-mode variant of renderInlineDiff: each
// segment carries its own background tint instead of a uniform bg over the
// whole line. Retained text inherits chroma syntax fg (so it reads as normal
// code) with no bg; removed segments get the red diff bg with a flat red fg
// (the deleted text wasn't lexed); added segments get the green diff bg over
// the appropriate slice of chroma's per-token fg.
//
// When lexer is nil, the function falls back to flat fg colors (yellow /
// red / green) so non-source content still gets a usable inline diff.
func renderInlineDiffWithBg(oldText, newText string, lexer chroma.Lexer) string {
	diffs := intraLineDiffs(oldText, newText)

	if lexer == nil {
		var b strings.Builder
		for _, d := range diffs {
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				b.WriteString(diffRetainedStyle.Render(d.Text))
			case diffmatchpatch.DiffDelete:
				b.WriteString(diffRemoveLineStyle.Render(d.Text))
			case diffmatchpatch.DiffInsert:
				b.WriteString(diffAddLineStyle.Render(d.Text))
			}
		}
		return b.String()
	}

	// Lex newText so we can pull per-token chroma styling for the retained
	// and inserted segments. The diff cursor advances byte-for-byte through
	// newText for Equal and Insert segments; Delete segments don't consume
	// any newText bytes (the deleted content lives in oldText only).
	iter, err := lexer.Tokenise(nil, newText)
	if err != nil {
		return renderInlineDiffWithBg(oldText, newText, nil)
	}
	tokens := iter.Tokens()

	var b strings.Builder
	tokIdx, tokOffset := 0, 0
	for _, d := range diffs {
		if d.Type == diffmatchpatch.DiffDelete {
			b.WriteString(diffRemoveLineStyle.Render(d.Text))
			continue
		}
		// Slice tokens to cover len(d.Text) bytes from newText.
		need := len(d.Text)
		var segTokens []chroma.Token
		for need > 0 && tokIdx < len(tokens) {
			tk := tokens[tokIdx]
			avail := len(tk.Value) - tokOffset
			if avail <= need {
				segTokens = append(segTokens, chroma.Token{Type: tk.Type, Value: tk.Value[tokOffset:]})
				need -= avail
				tokIdx++
				tokOffset = 0
			} else {
				segTokens = append(segTokens, chroma.Token{Type: tk.Type, Value: tk.Value[tokOffset : tokOffset+need]})
				tokOffset += need
				need = 0
			}
		}
		var segBuf strings.Builder
		if err := chromaFormatter.Format(&segBuf, chromaStyle, chroma.Literator(segTokens...)); err != nil {
			// Fallback: emit raw text with no styling for this segment.
			segBuf.WriteString(d.Text)
		}
		seg := segBuf.String()
		if d.Type == diffmatchpatch.DiffInsert {
			seg = applyDiffBg(seg, diffAddBgOpen)
		}
		b.WriteString(seg)
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

		// highlighted holds the chroma-rendered version of the current source
		// line (when a lexer is set). It's used as the body for added/
		// changed/whole-file-removed cases so syntax tokens stay visible
		// under the bg tint.
		highlighted := line
		if i < len(m.highlightedLines) {
			highlighted = m.highlightedLines[i]
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
				result = append(result, diffRemoveLineStyle.Render(gutterMark+removed))
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
					result = append(result, diffRemoveLineStyle.Render(gutterMarkDel+extra))
				}
			}

			oldLine := ann.removedLines[len(ann.removedLines)-1]
			contentWidth := m.width - gutterWidth
			if contentWidth <= 0 {
				contentWidth = m.width
			}
			diffSize := inlineDiffSize(oldLine, line)
			if diffSize <= contentWidth/4 {
				// Small diff: gutter stays yellow-bg; body switches to
				// per-segment bg so retained text reads as neutral context
				// while red/green tints flag the actual edits.
				gutterPart := diffChangeLineStyle.Render(prefix + gutter)
				if !m.lineNumbers {
					gutterPart = diffChangeLineStyle.Render(gutter)
				}
				result = append(result, gutterPart+renderInlineDiffWithBg(oldLine, line, m.lexer))
			} else {
				// Large diff: deleted version (red bg) on top, new version
				// (green bg, syntax-highlighted) on bottom.
				gutterMarkDel := " ~ "
				if m.lineNumbers {
					gutterMarkDel = strings.Repeat(" ", numWidth) + " ~ "
				}
				result = append(result, diffRemoveLineStyle.Render(gutterMarkDel+oldLine))
				addGutter := diffAddLineStyle.Render(prefix + gutter)
				if !m.lineNumbers {
					addGutter = diffAddLineStyle.Render(gutter)
				}
				result = append(result, addGutter+applyDiffBg(highlighted, diffAddBgOpen))
			}
		} else if hasAnn && ann.kind == diffLineRemoved {
			// Completely deleted file — mark every line with "-"
			gutter := " - "
			gutterPart := diffRemoveLineStyle.Render(prefix + gutter)
			if !m.lineNumbers {
				gutterPart = diffRemoveLineStyle.Render(gutter)
			}
			result = append(result, gutterPart+applyDiffBg(highlighted, diffRemoveBgOpen))
		} else if hasAnn && (ann.kind == diffLineAdded || ann.kind == diffLineChanged) {
			var gutter, bgOpen string
			var lineStyle lipgloss.Style
			if ann.kind == diffLineChanged {
				gutter = " ~ "
				lineStyle = diffChangeLineStyle
				bgOpen = diffChangeBgOpen
			} else {
				gutter = " + "
				lineStyle = diffAddLineStyle
				bgOpen = diffAddBgOpen
			}
			gutterPart := lineStyle.Render(prefix + gutter)
			if !m.lineNumbers {
				gutterPart = lineStyle.Render(gutter)
			}
			result = append(result, gutterPart+applyDiffBg(highlighted, bgOpen))
		} else {
			gutter := "   "
			if m.lineNumbers {
				result = append(result, prefix+gutter+highlighted)
			} else {
				result = append(result, gutter+highlighted)
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
	body := m.viewport.View()
	if m.height >= 1 {
		right := m.titleRight
		if m.titleDynamic {
			hunkPart := m.hunkTitleRight()
			if m.diffPrefix != "" && len(m.diffHunks) > 0 {
				hunkPart = m.diffPrefix + " · " + hunkPart
			}
			right = fmt.Sprintf("%s · %d%%", hunkPart, m.progressPercent())
		}
		title := mainPaneTitleStyle.Render(renderTitleRow(m.titleLeft, right, m.width))
		body = lipgloss.JoinVertical(lipgloss.Left, title, body)
	}
	// lipgloss v2: Width/Height set the outer dimensions (includes borders).
	// Add 2 for border characters on each axis.
	return style.Width(m.width + 2).Height(m.height + 2).Render(body)
}

// renderTitleRow lays out a left-aligned and right-aligned string into a single
// row of exactly width display columns. When left+right would collide, left is
// truncated with an ellipsis. If right alone is wider than width, right is
// truncated and left is dropped entirely.
func renderTitleRow(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	rightW := runewidth.StringWidth(right)
	if rightW > width {
		return fitToWidth(runewidth.Truncate(right, width, ""), width)
	}
	leftW := runewidth.StringWidth(left)
	gap := 1
	if leftW == 0 || rightW == 0 {
		gap = 0
	}
	if leftW+gap+rightW > width {
		budget := width - rightW - gap
		if budget <= 0 {
			left = ""
			leftW = 0
		} else {
			left = runewidth.Truncate(left, budget, "…")
			leftW = runewidth.StringWidth(left)
		}
	}
	pad := width - leftW - rightW
	if pad < 0 {
		pad = 0
	}
	return fitToWidth(left+strings.Repeat(" ", pad)+right, width)
}

// fitToWidth pads or truncates s so its display width is exactly width. Acts as
// a safety net against width-estimate drift from combining characters.
func fitToWidth(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w == width {
		return s
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	s = runewidth.Truncate(s, width, "")
	if w := runewidth.StringWidth(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
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
	bottomOffset := m.viewport.YOffset() + m.viewport.Height() - 1
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
