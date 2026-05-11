package ui

import (
	"sort"
	"testing"

	"pgregory.net/rapid"
)

// Property: nextDiffLine wraps and never returns the same line for direction +1
// when there's at least one strictly-greater diff line.
func TestNextDiffLineForward(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		diffLines := rapid.SliceOfN(rapid.IntRange(1, 200), 1, 20).Draw(t, "diffLines")
		sort.Ints(diffLines)
		current := rapid.IntRange(0, 250).Draw(t, "current")

		got, ok := nextDiffLine(diffLines, current, +1)
		if !ok {
			t.Fatalf("expected ok with len=%d", len(diffLines))
		}
		// If any diff line is > current, the result must equal the first such line.
		var expected int
		found := false
		for _, l := range diffLines {
			if l > current {
				expected = l
				found = true
				break
			}
		}
		if !found {
			expected = diffLines[0] // wrap
		}
		if got != expected {
			t.Fatalf("current=%d diffLines=%v got=%d want=%d", current, diffLines, got, expected)
		}
	})
}

// Property: empty diff lines returns (-1, false).
func TestNextDiffLineEmpty(t *testing.T) {
	for _, dir := range []int{-1, +1} {
		got, ok := nextDiffLine(nil, 5, dir)
		if ok || got != -1 {
			t.Errorf("dir=%d: got=%d ok=%v want (-1,false)", dir, got, ok)
		}
	}
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
