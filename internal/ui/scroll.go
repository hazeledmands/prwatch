package ui

// Scroll-window arithmetic, shared by everything in the UI that shows a
// window onto a longer list: the sidebar, the help overlay, and the main
// pane's cursor.
//
// These are here because the same two calculations were written four times
// and the copies did not agree. The help overlay's j/k and wheel handlers
// compared the offset against an unguarded `len(lines)-visibleHeight`, which
// is negative when the content is shorter than the window, while its own
// PageDown and GoBottom used `max(0, ...)` for the identical bound. The
// sidebar spelled the guarded form out twice, in ScrollDown and in
// clampOffsetBounds. Only one of the four could be wrong at a time, and which
// one depended on the key you pressed.
//
// `charm.land/bubbles/v2/viewport` has an EnsureVisible of its own, and it is
// deliberately not used: it sets the offset to the index outright, so
// scrolling *down* to a row puts that row at the top of the window rather
// than the bottom. Every caller here wants the minimal move instead, which is
// what arrowing down through a list should do.

// maxScrollOffset is the largest offset that still shows content: the point
// where the last item sits on the window's bottom row. Content that fits
// entirely in the window — or a window with no rows at all — cannot scroll.
func maxScrollOffset(total, visible int) int {
	if visible <= 0 || total <= visible {
		return 0
	}
	return total - visible
}

// clampOffset constrains a scroll offset to [0, maxScrollOffset(total,
// visible)]. An offset already in range is returned unchanged.
//
// Use it after the content or the window changes, when the user's scroll
// position should be preserved as far as it still can be.
func clampOffset(offset, total, visible int) int {
	if offset < 0 {
		return 0
	}
	if ceiling := maxScrollOffset(total, visible); offset > ceiling {
		return ceiling
	}
	return offset
}

// ensureVisible returns the offset closest to the current one that puts index
// inside the window [offset, offset+visible). Scrolling up lands index on the
// window's top row, scrolling down lands it on the bottom row, and an index
// already on screen doesn't scroll at all.
//
// The result is clamped, so this also fixes an offset that arrived out of
// range — scrolling to the index only ever constrains the offset relative to
// the index itself, and an offset past the ceiling with the index already
// inside the window would otherwise survive untouched.
func ensureVisible(offset, index, total, visible int) int {
	offset = clampOffset(offset, total, visible)
	if visible <= 0 {
		return offset
	}
	if index < offset {
		offset = index
	} else if index >= offset+visible {
		offset = index - visible + 1
	}
	return clampOffset(offset, total, visible)
}
