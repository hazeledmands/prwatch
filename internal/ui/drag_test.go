package ui

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// State machine invariants (no Model required)
// ---------------------------------------------------------------------------

// Property: Begin activates the drag, clears scrollDir, and places both
// endpoints at the click position so HasRange is false until the mouse moves.
func TestDragSelection_BeginInitializesState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x := rapid.IntRange(-100, 100).Draw(t, "x")
		y := rapid.IntRange(-100, 100).Draw(t, "y")
		d := newDragSelection()
		d.scrollDir = +1 // poison
		d.Begin(x, y, nil)
		if !d.IsActive() {
			t.Fatal("Begin should activate the drag")
		}
		if d.scrollDir != 0 {
			t.Fatalf("Begin should clear scrollDir, got %d", d.scrollDir)
		}
		if d.HasRange() {
			t.Fatal("after Begin, start==end so HasRange should be false")
		}
		if d.startX != x || d.startY != y || d.endX != x || d.endY != y {
			t.Fatalf("Begin coords: start=(%d,%d) end=(%d,%d) want both (%d,%d)",
				d.startX, d.startY, d.endX, d.endY, x, y)
		}
	})
}

// Property: MoveEnd updates the end point but never the start; HasRange
// becomes true iff the end differs from the start.
func TestDragSelection_MoveEndUpdatesRange(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x1 := rapid.IntRange(0, 100).Draw(t, "x1")
		y1 := rapid.IntRange(0, 100).Draw(t, "y1")
		x2 := rapid.IntRange(0, 100).Draw(t, "x2")
		y2 := rapid.IntRange(0, 100).Draw(t, "y2")
		d := newDragSelection()
		d.Begin(x1, y1, nil)
		d.MoveEnd(x2, y2)
		if d.startX != x1 || d.startY != y1 {
			t.Fatalf("MoveEnd disturbed start: (%d,%d) want (%d,%d)", d.startX, d.startY, x1, y1)
		}
		if d.endX != x2 || d.endY != y2 {
			t.Fatalf("MoveEnd end: (%d,%d) want (%d,%d)", d.endX, d.endY, x2, y2)
		}
		wantRange := x1 != x2 || y1 != y2
		if d.HasRange() != wantRange {
			t.Fatalf("HasRange=%v want %v after MoveEnd from (%d,%d) to (%d,%d)",
				d.HasRange(), wantRange, x1, y1, x2, y2)
		}
	})
}

// Property: Release returns true iff the drag was active; it always
// deactivates and zeroes scrollDir, and records the release coordinates.
func TestDragSelection_ReleaseDeactivates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		x1 := rapid.IntRange(0, 100).Draw(t, "x1")
		y1 := rapid.IntRange(0, 100).Draw(t, "y1")
		x2 := rapid.IntRange(0, 100).Draw(t, "x2")
		y2 := rapid.IntRange(0, 100).Draw(t, "y2")

		d := newDragSelection()
		d.Begin(x1, y1, nil)
		d.scrollDir = -1 // simulate scrolling at release time
		if !d.Release(x2, y2) {
			t.Fatal("Release on active drag should return true")
		}
		if d.IsActive() {
			t.Fatal("Release should deactivate")
		}
		if d.scrollDir != 0 {
			t.Fatalf("Release should clear scrollDir, got %d", d.scrollDir)
		}
		if d.endX != x2 || d.endY != y2 {
			t.Fatalf("Release end coords: (%d,%d) want (%d,%d)", d.endX, d.endY, x2, y2)
		}

		// A second release with the drag now inactive returns false.
		if d.Release(x2, y2) {
			t.Fatal("Release on inactive drag should return false")
		}
	})
}

// Property: Cancel deactivates from any state and is idempotent.
func TestDragSelection_CancelIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		startActive := rapid.Bool().Draw(t, "startActive")
		x := rapid.IntRange(0, 100).Draw(t, "x")
		y := rapid.IntRange(0, 100).Draw(t, "y")
		dir := rapid.IntRange(-1, 1).Draw(t, "scrollDir")

		d := newDragSelection()
		if startActive {
			d.Begin(x, y, nil)
			d.scrollDir = dir
		}
		d.Cancel()
		if d.IsActive() {
			t.Fatal("Cancel should deactivate")
		}
		if d.scrollDir != 0 {
			t.Fatalf("Cancel should clear scrollDir, got %d", d.scrollDir)
		}

		// Idempotent: calling again is a no-op.
		d.Cancel()
		if d.IsActive() || d.scrollDir != 0 {
			t.Fatal("second Cancel changed state")
		}
	})
}

// ---------------------------------------------------------------------------
// Geometry-using invariants
// ---------------------------------------------------------------------------

// dragModel builds a Model populated with predictable plain content and
// dimensions chosen so the main pane content area is non-degenerate.
func dragModel(t *rapid.T, width, height, nLines int) *Model {
	mock := genMockGit(t)
	if len(mock.changedFiles.Committed) == 0 && len(mock.changedFiles.Uncommitted) == 0 {
		mock.changedFiles.Committed = []string{"file.go"}
	}
	var srcLines []string
	for i := 0; i < nLines; i++ {
		srcLines = append(srcLines, fmt.Sprintf("line %02d: alpha bravo charlie delta echo foxtrot golf hotel", i+1))
	}
	src := strings.Join(srcLines, "\n")
	mock.fileContent = src

	m := initModel(mock, FilesMode, width, height)
	m.mainPane.ClearDiffAnnotations()
	m.mainPane.SetLineNumbers(false)
	m.mainPane.SetWordWrap(false)
	m.mainPane.SetPlainContent(src)
	return m
}

// Property: ApplyHighlight only inserts ANSI escape codes; the visible
// characters (after stripping ANSI for width) are unchanged. This is the
// "round-trip through reverse-video is lossless" invariant from §6.
func TestDragApplyHighlight_PreservesStrippedContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(15, 40).Draw(t, "height")
		nLines := rapid.IntRange(5, 25).Draw(t, "nLines")

		m := dragModel(t, width, height, nLines)
		geom := m.dragGeom()

		statusRows := geom.statusRows
		minY := statusRows + 2 // status bar + top border + title row
		maxY := height - 2     // bottom border row
		if maxY <= minY {
			return
		}
		minX := geom.sidebarW + 1 + geom.pane.gutterWidth
		maxX := width - 2
		if maxX <= minX {
			return
		}

		x1 := rapid.IntRange(minX, maxX).Draw(t, "x1")
		y1 := rapid.IntRange(minY, maxY).Draw(t, "y1")
		x2 := rapid.IntRange(minX, maxX).Draw(t, "x2")
		y2 := rapid.IntRange(minY, maxY).Draw(t, "y2")

		m.drag.startX = x1
		m.drag.startY = y1
		m.drag.endX = x2
		m.drag.endY = y2
		m.drag.active = true

		v := m.mainPane.viewport.View()
		highlighted := m.drag.ApplyHighlight(v, geom)

		if stripANSIForWidth(highlighted) != stripANSIForWidth(v) {
			t.Fatalf("ApplyHighlight changed visible content\nbefore: %q\nafter:  %q",
				stripANSIForWidth(v), stripANSIForWidth(highlighted))
		}
	})
}

// Property: when the drag start is left of the gutter, ApplyHighlight
// clamps the start to the gutter offset — no reverse-video escape ever
// appears at a display column inside the gutter region.
func TestDragApplyHighlight_NeverHighlightsGutter(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(15, 40).Draw(t, "height")
		nLines := rapid.IntRange(5, 25).Draw(t, "nLines")

		m := dragModel(t, width, height, nLines)
		// Force a non-zero gutter so the test is meaningful.
		m.mainPane.SetLineNumbers(true)
		m.mainPane.SetPlainContent(m.mainPane.content)
		geom := m.dragGeom()
		gutterOffset := geom.sidebarW + 1 + geom.pane.gutterWidth
		if geom.pane.gutterWidth == 0 {
			return // nothing to clip against
		}

		statusRows := geom.statusRows
		minY := statusRows + 2
		maxY := height - 2
		if maxY <= minY {
			return
		}
		// Start anywhere from column 0 up through the gutter; this is the
		// case ApplyHighlight is supposed to clamp.
		x1 := rapid.IntRange(0, gutterOffset).Draw(t, "x1")
		y1 := rapid.IntRange(minY, maxY).Draw(t, "y1")
		x2 := rapid.IntRange(gutterOffset, width-2).Draw(t, "x2")
		y2 := rapid.IntRange(y1, maxY).Draw(t, "y2")

		m.drag.startX = x1
		m.drag.startY = y1
		m.drag.endX = x2
		m.drag.endY = y2
		m.drag.active = true

		// Render the full view (drag highlight is applied to the padded
		// final render in View()) and check every line.
		full := stripPaddingNewlines(m.View().Content)
		for row, line := range strings.Split(full, "\n") {
			idx := strings.Index(line, "\x1b[7m")
			if idx < 0 {
				continue
			}
			// Display-width of the un-styled prefix up to the first
			// \x1b[7m — this is the screen column where the highlight
			// begins. It must be >= gutterOffset.
			col := displayWidthOf(stripANSIForWidth(line[:idx]))
			if col < gutterOffset {
				t.Fatalf("highlight starts at column %d (< gutter %d) on row %d\n  line: %q\n  drag=(%d,%d)->(%d,%d)",
					col, gutterOffset, row, line, x1, y1, x2, y2)
			}
		}
	})
}

// Property: AdvanceAutoScroll either makes progress (viewport moves and the
// drag start is re-anchored by the same delta) or stops by setting scrollDir
// to 0. Repeated calls therefore terminate — they cannot loop forever while
// scrollDir stays non-zero.
func TestDragAdvanceAutoScroll_Terminates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		width := rapid.IntRange(60, 140).Draw(t, "width")
		height := rapid.IntRange(20, 40).Draw(t, "height")
		nLines := rapid.IntRange(60, 200).Draw(t, "nLines") // plenty to scroll

		m := dragModel(t, width, height, nLines)
		geom := m.dragGeom()

		statusRows := geom.statusRows
		contentStartY := statusRows + 2
		minX := geom.sidebarW + 1 + geom.pane.gutterWidth
		if minX >= width-2 || contentStartY >= height-2 {
			return
		}

		dir := rapid.SampledFrom([]int{-1, +1}).Draw(t, "dir")
		// Place the drag start inside the content area; the direction
		// determines whether we drag past the top or bottom edge.
		m.drag.active = true
		m.drag.startX = minX + 2
		m.drag.startY = contentStartY
		m.drag.endX = minX + 2
		m.drag.endY = contentStartY
		// Force a known starting scroll position so "scroll up" has room.
		if dir < 0 {
			m.mainPane.viewport.ScrollDown(20)
		}
		m.drag.scrollDir = dir

		maxIters := 1 + 2*nLines // generous bound; should terminate well before
		startOffset := m.mainPane.viewport.YOffset()
		startY := m.drag.startY
		moved := 0
		for i := 0; i < maxIters; i++ {
			prevOffset := m.mainPane.viewport.YOffset()
			prevStartY := m.drag.startY
			prevDir := m.drag.scrollDir
			if prevDir == 0 {
				break
			}

			m.drag.AdvanceAutoScroll(geom)

			curOffset := m.mainPane.viewport.YOffset()
			delta := curOffset - prevOffset
			if delta != 0 {
				// Drag startY must be re-anchored by the same delta so the
				// original click stays on the same content row.
				if m.drag.startY != prevStartY-delta {
					t.Fatalf("startY not re-anchored: was %d, expected %d, got %d (delta=%d)",
						prevStartY, prevStartY-delta, m.drag.startY, delta)
				}
				// And we keep going in the same direction.
				if m.drag.scrollDir != prevDir {
					t.Fatalf("scrollDir flipped while making progress: %d -> %d",
						prevDir, m.drag.scrollDir)
				}
				moved++
			} else {
				// No movement means we hit the edge; scrollDir must be 0.
				if m.drag.scrollDir != 0 {
					t.Fatalf("no viewport progress but scrollDir=%d (should be 0)", m.drag.scrollDir)
				}
			}
		}
		if m.drag.scrollDir != 0 {
			t.Fatalf("AdvanceAutoScroll did not terminate after %d iterations (dir=%d, startOff=%d, startY=%d, moves=%d)",
				maxIters, dir, startOffset, startY, moved)
		}
	})
}

// stripPaddingNewlines removes leading/trailing blank padding from a rendered
// view so split-by-line behaves predictably for highlight row checks.
func stripPaddingNewlines(s string) string {
	return strings.Trim(s, "\n")
}
