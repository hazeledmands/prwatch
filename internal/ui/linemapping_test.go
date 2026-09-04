package ui

import (
	"fmt"
	"strings"
	"testing"
)

// longLines builds n lines of the given width, with `needle` embedded in
// line `needleLine` (1-indexed).
func longLines(n, width, needleLine int, needle string) string {
	lines := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		body := strings.Repeat("x", width)
		if i == needleLine {
			body = needle + strings.Repeat("y", width)
		}
		lines = append(lines, fmt.Sprintf("%s_%d", body, i))
	}
	return strings.Join(lines, "\n")
}

// TestSearch_NavigatesToSourceLine_Wrapped covers CODE_REVIEW A1 sub-item 3:
// with word wrap on, a match's content-line index is not its viewport row,
// so navigating with the raw offset lands far above the match.
func TestSearch_NavigatesToSourceLine_Wrapped(t *testing.T) {
	const needleLine = 25
	for _, tc := range []struct {
		name    string
		setup   func(mp *mainPane, content string)
		content string
	}{
		{
			name:    "plain file content",
			content: longLines(40, 60, needleLine, "needle"),
			setup:   func(mp *mainPane, c string) { mp.SetPlainContent(c) },
		},
		{
			name: "diff content",
			// Diff content has no source→formatted map, but its rows still
			// wrap, so the same mapping is required.
			content: "@@ -1,40 +1,40 @@\n" + longLines(40, 60, needleLine-1, "needle"),
			setup:   func(mp *mainPane, c string) { mp.SetContent(c) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mp := newMainPane()
			mp.SetSize(30, 10)
			tc.setup(mp, tc.content)

			s := newSearchOverlay()
			s.Open()
			s.query = "needle"
			s.refresh(searchHooks(mp))

			if len(s.matches) != 1 {
				t.Fatalf("expected exactly one match, got %v", s.matches)
			}
			wantSource := s.matches[0] + 1
			if got := mp.viewportToSourceLine(); got != wantSource {
				t.Errorf("after search, viewport top is source line %d, want %d (match at content line %d, viewport row %d)",
					got, wantSource, s.matches[0], mp.ScrollTop())
			}
			// End-user invariant: the match is actually on screen, near the
			// top. This catches the diff-mode case, where both halves of the
			// raw mapping are 1:1 and so agree with each other while being
			// wrong about the rendered rows.
			view := stripANSI(mp.View(false))
			rows := strings.Split(view, "\n")
			top := rows
			if len(top) > 3 {
				top = top[:3]
			}
			if !strings.Contains(strings.Join(top, "\n"), "needle") {
				t.Errorf("after search, match is not in the top rows of the viewport; got:\n%s",
					strings.Join(rows[:min(len(rows), 6)], "\n"))
			}
		})
	}
}

// TestCurrentLineNumber_WrapAware covers the `$EDITOR +N` half of A1
// sub-item 3: the line handed to the editor must be the source line at the
// viewport top, not the raw scroll offset.
func TestCurrentLineNumber_WrapAware(t *testing.T) {
	m := NewModel("/tmp/test-repo", &mockGit{})
	m.width = 100
	m.height = 40
	m.mainPane.SetSize(30, 20)
	m.mainPane.SetPlainContent(longLines(40, 60, 1, "needle"))

	for _, want := range []int{5, 12, 25} {
		m.mainPane.ScrollToSourceLine(want)
		if got := m.currentLineNumber(); got != want {
			t.Errorf("scrolled to source line %d: currentLineNumber() = %d (viewport row %d)",
				want, got, m.mainPane.ScrollTop())
		}
	}
}
