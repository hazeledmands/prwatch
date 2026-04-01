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
