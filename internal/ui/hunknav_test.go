package ui

import (
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// hunkNavShape describes a main-pane fixture for hunk-nav tests. The
// non-default fields exist to break the row↔source identity map: short
// lines at width 80 never wrap, so a fixture built only from them can't
// see a coordinate-system bug in the anchor. longEvery adds wrap
// continuation rows below a source line; removedPerHunk adds removed-line
// decoration rows above one, which have no source line of their own.
type hunkNavShape struct {
	height        int
	lines         int
	starts        []int
	longEvery     int // every Nth line is wider than the pane (0: none)
	removedPerHun int // removed lines attached to each hunk start (0: none)
}

// newHunkNavModel builds a FilesMode model whose main pane holds a file of
// shape s with s.starts installed as the hunk list.
func newHunkNavModel(s hunkNavShape) *Model {
	m := NewModel("/tmp", testGit())
	m.loading = false
	m.width = 80
	m.height = s.height
	m.updateLayout()

	body := make([]string, s.lines)
	for i := range body {
		body[i] = "kept line"
		if s.longEvery > 0 && (i+1)%s.longEvery == 0 {
			// ~3x the pane width, so it wraps onto continuation rows.
			body[i] = strings.TrimSpace(strings.Repeat("a wide word ", 22))
		}
	}

	hunks := make([]diffHunk, len(s.starts))
	for i, start := range s.starts {
		hunks[i] = diffHunk{StartLine: start, EndLine: start}
	}

	// Files mode is the plain-content path: SetPlainContent installs the
	// body, and the annotations decorate it with gutter marks and (with
	// showRemoved on, the default) removed-line rows. SetContent's diff
	// path skips applyFileViewFormatting and renders no decoration rows.
	m.mainPane.SetPlainContent(strings.Join(body, "\n"))
	if s.removedPerHun > 0 {
		ann := make(map[int]diffAnnotation, len(s.starts))
		for _, start := range s.starts {
			removed := make([]string, s.removedPerHun)
			for i := range removed {
				removed[i] = "removed line"
			}
			ann[start] = diffAnnotation{kind: diffLineAdded, removedLines: removed}
		}
		m.mainPane.SetDiffAnnotations(ann)
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

// Guard on the fixture itself: longEvery and removedPerHun must actually
// add rows, or the tests below are asserting over an identity row↔source
// map and can't see a coordinate-system bug in the anchor.
func TestHunkNavShape_BreaksRowIdentity(t *testing.T) {
	base := hunkNavShape{height: 24, lines: 40, starts: []int{5, 20}}
	plain := viewportContentRowCount(newHunkNavModel(base).mainPane)
	if plain != base.lines {
		t.Fatalf("plain fixture: %d rows for %d lines, expected the identity map", plain, base.lines)
	}

	wrapped := base
	wrapped.longEvery = 3
	if got := viewportContentRowCount(newHunkNavModel(wrapped).mainPane); got <= plain {
		t.Errorf("longEvery=3: %d rows, want more than the %d unwrapped rows — no wrap engaged", got, plain)
	}

	decorated := base
	decorated.removedPerHun = 2
	if got := viewportContentRowCount(newHunkNavModel(decorated).mainPane); got <= plain {
		t.Errorf("removedPerHun=2: %d rows, want more than %d — no decoration rows rendered", got, plain)
	}
}

// Regression: hunk nav inferred its "current hunk" anchor back from the
// scroll position (YOffset + 30% margin). When the margin subtraction
// clamped YOffset to 0 — any hunk within the margin of the file's top —
// the inferred anchor sat *below* the hunk it had just jumped to, so the
// next J skipped one or more hunks.
func TestHunkNav_NoSkipNearTopClamp(t *testing.T) {
	tests := []struct {
		name  string
		shape hunkNavShape
	}{
		{"hunks inside the top margin", hunkNavShape{height: 24, lines: 60, starts: []int{2, 4, 20, 40}}},
		{"first hunk at line 1", hunkNavShape{height: 24, lines: 60, starts: []int{1, 3, 5, 30}}},
		{"tight cluster at top", hunkNavShape{height: 40, lines: 80, starts: []int{1, 2, 3, 4, 60}}},
		{"clustered near EOF", hunkNavShape{height: 24, lines: 40, starts: []int{5, 36, 38, 40}}},
		// The geometry TestProperty_HunkNavAdjacency shrank to when it first
		// caught this bug (seed 5050539661895286733), kept as a fixed case
		// because adding generator dimensions invalidates recorded seeds.
		{"shrunk property counterexample", hunkNavShape{height: 9, lines: 2, starts: []int{1, 2}}},
		{"top clamp with wrapped lines", hunkNavShape{height: 24, lines: 60, starts: []int{1, 3, 5, 30}, longEvery: 3}},
		{"top clamp with removed-line rows", hunkNavShape{height: 24, lines: 60, starts: []int{1, 3, 5, 30}, removedPerHun: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			starts := tc.shape.starts
			m := newHunkNavModel(tc.shape)
			m.jumpToFirstDiff()
			if got := hunkIndexAtCursor(m); got != 0 {
				t.Fatalf("jumpToFirstDiff: cursor on hunk %d (line %d), want hunk 0",
					got, m.cursor.Pos(m.mainPane).SourceLine)
			}
			// One J per hunk must visit every hunk in order and wrap.
			for i := 1; i <= len(starts); i++ {
				m.jumpToNextDiff(+1)
				want := i % len(starts)
				if got := hunkIndexAtCursor(m); got != want {
					t.Fatalf("after %d J presses: on hunk %d (line %d), want hunk %d (line %d)",
						i, got, m.cursor.Pos(m.mainPane).SourceLine, want, starts[want])
				}
			}
			// And K walks back down the list.
			for i := 1; i <= len(starts); i++ {
				m.jumpToNextDiff(-1)
				want := ((-i)%len(starts) + len(starts)) % len(starts)
				if got := hunkIndexAtCursor(m); got != want {
					t.Fatalf("after %d K presses: on hunk %d (line %d), want hunk %d (line %d)",
						i, got, m.cursor.Pos(m.mainPane).SourceLine, want, starts[want])
				}
			}
		})
	}
}

// Decoration rows (removed-line rows) have no source line of their own,
// so a cursor parked on one resolves to a *preceding* source line — not
// to the hunk start the removals attach to. That is what makes J from a
// decoration row land on the hunk below it rather than skipping past it,
// and K land on the previous hunk. Pinned down here because
// HunkNavAnchor's correctness argument depends on which side of the hunk
// start a decoration row falls.
func TestHunkNavAnchor_OnDecorationRow(t *testing.T) {
	const hunkStart = 20
	starts := []int{5, hunkStart, 40}
	m := newHunkNavModel(hunkNavShape{
		height: 24, lines: 60, starts: starts, removedPerHun: 2,
	})
	startRow, ok := m.mainPane.sourceToFormatLine[hunkStart]
	if !ok {
		t.Fatalf("no formatted row for source line %d", hunkStart)
	}
	// The removed-line rows render immediately above the hunk start.
	for _, back := range []int{1, 2} {
		m.cursor.vpRow = startRow - back
		anchor := m.nav().HunkNavAnchor()
		if anchor >= hunkStart || anchor <= starts[0] {
			t.Fatalf("cursor %d row(s) above hunk start %d: anchor %d, want strictly between %d and %d",
				back, hunkStart, anchor, starts[0], hunkStart)
		}
		// So J targets this hunk, and K the one before it.
		m.jumpToNextDiff(+1)
		if got := m.cursor.SourceLine(m.mainPane); got != hunkStart {
			t.Errorf("J from decoration row %d above hunk start %d: landed on %d, want %d",
				back, hunkStart, got, hunkStart)
		}
		m.cursor.vpRow = startRow - back
		m.jumpToNextDiff(-1)
		if got := m.cursor.SourceLine(m.mainPane); got != starts[0] {
			t.Errorf("K from decoration row %d above hunk start %d: landed on %d, want %d",
				back, hunkStart, got, starts[0])
		}
	}
}

// Property: from any hunk, one J lands on the immediately-following hunk
// (wrapping), one K on the immediately-preceding one, and J-then-K returns
// to the starting hunk — for any hunk layout and viewport height.
func TestProperty_HunkNavAdjacency(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Dimensions are drawn in a fixed order — geometry first, then
		// rendering shape — so a recorded seed keeps its meaning as long as
		// the signature holds. Adding a dimension invalidates existing
		// seeds either way (rapid replays the whole value stream, so even
		// appending a draw exhausts it), so a geometry worth keeping gets
		// promoted into TestHunkNav_NoSkipNearTopClamp's table rather than
		// living only as a seed.
		height := rapid.IntRange(6, 40).Draw(t, "height")
		lines := rapid.IntRange(1, 120).Draw(t, "lines")
		n := rapid.IntRange(1, 8).Draw(t, "n")
		starts := rapid.SliceOfNDistinct(rapid.IntRange(1, lines), n, n,
			func(i int) int { return i }).Draw(t, "starts")
		sort.Ints(starts)

		m := newHunkNavModel(hunkNavShape{
			height:        height,
			lines:         lines,
			starts:        starts,
			longEvery:     rapid.IntRange(0, 5).Draw(t, "longEvery"),
			removedPerHun: rapid.IntRange(0, 3).Draw(t, "removedPerHunk"),
		})
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
