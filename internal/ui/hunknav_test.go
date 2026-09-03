package ui

import (
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// newHunkNavModel builds a FilesMode model whose main pane holds a plain
// file of `lines` lines with the given hunk start lines installed. Plain
// content keeps source-line→viewport-row an identity map, so the test is
// about which hunk nav targets, not about diff rendering.
func newHunkNavModel(height, lines int, starts []int) *Model {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = height
	m.updateLayout()
	body := make([]string, lines)
	for i := range body {
		body[i] = "kept line"
	}
	m.mainPane.SetPlainContent(strings.Join(body, "\n"))
	hunks := make([]diffHunk, len(starts))
	for i, s := range starts {
		hunks[i] = diffHunk{StartLine: s, EndLine: s}
	}
	m.mainPane.SetDiffHunks(hunks)
	m.mode = FilesMode
	return m
}

// hunkIndexAtCursor reports which hunk the nav state currently points at,
// by matching the cursor's source line against the hunk starts. Returns -1
// when the cursor isn't on a hunk start.
func hunkIndexAtCursor(m *Model) int {
	line := m.cursor.Pos(m.mainPane).SourceLine
	for i, h := range m.mainPane.diffHunks {
		if h.StartLine == line {
			return i
		}
	}
	return -1
}

// Regression: hunk nav inferred its "current hunk" anchor back from the
// scroll position (YOffset + 30% margin). When the margin subtraction
// clamped YOffset to 0 — any hunk within the margin of the file's top —
// the inferred anchor sat *below* the hunk it had just jumped to, so the
// next J skipped one or more hunks.
func TestHunkNav_NoSkipNearTopClamp(t *testing.T) {
	tests := []struct {
		name   string
		height int
		lines  int
		starts []int
	}{
		{"hunks inside the top margin", 24, 60, []int{2, 4, 20, 40}},
		{"first hunk at line 1", 24, 60, []int{1, 3, 5, 30}},
		{"tight cluster at top", 40, 80, []int{1, 2, 3, 4, 60}},
		{"clustered near EOF", 24, 40, []int{5, 36, 38, 40}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newHunkNavModel(tc.height, tc.lines, tc.starts)
			m.jumpToFirstDiff()
			if got := hunkIndexAtCursor(m); got != 0 {
				t.Fatalf("jumpToFirstDiff: cursor on hunk %d (line %d), want hunk 0",
					got, m.cursor.Pos(m.mainPane).SourceLine)
			}
			// One J per hunk must visit every hunk in order and wrap.
			for i := 1; i <= len(tc.starts); i++ {
				m.jumpToNextDiff(+1)
				want := i % len(tc.starts)
				if got := hunkIndexAtCursor(m); got != want {
					t.Fatalf("after %d J presses: on hunk %d (line %d), want hunk %d (line %d)",
						i, got, m.cursor.Pos(m.mainPane).SourceLine, want, tc.starts[want])
				}
			}
			// And K walks back down the list.
			for i := 1; i <= len(tc.starts); i++ {
				m.jumpToNextDiff(-1)
				want := ((-i)%len(tc.starts) + len(tc.starts)) % len(tc.starts)
				if got := hunkIndexAtCursor(m); got != want {
					t.Fatalf("after %d K presses: on hunk %d (line %d), want hunk %d (line %d)",
						i, got, m.cursor.Pos(m.mainPane).SourceLine, want, tc.starts[want])
				}
			}
		})
	}
}

// Property: from any hunk, one J lands on the immediately-following hunk
// (wrapping), one K on the immediately-preceding one, and J-then-K returns
// to the starting hunk — for any hunk layout and viewport height.
func TestProperty_HunkNavAdjacency(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		height := rapid.IntRange(6, 40).Draw(t, "height")
		lines := rapid.IntRange(1, 120).Draw(t, "lines")
		n := rapid.IntRange(1, 8).Draw(t, "n")
		starts := rapid.SliceOfNDistinct(rapid.IntRange(1, lines), n, n,
			func(i int) int { return i }).Draw(t, "starts")
		sort.Ints(starts)

		m := newHunkNavModel(height, lines, starts)
		m.jumpToFirstDiff()
		if got := hunkIndexAtCursor(m); got != 0 {
			t.Fatalf("jumpToFirstDiff: on hunk %d, want 0 (starts=%v h=%d lines=%d)",
				got, starts, height, lines)
		}
		for step := 1; step <= 2*n; step++ {
			before := hunkIndexAtCursor(m)
			m.jumpToNextDiff(+1)
			after := hunkIndexAtCursor(m)
			if want := (before + 1) % n; after != want {
				t.Fatalf("J from hunk %d → %d, want %d (starts=%v h=%d lines=%d)",
					before, after, want, starts, height, lines)
			}
			m.jumpToNextDiff(-1)
			if back := hunkIndexAtCursor(m); back != before {
				t.Fatalf("J then K from hunk %d → %d (starts=%v h=%d lines=%d)",
					before, back, starts, height, lines)
			}
			m.jumpToNextDiff(+1)
		}
	})
}
