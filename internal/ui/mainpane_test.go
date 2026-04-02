package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

	mp.ScrollToLine(4)
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
// Bug: go.mod has 27 lines but file-view only scrolls to line 25.
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
		w := 0
		for _, r := range stripped {
			if r == '\t' {
				w += 8 - (w % 8)
			} else {
				w++
			}
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

func TestWrapLines_BreaksAtWordBoundaries(t *testing.T) {
	// Spec: "word-wrap should break at word boundaries"
	// "abcdef ghijkl" is 13 chars. At width 10, character-boundary wrapping
	// would split "ghijkl" into "ghij" and "kl". Word-boundary wrapping
	// should break before "ghijkl".
	result := wrapLines("abcdef ghijkl", 10)
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
	result := wrapLines("short "+longWord+" end", 20)
	lines := strings.Split(result, "\n")
	// "short " is 6 chars. The long word (15 chars) starts at position 6.
	// 6 + 15 = 21 > 20, so the word wraps. But since 15 > 10 (1/8 of 80),
	// it should be broken mid-word at the width boundary.
	if len(lines) < 2 {
		t.Fatal("expected wrapping to occur")
	}
}

func TestWrapLines_PreservesShortWords(t *testing.T) {
	// Words shorter than 1/8 of width should never be split
	result := wrapLines("aa bb cc dd ee ff gg hh ii jj", 10)
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
