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
	for i := 0; i < 50; i++ {
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
	for i := 0; i < 50; i++ {
		lines = append(lines, "line")
	}
	mp.SetContent(strings.Join(lines, "\n"))

	mp.GoToBottom()
	if mp.ScrollTop() == 0 {
		t.Error("GoToBottom should scroll past 0")
	}
}

func TestMainPane_SearchAndHighlight(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\nline2\ntarget line\nline4\nline5")

	mp.SearchAndHighlight("target")
	if mp.ScrollTop() != 2 {
		t.Errorf("search should scroll to line 2, got %d", mp.ScrollTop())
	}
}

func TestMainPane_SearchAndHighlight_CaseInsensitive(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\nline2\nTARGET line\nline4\nline5")

	mp.SearchAndHighlight("target")
	if mp.ScrollTop() != 2 {
		t.Errorf("case-insensitive search should find TARGET, got offset %d", mp.ScrollTop())
	}
}

func TestMainPane_SearchAndHighlight_NoWrapAround(t *testing.T) {
	// Search only searches visible content, so it should NOT wrap around
	mp := newMainPane()
	mp.SetSize(80, 3)
	// Need enough content for scrolling: viewport=3, so 6+ lines needed
	mp.SetContent("target line\nline2\nline3\nline4\nline5\nline6\nline7\nline8")

	// Scroll past the target — target at line 0 is no longer visible (showing lines 4-6)
	mp.viewport.SetYOffset(4)

	mp.SearchAndHighlight("target")
	// Should stay at offset 4 since "target" is at line 0, not in visible range (4-6)
	if mp.ScrollTop() != 4 {
		t.Errorf("search should not wrap around, expected offset 4, got %d", mp.ScrollTop())
	}
}

func TestMainPane_SearchAndHighlight_EmptyContent(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("")

	mp.SearchAndHighlight("something") // should not panic
}

func TestMainPane_SearchAndHighlight_NotFound(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 3)
	mp.SetContent("line1\nline2\nline3")

	mp.SearchAndHighlight("nonexistent")
	// Should not panic, offset stays at 0
	if mp.ScrollTop() != 0 {
		t.Errorf("search miss should not change offset, got %d", mp.ScrollTop())
	}
}

func TestMainPane_SearchOnlySearchesVisible(t *testing.T) {
	// Spec: "[/] to open a search (only searches what is currently visible)"
	mp := newMainPane()
	mp.SetSize(80, 3) // viewport shows 3 lines at a time
	mp.SetContent("line1\nline2\nline3\ntarget_hidden\nline5\nline6\nline7")

	// Viewport starts at 0, showing lines 0-2. "target_hidden" is at line 3, NOT visible.
	mp.SearchAndHighlight("target_hidden")

	// Search should NOT find it because it's not in the visible viewport
	if mp.ScrollTop() != 0 {
		t.Errorf("search should not find non-visible content, but scrolled to %d", mp.ScrollTop())
	}
}

func TestMainPane_SearchFindsVisibleContent(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 5) // viewport shows 5 lines
	mp.SetContent("line1\nline2\ntarget_visible\nline4\nline5\nline6\nline7")

	// "target_visible" is at line 2, which IS visible (viewport shows lines 0-4)
	mp.SearchAndHighlight("target_visible")

	if mp.ScrollTop() != 2 {
		t.Errorf("search should scroll to visible target at line 2, got %d", mp.ScrollTop())
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
