package ui

// nextDiffLine picks the next (direction=+1) or previous (direction=-1) diff
// line from diffLines relative to currentLine. Wraps to the first/last when no
// strictly-forward/backward neighbor exists. Returns (-1, false) when there
// are no diff lines at all.
func nextDiffLine(diffLines []int, currentLine, direction int) (int, bool) {
	if len(diffLines) == 0 {
		return -1, false
	}
	if direction > 0 {
		for _, l := range diffLines {
			if l > currentLine {
				return l, true
			}
		}
		return diffLines[0], true
	}
	for i := len(diffLines) - 1; i >= 0; i-- {
		if diffLines[i] < currentLine {
			return diffLines[i], true
		}
	}
	return diffLines[len(diffLines)-1], true
}

// nextLeafIndex returns the next (direction=+1) or previous (direction=-1)
// sidebar item index that is a leaf (not a separator, cutline, or directory),
// starting from start. Returns -1 if no leaf exists. Wraps modulo len(items).
func nextLeafIndex(items []sidebarItem, start, direction int) int {
	n := len(items)
	if n == 0 {
		return -1
	}
	for i := 1; i < n; i++ {
		idx := (start + i*direction + n) % n
		it := items[idx]
		if it.kind != itemSeparator && it.kind != itemCutline && !it.isDir {
			return idx
		}
	}
	return -1
}
