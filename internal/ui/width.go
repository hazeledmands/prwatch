package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// This file is the single source of truth for display-width math. See
// PROMPT.md, "unicode width accounting" (under `## layout`):
//
//   - the terminal cell grid is ground truth
//   - all width math comes from ONE grapheme-cluster-aware function — the same
//     measurement the renderer uses — always applied to whole strings
//   - grapheme clusters are indivisible: no cursor, selection endpoint, wrap
//     break, or slice may land inside one
//
// Nothing outside this file may call `github.com/mattn/go-runewidth` directly;
// `TestNoDirectRunewidthOutsideOracle` (width_test.go) enforces that.
//
// The renderer, lipgloss v2, measures every string it lays out with
// `ansi.StringWidth` (charm.land/lipgloss/v2 size.go:17, align.go:17,
// join.go:88/140, position.go:56, whitespace.go:53), and this oracle is defined
// to agree with it: it sums exactly the grapheme-cluster widths that
// `ansi.StringWidth` sums, via the same `ansi.FirstGraphemeCluster`.
//
// Two deliberate exceptions, and the reason the oracle is defined as the walk
// rather than as a direct call to `ansi.StringWidth`:
//
//  1. That function has an ASCII fast path that does NOT participate in grapheme
//     segmentation: it prints an ASCII byte as one cell and then measures
//     whatever follows as if it began a new cluster. So it scores "Aः" (ASCII
//     base plus U+0903, a spacing mark) as 2 and " 🏿" (space plus an emoji
//     modifier) as 3, though each is one cluster of width 1.
//  2. It breaks clusters at escape boundaries, so a ZWJ sequence with a color
//     span per component measures 6 instead of 2 (see eachDisplayCluster).
//
// Reproducing either quirk would mean splitting a cluster, which PROMPT.md
// forbids and which is not exotic at all: decomposed accented Latin text ("e" +
// U+0301) is ordinary content, and splitting it makes a selection begin with a
// bare floating accent. A real terminal agrees with segmentation in both cases —
// an SGR sequence emits no cell and cannot split a glyph on screen — so
// following the cell grid means following the clusters.
//
// So the walk keeps clusters whole, and this oracle agrees with the renderer
// everywhere except those two classes, where the renderer's own measure
// contradicts its own segmentation. That residue is precisely the class
// PROMPT.md puts out of scope ("terminal emulators disagree with each other on
// exotic scripts ... that residual variance is accepted"). Everything the app
// actually renders — every subsystem — agrees with this one function, which is
// the guarantee the spec asks for.
//
// Widths are NOT additive across concatenation: a cluster can absorb the text
// that follows it (U+0600 and friends are Prepend-class) or merge backward into
// it (combining marks), so `displayWidth(a+b)` may be less than
// `displayWidth(a) + displayWidth(b)`. Always measure the whole final string.

// displayWidth returns the number of terminal cells s occupies. It is the width
// oracle: every geometry decision in this package resolves to this function.
// ANSI escape sequences are ignored; grapheme clusters are measured as a unit.
func displayWidth(s string) int {
	// Fast path: plain printable ASCII, no escapes, no clusters possible.
	if n, ok := asciiWidth(s); ok {
		return n
	}
	w := 0
	eachDisplayCluster(s, func(c displayCluster) bool {
		w += c.Width
		return true
	})
	return w
}

// asciiWidth returns len(s) when s is entirely printable ASCII — the
// overwhelmingly common case, where one byte is one cell and no grapheme
// clustering is possible. ok is false when s needs the full walk.
func asciiWidth(s string) (int, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return 0, false
		}
	}
	return len(s), true
}

// rendererWidth returns lipgloss's own measurement of s.
//
// Use this ONLY for geometry that lipgloss itself lays out — that is, where a
// string is handed to `style.Width(n).Render(...)` and lipgloss decides whether
// to wrap it. For such a decision lipgloss is the authority by construction: no
// amount of trimming under the oracle prevents a wrap that lipgloss's own
// measure triggers. Everything else — content geometry, cursor columns,
// selection, wrapping we perform ourselves — uses displayWidth.
//
// The two measures agree except in the classes described in the file comment
// above, so this distinction is invisible for ordinary content. It exists so
// that when they do differ, each decision is made in the frame of whoever acts
// on it, rather than mixing the two and wrapping a row nobody budgeted for.
func rendererWidth(s string) int {
	return lipgloss.Width(s)
}

// fitToRendererWidth trims s with an ellipsis until THE RENDERER agrees it fits
// within width, returning s unchanged when it already does.
//
// It exists because truncating under the oracle is not sufficient to stop
// lipgloss wrapping: on the divergence classes the oracle can consider a string
// comfortably inside the budget while lipgloss measures it wider and hard-wraps
// it onto another row. In a status bar that silently adds a row, breaking the
// agreement between statusBarLineCount and renderStatusBar and shifting every
// click target below it — see TestStatusBarRowCountMatchesLayout.
func fitToRendererWidth(s string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	out := truncateToWidth(s, width, tail)
	// Shrink the oracle-side budget until the renderer is satisfied. Each step
	// drops at least one cell and budget 0 yields "", so this terminates.
	for budget := width; rendererWidth(out) > width && budget > 0; {
		budget--
		out = truncateToWidth(s, budget, tail)
	}
	return out
}

// displayCluster is one atom of a display-width walk: either a grapheme cluster
// or an ANSI escape sequence (which occupies no cells).
type displayCluster struct {
	// Text is the cluster's bytes, a substring of the walked string.
	Text string
	// ByteOff is Text's offset within the walked string.
	ByteOff int
	// Col is the display column at which this cluster starts.
	Col int
	// Width is how many cells the cluster occupies; 0 for escape sequences,
	// and also 0 for clusters that render into a previous cell or none at all.
	Width int
	// IsEscape reports whether Text is an ANSI escape sequence rather than
	// printable content.
	IsEscape bool
}

// eachDisplayCluster walks s one atom at a time — a grapheme cluster, an ASCII
// character, or an ANSI escape sequence — in order, reporting each atom's byte
// offset and starting display column. Iteration stops early if fn returns false.
//
// The summed Width of the atoms equals displayWidth(s) exactly; the two are
// pinned together by TestClusterWalkAgreesWithOracle over random input. That
// agreement is the whole point: geometry derived from this walk can never drift
// from the width the renderer will lay out. Use it for any walk needing
// per-position geometry — never step rune-by-rune, since a rune-level width is
// meaningless inside a multi-rune cluster.
//
// Grapheme segmentation is applied uniformly, ASCII included, so that a base
// character and its combining marks are always one atom. See the file comment
// for why this deliberately does not reproduce ansi.StringWidth's ASCII fast
// path.
// Escape sequences are transparent to clustering: segmentation runs over the
// ANSI-stripped text, so an SGR sequence sitting inside a grapheme cluster does
// not split it. This matters in practice — the syntax highlighter emits one
// color span per token, which routinely lands escapes between the parts of a ZWJ
// emoji sequence ("ESC[..m👩ESC[0m ESC[..m<ZWJ>ESC[0m ESC[..m👩ESC[0m..."). Breaking
// clusters there would measure that family emoji as 6 cells instead of 2, and
// would make displayWidth(s) != displayWidth(stripANSIForWidth(s)) — i.e. the
// width of a line would depend on how the highlighter chunked its spans, which
// contradicts this file's "ANSI escape sequences are ignored" contract.
// TestWidthIgnoresEscapePlacement pins that invariant.
//
// Atoms still tile the string exactly. Escapes *between* clusters are reported
// as their own zero-width IsEscape atoms; escapes *inside* a cluster travel with
// it, since the cluster is indivisible.
func eachDisplayCluster(s string, fn func(displayCluster) bool) {
	// Fast path: no escapes, so segmentation runs directly on s.
	if strings.IndexByte(s, 0x1b) < 0 {
		col := 0
		for i := 0; i < len(s); {
			text, w := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
			if len(text) == 0 {
				text = s[i : i+1]
				w = 0
			}
			if !fn(displayCluster{Text: text, ByteOff: i, Col: col, Width: w}) {
				return
			}
			col += w
			i += len(text)
		}
		return
	}

	escapes := ansiStripRE.FindAllStringIndex(s, -1)
	stripped := ansiStripRE.ReplaceAllString(s, "")

	orig := 0 // byte offset in s
	e := 0    // index of the next escape in s at or after orig
	col := 0

	// emitEscapesAt reports any escapes starting exactly at orig, advancing past
	// them. Returns false if iteration should stop.
	emitEscapesAt := func() bool {
		for e < len(escapes) && escapes[e][0] == orig {
			end := escapes[e][1]
			if !fn(displayCluster{Text: s[orig:end], ByteOff: orig, Col: col, Width: 0, IsEscape: true}) {
				return false
			}
			orig = end
			e++
		}
		return true
	}

	for sp := 0; sp < len(stripped); {
		text, w := ansi.FirstGraphemeCluster(stripped[sp:], ansi.GraphemeWidth)
		if len(text) == 0 {
			text = stripped[sp : sp+1]
			w = 0
		}
		if !emitEscapesAt() {
			return
		}
		// Consume len(text) content bytes from s, stepping over any escapes
		// interleaved inside the cluster so they travel with it.
		start := orig
		for consumed := 0; consumed < len(text); {
			if e < len(escapes) && escapes[e][0] == orig {
				orig = escapes[e][1]
				e++
				continue
			}
			orig++
			consumed++
		}
		if !fn(displayCluster{Text: s[start:orig], ByteOff: start, Col: col, Width: w}) {
			return
		}
		col += w
		sp += len(text)
	}
	// Any escapes trailing after the last content byte.
	emitEscapesAt()
}

// truncateToWidth returns the longest prefix of s whose display width is at most
// width, never cutting a grapheme cluster in half. tail (e.g. "…") is appended
// if s had to be shortened, and the result including tail still measures at most
// width.
//
// EVERY ANSI escape sequence in s is preserved, including those past the
// truncation point — the walk continues to the end of the string once the
// budget is spent, emitting escapes and dropping only printable content. This is
// ansi.Truncate's policy and it is load-bearing, not tidiness: escape sequences
// come in open/close pairs, so stopping at the cut would drop the closing half.
// A status bar carrying a `makeHyperlink` (OSC 8) cut mid-link would emit the
// opening OSC 8 with no terminator, and the "…" plus everything rendered after
// it would become part of the clickable link. The same applies to SGR resets,
// which would otherwise leak color past the truncation.
func truncateToWidth(s string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	budget := width - displayWidth(tail)
	if budget < 0 {
		budget = 0
	}
	var b strings.Builder
	spent := false
	eachDisplayCluster(s, func(c displayCluster) bool {
		if c.IsEscape {
			b.WriteString(c.Text)
			return true
		}
		if spent || c.Col+c.Width > budget {
			spent = true
			// Content is dropped, but a cluster can carry escapes inside it
			// (a color span landing mid-cluster), so keep those.
			b.WriteString(ansiEscapesIn(c.Text))
			return true
		}
		b.WriteString(c.Text)
		return true
	})
	return b.String() + tail
}

// ansiEscapesIn returns just the ANSI escape sequences of s, concatenated in
// order, with all printable content removed.
func ansiEscapesIn(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return ""
	}
	return strings.Join(ansiStripRE.FindAllString(s, -1), "")
}

// padToWidth appends spaces to s until it measures exactly width cells, then
// returns it. If s already measures at least width, it is returned unchanged.
//
// It adds the whole shortfall in one shot and VERIFIES the result with a single
// re-measure, falling back to appending one space at a time only when that
// verification fails. The one-shot cannot be trusted blindly because appending a
// space does not always add a cell: a Prepend-class cluster at the end of s
// absorbs the first space into itself (see the "ः؀" case in
// TestPadToWidth_AbsorbedSpaces). But when the fast path's re-measure says the
// result is exactly `width`, it is correct by definition, whatever the tail.
//
// The fallback used to be the only path, which made this O(width²) per row — two
// measurements became one per padded cell. padToHeight runs it on every row of
// every frame, so an empty 192x51 render spent 13.6ms almost entirely here and
// tripped the 1s View() timeout under -race. See BenchmarkViewEmpty and the perf
// entry in BUG_REPORTS.md.
//
// In the fallback, each appended
// space raises the width by 0 or 1 and never lowers it, and a cluster can
// absorb only a bounded number of spaces, so the loop converges on width exactly.
func padToWidth(s string, width int) string {
	for {
		w := displayWidth(s)
		if w >= width {
			return s
		}
		// Add the shortfall at once; keep it only if it measures exactly.
		if out := s + strings.Repeat(" ", width-w); displayWidth(out) == width {
			return out
		}
		// The tail absorbed the padding rather than growing by it. Give it a
		// single space to complete that cluster, then retry the one-shot: a
		// cluster can only swallow one space, and there are finitely many, so
		// this converges — and in practice always on the next iteration.
		s += " "
	}
}

// fitToWidth returns s adjusted to measure exactly width cells: truncated at a
// cluster boundary if too wide, space-padded if too narrow.
func fitToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) > width {
		s = truncateToWidth(s, width, "")
	}
	return padToWidth(s, width)
}
