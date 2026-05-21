package ui

// nextHunkStart picks the next (direction=+1) or previous (direction=-1)
// hunk's StartLine relative to currentLine. Hunks are assumed sorted by
// StartLine. Strictly-greater (or strictly-less) is the comparison, so
// repeated forward presses from inside a hunk advance one hunk per
// press — currently-inside-K never short-circuits to K's own start.
// Backward from K's start goes to K-1's start; backward from inside K
// goes to K's start (vim ]c/[c behavior). Wraps to the first/last when
// no strictly-forward/backward neighbor exists. Returns (-1, false)
// when the hunk list is empty.
func nextHunkStart(hunks []diffHunk, currentLine, direction int) (int, bool) {
	if len(hunks) == 0 {
		return -1, false
	}
	if direction > 0 {
		for _, h := range hunks {
			if h.StartLine > currentLine {
				return h.StartLine, true
			}
		}
		return hunks[0].StartLine, true
	}
	for i := len(hunks) - 1; i >= 0; i-- {
		if hunks[i].StartLine < currentLine {
			return hunks[i].StartLine, true
		}
	}
	return hunks[len(hunks)-1].StartLine, true
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
