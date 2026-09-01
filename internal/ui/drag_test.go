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

// drawEndpoint generates a random endpoint for property tests: either
// inside content (with a Position and VpRow) or outside (OutsideDir set
// to -1 or +1).
func drawEndpoint(t *rapid.T, tag string) endpoint {
	if rapid.Bool().Draw(t, tag+"_outside") {
		dir := rapid.SampledFrom([]int{-1, 1}).Draw(t, tag+"_dir")
		return endpoint{OutsideDir: dir}
	}
	return endpoint{
		Pos: Position{
			SourceLine: rapid.IntRange(1, 100).Draw(t, tag+"_line"),
			Column:     rapid.IntRange(0, 200).Draw(t, tag+"_col"),
		},
		VpRow: rapid.IntRange(0, 200).Draw(t, tag+"_vp"),
	}
}

// Property: Begin activates the drag, clears scrollDir, and places both
// endpoints at the click position so HasRange is false until the mouse moves.
func TestDragSelection_BeginInitializesState(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		e := drawEndpoint(t, "click")
		d := newDragSelection()
		d.scrollDir = +1 // poison
		d.Begin(e)
		if !d.IsActive() {
			t.Fatal("Begin should activate the drag")
		}
		if d.scrollDir != 0 {
			t.Fatalf("Begin should clear scrollDir, got %d", d.scrollDir)
		}
		if d.HasRange() {
			t.Fatal("after Begin, anchor==active so HasRange should be false")
		}
		if d.anchor != e || d.active != e {
			t.Fatalf("Begin endpoints: anchor=%+v active=%+v want both %+v",
				d.anchor, d.active, e)
		}
	})
}

// Property: MoveEnd updates the active end but never the anchor; HasRange
// becomes true iff the active end differs from the anchor.
func TestDragSelection_MoveEndUpdatesRange(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		e1 := drawEndpoint(t, "begin")
		e2 := drawEndpoint(t, "move")
		d := newDragSelection()
		d.Begin(e1)
		d.MoveEnd(e2)
		if d.anchor != e1 {
			t.Fatalf("MoveEnd disturbed anchor: %+v want %+v", d.anchor, e1)
		}
		if d.active != e2 {
			t.Fatalf("MoveEnd active: %+v want %+v", d.active, e2)
		}
		wantRange := e1 != e2
		if d.HasRange() != wantRange {
			t.Fatalf("HasRange=%v want %v after MoveEnd from %+v to %+v",
				d.HasRange(), wantRange, e1, e2)
		}
	})
}

// Property: Release returns true iff the drag was active; it always
// deactivates and zeroes scrollDir, and records the release endpoint.
func TestDragSelection_ReleaseDeactivates(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		e1 := drawEndpoint(t, "begin")
		e2 := drawEndpoint(t, "release")

		d := newDragSelection()
		d.Begin(e1)
		d.scrollDir = -1 // simulate scrolling at release time
		if !d.Release(e2) {
			t.Fatal("Release on active drag should return true")
		}
		if d.IsActive() {
			t.Fatal("Release should deactivate")
		}
		if d.scrollDir != 0 {
			t.Fatalf("Release should clear scrollDir, got %d", d.scrollDir)
		}
		if d.active != e2 {
			t.Fatalf("Release active: %+v want %+v", d.active, e2)
		}

		// A second release with the drag now inactive returns false.
		if d.Release(e2) {
			t.Fatal("Release on inactive drag should return false")
		}
	})
}

// Property: Cancel deactivates from any state and is idempotent.
func TestDragSelection_CancelIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		startActive := rapid.Bool().Draw(t, "startActive")
		e := drawEndpoint(t, "click")
		dir := rapid.IntRange(-1, 1).Draw(t, "scrollDir")

		d := newDragSelection()
		if startActive {
			d.Begin(e)
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
	t.Parallel()
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

		setDragRect(m, x1, y1, x2, y2)

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
	t.Parallel()
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

		setDragRect(m, x1, y1, x2, y2)

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

// Property: AdvanceAutoScroll either makes viewport progress and keeps
// the anchor's source position stable (no re-anchor needed — source
// coords are scroll-invariant) or stops by setting scrollDir to 0.
// Repeated calls therefore terminate.
func TestDragAdvanceAutoScroll_Terminates(t *testing.T) {
	t.Parallel()
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
		// Place the drag at a known click inside the content area; the
		// direction determines whether we drag past the top or bottom edge.
		m.drag.Begin(geom.clickAt(minX+2, contentStartY))
		// Force a known starting scroll position so "scroll up" has room.
		if dir < 0 {
			m.mainPane.viewport.ScrollDown(20)
		}
		m.drag.scrollDir = dir

		maxIters := 1 + 2*nLines // generous bound; should terminate well before
		startOffset := m.mainPane.viewport.YOffset()
		anchorBefore := m.drag.anchor
		moved := 0
		for i := 0; i < maxIters; i++ {
			prevOffset := m.mainPane.viewport.YOffset()
			prevDir := m.drag.scrollDir
			if prevDir == 0 {
				break
			}

			m.drag.AdvanceAutoScroll(geom)

			curOffset := m.mainPane.viewport.YOffset()
			delta := curOffset - prevOffset
			if delta != 0 {
				// Anchor stays put in source space — no re-anchoring across scrolls.
				if m.drag.anchor != anchorBefore {
					t.Fatalf("anchor changed during scroll: was %+v, now %+v",
						anchorBefore, m.drag.anchor)
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
			t.Fatalf("AdvanceAutoScroll did not terminate after %d iterations (dir=%d, startOff=%d, moves=%d)",
				maxIters, dir, startOffset, moved)
		}
	})
}

// stripPaddingNewlines removes leading/trailing blank padding from a rendered
// view so split-by-line behaves predictably for highlight row checks.
func stripPaddingNewlines(s string) string {
	return strings.Trim(s, "\n")
}

// setDragRect populates the drag endpoints as if a real Begin/MoveEnd
// pair drove a click-and-drag from (x1,y1) to (x2,y2). Tests that
// bypass Begin to set up a known drag state use this so
// SelectedText/ApplyHighlight see consistent state — clickAt resolves
// each end's Pos/VpRow/OutsideDir, including wrap-row disambiguation
// for clicks past a wrap row's right edge.
func setDragRect(m *Model, x1, y1, x2, y2 int) {
	g := m.dragGeom()
	m.drag.anchor = g.clickAt(x1, y1)
	m.drag.active = g.clickAt(x2, y2)
	m.drag.inProgress = true
}

// TestExtractLineFragment_GutterStrippedBeforeTrim pins the gutter/trim
// ordering. Trimming trailing whitespace *before* removing the gutter leaves
// a blank source line's gutter ("  12   " → "  12") shorter than the gutter
// width, so the length guard declines to strip it and the line-number digits
// leak into copied text — while the on-screen highlight correctly shows
// nothing. Stripping the gutter first makes blank lines copy as "".
func TestExtractLineFragment_GutterStrippedBeforeTrim(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		gw      int
		fromCol int
		toCol   int
		want    string
	}{
		{
			name: "blank body with line-number gutter",
			line: " 12   ", gw: 6, fromCol: 0, toCol: maxColumn, want: "",
		},
		{
			name: "blank body, gutter mark only",
			line: "   ", gw: 3, fromCol: 0, toCol: maxColumn, want: "",
		},
		{
			name: "whitespace-only body",
			line: " 12      ", gw: 6, fromCol: 0, toCol: maxColumn, want: "",
		},
		{
			name: "blank body, wide column range",
			line: "  7   ", gw: 6, fromCol: 0, toCol: 40, want: "",
		},
		{
			name: "non-blank body still extracted",
			line: " 12   hello", gw: 6, fromCol: 0, toCol: maxColumn, want: "hello",
		},
		{
			name: "leading whitespace in body preserved",
			line: " 12     indented", gw: 6, fromCol: 0, toCol: maxColumn, want: "  indented",
		},
		{
			name: "trailing whitespace in body trimmed",
			line: " 12   hello   ", gw: 6, fromCol: 0, toCol: maxColumn, want: "hello",
		},
		{
			name: "column clip inside body",
			line: " 12   hello world", gw: 6, fromCol: 6, toCol: 11, want: "world",
		},
		{
			name: "no gutter, blank line",
			line: "     ", gw: 0, fromCol: 0, toCol: maxColumn, want: "",
		},
		{
			name: "wide runes in body",
			line: " 12   日本語", gw: 6, fromCol: 0, toCol: maxColumn, want: "日本語",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLineFragment(tt.line, tt.fromCol, tt.toCol, tt.gw)
			if got != tt.want {
				t.Errorf("extractLineFragment(%q, %d, %d, %d) = %q, want %q",
					tt.line, tt.fromCol, tt.toCol, tt.gw, got, tt.want)
			}
		})
	}
}

// TestStripGutterDisplayWidth_BlankLine is the cursor-column half of the same
// ordering bug: a blank line's residual gutter reported a non-zero content
// width, letting the cursor sit in columns that hold no content.
func TestStripGutterDisplayWidth_BlankLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		gw   int
		want int
	}{
		{"blank body with line number", " 12   ", 6, 0},
		{"blank body, gutter mark only", "   ", 3, 0},
		{"non-blank body", " 12   hello", 6, 5},
		{"wide runes", " 12   日本語", 6, 6},
		{"no gutter", "hello", 0, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripGutterDisplayWidth(tt.line, tt.gw); got != tt.want {
				t.Errorf("stripGutterDisplayWidth(%q, %d) = %d, want %d", tt.line, tt.gw, got, tt.want)
			}
		})
	}
}
