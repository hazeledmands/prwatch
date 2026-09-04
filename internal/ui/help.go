package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// helpOverlay is the modal help screen state machine.
//
// State diagram (entrypoint Open → exits via dismiss/quit):
//
//	closed --Open()-->  open ────────────────────────────────────╮
//	   │              ↗     │  ╲                                 │
//	   │           "?"      │   '/'                              │
//	   │                    │    ↓                               │
//	   │              open+searching                             │
//	   │                    │                                    │
//	   │                  Enter (matches>0)                      │
//	   │                    ↓                                    │
//	   │              open+searchConfirmed (n/p navigation)      │
//	   ↑                                                         │
//	   ╰─────── q/esc/Help/any-unhandled-key ────────────────────╯
type helpOverlay struct {
	visible      bool
	scrollOffset int
	// search is the same input state machine the global search overlay uses;
	// what differs is only where matches come from and that a match snaps
	// scrollOffset instead of scrolling a viewport. See searchInput.
	search searchInput
}

func newHelpOverlay() *helpOverlay { return &helpOverlay{} }

func (h *helpOverlay) IsOpen() bool { return h.visible }

// searchHooks binds the shared input to the help overlay's own content and
// scroll offset.
func (h *helpOverlay) searchHooks() searchInputHooks {
	return searchInputHooks{
		Find: helpSearchMatches,
		// The overlay highlights at render time from the query itself, so
		// there is nothing to re-apply on an edit.
		Navigate: func(line int) { h.scrollOffset = line },
	}
}

// Open shows the help overlay. Resets scroll and clears search state.
func (h *helpOverlay) Open() {
	h.visible = true
	h.scrollOffset = 0
	h.search = searchInput{}
}

// Close dismisses the overlay back to the no-help state.
func (h *helpOverlay) Close() {
	h.visible = false
	h.scrollOffset = 0
	h.search = searchInput{}
}

// helpContentLines renders helpSections as a flat slice of lines, one per row,
// including the leading "Keybindings:" header and the trailing "Press q/esc..."
// footer.
//
// It is formatting only: which commands appear, in what order and grouping, and
// what each one is called all come from helpSections in keys.go, beside the
// keymap itself. The key column is generated from each binding's Keys(), so it
// cannot describe a key the app doesn't bind.
func helpContentLines() []string {
	width := 0
	for _, section := range helpSections {
		for _, b := range section {
			if w := len(keyList(b)); w > width {
				width = w
			}
		}
	}

	lines := []string{"Keybindings:", ""}
	for i, section := range helpSections {
		if i > 0 {
			lines = append(lines, "")
		}
		for _, b := range section {
			lines = append(lines, fmt.Sprintf("  %-*s  %s", width, keyList(b), b.Help().Desc))
		}
	}
	lines = append(lines, "", "Press q/esc to dismiss. Use j/k or mouse to scroll. / to search.")
	return lines
}

// helpSearchMatches returns the helpContentLines() indices matching query
// case-insensitively. An empty query matches nothing, the same as
// mainPane.FindMatches.
func helpSearchMatches(query string) []int {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var matches []int
	for i, line := range helpContentLines() {
		if strings.Contains(strings.ToLower(line), q) {
			matches = append(matches, i)
		}
	}
	return matches
}

// HandleKey processes a key press while help is open. Returns a tea.Cmd
// (for tea.Quit on Ctrl+C) plus a bool indicating whether the key was handled
// inside the overlay. visibleHeight is the number of help rows that fit
// on screen above the search bar.
func (h *helpOverlay) HandleKey(msg tea.KeyPressMsg, visibleHeight int) tea.Cmd {
	if h.search.IsSearching() {
		// Note that ctrl+c is not a cancel here, unlike in the global search:
		// it carries no text, so it falls through as an inert key.
		h.search.applyEditKey(msg, h.searchHooks())
		return nil
	}

	if h.search.IsConfirmed() {
		if h.search.applyNavKey(msg, h.searchHooks()) {
			return nil
		}
		// The key wasn't a search key, so the search dismissed itself and the
		// key is re-dispatched as a normal help-overlay key.
		return h.HandleKey(msg, visibleHeight)
	}

	helpLines := helpContentLines()

	switch {
	case key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.Help):
		h.Close()
		return nil
	case key.Matches(msg, keys.Search):
		h.search.begin()
		return nil
	case key.Matches(msg, keys.Down):
		if h.scrollOffset < len(helpLines)-visibleHeight {
			h.scrollOffset++
		}
		return nil
	case key.Matches(msg, keys.Up):
		if h.scrollOffset > 0 {
			h.scrollOffset--
		}
		return nil
	case key.Matches(msg, keys.PageDown):
		maxOffset := max(0, len(helpLines)-visibleHeight)
		h.scrollOffset = min(h.scrollOffset+visibleHeight, maxOffset)
		return nil
	case key.Matches(msg, keys.PageUp):
		h.scrollOffset = max(0, h.scrollOffset-visibleHeight)
		return nil
	case key.Matches(msg, keys.GoTop):
		h.scrollOffset = 0
		return nil
	case key.Matches(msg, keys.GoBottom):
		h.scrollOffset = max(0, len(helpLines)-visibleHeight)
		return nil
	case key.Matches(msg, keys.QuitImmediate):
		return tea.Quit
	default:
		h.Close()
		return nil
	}
}

// HandleWheel scrolls the help overlay one row up or down per wheel event.
func (h *helpOverlay) HandleWheel(direction int, visibleHeight int) {
	helpLines := helpContentLines()
	if direction < 0 && h.scrollOffset > 0 {
		h.scrollOffset--
	} else if direction > 0 && h.scrollOffset < len(helpLines)-visibleHeight {
		h.scrollOffset++
	}
}

// PageUp scrolls the help overlay up by one visible page (clamped at zero).
func (h *helpOverlay) PageUp(visibleHeight int) {
	h.scrollOffset = max(0, h.scrollOffset-visibleHeight)
}

// Render builds the visible help screen (without the surrounding status bar).
// visibleHeight is the number of help rows that fit on screen.
func (h *helpOverlay) Render(visibleHeight int) string {
	lines := helpContentLines()

	if q := h.search.Query(); q != "" {
		for i, line := range lines {
			lines[i] = highlightMatchInLine(line, q)
		}
	}

	end := h.scrollOffset + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}
	start := h.scrollOffset
	if start > len(lines) {
		start = len(lines)
	}
	visible := lines[start:end]

	result := strings.Join(visible, "\n")

	if h.search.IsSearching() {
		searchBar := "/" + h.search.query + "_"
		if len(h.search.matches) > 0 {
			searchBar += fmt.Sprintf("  %d/%d", h.search.matchIdx+1, len(h.search.matches))
		} else if h.search.query != "" {
			searchBar += "  0/0"
		}
		result += "\n" + searchBar
	} else if h.search.IsConfirmed() {
		searchBar := fmt.Sprintf("/%s  %d/%d", h.search.query, h.search.matchIdx+1, len(h.search.matches))
		result += "\n" + searchBar
	}

	return result
}
