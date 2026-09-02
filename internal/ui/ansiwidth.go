package ui

import (
	"regexp"
	"strings"
)

// ansiStripRE matches ANSI escape sequences (SGR and OSC 8 hyperlinks).
var ansiStripRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\]8;;[^\x1b]*\x1b\\`)

// stripANSIForWidth removes ANSI escape sequences for width calculation.
func stripANSIForWidth(s string) string {
	return ansiStripRE.ReplaceAllString(s, "")
}

// displayWidthOf returns the display width of a string, accounting for wide
// characters (CJK, emoji) and grapheme clusters.
//
// Deprecated in spirit: it is a thin alias kept because it reads better at some
// call sites. The oracle itself is displayWidth (width.go).
func displayWidthOf(s string) int {
	return displayWidth(s)
}

// padToHeight ensures the output has exactly the target number of lines,
// padding with empty lines or truncating as needed. Each line is also padded
// to the target width.
func padToHeight(content string, width, height int) string {
	if height <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")

	if len(lines) > height {
		lines = lines[:height]
	}

	emptyLine := strings.Repeat(" ", width)
	for i := range lines {
		// padToWidth, not `strings.Repeat(" ", width-w)`: display width is not
		// additive across concatenation, so a line ending in a Prepend-class
		// cluster absorbs the first appended space and a counted shortfall
		// leaves the row short of its promised width. This is the same bug that
		// mis-padded renderTitleRow, and these rows carry arbitrary file
		// content via RenderOnce.
		lines[i] = padToWidth(lines[i], width)
	}
	for len(lines) < height {
		lines = append(lines, emptyLine)
	}

	return strings.Join(lines, "\n")
}

// colRounding selects what happens to a grapheme cluster that only partly
// covers a requested display-column range — for instance the wide glyph under a
// click that landed on its second cell. Clusters are indivisible (PROMPT.md,
// "unicode width accounting"), so a partly-covered cluster is either taken whole
// or dropped whole; these are the two policies, and every caller must say which
// it means.
type colRounding int

const (
	// roundInward keeps only clusters that lie wholly inside [fromCol, toCol).
	// Use it when the result must fit a budget — clipping a rendered row to a
	// pane, where taking a straddling glyph would overflow the boundary.
	roundInward colRounding = iota

	// roundOutward keeps every cluster that overlaps [fromCol, toCol) at all,
	// symmetrically at both edges. Use it for selection and highlighting: per
	// PROMPT.md's mouse-behavior rule, an endpoint on any cell of a wide glyph
	// selects the whole glyph, at the start and the end alike.
	roundOutward
)

// displayColByteRange resolves the display-column range [fromCol, toCol) to a
// byte range in s under the given rounding policy, never landing inside a
// grapheme cluster or an ANSI escape sequence. It is the single primitive
// behind splitAtDisplayCols and sliceByDisplayCol.
//
// Zero-width atoms — ANSI escape sequences, and Prepend-class clusters that
// render into the following cell — occupy no cells of their own, so they cannot
// be placed by column overlap. They are attributed to the content that FOLLOWS
// them: that is what Prepend means, and it is also what keeps a style-setting
// escape with the text it styles.
//
// Concretely, the range's start extends backward over any zero-width atoms
// immediately preceding the first included cluster. Without that, outward
// rounding could pull a wide glyph into the selection while leaving the escape
// that colors it behind in the preceding segment, which re-styles the text on
// either side of the boundary.
func displayColByteRange(s string, fromCol, toCol int, mode colRounding) (start, end int) {
	start, end = -1, -1
	pendingZeroWidth := -1 // byte offset of the current run of zero-width atoms
	eachDisplayCluster(s, func(c displayCluster) bool {
		if c.Width == 0 {
			// Remember where this run began; it joins whatever content follows.
			if pendingZeroWidth < 0 {
				pendingZeroWidth = c.ByteOff
			}
			return true
		}
		var in bool
		if mode == roundOutward {
			in = c.Col < toCol && c.Col+c.Width > fromCol
		} else {
			in = c.Col >= fromCol && c.Col+c.Width <= toCol
		}
		if in {
			if start < 0 {
				start = c.ByteOff
				if pendingZeroWidth >= 0 {
					start = pendingZeroWidth
				}
			}
			end = c.ByteOff + len(c.Text)
		}
		pendingZeroWidth = -1
		return true
	})
	if start < 0 {
		// Nothing selected. Collapse to a point at the requested start so
		// callers splitting on the range get an empty middle segment.
		p := len(s)
		eachDisplayCluster(s, func(c displayCluster) bool {
			if c.Col >= fromCol {
				p = c.ByteOff
				return false
			}
			return true
		})
		return p, p
	}
	return start, end
}

// splitAtDisplayCols splits a line (which may contain ANSI escape codes) into
// three parts at display column boundaries: before fromCol, between fromCol
// and toCol, and after toCol. ANSI escape codes are preserved in whichever
// segment they fall in.
//
// The middle segment rounds outward: it covers every cluster the range touches,
// so a highlight painted over it covers whole glyphs rather than half of one.
func splitAtDisplayCols(line string, fromCol, toCol int) (before, middle, after string) {
	if fromCol >= toCol {
		start, _ := displayColByteRange(line, fromCol, toCol, roundOutward)
		return line[:start], "", line[start:]
	}
	start, end := displayColByteRange(line, fromCol, toCol, roundOutward)
	return line[:start], line[start:end], line[end:]
}

// sliceByDisplayCol extracts the substring of s covering display columns
// [fromCol, toCol) under the given rounding policy. It never slices inside a
// grapheme cluster or an ANSI escape sequence.
func sliceByDisplayCol(s string, fromCol, toCol int, mode colRounding) string {
	if fromCol >= toCol {
		return ""
	}
	start, end := displayColByteRange(s, fromCol, toCol, mode)
	return s[start:end]
}
