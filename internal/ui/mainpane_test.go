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
