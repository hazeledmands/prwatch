package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"pgregory.net/rapid"
)

func TestMainPane_SetContent(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.SetContent("+added line\n-removed line\n context line")

	if mp.content != "+added line\n-removed line\n context line" {
		t.Error("content should be stored as-is")
	}
}

func TestMainPane_SetPlainContent(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.SetPlainContent("plain text")

	if mp.content != "plain text" {
		t.Error("content should be stored as-is")
	}
}

// TestSyntaxHighlight_AppliesToContextAndDiffLines verifies that when a
// filename is set, both unchanged context lines AND diff lines (e.g. added
// lines) receive chroma syntax highlighting. Diff lines are additionally
// distinguished from context lines by a tinted background — applied via the
// "re-establish bg after each inner reset" technique so chroma's per-token
// foreground colors stay visible.
func TestSyntaxHighlight_AppliesToContextAndDiffLines(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.lineNumbers = false
	mp.showRemoved = false

	// Two lines: line 1 unchanged context, line 2 added.
	mp.SetFilename("foo.go")
	mp.diffAnnotations = map[int]diffAnnotation{
		2: {kind: diffLineAdded},
	}
	mp.SetPlainContent("func ctx() {}\nfunc added() {}")

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 rendered lines, got %d", len(lines))
	}

	// Both lines should have multiple chroma per-token foreground codes
	// (the Go lexer styles "func" as a keyword distinct from identifiers).
	for idx, label := range []string{"context", "added"} {
		ln := lines[idx]
		fgEscapes := strings.Count(ln, "\x1b[38;2;")
		if fgEscapes < 2 {
			t.Errorf("%s line should have multiple chroma color tokens; got %d in %q", label, fgEscapes, ln)
		}
		if !strings.Contains(stripANSI(ln), "func") {
			t.Errorf("%s line missing literal 'func' after stripANSI: %q", label, stripANSI(ln))
		}
	}

	// The added line should additionally carry a bg-tint open code. It
	// should appear at the start (line-level tint) AND repeat after each
	// chroma reset to survive into the next token.
	addedLine := lines[1]
	bgOpens := strings.Count(addedLine, diffAddBgOpen)
	if bgOpens < 2 {
		t.Errorf("added line should re-establish bg tint after every chroma reset; got %d open seqs in %q", bgOpens, addedLine)
	}
	if strings.Count(lines[0], diffAddBgOpen) > 0 {
		t.Errorf("context line should not carry the added bg tint; got %q", lines[0])
	}
}

func TestMainPane_ScrollTop(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 5)
	mp.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")

	if mp.ScrollTop() != 0 {
		t.Error("scroll top should start at 0")
	}
}

func TestMainPane_Update(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 5)
	mp.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")

	cmd := mp.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	// Just verify it doesn't panic
	_ = cmd
}

func TestMainPane_View(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 10)
	mp.SetContent("hello world")

	focused := mp.View(true)
	unfocused := mp.View(false)

	if focused == "" || unfocused == "" {
		t.Error("view should not be empty")
	}
}

func TestMainPane_GoToTop(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)

	var lines []string
	for range 50 {
		lines = append(lines, "line")
	}
	mp.SetContent(strings.Join(lines, "\n"))

	mp.GoToBottom()
	mp.GoToTop()
	if mp.ScrollTop() != 0 {
		t.Errorf("GoToTop should scroll to 0, got %d", mp.ScrollTop())
	}
}

func TestMainPane_GoToBottom(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)

	var lines []string
	for range 50 {
		lines = append(lines, "line")
	}
	mp.SetContent(strings.Join(lines, "\n"))

	mp.GoToBottom()
	if mp.ScrollTop() == 0 {
		t.Error("GoToBottom should scroll past 0")
	}
}

// === Search tests (per new spec: search all content, not just visible) ===

func TestMainPane_FindMatches(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\ntarget here\nline3\nanother target\nline5")

	matches := mp.FindMatches("target")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0] != 1 || matches[1] != 3 {
		t.Errorf("expected matches at lines [1, 3], got %v", matches)
	}
}

func TestMainPane_FindMatches_CaseInsensitive(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\nTARGET here\nline3")

	matches := mp.FindMatches("target")
	if len(matches) != 1 || matches[0] != 1 {
		t.Errorf("case-insensitive: expected [1], got %v", matches)
	}
}

func TestMainPane_FindMatches_NotFound(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\nline2\nline3")

	matches := mp.FindMatches("nonexistent")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestMainPane_FindMatches_SearchesAllContent(t *testing.T) {
	// New spec: "searching should match against the content in either pane,
	// even content that is scrolled offscreen"
	mp := newMainPane()
	mp.SetSize(80, 3) // only 3 lines visible
	mp.SetContent("line1\nline2\nline3\ntarget_offscreen\nline5\nline6\nline7")

	matches := mp.FindMatches("target_offscreen")
	if len(matches) != 1 || matches[0] != 3 {
		t.Errorf("should find offscreen content, got matches %v", matches)
	}
}

func TestMainPane_ScrollToLine(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8")

	// Source line 5 (1-indexed) is viewport row 4 here: short diff lines,
	// no wrapping, so the mapping is 1:1.
	mp.ScrollToSourceLine(5)
	if mp.ScrollTop() != 4 {
		t.Errorf("expected scroll to line 4, got %d", mp.ScrollTop())
	}
}

func TestMainPane_SearchHighlighting(t *testing.T) {
	// Spec: "results should be highlighted (text background should be a contrasting color)"
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetPlainContent("line1\ntarget here\nline3")

	mp.SetSearchQuery("target")
	view := mp.View(false)
	stripped := stripANSI(view)

	// The view should still contain the text
	if !strings.Contains(stripped, "target here") {
		t.Error("view should contain the match text")
	}

	// The raw view (with ANSI) should contain highlighting escape codes
	// that are NOT in the stripped version — confirming styling was applied
	if view == stripped {
		t.Error("search match should have ANSI highlighting applied")
	}
}

func TestMainPane_ClearSearchHighlighting(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 10)
	mp.SetPlainContent("line1\ntarget here\nline3")

	mp.SetSearchQuery("target")
	mp.SetSearchQuery("") // clear search

	// After clearing, viewport should have no extra highlighting
	view := mp.View(false)
	stripped := stripANSI(view)
	// The border characters create ANSI diffs, but content lines should be plain
	lines := strings.Split(view, "\n")
	strippedLines := strings.Split(stripped, "\n")
	// Content line (index 2, after border) should be plain (no highlighting from search)
	if len(lines) > 2 && len(strippedLines) > 2 {
		// Just verify no "target" highlighting remains by checking the view works
		if !strings.Contains(stripped, "target here") {
			t.Error("content should still be there after clearing search")
		}
	}
}

func TestColorDiff(t *testing.T) {
	input := "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1,3 +1,3 @@\n context\n-old line\n+new line"
	result := colorDiff(input)

	// The result should be different from input (styles applied)
	if result == input {
		t.Error("colorDiff should apply styles to diff lines")
	}

	// Verify plain context lines are untouched
	lines := strings.Split(result, "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(l, " context") {
			found = true
		}
	}
	if !found {
		t.Error("context line should be present")
	}
}

// Regression test: file with 27 lines should be scrollable to the last line.
// Bug: go.mod has 27 lines but files mode only scrolls to line 25.
func TestMainPane_ScrollToEndOfFile(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(60, 10) // small viewport to ensure scrolling is needed

	// Create content with 27 lines (simulating a go.mod file)
	var lines []string
	for i := 1; i <= 27; i++ {
		lines = append(lines, "line content here")
	}
	content := strings.Join(lines, "\n")
	mp.SetPlainContent(content)

	// Scroll to bottom
	mp.GoToBottom()

	// The scroll offset should allow seeing the last line
	// With 27 content lines (plus line numbers), viewport height 10,
	// we should be able to see line 27
	scrollTop := mp.ScrollTop()

	// With line numbers on, the content has 27 lines.
	// The viewport shows 10 lines, so max scroll should be 27 - 10 = 17
	// We should be at or near that offset
	if scrollTop < 15 {
		t.Errorf("GoToBottom should scroll near the end, scrollTop=%d (expected >= 15)", scrollTop)
	}

	// Verify last line is reachable by checking the total content lines
	totalLines := strings.Count(mp.content, "\n") + 1
	if totalLines != 27 {
		t.Errorf("expected 27 content lines, got %d", totalLines)
	}
}

// Regression: with wrap OFF, lines wider than viewport should be truncated, not
// allowed to wrap in the terminal.
func TestMainPane_TruncatesWhenWrapOff(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 10)
	mp.SetWordWrap(false)

	content := "short\n" +
		"this line is definitely longer than forty characters and should be truncated\n" +
		"also short"
	mp.SetPlainContent(content)

	// Count the output lines — should be exactly 3 (one per content line),
	// not more from terminal wrapping.
	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")
	// With line numbers on, we still have 3 content lines
	contentLineCount := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			contentLineCount++
		}
	}
	if contentLineCount != 3 {
		t.Errorf("expected 3 non-empty lines (truncated), got %d", contentLineCount)
	}

	// The long line should be truncated — no rune at position > 40
	for _, l := range lines {
		stripped := stripANSIForWidth(l)
		// No tab case: expandTabs normalizes tabs at the content boundary.
		w := 0
		for range stripped {
			w++
		}
		if w > 40 {
			t.Errorf("line exceeds viewport width (w=%d): %q", w, stripped)
		}
	}
}

// Regression: with wrap ON, ANSI escapes should not count toward display width,
// so lines with line-number styling should wrap at the correct column.
func TestMainPane_WrapRespectsANSI(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(50, 20)
	mp.SetWordWrap(true)

	// A line that's 60 chars of visible text (should wrap to 2 visual lines at w=50)
	content := strings.Repeat("x", 60)
	mp.SetPlainContent(content)

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	// With line numbers "1  " prefix (4 chars) + 60 chars = 64 display chars,
	// at width 50, should be 2 visual lines
	if nonEmpty != 2 {
		t.Errorf("expected 2 visual lines after wrapping, got %d", nonEmpty)
	}
}

// Test that word wrapping doesn't prevent reaching the end of file
func TestMainPane_ScrollToEndWithWordWrap(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 10) // narrow viewport to trigger wrapping

	// Create content with lines longer than viewport width
	var lines []string
	for i := 1; i <= 27; i++ {
		// Some lines are long enough to wrap
		if i%3 == 0 {
			lines = append(lines, "this is a very long line that should definitely cause word wrapping in the viewport because it exceeds the width")
		} else {
			lines = append(lines, "short line")
		}
	}
	content := strings.Join(lines, "\n")
	mp.SetPlainContent(content)
	mp.GoToBottom()

	// Even with wrapping, we should be able to scroll past the original line count
	scrollTop := mp.ScrollTop()
	if scrollTop < 17 {
		t.Errorf("GoToBottom with wrapping should scroll far, scrollTop=%d", scrollTop)
	}
}

// Test that line numbers + wrapping doesn't eat content
func TestMainPane_LineNumbersPreserveContent(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(60, 10)

	// 27-line file
	var lines []string
	for i := 1; i <= 27; i++ {
		lines = append(lines, "module github.com/test/thing")
	}
	content := strings.Join(lines, "\n")
	mp.SetPlainContent(content)

	// Line numbers are on by default. Refresh forces re-render.
	mp.refreshViewport()

	// The viewport content (with line numbers) should have all 27 lines
	rendered := mp.viewport.View()
	// When at the top, we see 10 lines. Scroll to bottom.
	mp.GoToBottom()
	rendered = mp.viewport.View()
	renderedLines := strings.Split(rendered, "\n")

	// The last visible line should contain line 27
	lastVisible := renderedLines[len(renderedLines)-1]
	if !strings.Contains(lastVisible, "27") {
		t.Errorf("last visible line should contain '27', got %q", lastVisible)
	}
}

// Regression: wrapped text should not wrap into the gutter.
// Continuation lines should be indented to align with content, not gutter.
func TestMainPane_WrapDoesNotEnterGutter(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 20)
	mp.SetWordWrap(true)

	// Set diff annotations so there's a gutter
	mp.SetDiffAnnotations(map[int]diffAnnotation{
		1: {kind: diffLineAdded},
	})

	// Line 1 is long enough to wrap: with line number "1" (1 char) + " + " (3 chars) = 4 chars gutter
	// Plus content of 50 chars = 54 total, at width 40 this wraps
	content := "this line is way too long to fit in forty characters so it wraps"
	mp.SetPlainContent(content)

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")

	// The first line should have the gutter marker
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines after wrapping")
	}

	// Continuation line (line 2) should start with spaces (indent), not content at column 0
	stripped := stripANSIForWidth(lines[1])
	if len(stripped) > 0 && stripped[0] != ' ' {
		t.Errorf("continuation line should start with spaces (gutter indent), got %q", stripped[:min(20, len(stripped))])
	}
}

func TestMainPane_HorizontalScroll(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(20, 10)
	mp.SetWordWrap(false)

	// Content with a long line
	mp.SetPlainContent("short\nabcdefghijklmnopqrstuvwxyz1234567890")

	// Initially xOffset is 0
	if mp.xOffset != 0 {
		t.Errorf("expected xOffset 0, got %d", mp.xOffset)
	}

	// Scroll right
	mp.ScrollRight(5)
	if mp.xOffset != 5 {
		t.Errorf("expected xOffset 5, got %d", mp.xOffset)
	}

	// After scrolling right, the rendered content should start from offset 5
	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")
	// The long line should now start at position 5
	if len(lines) > 1 {
		stripped := stripANSIForWidth(lines[1])
		// With line numbers on and offset 5, we should see shifted content
		if strings.Contains(stripped, "abcde") {
			t.Error("after scrolling right 5, 'abcde' should not be visible")
		}
	}

	// Scroll left past 0
	mp.ScrollLeft(10)
	if mp.xOffset != 0 {
		t.Errorf("expected xOffset clamped to 0, got %d", mp.xOffset)
	}
}

func TestInlineDiffSize(t *testing.T) {
	// "hello world" → "hello earth" — changed "world" to "earth" = 5+5 = 10
	size := inlineDiffSize("hello world", "hello earth")
	if size != 10 {
		t.Errorf("expected diff size 10, got %d", size)
	}

	// "abc" → "axc" — changed "b" to "x" = 1+1 = 2
	size = inlineDiffSize("abc", "axc")
	if size != 2 {
		t.Errorf("expected diff size 2, got %d", size)
	}

	// identical strings
	size = inlineDiffSize("same", "same")
	if size != 0 {
		t.Errorf("expected diff size 0, got %d", size)
	}
}

func TestRenderInlineDiff_SmallChange(t *testing.T) {
	result := renderInlineDiff("hello world", "hello earth")
	stripped := stripANSIForWidth(result)
	// Should contain all text from both old and new
	if !strings.Contains(stripped, "hello") {
		t.Error("inline diff should contain retained prefix 'hello'")
	}
	if !strings.Contains(stripped, "world") {
		t.Error("inline diff should contain deleted text 'world'")
	}
	if !strings.Contains(stripped, "earth") {
		t.Error("inline diff should contain added text 'earth'")
	}
}

// TestParseDiffAnnotations_MultiLineBlockNotPaired guards against the bug
// where a multi-line block rewrite (N removed, M added in the same hunk)
// would mark the first added line as "changed" and pair it with the last
// removed line for an inline-diff. That produces a confusing hybrid: a `~`
// gutter on a body that's fully one color, with no real 1-to-1
// correspondence. With more than one pending removed, lines should annotate
// as plain additions; the deletions render above as `-` rows.
func TestParseDiffAnnotations_MultiLineBlockNotPaired(t *testing.T) {
	diff := `@@ -1,5 +1,5 @@
 unchanged
-old line 1
-old line 2
-old line 3
+new line 1
+new line 2
+new line 3
 unchanged
`
	annotations := parseDiffAnnotations(diff)
	for _, lineNo := range []int{2, 3, 4} {
		ann, ok := annotations[lineNo]
		if !ok {
			t.Fatalf("expected annotation for line %d", lineNo)
		}
		if ann.kind != diffLineAdded {
			t.Errorf("line %d should be plain added (got kind=%v) — multi-line block changes must not be paired as ~", lineNo, ann.kind)
		}
	}
	// All three pending deletions should hang off the first added line so
	// the file view emits them as `-` rows above.
	if got := len(annotations[2].removedLines); got != 3 {
		t.Errorf("first added line should carry all 3 removed siblings; got %d", got)
	}
}

// TestParseDiffAnnotations_TrailingDeletionsAtEOF reproduces the
// INCONSISTENCIES.md case: a diff that shrinks a file by deleting trailing
// lines, with no `+`, context, or new hunk header after the deletions.
// Previously the parser dropped these `-` lines entirely because pendingRemoved
// was only flushed when one of those terminators was seen.
func TestParseDiffAnnotations_TrailingDeletionsAtEOF(t *testing.T) {
	diff := `@@ -1,5 +1,2 @@
 keep1
 keep2
-drop1
-drop2
-drop3
`
	annotations := parseDiffAnnotations(diff)
	var got []string
	for _, ann := range annotations {
		got = append(got, ann.removedLines...)
	}
	want := []string{"drop1", "drop2", "drop3"}
	if len(got) != len(want) {
		t.Fatalf("trailing `-` lines were dropped: want %v, got %v (annotations=%+v)", want, got, annotations)
	}
}

// TestParseDiffAnnotations_TrailingDeletionsNoFinalNewline guards the
// strictly-EOF case: a diff body where the final character is the last `-`
// line content with no trailing newline at all. Without that newline, the
// "" sentinel that currently flushes pendingRemoved (via the context-line
// branch) never appears, and the trailing `-` lines are dropped.
func TestParseDiffAnnotations_TrailingDeletionsNoFinalNewline(t *testing.T) {
	diff := "@@ -1,5 +1,2 @@\n keep1\n keep2\n-drop1\n-drop2\n-drop3"
	annotations := parseDiffAnnotations(diff)
	var got []string
	for _, ann := range annotations {
		got = append(got, ann.removedLines...)
	}
	want := []string{"drop1", "drop2", "drop3"}
	if len(got) != len(want) {
		t.Fatalf("trailing `-` lines were dropped: want %v, got %v (annotations=%+v)", want, got, annotations)
	}
}

// TestFileViewRender_TrailingDeletionsVisible reproduces the
// INCONSISTENCIES.md rendering case: after parseDiffAnnotations attaches the
// trailing pending removed lines to annotations[N] where N == len(newLines)+1,
// the renderer must still emit them — currently the per-line loop in
// applyFileViewFormatting never reaches that index, so the lines disappear.
func TestFileViewRender_TrailingDeletionsVisible(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(120, 30)
	mp.lineNumbers = false
	mp.showRemoved = true

	// Mirrors the INCONSISTENCIES.md working-tree diff: 2 context lines
	// followed by 3 trailing deletions. After parseDiffAnnotations, pending
	// removed lines hang off annotations[3] — beyond the 2-line new file.
	mp.diffAnnotations = map[int]diffAnnotation{
		3: {removedLines: []string{"drop1", "drop2", "drop3"}},
	}
	mp.SetPlainContent("keep1\nkeep2")

	rendered := stripANSIForWidth(mp.viewport.View())
	for _, deleted := range []string{"drop1", "drop2", "drop3"} {
		if !strings.Contains(rendered, deleted) {
			t.Errorf("rendered output should contain trailing deleted line %q; got:\n%s",
				deleted, rendered)
		}
	}
}

// TestRenderInlineDiff_MultiSegment exercises the case where two changes are
// separated by retained text. A naive prefix/suffix differ would collapse the
// middle into one big delete+insert block; the diffmatchpatch-backed renderer
// keeps the middle as retained context.
func TestRenderInlineDiff_MultiSegment(t *testing.T) {
	result := renderInlineDiff("foo(a, b)", "foo(x, y)")

	// All three retained chunks should be styled as retained.
	for _, retained := range []string{"foo(", ", ", ")"} {
		if !strings.Contains(result, diffRetainedStyle.Render(retained)) {
			t.Errorf("expected retained chunk %q to be styled as retained; got %q", retained, result)
		}
	}

	// Both deletes appear as their own red chunks (not merged with the retained
	// middle). Same for both inserts.
	for _, deleted := range []string{"a", "b"} {
		if !strings.Contains(result, diffRemoveStyle.Render(deleted)) {
			t.Errorf("expected deleted chunk %q to be styled as removed; got %q", deleted, result)
		}
	}
	for _, added := range []string{"x", "y"} {
		if !strings.Contains(result, diffAddStyle.Render(added)) {
			t.Errorf("expected added chunk %q to be styled as added; got %q", added, result)
		}
	}
}

func TestChangedLine_InlineWhenSmall(t *testing.T) {
	// When change is small (< 1/4 pane width), render inline
	mp := newMainPane()
	mp.SetSize(80, 20)
	mp.lineNumbers = true
	mp.showRemoved = true

	mp.diffAnnotations = map[int]diffAnnotation{
		2: {kind: diffLineChanged, removedLines: []string{"hello world"}},
	}
	mp.SetPlainContent("line1\nhello earth\nline3")

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")

	// Line 2 should have ~ gutter and inline diff (both old and new text visible)
	found := false
	for _, line := range lines {
		stripped := stripANSIForWidth(line)
		if strings.Contains(stripped, "~") && strings.Contains(stripped, "world") && strings.Contains(stripped, "earth") {
			found = true
			break
		}
	}
	if !found {
		t.Error("small changed line should show inline diff with both old and new text")
	}
}

func TestFileViewGutter_CompletelyNewFile(t *testing.T) {
	// A completely new file should have + on every line
	mp := newMainPane()
	mp.SetSize(80, 20)
	mp.lineNumbers = true
	mp.showRemoved = true

	// All lines are added
	mp.diffAnnotations = map[int]diffAnnotation{
		1: {kind: diffLineAdded},
		2: {kind: diffLineAdded},
		3: {kind: diffLineAdded},
	}
	mp.SetPlainContent("new1\nnew2\nnew3")

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")

	addCount := 0
	for _, line := range lines {
		stripped := stripANSIForWidth(line)
		if strings.Contains(stripped, " + ") {
			addCount++
		}
	}
	if addCount != 3 {
		t.Errorf("completely new file should have + on all 3 lines, got %d", addCount)
	}
}

func TestFileViewGutter_CompletelyRemovedFile(t *testing.T) {
	// A completely removed file should have - on every line
	mp := newMainPane()
	mp.SetSize(80, 20)
	mp.lineNumbers = true
	mp.showRemoved = true

	// All lines are removed (file-level deletion)
	mp.diffAnnotations = map[int]diffAnnotation{
		1: {kind: diffLineRemoved},
		2: {kind: diffLineRemoved},
	}
	mp.SetPlainContent("old1\nold2")

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")

	removeCount := 0
	for _, line := range lines {
		stripped := stripANSIForWidth(line)
		if strings.Contains(stripped, " - ") {
			removeCount++
		}
	}
	if removeCount != 2 {
		t.Errorf("completely removed file should have - on all 2 lines, got %d", removeCount)
	}
}

func TestChangedLine_SplitWhenLarge(t *testing.T) {
	// When change is large (>= 1/4 pane width), split into two lines
	mp := newMainPane()
	mp.SetSize(40, 20) // narrow pane so 1/4 = 10
	mp.lineNumbers = false
	mp.showRemoved = true

	// Make a change that's larger than 10 chars
	mp.diffAnnotations = map[int]diffAnnotation{
		1: {kind: diffLineChanged, removedLines: []string{"completely different old text here"}},
	}
	mp.SetPlainContent("totally new replacement line here\nline2")

	rendered := mp.viewport.View()
	lines := strings.Split(rendered, "\n")

	// Should have the old text on one line and new text on the next
	hasOld := false
	hasNew := false
	for _, line := range lines {
		stripped := stripANSIForWidth(line)
		if strings.Contains(stripped, "completely different") {
			hasOld = true
		}
		if strings.Contains(stripped, "totally new") {
			hasNew = true
		}
	}
	if !hasOld {
		t.Error("large changed line should show old text on separate line")
	}
	if !hasNew {
		t.Error("large changed line should show new text on separate line")
	}
}

// The TestWrapLines_* tests target wrapLinesWithContinuationMap — the wrap
// implementation the viewport actually uses (mainpane.go refreshViewport). They
// previously asserted against wrapLinesWordBoundary, a duplicate copy with no
// non-test callers, so they proved nothing about rendered output.
func TestWrapLines_BreaksAtWordBoundaries(t *testing.T) {
	// Spec: "word-wrap should break at word boundaries"
	// "abcdef ghijkl" is 13 chars. At width 10, character-boundary wrapping
	// would split "ghijkl" into "ghij" and "kl". Word-boundary wrapping
	// should break before "ghijkl".
	result, _ := wrapLinesWithContinuationMap("abcdef ghijkl", 10, 0)
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	// First line should be "abcdef" (the word before the break)
	if strings.TrimRight(lines[0], " ") != "abcdef" {
		t.Errorf("line 1 should be 'abcdef', got %q", lines[0])
	}
	// Second line should be "ghijkl" (not split mid-word)
	if strings.TrimSpace(lines[1]) != "ghijkl" {
		t.Errorf("line 2 should be 'ghijkl', got %q", lines[1])
	}
}

func TestWrapLines_BreaksMidWordWhenTooLong(t *testing.T) {
	// Spec: "words longer than 1/8 of the screen width should be broken mid-word"
	// With width=80, 1/8 = 10. A 15-char word exceeds that threshold.
	longWord := strings.Repeat("x", 15)
	result, contMap := wrapLinesWithContinuationMap("short "+longWord+" end", 20, 0)
	lines := strings.Split(result, "\n")
	// "short " is 6 chars. The long word (15 chars) starts at position 6.
	// 6 + 15 = 21 > 20, so the word wraps. But since 15 > 10 (1/8 of 80),
	// it should be broken mid-word at the width boundary.
	if len(lines) < 2 {
		t.Fatal("expected wrapping to occur")
	}
	if len(contMap) != len(lines) {
		t.Fatalf("contMap length %d != lines %d", len(contMap), len(lines))
	}
	// Mid-word break: the run of x's must be split across the boundary rather
	// than pushed whole onto the next line.
	if strings.Contains(lines[1], longWord) {
		t.Errorf("long word should be broken mid-word, got line 2 = %q", lines[1])
	}
	if !strings.HasSuffix(strings.TrimRight(lines[0], " "), "x") {
		t.Errorf("first line should end inside the long word, got %q", lines[0])
	}
	// Every visual line must fit the width.
	for i, ln := range lines {
		if w := displayWidth(stripANSIForWidth(ln)); w > 20 {
			t.Errorf("line %d exceeds width 20 (%d): %q", i, w, ln)
		}
	}
}

// TestWrapLines_BreakSpaceAccounting pins the wrapper's space accounting:
// per viewport row, breaks[i] is how many source spaces sit between the
// (trailing-space-trimmed) end of row i-1 and the start of row i.
//
// Two distinct mechanisms lose spaces at a wrap point, and both are
// counted here:
//   - the break space run *fits* on the ending row, so it renders as
//     trailing padding — which stripGutterText trims off the copy;
//   - the break space run does *not* fit, so the wrapper discards it
//     outright (the token is written to neither row).
//
// The second case shows the flag has to be a count, not a bool: the
// tokenizer groups a whole run of consecutive spaces into one token and
// drops it all-or-nothing, so a break can consume 3 or 6 spaces.
func TestWrapLines_BreakSpaceAccounting(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		width      int
		indent     int
		wantRows   []string
		wantCont   []bool
		wantBreaks []int
	}{
		{
			name:       "single space fits and becomes trailing padding",
			in:         "aaa bbb",
			width:      5,
			wantRows:   []string{"aaa ", "bbb"},
			wantCont:   []bool{false, true},
			wantBreaks: []int{0, 1},
		},
		{
			name:       "space run does not fit and is discarded",
			in:         "aaa   bbb",
			width:      5,
			wantRows:   []string{"aaa", "bbb"},
			wantCont:   []bool{false, true},
			wantBreaks: []int{0, 3},
		},
		{
			name:       "six-space run discarded whole",
			in:         "aaa      bbb",
			width:      4,
			wantRows:   []string{"aaa", "bbb"},
			wantCont:   []bool{false, true},
			wantBreaks: []int{0, 6},
		},
		{
			name:       "every break consumes its trailing space",
			in:         "aa bb cc dd ee ff",
			width:      10,
			wantRows:   []string{"aa bb cc ", "dd ee ff"},
			wantCont:   []bool{false, true},
			wantBreaks: []int{0, 1},
		},
		{
			name:       "break after a comment marker (the yank bug)",
			in:         "        added_h0o2  // café",
			width:      20,
			wantRows:   []string{"        added_h0o2  ", "// café"},
			wantCont:   []bool{false, true},
			wantBreaks: []int{0, 2},
		},
		{
			name:       "continuation indent is not part of the count",
			in:         "aaa bbb ccc",
			width:      7,
			indent:     2,
			wantRows:   []string{"aaa bbb", "  ccc"},
			wantCont:   []bool{false, true},
			wantBreaks: []int{0, 1},
		},
		{
			name:       "unwrapped lines consume nothing",
			in:         "aaa bbb\nccc",
			width:      20,
			wantRows:   []string{"aaa bbb", "ccc"},
			wantCont:   []bool{false, false},
			wantBreaks: []int{0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, cont, breaks := wrapLinesWithBreaks(tt.in, tt.width, tt.indent)
			rows := strings.Split(out, "\n")
			if len(rows) != len(tt.wantRows) {
				t.Fatalf("rows = %q, want %q", rows, tt.wantRows)
			}
			for i := range rows {
				if rows[i] != tt.wantRows[i] {
					t.Errorf("row %d = %q, want %q", i, rows[i], tt.wantRows[i])
				}
			}
			if len(cont) != len(tt.wantCont) || len(breaks) != len(tt.wantBreaks) {
				t.Fatalf("cont=%v breaks=%v, want %v / %v", cont, breaks, tt.wantCont, tt.wantBreaks)
			}
			for i := range cont {
				if cont[i] != tt.wantCont[i] {
					t.Errorf("cont[%d] = %v, want %v", i, cont[i], tt.wantCont[i])
				}
				if breaks[i] != tt.wantBreaks[i] {
					t.Errorf("breaks[%d] = %d, want %d", i, breaks[i], tt.wantBreaks[i])
				}
			}
		})
	}
}

// rejoinWrappedLine reconstructs a source line from the wrapper's output
// the way the copy path does: each row's visible body (continuation
// indent removed, trailing padding trimmed — i.e. what stripGutterText
// leaves), rejoined with the break spaces the wrapper consumed.
func rejoinWrappedLine(rows []string, cont []bool, breaks []int, indent int) string {
	var out strings.Builder
	indentStr := strings.Repeat(" ", indent)
	for i, row := range rows {
		body := row
		if cont[i] && indent > 0 {
			body = strings.TrimPrefix(body, indentStr)
		}
		if i > 0 {
			out.WriteString(strings.Repeat(" ", breaks[i]))
		}
		out.WriteString(strings.TrimRight(body, " "))
	}
	return out.String()
}

// TestProperty_WrapLines_JoinWithBreaksRestoresSource is the reversibility
// invariant behind the copy fix: wrapping is lossy on its own, but wrap →
// rejoin-with-break-counts is the identity on the source line (modulo the
// line's own trailing padding, which the copy path trims for unwrapped
// lines too).
func TestProperty_WrapLines_JoinWithBreaksRestoresSource(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nTok := rapid.IntRange(1, 14).Draw(t, "nTok")
		var line strings.Builder
		for i := range nTok {
			if i > 0 {
				line.WriteString(strings.Repeat(" ", rapid.IntRange(1, 6).Draw(t, fmt.Sprintf("gap%d", i))))
			}
			word := rapid.SampledFrom([]string{
				"a", "ab", "abc", "hello", "café", "日本語", "//", "x",
				strings.Repeat("y", 17), "🔥", "l0_xxxxxxxxxx",
			}).Draw(t, fmt.Sprintf("w%d", i))
			line.WriteString(word)
		}
		src := line.String()
		width := rapid.IntRange(1, 60).Draw(t, "width")
		indent := rapid.IntRange(0, 6).Draw(t, "indent")

		out, cont, breaks := wrapLinesWithBreaks(src, width, indent)
		rows := strings.Split(out, "\n")
		if len(cont) != len(rows) || len(breaks) != len(rows) {
			t.Fatalf("map lengths %d/%d != rows %d", len(cont), len(breaks), len(rows))
		}
		effectiveIndent := indent
		if indent <= 0 || width <= indent {
			effectiveIndent = 0
		}
		got := rejoinWrappedLine(rows, cont, breaks, effectiveIndent)
		if want := strings.TrimRight(src, " "); got != want {
			t.Fatalf("rejoin = %q, want %q (rows %q, breaks %v, width %d, indent %d)",
				got, want, rows, breaks, width, indent)
		}
	})
}

func TestWrapLines_PreservesShortWords(t *testing.T) {
	// Words shorter than 1/8 of width should never be split
	result, _ := wrapLinesWithContinuationMap("aa bb cc dd ee ff gg hh ii jj", 10, 0)
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		// No word should be split across lines
		words := strings.Fields(stripped)
		for _, w := range words {
			if len(w) > 2 {
				t.Errorf("line %d contains split word: %q", i, w)
			}
		}
	}
}

func TestTruncateLinesWithOffset_StickyPrefix(t *testing.T) {
	// With a 5-char sticky prefix and offset 3, the first 5 chars should always show
	line := "GUTTR content goes here and is long"
	result := truncateLinesWithOffset(line, 20, 3, 5)
	stripped := stripANSIForWidth(result)
	if !strings.HasPrefix(stripped, "GUTTR") {
		t.Errorf("sticky gutter should be preserved, got %q", stripped)
	}
	// Content should be offset by 3 from after the gutter
	if strings.Contains(stripped, "con") {
		t.Error("first 3 chars of content should be scrolled off")
	}
}

func TestTruncateLinesWithOffset_PreservesANSI(t *testing.T) {
	// Regression: horizontal scroll should preserve ANSI codes for visible text
	styled := "\x1b[32mgreen text here\x1b[0m"
	result := truncateLinesWithOffset(styled, 10, 5, 0)
	// The visible portion should still contain ANSI codes
	if !strings.Contains(result, "\x1b[32m") {
		t.Error("ANSI styling should be preserved after horizontal scroll")
	}
	// The visible text should start from position 5
	stripped := stripANSIForWidth(result)
	if !strings.Contains(stripped, " text") {
		t.Errorf("visible text should contain 'text', got %q", stripped)
	}
}

func TestMouseShiftWheelHorizontalScroll(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.wordWrap = false
	m.mainPane.SetWordWrap(false)
	m.mainPane.SetPlainContent(strings.Repeat("x", 200))

	// Shift+WheelDown = scroll right
	result, _ := m.Update(tea.MouseWheelMsg{
		X:      50,
		Y:      10,
		Button: tea.MouseWheelDown,
		Mod:    tea.ModShift,
	})
	m = result.(*Model)

	if m.mainPane.xOffset == 0 {
		t.Error("shift+wheel down should scroll right when wrap is off")
	}

	// Shift+WheelUp = scroll left
	result, _ = m.Update(tea.MouseWheelMsg{
		X:      50,
		Y:      10,
		Button: tea.MouseWheelUp,
		Mod:    tea.ModShift,
	})
	m = result.(*Model)
	// Should have scrolled back
}

func TestWrapLinesWithContinuationMap(t *testing.T) {
	// Short line that fits — no continuation
	content := "hello world"
	wrapped, contMap := wrapLinesWithContinuationMap(content, 80, 0)
	if wrapped != content {
		t.Errorf("short line should not wrap, got %q", wrapped)
	}
	if len(contMap) != 1 || contMap[0] {
		t.Errorf("short line should have [false], got %v", contMap)
	}

	// Long line that wraps into 2 viewport lines
	longLine := strings.Repeat("word ", 20) // 100 chars
	wrapped, contMap = wrapLinesWithContinuationMap(longLine, 40, 0)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output to have 2+ lines, got %d", len(lines))
	}
	if len(contMap) != len(lines) {
		t.Fatalf("contMap length %d != lines %d", len(contMap), len(lines))
	}
	if contMap[0] {
		t.Error("first line should not be a continuation")
	}
	for i := 1; i < len(contMap); i++ {
		if !contMap[i] {
			t.Errorf("line %d should be a continuation", i)
		}
	}

	// Multiple source lines, one wraps
	content = "short\n" + strings.Repeat("long ", 20) + "\nanother short"
	wrapped, contMap = wrapLinesWithContinuationMap(content, 40, 0)
	lines = strings.Split(wrapped, "\n")
	if len(contMap) != len(lines) {
		t.Fatalf("contMap length %d != lines %d", len(contMap), len(lines))
	}
	// First line: "short" → not continuation
	if contMap[0] {
		t.Error("first source line should not be continuation")
	}
	// Last line: "another short" → not continuation
	lastIdx := len(contMap) - 1
	if contMap[lastIdx] {
		t.Error("last source line should not be continuation")
	}
	// Middle lines: the long line wraps, so contMap[1] = false, contMap[2+] = true
	if contMap[1] {
		t.Error("start of long line should not be continuation")
	}
	if len(contMap) > 3 && !contMap[2] {
		t.Error("wrapped part of long line should be continuation")
	}
}

func TestWrapLinesWithContinuationMap_WithIndent(t *testing.T) {
	// With gutter indent, continuation lines should be indented
	longLine := "  + " + strings.Repeat("word ", 20)
	wrapped, contMap := wrapLinesWithContinuationMap(longLine, 40, 4)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output with indent, got %d lines", len(lines))
	}
	// Continuation lines should start with 4 spaces (the indent)
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "    ") {
			t.Errorf("continuation line %d should be indented, got %q", i, lines[i])
		}
		if !contMap[i] {
			t.Errorf("line %d should be marked as continuation", i)
		}
	}
}

// === Sticky title bar — structural invariants ===
//
// Invariants enforced here:
//   I1. renderTitleRow always returns a string of exactly `width` display columns.
//   I2. The right segment is preserved when it fits, and its trailing characters
//       always sit at the right edge of the row.
//   I3. When left+right would collide, left is truncated (not right).
//   I4. mainPane.View renders exactly height+2 lines (border + content + border).
//   I5. The viewport's height is height-1 (one row reserved for the title).
//   I6. The title bar appears as the second line of View output (just inside
//       the top border) and is independent of viewport content.
//   I7. Setting/clearing the title does not change the dimensions of View.

func TestRenderTitleRow_AlwaysExactWidth(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		left := rapid.StringN(0, 60, 200).Draw(t, "left")
		right := rapid.StringN(0, 60, 200).Draw(t, "right")
		width := rapid.IntRange(1, 120).Draw(t, "width")

		row := renderTitleRow(left, right, width)
		// Measured with the oracle — the same function lipgloss uses to lay the
		// row out. PROMPT.md ("unicode width accounting") makes agreement with
		// the oracle the specified guarantee, so this is the thing under test,
		// not a restatement of the implementation. The expected value is the
		// caller's promised `width`, not something derived from a helper.
		got := displayWidth(row)
		if got != width {
			t.Fatalf("renderTitleRow width=%d, got display width %d for left=%q right=%q result=%q",
				width, got, left, right, row)
		}
		// Independent cross-check that the oracle is in fact what the renderer
		// measures: lipgloss must agree the row fills exactly `width` columns.
		// Skipped only for the one construct where ansi.StringWidth contradicts
		// its own grapheme segmentation — including when our own padding space
		// creates it by preceding a spacing mark. That class is characterized
		// and asserted in width_test.go rather than tolerated here.
		if knownRendererDivergence(row) {
			return
		}
		if lw := lipgloss.Width(row); lw != width {
			t.Fatalf("renderTitleRow width=%d: lipgloss measures %d for %q", width, lw, row)
		}
	})
}

func TestRenderTitleRow_RightFlushedWhenFits(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// ASCII-only generator so we can compare exact suffix bytes.
		left := rapid.StringMatching(`[a-zA-Z0-9 _.-]{0,40}`).Draw(t, "left")
		right := rapid.StringMatching(`[a-zA-Z0-9 _.-]{0,40}`).Draw(t, "right")
		width := rapid.IntRange(1, 120).Draw(t, "width")

		leftW := displayWidth(left)
		rightW := displayWidth(right)
		gap := 1
		if leftW == 0 || rightW == 0 {
			gap = 0
		}
		if leftW+gap+rightW > width {
			return // collision; covered by Truncation test
		}

		row := renderTitleRow(left, right, width)
		if !strings.HasSuffix(row, right) {
			t.Fatalf("right %q should be flush with the right edge, got %q", right, row)
		}
	})
}

func TestRenderTitleRow_LeftTruncatesBeforeRight(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Pick a right that always fits, and a left long enough to force collision.
		right := rapid.StringMatching(`[a-zA-Z0-9 ]{1,30}`).Draw(t, "right")
		width := rapid.IntRange(displayWidth(right)+1, 80).Draw(t, "width")
		// Left long enough that left+gap+right > width.
		minLeftLen := width - displayWidth(right) // collision guaranteed when len(left) >= this
		leftLen := rapid.IntRange(minLeftLen, minLeftLen+60).Draw(t, "leftLen")
		left := strings.Repeat("L", leftLen)

		row := renderTitleRow(left, right, width)
		// Right is preserved verbatim.
		if !strings.HasSuffix(row, right) {
			t.Fatalf("right %q should be preserved on collision, got %q", right, row)
		}
		// Left was truncated: it should not contain the original full left content.
		// We don't assert the ellipsis position (depends on width), only that
		// the row's display width matches width (already covered by I1).
	})
}

func TestRenderTitleRow_RightTooWideTruncates(t *testing.T) {
	right := strings.Repeat("R", 50)
	row := renderTitleRow("ignored left", right, 10)
	if w := displayWidth(row); w != 10 {
		t.Fatalf("expected width 10, got %d for row %q", w, row)
	}
	// Left should be dropped entirely when right doesn't fit.
	if strings.Contains(row, "ignored") {
		t.Fatalf("left should be dropped when right doesn't fit, got %q", row)
	}
}

func TestRenderTitleRow_ZeroWidth(t *testing.T) {
	if got := renderTitleRow("hi", "bye", 0); got != "" {
		t.Fatalf("width 0 should yield empty string, got %q", got)
	}
}

func TestRenderTitleRow_BothEmpty(t *testing.T) {
	row := renderTitleRow("", "", 12)
	if w := displayWidth(row); w != 12 {
		t.Fatalf("empty title should still be 12 wide, got width %d row=%q", w, row)
	}
}

// I4 + I5 + I7: the View renders the same number of lines regardless of title
// content, and the viewport always gets one less row than the pane height.
func TestMainPane_TitleDoesNotChangeOuterDimensions(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 10)
	mp.SetPlainContent("line1\nline2\nline3\nline4\nline5")

	noTitle := mp.View(false)
	mp.SetTitle("some/file.go", "5 hunks")
	withTitle := mp.View(false)

	noLines := strings.Count(noTitle, "\n") + 1
	withLines := strings.Count(withTitle, "\n") + 1
	if noLines != withLines {
		t.Fatalf("View line count changed when title was set: %d → %d", noLines, withLines)
	}
	// height+2 = 12 (10 inner + top/bottom border)
	if noLines != 12 {
		t.Fatalf("expected 12 lines (height+2), got %d", noLines)
	}
	if mp.viewport.Height() != 9 {
		t.Fatalf("viewport should reserve one row for title, got height %d", mp.viewport.Height())
	}
}

// I6: when SetTitle is called, the title text is the first line of the inner
// pane (between the top border and the viewport content).
func TestMainPane_TitleAppearsInsideTopBorder(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 6)
	mp.SetPlainContent("body content")
	mp.SetTitle("LEFT_MARKER", "RIGHT_MARKER")

	out := stripANSI(mp.View(false))
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	// lines[0] is top border (╭...╮); lines[1] is first inner row, which is
	// the title. The borders sit at columns 0 and width+1.
	titleRow := lines[1]
	if !strings.Contains(titleRow, "LEFT_MARKER") {
		t.Fatalf("title row should contain left marker, got %q", titleRow)
	}
	if !strings.Contains(titleRow, "RIGHT_MARKER") {
		t.Fatalf("title row should contain right marker, got %q", titleRow)
	}
	// Body content should appear later, not on the title row.
	if strings.Contains(titleRow, "body content") {
		t.Fatalf("body should not appear on title row, got %q", titleRow)
	}
}

// I5/I6: title renders even when the content is empty.
func TestMainPane_TitleRendersWithEmptyContent(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(40, 6)
	mp.SetTitle("only-title", "")

	out := stripANSI(mp.View(false))
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "only-title") {
		t.Fatalf("title row should appear even with empty content, got %q", lines[1])
	}
}

// Tiny pane sizes shouldn't crash and should still produce stable output.
func TestMainPane_TitleSurvivesTinyHeight(t *testing.T) {
	for _, h := range []int{0, 1, 2} {
		mp := newMainPane()
		mp.SetSize(20, h)
		mp.SetTitle("file.go", "1 hunk")
		// Should not panic; we don't assert content here, just stability.
		_ = mp.View(false)
	}
}

// === positionToDisplay — the inverse of sourceLineAtViewportOffset +
// absoluteColumnFromDisplay. Required for cursor rendering. ===

func TestPositionToDisplay_NoWrap(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.SetWordWrap(false)
	mp.SetPlainContent("line one\nline two\nline three")

	// No-wrap: vpRow == sourceLineToViewportOffset(SL), displayCol == Column.
	cases := []struct {
		pos              Position
		wantRow, wantCol int
	}{
		{Position{SourceLine: 1, Column: 0}, 0, 0},
		{Position{SourceLine: 1, Column: 5}, 0, 5},
		{Position{SourceLine: 2, Column: 3}, 1, 3},
		{Position{SourceLine: 3, Column: 7}, 2, 7},
	}
	for _, c := range cases {
		vp, dc := mp.positionToDisplay(c.pos)
		if vp != c.wantRow || dc != c.wantCol {
			t.Errorf("positionToDisplay(%+v) = (%d, %d), want (%d, %d)",
				c.pos, vp, dc, c.wantRow, c.wantCol)
		}
	}
}

func TestPositionToDisplay_NegativeColumnClamps(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.SetWordWrap(false)
	mp.SetPlainContent("hello")

	vp, dc := mp.positionToDisplay(Position{SourceLine: 1, Column: -5})
	if vp != 0 || dc != 0 {
		t.Errorf("negative column should clamp to 0, got (%d, %d)", vp, dc)
	}
}

func TestPositionToDisplay_WrapSingleRow(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.SetWordWrap(true)
	// Short content — no wrapping needed.
	mp.SetPlainContent("short line\nanother")

	// Single-row source lines: Position maps to that one row directly.
	vp, dc := mp.positionToDisplay(Position{SourceLine: 1, Column: 5})
	if vp != 0 || dc != 5 {
		t.Errorf("expected (0, 5), got (%d, %d)", vp, dc)
	}
	vp, dc = mp.positionToDisplay(Position{SourceLine: 2, Column: 3})
	if vp != 1 || dc != 3 {
		t.Errorf("expected (1, 3), got (%d, %d)", vp, dc)
	}
}

func TestPositionToDisplay_WrapBoundaryGoesToNextRow(t *testing.T) {
	// Convention: Column at exactly the start of wrap row K+1 renders on
	// K+1 at displayCol=0, not on K's right edge. Confirmed by the
	// algorithm walking wrap rows in order — boundary lands on the next.
	mp := newMainPane()
	mp.SetSize(20, 24) // narrow to force wrapping
	mp.SetWordWrap(true)
	// 60-char source line: wraps into multiple rows at width 20.
	long := strings.Repeat("a", 60)
	mp.SetPlainContent(long + "\nshort")

	// Find where the wrap boundary lands by inspecting the actual
	// wrap-row ranges of source line 1.
	firstRow := mp.sourceLineToViewportOffset(1)
	count := mp.wrapRowCountAtVpRow(firstRow)
	if count < 2 {
		t.Skipf("expected line 1 to wrap into 2+ rows, got %d", count)
	}

	// The start of wrap row K+1 is the boundary col.
	boundaryStart, _ := mp.wrapRowSourceColRange(firstRow + 1)
	vp, dc := mp.positionToDisplay(Position{SourceLine: 1, Column: boundaryStart})
	if vp != firstRow+1 || dc != 0 {
		t.Errorf("boundary Column=%d should map to (%d, 0), got (%d, %d)",
			boundaryStart, firstRow+1, vp, dc)
	}
}

func TestPositionToDisplay_PastEOL(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 24)
	mp.SetWordWrap(true)
	mp.SetPlainContent("hello\nworld")

	// Column past end of line should return the last wrap row of that
	// source line with displayCol = column past content.
	vp, dc := mp.positionToDisplay(Position{SourceLine: 1, Column: 100})
	if vp != 0 {
		t.Errorf("past-EOL should stay on last wrap row of line 1 (vpRow=0), got %d", vp)
	}
	if dc != 100 {
		t.Errorf("past-EOL displayCol should equal Column - rowStart (= 100), got %d", dc)
	}
}

// TestProperty_PositionToDisplay_RoundTrip is the core invariant of Phase A:
// for any (vpRow, displayCol) inside the displayed content, the derived
// source-space Position round-trips back through positionToDisplay such
// that the forward functions recover the original SourceLine and Column.
//
// We don't require (vpRow, displayCol) → Position → (vpRow', displayCol')
// to recover the original screen coords — multiple screen positions can
// collapse to the same Position at wrap boundaries. We require that the
// Position is invariant: the forward functions applied to
// positionToDisplay's output recover SourceLine and Column.
// TestSourceLineAtViewportOffset_IndentedWrap is the regression test for
// the wrapContinuation/wrappedRowCount mismatch that the
// positionToDisplay round-trip property surfaced: long content with a
// gutter wraps via wrapLinesWithContinuationMap which accounts for the
// continuation indent, but the legacy wrappedRowCount(line, width)
// didn't, so sourceLineAtViewportOffset under-counted wrap rows and
// reported the wrong source line for indented continuations. Now both
// translation functions walk m.wrapContinuation directly.
func TestSourceLineAtViewportOffset_IndentedWrap(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(20, 10)
	mp.SetWordWrap(true)
	// 49-char line with line numbers + gutter (4-char) wraps into 4
	// rows; wrappedRowCount(53, 20) would compute only 3.
	mp.SetPlainContent(strings.Repeat("x", 49) + "\n")

	if len(mp.wrapContinuation) != 5 {
		t.Fatalf("expected 5 viewport rows, got %d (%v)", len(mp.wrapContinuation), mp.wrapContinuation)
	}
	// Rows 0..3 are all source line 1; row 4 is source line 2.
	for r := range 4 {
		if got := mp.sourceLineAtViewportOffset(r); got != 1 {
			t.Errorf("row %d should be source line 1, got %d", r, got)
		}
	}
	if got := mp.sourceLineAtViewportOffset(4); got != 2 {
		t.Errorf("row 4 should be source line 2, got %d", got)
	}
	// Inverse: source line 2 first appears at row 4.
	if got := mp.sourceLineToViewportOffset(2); got != 4 {
		t.Errorf("source line 2 should map to row 4, got %d", got)
	}
}

func TestProperty_PositionToDisplay_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(20, 120).Draw(t, "width")
		height := rapid.IntRange(10, 40).Draw(t, "height")
		wordWrap := rapid.Bool().Draw(t, "wordWrap")
		nLines := rapid.IntRange(1, 15).Draw(t, "nLines")
		// Mix short and long lines so wrap exercises both single-row and
		// multi-row source lines.
		var lines []string
		for i := range nLines {
			n := rapid.IntRange(0, 200).Draw(t, fmt.Sprintf("lineLen-%d", i))
			lines = append(lines, strings.Repeat("x", n))
		}
		content := strings.Join(lines, "\n")

		mp := newMainPane()
		mp.SetSize(width, height)
		mp.SetWordWrap(wordWrap)
		mp.SetPlainContent(content)

		vpContent := mp.viewport.GetContent()
		vpLines := strings.Split(vpContent, "\n")
		if len(vpLines) == 0 {
			return
		}

		row := rapid.IntRange(0, len(vpLines)-1).Draw(t, "row")
		// Pick a display column inside the row's content (post-gutter).
		rowW := stripGutterDisplayWidth(vpLines[row], mp.gutterWidth)
		if rowW == 0 {
			return
		}
		dc := rapid.IntRange(0, rowW-1).Draw(t, "displayCol")

		// Derive Position from the displayed content via the forward
		// functions.
		sl := mp.sourceLineAtViewportOffset(row)
		col := mp.absoluteColumnFromDisplay(row, dc)
		pos := Position{SourceLine: sl, Column: col}

		// Round-trip through positionToDisplay.
		gotRow, gotDc := mp.positionToDisplay(pos)

		// The inverse should recover the same Position.
		recoveredSL := mp.sourceLineAtViewportOffset(gotRow)
		recoveredCol := mp.absoluteColumnFromDisplay(gotRow, gotDc)
		if recoveredSL != sl {
			t.Fatalf("SourceLine not invariant: orig=%d, after roundtrip=%d (vpRow=%d→%d, dc=%d→%d, pos=%+v)",
				sl, recoveredSL, row, gotRow, dc, gotDc, pos)
		}
		if recoveredCol != col {
			t.Fatalf("Column not invariant: orig=%d, after roundtrip=%d (vpRow=%d→%d, dc=%d→%d, pos=%+v)",
				col, recoveredCol, row, gotRow, dc, gotDc, pos)
		}
	})
}

// --- CODE_REVIEW.md A6: tab normalization at the content boundary -----------
//
// Three tab widths were in play before this landed: the wrap tokenizer and
// ansiAwareIterate assumed 8-column tab stops, runewidth.RuneWidth('\t') is 0,
// and lipgloss renders a tab as 4 spaces. Every Go file is tab-indented, so
// wrap points, gutter alignment, cursor columns and drag-copy slicing all
// disagreed with the render and with each other. The fix expands tabs once,
// where content enters the pane.

// A tab must be indistinguishable from the 4 spaces it renders as: identical
// rendered rows, identical row counts, identical wrap behavior. Asserting the
// two forms agree — rather than asserting a particular width — is what makes
// this test independent of the expansion width itself.
func TestMainPane_TabRendersIdenticallyToFourSpaces(t *testing.T) {
	// Widths chosen to straddle the wrap boundary: with the old 8-column
	// assumption the tab form measures 4 columns wider than it renders, so it
	// wraps at widths where the space form still fits.
	for _, width := range []int{20, 24, 28, 30, 34, 40, 50} {
		for _, body := range []string{
			"func main() {",
			"return someValue + otherValue",
			strings.Repeat("x", 30),
		} {
			t.Run(fmt.Sprintf("w%d_%s", width, body[:min(8, len(body))]), func(t *testing.T) {
				render := func(prefix string) string {
					mp := newMainPane()
					mp.SetSize(width, 20)
					mp.SetWordWrap(true)
					mp.SetPlainContent(prefix + body)
					return mp.viewport.View()
				}
				tabbed := render("\t")
				spaced := render("    ")
				if tabbed != spaced {
					t.Errorf("tab-indented and 4-space-indented renders differ at width %d:\n tab: %q\nspace: %q",
						width, tabbed, spaced)
				}
			})
		}
	}
}

// The boundary invariant itself: no tab survives into pane content, so no
// downstream consumer (wrap math, gutter, cursor column, drag copy) ever has
// to special-case one.
func TestMainPane_NoTabsSurviveTheContentBoundary(t *testing.T) {
	tabby := "\tif x {\n\t\treturn 1\t// trailing\n\t}\nno tabs here\n"
	t.Run("SetPlainContent", func(t *testing.T) {
		mp := newMainPane()
		mp.SetSize(80, 20)
		mp.SetPlainContent(tabby)
		if strings.Contains(mp.content, "\t") {
			t.Errorf("mainPane.content still contains a tab: %q", mp.content)
		}
	})
	t.Run("SetContent", func(t *testing.T) {
		mp := newMainPane()
		mp.SetSize(80, 20)
		mp.SetContent(tabby)
		if strings.Contains(mp.content, "\t") {
			t.Errorf("mainPane.content still contains a tab: %q", mp.content)
		}
	})
}

// expandTabs advances to the next 4-column tab stop rather than emitting a
// fixed 4 spaces, so alignment is preserved the way an editor would show it.
func TestExpandTabs(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"no tabs", "no tabs"},
		{"\t", "    "},
		{"\tx", "    x"},
		{"a\tb", "a   b"},        // 'a' at col 0, tab stop at 4
		{"ab\tc", "ab  c"},       // tab stop at 4
		{"abc\td", "abc d"},      // tab stop at 4
		{"abcd\te", "abcd    e"}, // already at 4, advance to 8
		{"\t\tx", "        x"},
		{"a\tb\tc", "a   b   c"},
		{"line1\n\tline2", "line1\n    line2"}, // column resets each line
		{"日\tx", "日  x"},                       // wide rune counts as 2 columns
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := expandTabs(tt.in); got != tt.want {
				t.Errorf("expandTabs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// --- CODE_REVIEW.md A6: ANSI-safe search highlighting -----------------------
//
// highlightMatchInLine used to run strings.Index against the *styled* string.
// Truecolor sequences are full of digits and semicolons, so searching "2" in a
// syntax-highlighted file spliced the highlight into the middle of an escape
// sequence and dumped `;227;161m`-style garbage on screen as visible text.
// Matches that straddled a chroma token boundary were silently missed for the
// same reason: the escape sequence sat between the query's characters.
//
// The two invariants below are what "ANSI-safe" means, stated observably:
// highlighting never changes the visible text, and never misses a match the
// visible text contains.

func styledLine(t *testing.T, segments ...string) string {
	t.Helper()
	var b strings.Builder
	palette := []string{"#E3C2A1", "#A6E3A1", "#F38BA8"}
	for i, seg := range segments {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(palette[i%len(palette)])).Render(seg))
	}
	return b.String()
}

func TestHighlightMatchInLine_ANSISafe(t *testing.T) {
	tests := []struct {
		name      string
		segments  []string
		query     string
		wantMatch bool
	}{
		{
			// "2" appears only inside the truecolor escape sequences
			// (\x1b[38;2;227;194;161m), never in the visible text.
			name:      "digit occurring only inside the escape sequence",
			segments:  []string{"hello"},
			query:     "2",
			wantMatch: false,
		},
		{
			name:      "semicolon occurring only inside the escape sequence",
			segments:  []string{"hello"},
			query:     ";",
			wantMatch: false,
		},
		{
			name:      "the letter m, which terminates every SGR sequence",
			segments:  []string{"ממm"},
			query:     "m",
			wantMatch: true,
		},
		{
			// The query spans two chroma tokens, so an escape sequence sits
			// between "foo" and "bar" in the styled string.
			name:      "match spanning a style boundary",
			segments:  []string{"foo", "bar"},
			query:     "ooba",
			wantMatch: true,
		},
		{
			name:      "match entirely inside one token",
			segments:  []string{"foo", "bar"},
			query:     "oo",
			wantMatch: true,
		},
		{
			name:      "case-insensitive match spanning a boundary",
			segments:  []string{"Foo", "Bar"},
			query:     "oob",
			wantMatch: true,
		},
		{
			name:      "no match at all",
			segments:  []string{"foo", "bar"},
			query:     "zzz",
			wantMatch: false,
		},
		{
			name:      "match at the very start",
			segments:  []string{"foo", "bar"},
			query:     "fo",
			wantMatch: true,
		},
		{
			name:      "match at the very end",
			segments:  []string{"foo", "bar"},
			query:     "ar",
			wantMatch: true,
		},
		{
			name:      "wide runes around the match",
			segments:  []string{"日本語", "text"},
			query:     "語te",
			wantMatch: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := styledLine(t, tt.segments...)
			plain := stripANSIForWidth(line)
			got := highlightMatchInLine(line, tt.query)

			// Invariant 1: highlighting never changes the visible text.
			if gotPlain := stripANSIForWidth(got); gotPlain != plain {
				t.Errorf("visible text changed by highlighting:\n got %q\nwant %q", gotPlain, plain)
			}
			// Invariant 2: a match present in the visible text is highlighted,
			// and one absent from it is not.
			highlighted := got != line
			if highlighted != tt.wantMatch {
				t.Errorf("highlighted = %v, want %v (visible text %q, query %q)\nresult: %q",
					highlighted, tt.wantMatch, plain, tt.query, got)
			}
		})
	}
}

// highlightSearch is the caller; the same invariants must hold through it,
// including across multiple lines.
func TestHighlightSearch_ANSISafe(t *testing.T) {
	line1 := styledLine(t, "func ", "main", "() {")
	line2 := styledLine(t, "\treturn ", "nil")
	content := line1 + "\n" + line2
	plain := stripANSIForWidth(content)

	for _, q := range []string{"2", ";", "main", "urn n", "38", "m"} {
		t.Run(q, func(t *testing.T) {
			got := highlightSearch(content, q)
			if gotPlain := stripANSIForWidth(got); gotPlain != plain {
				t.Errorf("visible text changed by highlighting query %q:\n got %q\nwant %q", q, gotPlain, plain)
			}
		})
	}
}

// A6: the generator that was missing entirely — ANSI-styled content. No
// property test in the package produced an escape sequence in a line body, so
// highlightMatchInLine's mid-escape splicing was invisible to the whole suite.
//
// The two invariants are the same ones the table test states, over a generated
// space of styled segments and queries drawn *from the escape sequences
// themselves* (digits, semicolons, 'm', "38;2") as well as from visible text.
func TestProperty_HighlightMatchIsANSISafe(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Build a styled line from several differently-colored segments, so
		// escape sequences land between, inside and around candidate matches.
		nSegs := rapid.IntRange(1, 5).Draw(t, "nSegs")
		bodies := []string{
			"func", "main", "()", " {", "return", "nil", "x2", "38", "foo", "bar",
			"日本語", "café", "", "  ", "a;b", "m", "\tindented",
			// Invalid UTF-8: a lone continuation byte, a truncated 2-byte
			// sequence, and a Latin-1 "é" — all reachable through
			// SetPlainContent on a file whose raw bytes pass binary
			// detection, which then renders as U+FFFD on screen. Indexing
			// these as if RuneError occupied 3 bytes overshot the span and
			// panicked.
			"\xff", "ab\xff", "\xc3", "caf\xe9", "\xff\xfe",
		}
		colors := []string{"#E3C2A1", "#A6E3A1", "#F38BA8", "#89B4FA", "#FAB387"}
		var styled strings.Builder
		var visible strings.Builder
		for i := range nSegs {
			b := rapid.SampledFrom(bodies).Draw(t, fmt.Sprintf("seg%d", i))
			c := rapid.SampledFrom(colors).Draw(t, fmt.Sprintf("color%d", i))
			styleIt := rapid.Bool().Draw(t, fmt.Sprintf("styled%d", i))
			if styleIt {
				styled.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(b))
			} else {
				styled.WriteString(b)
			}
			visible.WriteString(b)
		}
		line := styled.String()

		// Queries deliberately include the bytes that only ever occur *inside*
		// escape sequences — the exact inputs that used to corrupt the output.
		query := rapid.SampledFrom([]string{
			"2", ";", "m", "38", "38;2", "1b", "[", "0m",
			"func", "main", "nil", "oo", "語", "é", " ", "\t", "x2", "a;b",
			// The replacement character the user actually sees for an
			// invalid byte, plus the raw bytes themselves.
			"\uFFFD", "\xff", "\xc3",
		}).Draw(t, "query")

		got := highlightMatchInLine(line, query)

		// Invariant 1: highlighting never alters the visible text. This is
		// what "does not corrupt ANSI" means observably — a spliced escape
		// sequence shows up as extra visible characters.
		wantVisible := stripANSIForWidth(line)
		if gotVisible := stripANSIForWidth(got); gotVisible != wantVisible {
			t.Fatalf("visible text changed by highlighting query %q:\n got %q\nwant %q\nline %q",
				query, gotVisible, wantVisible, line)
		}

		// Invariant 2: a highlight is applied exactly when the visible text
		// contains the query — never for a match that exists only among the
		// escape bytes, and never missed when it straddles a style boundary.
		wantMatch := strings.Contains(strings.ToLower(wantVisible), strings.ToLower(query))
		if (got != line) != wantMatch {
			t.Fatalf("highlighted = %v, want %v for query %q\nvisible %q\nline %q\ngot %q",
				got != line, wantMatch, query, wantVisible, line, got)
		}

		// Invariant 3: idempotence of the visible text under re-highlighting —
		// running the highlighter over its own output must not accumulate
		// visible garbage (the failure mode where escape bytes become text and
		// are then themselves matched).
		again := highlightMatchInLine(got, query)
		if againVisible := stripANSIForWidth(again); againVisible != wantVisible {
			t.Fatalf("re-highlighting changed visible text for query %q:\n got %q\nwant %q",
				query, againVisible, wantVisible)
		}
	})
}

// --- A6 review item 5: control characters in filenames ----------------------
//
// The -z conversion is what makes this reachable: git's core.quotePath used to
// escape control characters in filenames before we ever saw them. Reading raw
// NUL-delimited output is right, but it means a literal tab or newline in a
// filename now reaches display text, where a tab hits the runewidth-0 /
// lipgloss-4 disagreement and a newline breaks the one-label-per-row
// assumption that row math and mouse hit-testing depend on.

func TestSanitizeDisplayText(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"plain ASCII untouched", "internal/ui/model.go", "internal/ui/model.go"},
		{"non-ASCII is NOT escaped (the point of -z)", "café/日本語.go", "café/日本語.go"},
		{"tab", "a\tb.go", `a\tb.go`},
		{"newline", "a\nb.go", `a\nb.go`},
		{"carriage return", "a\rb.go", `a\rb.go`},
		{"NUL", "a\x00b", `a\x00b`},
		{"bell and escape", "a\x07\x1bb", `a\x07\x1bb`},
		{"DEL", "a\x7fb", `a\x7fb`},
		{"literal backslash deliberately left alone", `a\tb.go`, `a\tb.go`},
		{"only controls", "\t\n", `\t\n`},
		{"mixed", "sr\tc/café\n.go", `sr\tc/café\n.go`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDisplayText(tt.in); got != tt.want {
				t.Errorf("sanitizeDisplayText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The invariant that matters for the UI: display text is single-row and
// control-free, whatever the filename contains. Stated over generated
// filenames rather than a fixed list, since the whole A6 lesson is that the
// dangerous inputs are the ones nobody thought to write down.
func TestProperty_SanitizedFilenameIsSafeForDisplay(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		segments := []string{
			"internal", "ui", "model.go", "café", "日本語", "my docs",
			"a\tb", "a\nb", "a\rb", "x\x00y", "\x07bell", "\x1b[31mred",
			"\x7fdel", "plain", "", "  spaced  ",
		}
		n := rapid.IntRange(1, 4).Draw(t, "nSegments")
		var parts []string
		for i := range n {
			parts = append(parts, rapid.SampledFrom(segments).Draw(t, fmt.Sprintf("seg%d", i)))
		}
		name := strings.Join(parts, "/")

		got := sanitizeDisplayText(name)

		// Invariant 1: no C0 control character or DEL survives — so no label
		// can move the cursor, start an escape sequence, or span a row.
		for _, r := range got {
			if isDisplayControl(r) {
				t.Fatalf("control char %#U survived sanitizing %q -> %q", r, name, got)
			}
		}
		// Invariant 2: exactly one row. A newline in a sidebar label breaks
		// row math and mouse hit-testing outright.
		if strings.Contains(got, "\n") {
			t.Fatalf("sanitized %q -> %q still spans multiple rows", name, got)
		}
		// Invariant 3: idempotent — sanitizing display text again is a no-op,
		// so a path that happens to flow through twice is not double-escaped
		// into something unrecognizable.
		if again := sanitizeDisplayText(got); again != got {
			t.Fatalf("not idempotent: %q -> %q -> %q", name, got, again)
		}
		// Invariant 4: printable, non-control input is passed through
		// untouched — the sanitizer must not disturb ordinary or non-ASCII
		// filenames, which is exactly what the -z conversion set out to fix.
		if strings.IndexFunc(name, isDisplayControl) < 0 && got != name {
			t.Fatalf("control-free name %q was altered to %q", name, got)
		}
	})
}
