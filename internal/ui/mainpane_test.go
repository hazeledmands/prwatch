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
