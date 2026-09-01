package ui

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// Property: nextHunkStart wraps and returns the first hunk whose StartLine
// is strictly greater than the current line. From inside a hunk K, forward
// goes to hunk K+1's start (not to a different line within K) — this is
// what makes nav "hunk-grain": repeated presses advance one hunk per press.
func TestNextHunkStartForward(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		hunks := drawHunks(t, "hunks")
		current := rapid.IntRange(0, 250).Draw(t, "current")

		got, ok := nextHunkStart(hunks, current, +1)
		if !ok {
			t.Fatalf("expected ok with len=%d", len(hunks))
		}
		var expected int
		found := false
		for _, h := range hunks {
			if h.StartLine > current {
				expected = h.StartLine
				found = true
				break
			}
		}
		if !found {
			expected = hunks[0].StartLine // wrap
		}
		if got != expected {
			t.Fatalf("current=%d hunks=%+v got=%d want=%d", current, hunks, got, expected)
		}
	})
}

// Property: backward symmetric to forward — first hunk whose StartLine is
// strictly less than current, wrapping to the last. From the start of hunk
// K (currentLine == K.StartLine), backward goes to K-1's start (so K can
// be revisited by a forward press). From the middle of hunk K, backward
// goes to K's start (vim-style "jump to start of current chunk").
func TestNextHunkStartBackward(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		hunks := drawHunks(t, "hunks")
		current := rapid.IntRange(0, 250).Draw(t, "current")

		got, ok := nextHunkStart(hunks, current, -1)
		if !ok {
			t.Fatalf("expected ok with len=%d", len(hunks))
		}
		var expected int
		found := false
		for i := len(hunks) - 1; i >= 0; i-- {
			if hunks[i].StartLine < current {
				expected = hunks[i].StartLine
				found = true
				break
			}
		}
		if !found {
			expected = hunks[len(hunks)-1].StartLine // wrap
		}
		if got != expected {
			t.Fatalf("current=%d hunks=%+v got=%d want=%d", current, hunks, got, expected)
		}
	})
}

// Property: empty hunks returns (-1, false).
func TestNextHunkStartEmpty(t *testing.T) {
	for _, dir := range []int{-1, +1} {
		got, ok := nextHunkStart(nil, 5, dir)
		if ok || got != -1 {
			t.Errorf("dir=%d: got=%d ok=%v want (-1,false)", dir, got, ok)
		}
	}
}

// drawHunks generates a sorted list of diffHunks with non-overlapping
// StartLines so the "next strictly greater" property is unambiguous.
func drawHunks(t *rapid.T, tag string) []diffHunk {
	n := rapid.IntRange(1, 10).Draw(t, tag+"_n")
	starts := rapid.SliceOfNDistinct(rapid.IntRange(1, 200), n, n, func(i int) int { return i }).Draw(t, tag+"_starts")
	sort.Ints(starts)
	hunks := make([]diffHunk, len(starts))
	for i, s := range starts {
		hunks[i] = diffHunk{StartLine: s, EndLine: s} // 1-line hunks suffice for nav tests
	}
	return hunks
}

// Property: nextLeafIndex skips separators/cutlines/dirs and wraps.
func TestNextLeafIndexSkipsAndWraps(t *testing.T) {
	items := []sidebarItem{
		{label: "leaf0", kind: itemNormal},
		{kind: itemSeparator},
		{label: "dir1", kind: itemNormal, isDir: true},
		{label: "leaf1", kind: itemNormal},
		{kind: itemCutline},
		{label: "leaf2", kind: itemNormal},
	}
	idx := nextLeafIndex(items, 0, +1)
	if idx != 3 {
		t.Errorf("forward from 0: got %d want 3", idx)
	}
	idx = nextLeafIndex(items, 5, +1)
	if idx != 0 {
		t.Errorf("wrap forward from 5: got %d want 0", idx)
	}
	idx = nextLeafIndex(items, 0, -1)
	if idx != 5 {
		t.Errorf("backward from 0: got %d want 5", idx)
	}
}

// Property: empty items returns -1.
func TestNextLeafIndexEmpty(t *testing.T) {
	if idx := nextLeafIndex(nil, 0, +1); idx != -1 {
		t.Errorf("got %d want -1", idx)
	}
}
