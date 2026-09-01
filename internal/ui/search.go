package ui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// searchView is the narrow interface searchOverlay needs from the main pane to
// find and navigate matches.
type searchView interface {
	FindMatches(query string) []int
	SetSearchQuery(query string)
	ScrollToSourceLine(sourceLine int)
}

// searchOverlay owns the global search input state machine that lives at the
// bottom of the screen across all modes. Per the spec, search only matches
// against main-pane content; the sidebar-pane branch from the original
// searchMatch type is no longer needed.
//
// State diagram:
//
//	closed --Open()--> active --Enter (non-empty)--> confirmed
//	   ↑                  │                              │
//	   ╰───── Clear() ────┴── Enter (empty) ─── Escape ──╯
type searchOverlay struct {
	searching bool
	confirmed bool
	query     string
	matches   []int
	matchIdx  int
}

func newSearchOverlay() *searchOverlay { return &searchOverlay{} }

func (s *searchOverlay) IsSearching() bool { return s.searching }
func (s *searchOverlay) IsConfirmed() bool { return s.confirmed }
func (s *searchOverlay) IsActive() bool    { return s.searching || s.confirmed }
func (s *searchOverlay) Query() string     { return s.query }

// Open begins a fresh input.
func (s *searchOverlay) Open() {
	s.searching = true
	s.confirmed = false
	s.query = ""
	s.matches = nil
	s.matchIdx = 0
}

// Clear cancels search entirely and tells view to drop its highlight query.
func (s *searchOverlay) Clear(view searchView) {
	s.searching = false
	s.confirmed = false
	s.query = ""
	s.matches = nil
	s.matchIdx = 0
	if view != nil {
		view.SetSearchQuery("")
	}
}

// updateMatches refreshes the match index list against the current query and
// scrolls the view to the first match if any.
func (s *searchOverlay) updateMatches(view searchView) {
	s.matches = view.FindMatches(s.query)
	s.matchIdx = 0
	view.SetSearchQuery(s.query)
	s.navigateToCurrent(view)
}

// navigateToCurrent scrolls the view to whichever match matchIdx points at.
func (s *searchOverlay) navigateToCurrent(view searchView) {
	if len(s.matches) == 0 {
		return
	}
	// FindMatches returns 0-indexed content lines; ScrollToSourceLine takes
	// 1-indexed source lines and maps them through formatting + wrapping.
	view.ScrollToSourceLine(s.matches[s.matchIdx] + 1)
}

// HandleInputKey processes a key while the user is typing into the search
// input. Returns true when the key was consumed (which is always, while
// searching). After Enter on empty input the overlay closes (Clear was
// called); after Enter on non-empty input it transitions to confirmed (if
// there were matches) or stays open without matches.
func (s *searchOverlay) HandleInputKey(msg tea.KeyPressMsg, view searchView) {
	switch {
	case key.Matches(msg, keys.QuitImmediate), msg.Code == tea.KeyEscape:
		s.Clear(view)
	case msg.Code == tea.KeyEnter:
		if s.query == "" {
			s.Clear(view)
			return
		}
		s.searching = false
		if len(s.matches) > 0 {
			s.confirmed = true
		}
	case msg.Code == tea.KeyBackspace:
		if len(s.query) > 0 {
			s.query = s.query[:len(s.query)-1]
		}
		if s.query == "" {
			s.Clear(view)
			return
		}
		s.updateMatches(view)
	default:
		if msg.Text != "" {
			s.query += msg.Text
		}
		s.updateMatches(view)
	}
}

// HandleNavKey processes a key while in confirmed (n/p navigation) mode.
// Returns true when the key was handled here. On unhandled keys (anything
// other than search-next/search-prev/escape/quit) the overlay clears so the
// caller can re-dispatch the same key as a normal mode key.
func (s *searchOverlay) HandleNavKey(msg tea.KeyPressMsg, view searchView) bool {
	switch {
	case key.Matches(msg, keys.SearchNext):
		if len(s.matches) > 0 {
			s.matchIdx = (s.matchIdx + 1) % len(s.matches)
			s.navigateToCurrent(view)
		}
		return true
	case key.Matches(msg, keys.SearchPrev):
		if len(s.matches) > 0 {
			s.matchIdx = (s.matchIdx - 1 + len(s.matches)) % len(s.matches)
			s.navigateToCurrent(view)
		}
		return true
	case msg.Code == tea.KeyEscape, key.Matches(msg, keys.QuitConfirm):
		s.Clear(view)
		return true
	default:
		s.Clear(view)
		return false
	}
}

// RenderBar returns the search-bar line shown at the bottom of the screen
// during searching or confirmed mode. Returns "" when search is inactive.
func (s *searchOverlay) RenderBar() string {
	if !s.IsActive() {
		return ""
	}
	var bar string
	if s.searching {
		bar = "/" + s.query + "_"
	} else {
		bar = "/" + s.query
	}
	if len(s.matches) > 0 {
		bar += fmt.Sprintf("  %d/%d", s.matchIdx+1, len(s.matches))
	} else if s.query != "" {
		bar += "  0/0"
	}
	return bar
}
