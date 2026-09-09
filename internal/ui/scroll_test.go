package ui

import (
	"testing"

	"pgregory.net/rapid"
)

// maxOffsetOracle is the definition the properties below are checked
// against, written out independently of the implementation.
func maxOffsetOracle(total, visible int) int {
	if visible <= 0 {
		return 0
	}
	if total <= visible {
		return 0
	}
	return total - visible
}

func TestClampOffset_Table(t *testing.T) {
	tests := []struct {
		name                   string
		offset, total, visible int
		want                   int
	}{
		{"in range", 3, 20, 10, 3},
		{"above ceiling", 15, 20, 10, 10},
		{"at ceiling", 10, 20, 10, 10},
		{"negative", -4, 20, 10, 0},
		{"content shorter than window", 5, 3, 10, 0},
		{"content exactly fills window", 5, 10, 10, 0},
		{"empty content", 5, 0, 10, 0},
		{"zero-height window", 5, 20, 0, 0},
		{"negative-height window", 5, 20, -3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampOffset(tt.offset, tt.total, tt.visible); got != tt.want {
				t.Errorf("clampOffset(%d, %d, %d) = %d, want %d",
					tt.offset, tt.total, tt.visible, got, tt.want)
			}
		})
	}
}

func TestEnsureVisible_Table(t *testing.T) {
	tests := []struct {
		name                          string
		offset, index, total, visible int
		want                          int
	}{
		{"already visible", 5, 8, 40, 10, 5},
		{"at window top", 5, 5, 40, 10, 5},
		{"at window bottom", 5, 14, 40, 10, 5},
		// Scrolling up puts the index at the top of the window; scrolling
		// down puts it at the bottom. Both are the minimal move.
		{"above window", 10, 4, 40, 10, 4},
		{"below window", 5, 20, 40, 10, 11},
		{"one past the bottom", 5, 15, 40, 10, 6},
		{"index beyond content", 0, 99, 40, 10, 30},
		{"negative index", 5, -3, 40, 10, 0},
		{"zero-height window", 5, 20, 40, 0, 0},
		{"content shorter than window", 0, 2, 3, 10, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureVisible(tt.offset, tt.index, tt.total, tt.visible)
			if got != tt.want {
				t.Errorf("ensureVisible(%d, %d, %d, %d) = %d, want %d",
					tt.offset, tt.index, tt.total, tt.visible, got, tt.want)
			}
		})
	}
}

// scrollArgs draws a plausible (offset, total, visible) triple, deliberately
// including the degenerate shapes — empty content, zero and negative window
// heights, offsets already out of range — since those are exactly where the
// hand-rolled copies this replaced disagreed with each other.
func scrollArgs(t *rapid.T) (offset, total, visible int) {
	total = rapid.IntRange(0, 200).Draw(t, "total")
	visible = rapid.IntRange(-2, 60).Draw(t, "visible")
	offset = rapid.IntRange(-10, 220).Draw(t, "offset")
	return offset, total, visible
}

func TestProperty_ClampOffsetInBounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		offset, total, visible := scrollArgs(t)
		got := clampOffset(offset, total, visible)
		ceiling := maxOffsetOracle(total, visible)
		if got < 0 || got > ceiling {
			t.Fatalf("clampOffset(%d, %d, %d) = %d, outside [0, %d]",
				offset, total, visible, got, ceiling)
		}
	})
}

func TestProperty_ClampOffsetIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		offset, total, visible := scrollArgs(t)
		once := clampOffset(offset, total, visible)
		twice := clampOffset(once, total, visible)
		if once != twice {
			t.Fatalf("clampOffset not idempotent for (%d, %d, %d): %d then %d",
				offset, total, visible, once, twice)
		}
	})
}

// An offset already in range is left exactly where it is: clamping is a
// correction, never a nudge.
func TestProperty_ClampOffsetFixedPointInRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(0, 200).Draw(t, "total")
		visible := rapid.IntRange(-2, 60).Draw(t, "visible")
		ceiling := maxOffsetOracle(total, visible)
		offset := rapid.IntRange(0, ceiling).Draw(t, "offset")
		if got := clampOffset(offset, total, visible); got != offset {
			t.Fatalf("clampOffset moved an in-range offset %d to %d (total %d, visible %d)",
				offset, got, total, visible)
		}
	})
}

func TestProperty_EnsureVisibleInBounds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		offset, total, visible := scrollArgs(t)
		index := rapid.IntRange(-10, 220).Draw(t, "index")
		got := ensureVisible(offset, index, total, visible)
		ceiling := maxOffsetOracle(total, visible)
		if got < 0 || got > ceiling {
			t.Fatalf("ensureVisible(%d, %d, %d, %d) = %d, outside [0, %d]",
				offset, index, total, visible, got, ceiling)
		}
	})
}

func TestProperty_EnsureVisibleIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		offset, total, visible := scrollArgs(t)
		index := rapid.IntRange(-10, 220).Draw(t, "index")
		once := ensureVisible(offset, index, total, visible)
		twice := ensureVisible(once, index, total, visible)
		if once != twice {
			t.Fatalf("ensureVisible not idempotent for (%d, %d, %d, %d): %d then %d",
				offset, index, total, visible, once, twice)
		}
	})
}

// The whole point of the function: when the index really can be shown, it
// ends up inside the window.
func TestProperty_EnsureVisibleActuallyShowsTheIndex(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(1, 200).Draw(t, "total")
		visible := rapid.IntRange(1, 60).Draw(t, "visible")
		offset := rapid.IntRange(-10, 220).Draw(t, "offset")
		index := rapid.IntRange(0, total-1).Draw(t, "index")

		got := ensureVisible(offset, index, total, visible)
		if index < got || index >= got+visible {
			t.Fatalf("ensureVisible(%d, %d, %d, %d) = %d leaves index outside [%d, %d)",
				offset, index, total, visible, got, got, got+visible)
		}
	})
}

// Minimality: no offset closer to the original would have worked. This is the
// property that distinguishes prwatch's scroll-to-index from
// viewport.EnsureVisible, which jumps the index to the top of the window.
func TestProperty_EnsureVisibleIsMinimal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(1, 200).Draw(t, "total")
		visible := rapid.IntRange(1, 60).Draw(t, "visible")
		offset := rapid.IntRange(-10, 220).Draw(t, "offset")
		index := rapid.IntRange(0, total-1).Draw(t, "index")

		got := ensureVisible(offset, index, total, visible)
		ceiling := maxOffsetOracle(total, visible)
		start := clampOffset(offset, total, visible)
		best := got

		for candidate := 0; candidate <= ceiling; candidate++ {
			if index < candidate || index >= candidate+visible {
				continue
			}
			if abs(candidate-start) < abs(best-start) {
				best = candidate
			}
		}
		if abs(got-start) != abs(best-start) {
			t.Fatalf("ensureVisible(%d, %d, %d, %d) = %d moves %d from %d; %d moves only %d",
				offset, index, total, visible, got, abs(got-start), start, best, abs(best-start))
		}
	})
}

// An index already inside the window doesn't scroll at all — except for the
// correction an out-of-range offset needs anyway.
func TestProperty_EnsureVisibleNoOpWhenAlreadyVisible(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		total := rapid.IntRange(1, 200).Draw(t, "total")
		visible := rapid.IntRange(1, 60).Draw(t, "visible")
		ceiling := maxOffsetOracle(total, visible)
		offset := rapid.IntRange(0, ceiling).Draw(t, "offset")

		high := min(offset+visible-1, total-1)
		index := rapid.IntRange(offset, high).Draw(t, "index")

		if got := ensureVisible(offset, index, total, visible); got != offset {
			t.Fatalf("ensureVisible scrolled from %d to %d for an already-visible index %d (total %d, visible %d)",
				offset, got, index, total, visible)
		}
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
