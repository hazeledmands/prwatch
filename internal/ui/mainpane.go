package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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
//
// This is the pane's *second* content boundary: an annotation's removedLines
// are rendered inline as pane rows (Shift+D), so they get the same tab
// normalization as SetContent/SetPlainContent. Without it a removed line
// carried a raw tab into the render, where lipgloss expanded it to 4 columns
// while the pane's own width math counted it as 0 — the same class of
// disagreement expandTabs exists to end.
func (m *mainPane) SetDiffAnnotations(annotations map[int]diffAnnotation) {
	m.diffAnnotations = expandTabsInAnnotations(annotations)
	m.refreshViewport()
}

// expandTabsInAnnotations returns annotations whose removedLines have been
// tab-normalized. Returns a fresh map so the caller's copy is untouched.
func expandTabsInAnnotations(annotations map[int]diffAnnotation) map[int]diffAnnotation {
	if annotations == nil {
		return nil
	}
	out := make(map[int]diffAnnotation, len(annotations))
	for line, ann := range annotations {
		if len(ann.removedLines) > 0 {
			removed := make([]string, len(ann.removedLines))
			for i, r := range ann.removedLines {
				removed[i] = expandTabs(r)
			}
			ann.removedLines = removed
		}
		out[line] = ann
	}
	return out
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

// parseDiffHunks extracts hunks from a unified diff, recording each
// hunk's *change-line* range — the new-file line bounds spanned by its
// `+`/`-` markers — rather than the header's full new-file range
// (which includes leading and trailing context). That way "hunk visible
// in the viewport" tracks "you can see something that actually changed,"
// not "you can see the hunk's leading context."
//
// Walks the hunk body to find where the changes are:
//   - `+line` advances newLineNo and marks a change at the line it
//     just emitted.
//   - `-line` doesn't advance newLineNo (removed lines aren't in the
//     new file) but marks a change at the current newLineNo — the line
//     the removal visually attaches to in the rendered view.
//   - ` line` (context) just advances newLineNo.
//
// Pure-deletion hunks (header `+A,0`) anchor to A: removed lines attach
// to that line in the rendered view, so hunk-grain nav has a target.
// Their EndLine equals StartLine so visibleHunkRange treats them as a
// 1-line hunk visible whenever the anchor row is in the viewport.
// Headers with a 0 anchor (`+0,0`) clamp to line 1 — that's where
// pre-line-1 removals visually attach.
func parseDiffHunks(unifiedDiff string) []diffHunk {
	if unifiedDiff == "" {
		return nil
	}
	var hunks []diffHunk
	var inHunk bool
	var newLineNo int
	var firstChange, lastChange int // 0 means "no change seen yet"

	finish := func() {
		if !inHunk || firstChange == 0 {
			return
		}
		hunks = append(hunks, diffHunk{StartLine: firstChange, EndLine: lastChange})
	}

	var sc diffScanner
	for _, line := range strings.Split(unifiedDiff, "\n") {
		kind := sc.classify(line)
		if kind == rowDiffHeader {
			// File boundary in a multi-file diff: close the open hunk so
			// the next file's header lines can't extend it.
			finish()
			inHunk = false
			continue
		}
		if kind == rowHunkHeader {
			finish()
			inHunk = false
			start, count := parseHunkHeader(line)
			if start <= 0 && count > 0 {
				continue // malformed
			}
			if start < 1 {
				start = 1
			}
			inHunk = true
			newLineNo = start
			firstChange = 0
			lastChange = 0
			if count == 0 {
				// Pure-deletion hunk: removed-line annotations attach
				// to line `start` even though the body has no `+` to
				// pin a change to.
				firstChange = start
				lastChange = start
			}
			continue
		}
		if !inHunk {
			continue
		}
		switch kind {
		case rowAdd:
			if firstChange == 0 {
				firstChange = newLineNo
			}
			lastChange = newLineNo
			newLineNo++
		case rowRemove:
			if firstChange == 0 {
				firstChange = newLineNo
			}
			lastChange = newLineNo
			// removed lines don't advance newLineNo
		case rowContext:
			if strings.HasPrefix(line, " ") {
				newLineNo++
			}
		}
	}
	finish()
	return hunks
}

// ToggleShowRemoved toggles display of removed lines.
func (m *mainPane) ToggleShowRemoved() {
	m.showRemoved = !m.showRemoved
	m.refreshViewport()
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
	newLineNo := 0

	var sc diffScanner
	for _, line := range lines {
		kind := sc.classify(line)
		if kind == rowHunkHeader {
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
		switch kind {
		case rowDiffHeader, rowFileHeader, rowMeta, rowNoNewline:
			// Header/metadata between files, and the no-newline marker.
			continue
		}
		if kind == rowAdd {
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
		} else if kind == rowRemove {
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

	// Flush any remaining pendingRemoved lines that the loop didn't consume.
	// This happens when the diff ends on `-` lines with no trailing `+`,
	// context, or new hunk header to flush against — e.g. a file shrunk by
	// pure deletion at end-of-file, where the diff has no final newline.
	if len(pendingRemoved) > 0 && newLineNo > 0 {
		ann := annotations[newLineNo]
		ann.removedLines = append(ann.removedLines, pendingRemoved...)
		annotations[newLineNo] = ann
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

// SetSize resizes the pane. A width change re-wraps the content: without
// the refresh the viewport kept rows wrapped at the old width until the
// next content-setting tick, so a resize left stale wrap points (and a
// stale row↔source mapping) on screen. Height doesn't affect wrapping.
func (m *mainPane) SetSize(w, h int) {
	widthChanged := m.width != w
	m.width = w
	m.height = h
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(viewportHeightFor(h))
	if widthChanged {
		m.refreshViewport()
	}
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
	vr := m.visibleRange()
	topLine := vr.Start.SourceLine
	bottomLine := vr.End.SourceLine
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
	bottom := m.visibleRange().End.SourceLine
	if bottom >= total {
		return 100
	}
	return (bottom * 100) / total
}

// visibleHunkRange returns the inclusive [first, last] indices of hunks
// that intersect the visible source-line range [topLine, bottomLine].
// Returns (-1, -1) when no hunks intersect.
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

// tabWidth is the column span of a tab everywhere in the UI. 4 matches what
// lipgloss already renders, so expanding at the boundary changes no pixels.
const tabWidth = 4

// expandTabs replaces each tab with spaces up to the next tabWidth-column tab
// stop, resetting the column at every newline and skipping ANSI escape
// sequences (which occupy no columns).
//
// This is the single tab-handling site in the UI. Before it existed, three
// different widths were in play — the wrap tokenizer and ansiAwareIterate used
// 8-column stops, runewidth.RuneWidth('\t') reports 0, and lipgloss renders 4
// — so on tab-indented files (i.e. every Go file) wrap points, gutter
// alignment, cursor columns and drag-copy slicing silently disagreed with the
// render and with each other. Normalizing once here means no downstream
// consumer needs to know what a tab is.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)
	col := 0
	inEscape := false
	for _, r := range s {
		if inEscape {
			b.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		switch r {
		case '\x1b':
			b.WriteRune(r)
			inEscape = true
		case '\n':
			b.WriteRune(r)
			col = 0
		case '\t':
			n := tabWidth - (col % tabWidth)
			b.WriteString(strings.Repeat(" ", n))
			col += n
		default:
			b.WriteRune(r)
			col += runewidth.RuneWidth(r)
		}
	}
	return b.String()
}

func (m *mainPane) SetContent(content string) {
	m.content = expandTabs(content)
	m.isDiff = true
	m.refreshViewport()
}

func (m *mainPane) SetPlainContent(content string) {
	m.content = expandTabs(content)
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
		// Diff content has no gutter/line-number formatting, so there is no
		// source-line → formatted-line mapping to maintain. Clear any
		// leftover from a prior plain-content render so callers (e.g.
		// viewportToSourceLine, ScrollToSourceLine) fall through to the
		// 1:1 viewport-line mapping appropriate for diff content.
		m.sourceToFormatLine = nil
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

	// Emit any "tail" annotations whose line number is past the last source
	// line. These come from diffs that end on `-` lines — parseDiffAnnotations
	// attaches the pending removed batch to annotations[newLineNo], which is
	// one beyond the last new-file line for an end-of-file deletion. Without
	// this pass the rows never render and the deletions disappear from view.
	if m.showRemoved {
		tailKeys := make([]int, 0)
		for k := range m.diffAnnotations {
			if k > len(lines) {
				tailKeys = append(tailKeys, k)
			}
		}
		sort.Ints(tailKeys)
		for _, k := range tailKeys {
			ann := m.diffAnnotations[k]
			for _, removed := range ann.removedLines {
				gutterMark := " - "
				if m.lineNumbers {
					gutterMark = strings.Repeat(" ", numWidth) + " - "
				}
				result = append(result, diffRemoveLineStyle.Render(gutterMark+removed))
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

// ScrollToSourceLine scrolls the viewport so the given 1-indexed source
// line sits at the top. This is the only scroll-to-a-line entry point:
// there is deliberately no raw "scroll to viewport row N" method, because
// every caller means a *content* line, and formatting (gutter, removed
// lines) plus word wrapping make row ≠ line.
func (m *mainPane) ScrollToSourceLine(sourceLine int) {
	m.viewport.SetYOffset(m.sourceLineToViewportOffset(sourceLine))
}

// hunkNavMargin returns the leading-context row count used when
// scrolling to a hunk start. ~30% of viewport height — Vim-style
// centering that leaves room above the target for orientation.
func (m *mainPane) hunkNavMargin() int {
	return m.viewport.Height() * 3 / 10
}

// scrollToHunkStart scrolls the viewport so the given source line sits
// hunkNavMargin rows down from the top, leaving leading context above.
// Used by hunk-grain navigation (jumpToFirstDiff / jumpToNextDiff) so
// hunks don't show up flush against the viewport top.
func (m *mainPane) scrollToHunkStart(sourceLine int) {
	target := m.sourceLineToViewportOffset(sourceLine)
	yOffset := target - m.hunkNavMargin()
	if yOffset < 0 {
		yOffset = 0
	}
	m.viewport.SetYOffset(yOffset)
}

// hunkNavCursor returns the source line at the row hunkNavMargin below
// the viewport top — i.e., where a scrollToHunkStart would place the
// target line. After scrollToHunkStart this equals the hunk's
// StartLine, so jumpToNextDiff uses it as the "current hunk" reference
// for advancing through the hunk list. Without this, the reference
// would be viewportTop.SourceLine (= hunkStart - margin) and J would
// re-find the current hunk instead of advancing.
func (m *mainPane) hunkNavCursor() int {
	return m.sourceLineAtViewportOffset(m.viewport.YOffset() + m.hunkNavMargin())
}

// viewportToSourceLine converts the viewport's scroll offset to the closest
// source file line number. Thin wrapper around sourceLineAtViewportOffset
// with the top-of-viewport row.
func (m *mainPane) viewportToSourceLine() int {
	return m.sourceLineAtViewportOffset(m.viewport.YOffset())
}

// viewportBottomSourceLine returns the source line at the bottom of the
// visible viewport. Thin wrapper around sourceLineAtViewportOffset with
// the bottom-of-viewport row.
func (m *mainPane) viewportBottomSourceLine() int {
	bottomRow := m.viewport.YOffset() + m.viewport.Height() - 1
	if bottomRow < 0 {
		bottomRow = 0
	}
	return m.sourceLineAtViewportOffset(bottomRow)
}

// sourceLineAtViewportOffset returns the 1-indexed source line displayed
// at the given 0-indexed viewport row (0 = first visible row). Wrap-aware:
// with word wrap on, walks m.wrapContinuation to find which formatted
// line the row belongs to, then maps that formatted line back to a
// source line via reverseMap. For rows that fall on a rendered-only
// formatted line (removed-line prefix above a source line, "tail"
// annotation past EOF), returns the most recently mapped source line
// ≤ that row.
//
// Uses wrapContinuation (populated by wrapLinesWithContinuationMap) as
// the authoritative source for "which formatted line owns this viewport
// row" — earlier versions computed this via wrappedRowCount(line, width)
// which didn't account for the indent applied to continuation rows, so
// it undercounted wrap rows for indented content (gutter present) and
// returned the wrong source line for rows past the predicted count.
func (m *mainPane) sourceLineAtViewportOffset(target int) int {
	formattedIdx := m.viewportRowToFormatLine(target)
	if len(m.sourceToFormatLine) == 0 {
		// Diff content: formatted lines are the source lines, 1:1.
		return formattedIdx + 1
	}
	return mostRecentSourceLineAtOrBefore(m.buildReverseSourceMap(), formattedIdx)
}

// viewportRowToFormatLine converts a 0-indexed viewport row to the
// 0-indexed pre-wrap formatted line that owns it. Inverse of
// formatLineToViewportRow. With wrap off, the two are the same number.
func (m *mainPane) viewportRowToFormatLine(row int) int {
	row = max(row, 0)
	if !m.wordWrap || m.width <= 0 || len(m.wrapContinuation) == 0 {
		return row
	}
	// Walk wrapContinuation: each false entry marks the first row of a
	// new formatted line. Count false entries up through `row` to find
	// the formatted-line index that row belongs to.
	formattedIdx := -1
	for i := 0; i <= row && i < len(m.wrapContinuation); i++ {
		if !m.wrapContinuation[i] {
			formattedIdx++
		}
	}
	// Past end of content: stick with the last formatted index we counted.
	return max(formattedIdx, 0)
}

// formatLineToViewportRow converts a 0-indexed pre-wrap formatted line to
// the 0-indexed viewport row where it starts. Inverse of
// viewportRowToFormatLine.
func (m *mainPane) formatLineToViewportRow(formattedIdx int) int {
	formattedIdx = max(formattedIdx, 0)
	if !m.wordWrap || m.width <= 0 || len(m.wrapContinuation) == 0 {
		return formattedIdx
	}
	// Find the formattedIdx-th false entry (0-indexed); that entry's index
	// is the first viewport row of the target formatted line.
	seen := -1
	for i, cont := range m.wrapContinuation {
		if !cont {
			seen++
			if seen == formattedIdx {
				return i
			}
		}
	}
	// Formatted index is past the end of wrapped content (shouldn't
	// normally happen). Return the last row.
	return len(m.wrapContinuation)
}

// sourceLineToViewportOffset returns the 0-indexed viewport row at which
// the given 1-indexed source line first appears. Inverse of
// sourceLineAtViewportOffset; wrap-aware via wrapContinuation. Returns 0
// when the source line has no formatted mapping (treated as "before any
// content"). Used by ApplyHighlight / SelectedText to convert source-space
// positions back to screen rows for rendering.
func (m *mainPane) sourceLineToViewportOffset(sourceLine int) int {
	if len(m.sourceToFormatLine) == 0 {
		// Diff content: formatted lines are the source lines, 1:1.
		return m.formatLineToViewportRow(sourceLine - 1)
	}
	formattedIdx, ok := m.sourceToFormatLine[sourceLine]
	if !ok {
		return 0
	}
	return m.formatLineToViewportRow(formattedIdx)
}

// buildReverseSourceMap returns formattedIndex → sourceLine, the inverse
// of m.sourceToFormatLine.
func (m *mainPane) buildReverseSourceMap() map[int]int {
	out := make(map[int]int, len(m.sourceToFormatLine))
	for src, fmt := range m.sourceToFormatLine {
		out[fmt] = src
	}
	return out
}

// mostRecentSourceLineAtOrBefore looks up reverseMap[target], falling
// back to the largest entry whose formatted index ≤ target. Used by the
// no-wrap branch of sourceLineAtViewportOffset and is the same fallback
// the wrap branch arrives at via online lastSrc tracking.
func mostRecentSourceLineAtOrBefore(reverseMap map[int]int, target int) int {
	if src, ok := reverseMap[target]; ok {
		return src
	}
	best := 1
	for formattedIdx, srcLine := range reverseMap {
		if formattedIdx <= target && srcLine > best {
			best = srcLine
		}
	}
	return best
}

// absoluteColumnFromDisplay translates a (viewport-row, display-column)
// click into a source-line-absolute, gutter-relative column. With wrap
// off (or no wrap continuation map / display col on a non-continuation
// row) the result equals displayCol. On a continuation row it walks
// back through preceding continuation rows of the same source line,
// summing their displayed widths (gutter stripped) so the returned
// column is in the unwrapped source line's coordinate frame.
//
// Source-space SelectedText/ApplyHighlight rely on this so partial
// wrap-row drag selections clip at the right source columns even when
// word-boundary wrap put the click on the second or third wrap row of
// a long source line.
func (m *mainPane) absoluteColumnFromDisplay(vpRow, displayCol int) int {
	if displayCol < 0 {
		displayCol = 0
	}
	if !m.wordWrap || vpRow < 0 || vpRow >= len(m.wrapContinuation) || !m.wrapContinuation[vpRow] {
		return displayCol
	}
	vpLines := strings.Split(m.viewport.GetContent(), "\n")
	base := 0
	for i := vpRow - 1; i >= 0; i-- {
		if i >= len(vpLines) {
			continue
		}
		base += stripGutterDisplayWidth(vpLines[i], m.gutterWidth)
		if i >= len(m.wrapContinuation) || !m.wrapContinuation[i] {
			break
		}
	}
	return base + displayCol
}

// wrapRowCountAtVpRow returns the number of wrap rows belonging to the
// source line whose first wrap row is at vpRow. In no-wrap mode (or
// when wrapContinuation isn't populated) this is always 1; in wrap mode
// it counts consecutive wrapContinuation=true rows after vpRow.
func (m *mainPane) wrapRowCountAtVpRow(vpRow int) int {
	if !m.wordWrap || vpRow < 0 {
		return 1
	}
	count := 1
	for i := vpRow + 1; i < len(m.wrapContinuation); i++ {
		if !m.wrapContinuation[i] {
			break
		}
		count++
	}
	return count
}

// wrapRowSourceColRange returns the source-absolute, gutter-relative
// column range [start, end] (inclusive) covered by the wrap row at
// vpRow. The start comes from absoluteColumnFromDisplay (with display
// col 0); the end is start + displayed-width-of-row - 1. Used by
// buildHighlightClips to intersect a wrap row with the selection's
// source-column range.
func (m *mainPane) wrapRowSourceColRange(vpRow int) (int, int) {
	start := m.absoluteColumnFromDisplay(vpRow, 0)
	vpLines := strings.Split(m.viewport.GetContent(), "\n")
	if vpRow < 0 || vpRow >= len(vpLines) {
		return start, start
	}
	w := stripGutterDisplayWidth(vpLines[vpRow], m.gutterWidth)
	if w == 0 {
		return start, start
	}
	return start, start + w - 1
}

// positionToDisplay returns the (viewport row, gutter-relative display
// column) where pos renders. Inverse of sourceLineAtViewportOffset +
// absoluteColumnFromDisplay; composes sourceLineToViewportOffset,
// wrapRowCountAtVpRow, and wrapRowSourceColRange.
//
// Used by cursor rendering and any caller that needs to know where on
// screen a source-space Position lives.
//
// Wrap-boundary convention. When pos.Column lands exactly at a wrap-row
// boundary (Column == start of wrap row K+1, == end+1 of wrap row K),
// returns the lower row's left edge (vpRow=K+1, displayCol=0). The
// algorithm walks wrap rows in order and stops at the first row whose
// source-column range contains Column; since rows are contiguous and
// non-overlapping, boundary columns land on the next row by definition.
// This matches the natural semantics — Position{SL, Column} identifies
// the character at that column, and that character is the first char of
// the row that starts there.
//
// Past EOL. If pos.Column exceeds the source line's last content column
// (cursor past end-of-line), returns the last wrap row of the source
// line with displayCol set to the column's offset past the row's start.
// The returned displayCol may exceed the row's content width.
func (m *mainPane) positionToDisplay(pos Position) (vpRow, displayCol int) {
	col := max(pos.Column, 0)
	firstRow := m.sourceLineToViewportOffset(pos.SourceLine)
	if !m.wordWrap || m.width <= 0 || len(m.wrapContinuation) == 0 {
		return firstRow, col
	}
	count := m.wrapRowCountAtVpRow(firstRow)
	for i := range count {
		row := firstRow + i
		start, end := m.wrapRowSourceColRange(row)
		isLast := i == count-1
		if isLast {
			if col >= start {
				return row, col - start
			}
		} else if col <= end {
			return row, col - start
		}
	}
	return firstRow, col
}

// stripGutterDisplayWidth returns the displayed width of a rendered
// viewport row's main content — ANSI stripped, trailing spaces trimmed,
// gutter (first gw chars) removed.
func stripGutterDisplayWidth(line string, gw int) int {
	return displayWidthOf(stripGutterText(line, gw))
}

// stripGutterText is the single source of truth for "the visible body of a
// rendered viewport line": ANSI codes removed, the gutter prefix removed, and
// trailing padding trimmed — in that order.
//
// Order matters. Trimming first shrinks a blank line ("  12   ") below the
// gutter width, so the length guard declines to strip and the line-number
// digits survive as if they were content — leaking the gutter into copied text
// and into cursor-column math. The gutter is pure ASCII, so gw (a display
// width) is also a valid byte offset.
func stripGutterText(line string, gw int) string {
	stripped := stripANSIForWidth(line)
	if gw > 0 {
		if len(stripped) <= gw {
			return ""
		}
		stripped = stripped[gw:]
	}
	return strings.TrimRight(stripped, " ")
}

// visibleRange returns the [top, bottom] source-line range currently
// visible in the viewport. Source lines are mapped through any wrap /
// inline-removal formatting via viewportToSourceLine and
// viewportBottomSourceLine. The visible-range-as-a-pair pattern is
// already used implicitly by hunkTitleRight, progressPercent (bottom
// only), and drag selection — this method names it.
//
// Positions are line-only; file/document identity is paired with the
// range externally by callers that need it (see PLAN.md).
func (m *mainPane) visibleRange() Range {
	top := m.viewportToSourceLine()
	bottom := m.viewportBottomSourceLine()
	if bottom < top {
		bottom = top
	}
	return Range{
		Start: Position{SourceLine: top},
		End:   Position{SourceLine: bottom},
	}
}

// highlightSearch applies a contrasting background to matching text in each line.
func highlightSearch(content, query string) string {
	if query == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = highlightMatchInLine(line, query)
	}
	return strings.Join(lines, "\n")
}

// searchHighlightOpen is the SGR open-sequence for the search highlight, used
// to re-establish it after each inner reset inside a matched span (the same
// technique as applyDiffBg).
var searchHighlightOpen = styleOpenSeq(searchHighlightStyle)

// styleOpenSeq returns just the SGR open-sequence for a style, with no
// trailing reset.
func styleOpenSeq(st lipgloss.Style) string {
	rendered := st.Render("")
	return strings.TrimSuffix(strings.TrimSuffix(rendered, "\x1b[0m"), "\x1b[m")
}

// visibleIndex maps the *visible* text of an ANSI-styled line back to byte
// spans of the original styled line.
type visibleIndex struct {
	lower    string // visible text, lowercased per rune, for matching
	srcStart []int  // per lower byte: start byte offset in the styled line
	srcEnd   []int  // per lower byte: end byte offset in the styled line
}

// indexVisible walks line once, skipping ANSI escape sequences, and records
// for every byte of the lowercased visible text which byte span of the styled
// line produced it. Lowercasing per rune (rather than over the whole string)
// is what keeps the mapping valid even when a rune's lowercase form has a
// different byte length.
//
// Escape sequences are terminated on any letter, matching the package's other
// ANSI walkers (ansiAwareIterate, stripANSIForWidth).
//
// The decode is manual rather than `for i, r := range line` because that form
// cannot express the span of invalid UTF-8: it yields RuneError for a single
// bad byte, and utf8.RuneLen(RuneError) is 3, so the recorded span overshot
// the byte the rune actually occupied — corrupting spans mid-string and
// panicking with a slice-bounds error when the overshoot ran past the end.
// DecodeRuneInString's size is the real byte count in every case.
func indexVisible(line string) visibleIndex {
	var vi visibleIndex
	var lower []byte
	inEscape := false
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			i += size
			continue
		}
		if r == '\x1b' {
			inEscape = true
			i += size
			continue
		}
		start, end := i, i+size
		before := len(lower)
		lower = utf8.AppendRune(lower, unicode.ToLower(r))
		for range len(lower) - before {
			vi.srcStart = append(vi.srcStart, start)
			vi.srcEnd = append(vi.srcEnd, end)
		}
		i += size
	}
	vi.lower = string(lower)
	return vi
}

// highlightMatchInLine wraps matching substrings with the search highlight
// style, on text that may already contain ANSI escape codes.
//
// The search runs against the *visible* text, never the styled bytes. Doing it
// the other way round (as this used to) meant truecolor sequences — which are
// nothing but digits, semicolons and a terminating letter — matched queries
// like "2", ";" or "m", splicing the highlight into the middle of an escape
// sequence and dumping `7;194;161mhello` on screen as visible text. It also
// silently missed any match straddling a chroma token boundary, because an
// escape sequence sat between the query's characters.
func highlightMatchInLine(line, query string) string {
	if query == "" {
		return line
	}
	vi := indexVisible(line)
	q := strings.ToLower(query)
	if !strings.Contains(vi.lower, q) {
		return line
	}

	var out strings.Builder
	out.Grow(len(line))
	pos := 0  // byte offset into the styled line, already emitted
	from := 0 // byte offset into vi.lower, already searched
	for {
		idx := strings.Index(vi.lower[from:], q)
		if idx < 0 {
			break
		}
		ps := from + idx
		pe := ps + len(q)
		styledStart, styledEnd := vi.srcStart[ps], vi.srcEnd[pe-1]
		if styledStart >= pos {
			out.WriteString(line[pos:styledStart])
			// The matched span may itself contain escape sequences; re-emit the
			// highlight after each inner reset so it covers the whole match.
			out.WriteString(applyDiffBg(line[styledStart:styledEnd], searchHighlightOpen))
			pos = styledEnd
		}
		from = pe
	}
	out.WriteString(line[pos:])
	return out.String()
}

// ansiAwareIterate calls fn for each rune in line, passing the rune and its
// display width (0 for characters inside ANSI escape sequences, otherwise the
// rune's display width). It returns the total display width.
//
// Tabs need no special case: expandTabs removes them where content enters the
// pane, so anything reaching here is already spaces.
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
		w := runewidth.RuneWidth(r)
		fn(r, w)
		totalW += w
	}
	return totalW
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
			// No '\t' case: expandTabs already turned tabs into spaces at
			// the content boundary.
			isSpace := r == ' '
			if cur.Len() > 0 && isSpace != curIsSpace {
				tokens = append(tokens, token{text: cur.String(), displayW: curW, isSpace: curIsSpace})
				cur.Reset()
				curW = 0
			}
			curIsSpace = isSpace
			cur.WriteRune(r)
			curW += runewidth.RuneWidth(r)
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
	// m.content is the *unformatted* source, so maxContentWidth() already
	// excludes the gutter — only the on-screen width has the gutter taken out
	// of it. Subtracting the gutter from both terms (as this once did) clamps
	// gutterWidth columns early and makes the tail of the widest line
	// permanently unreachable.
	maxWidth := m.maxContentWidth()
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
	var sc diffScanner
	for i, line := range lines {
		switch sc.classify(line) {
		case rowFileHeader, rowDiffHeader:
			lines[i] = diffHeaderStyle.Render(line)
		case rowHunkHeader:
			lines[i] = diffHunkStyle.Render(line)
		case rowAdd:
			lines[i] = diffAddStyle.Render(line)
		case rowRemove:
			lines[i] = diffRemoveStyle.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
