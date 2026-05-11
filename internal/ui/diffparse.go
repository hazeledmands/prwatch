package ui

import (
	"strconv"
	"strings"
)

// shortstatFromDiff produces a one-line summary like
// "3 files changed, 42 insertions(+), 11 deletions(-)" from a unified diff.
func shortstatFromDiff(diff string) string {
	if diff == "" {
		return ""
	}
	files, ins, del := 0, 0, 0
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files++
			inHunk = false
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case !inHunk:
			// Skip headers between files.
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			// File header markers, not insertions/deletions.
		case strings.HasPrefix(line, "+"):
			ins++
		case strings.HasPrefix(line, "-"):
			del++
		}
	}
	if files == 0 {
		return ""
	}
	parts := []string{pluralize(files, "file") + " changed"}
	if ins > 0 {
		parts = append(parts, pluralize(ins, "insertion")+"(+)")
	}
	if del > 0 {
		parts = append(parts, pluralize(del, "deletion")+"(-)")
	}
	return strings.Join(parts, ", ")
}

// parseHunkNewStart extracts just the new-file start line from a hunk header.
func parseHunkNewStart(hunkLine string) int {
	plusIdx := strings.Index(hunkLine, "+")
	if plusIdx < 0 {
		return 0
	}
	rest := hunkLine[plusIdx+1:]
	commaIdx := strings.IndexAny(rest, ", ")
	if commaIdx < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:commaIdx])
	if err != nil {
		return 0
	}
	return n
}

// parseHunkHeader parses an @@ -X,Y +A,B @@ line. Returns the new-file start
// line and new-file line count. Returns zeros if the line is malformed.
func parseHunkHeader(line string) (start, count int) {
	if !strings.HasPrefix(line, "@@") {
		return 0, 0
	}
	closeIdx := strings.Index(line[2:], "@@")
	if closeIdx < 0 {
		return 0, 0
	}
	inner := line[2 : 2+closeIdx]
	plusIdx := strings.Index(inner, "+")
	if plusIdx < 0 {
		return 0, 0
	}
	numPart := strings.TrimSpace(inner[plusIdx+1:])
	if commaIdx := strings.Index(numPart, ","); commaIdx >= 0 {
		s, err := strconv.Atoi(numPart[:commaIdx])
		if err != nil {
			return 0, 0
		}
		c, err := strconv.Atoi(numPart[commaIdx+1:])
		if err != nil {
			return 0, 0
		}
		return s, c
	}
	s, err := strconv.Atoi(numPart)
	if err != nil {
		return 0, 0
	}
	return s, 1
}

// isBinaryContent checks if content appears to be binary by looking for null
// bytes or a high ratio of non-printable characters in the first 8KB.
func isBinaryContent(content string) bool {
	if len(content) == 0 {
		return false
	}
	sample := content
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	nonPrintable := 0
	for _, b := range []byte(sample) {
		if b == 0 {
			return true
		}
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			nonPrintable++
		}
	}
	return len(sample) > 0 && nonPrintable*10 > len(sample)
}

// extractDirs returns the unique directory paths implied by a list of file paths.
func extractDirs(files []string) []string {
	dirs := make(map[string]bool)
	for _, f := range files {
		parts := strings.Split(f, "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
	}
	var result []string
	for d := range dirs {
		result = append(result, d)
	}
	return result
}
