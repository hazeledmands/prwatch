package ui

import (
	"regexp"
	"strings"
	"unicode/utf8"

	runewidth "github.com/mattn/go-runewidth"
)

// ansiStripRE matches ANSI escape sequences (SGR and OSC 8 hyperlinks).
var ansiStripRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\]8;;[^\x1b]*\x1b\\`)

// stripANSIForWidth removes ANSI escape sequences for width calculation.
func stripANSIForWidth(s string) string {
	return ansiStripRE.ReplaceAllString(s, "")
}

// displayWidthOf returns the display width of a string, accounting for wide
// characters (CJK, emoji) and tab stops.
func displayWidthOf(s string) int {
	return runewidth.StringWidth(s)
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
		stripped := stripANSIForWidth(lines[i])
		w := displayWidthOf(stripped)
		if w < width {
			lines[i] += strings.Repeat(" ", width-w)
		}
	}
	for len(lines) < height {
		lines = append(lines, emptyLine)
	}

	return strings.Join(lines, "\n")
}

// splitAtDisplayCols splits a line (which may contain ANSI escape codes) into
// three parts at display column boundaries: before fromCol, between fromCol
// and toCol, and after toCol. ANSI escape codes are preserved in whichever
// segment they fall in.
func splitAtDisplayCols(line string, fromCol, toCol int) (before, middle, after string) {
	col := 0
	fromByte := -1
	toByte := -1
	inEscape := false
	for i, r := range line {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			if fromByte < 0 && col >= fromCol {
				fromByte = i
			}
			if toByte < 0 && col >= toCol {
				toByte = i
			}
			inEscape = true
			continue
		}
		if fromByte < 0 && col >= fromCol {
			fromByte = i
		}
		if toByte < 0 && col >= toCol {
			toByte = i
		}
		col += runewidth.RuneWidth(r)
	}
	if fromByte < 0 {
		fromByte = len(line)
	}
	if toByte < 0 {
		toByte = len(line)
	}
	if fromByte > toByte {
		fromByte = toByte
	}
	return line[:fromByte], line[fromByte:toByte], line[toByte:]
}

// sliceByDisplayCol extracts a substring from s between display columns
// [fromCol, toCol). This correctly handles multi-byte and double-width
// characters, avoiding mid-character byte slicing.
func sliceByDisplayCol(s string, fromCol, toCol int) string {
	if fromCol >= toCol {
		return ""
	}
	col := 0
	startByte := len(s)
	endByte := len(s)
	foundStart := false
	for i, r := range s {
		if !foundStart && col >= fromCol {
			startByte = i
			foundStart = true
		}
		w := runewidth.RuneWidth(r)
		col += w
		if foundStart && col >= toCol {
			endByte = i + utf8.RuneLen(r)
			return s[startByte:endByte]
		}
	}
	if !foundStart {
		return ""
	}
	return s[startByte:endByte]
}
