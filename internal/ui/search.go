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

// searchOverlay is the global search input that lives at the bottom of the
// screen across all modes. Per the spec, search only matches against main-pane
// content; the sidebar-pane branch from the original searchMatch type is no
// longer needed.
//
// It is a searchInput bound to the main pane: the state machine and every key
// it understands live there and are shared with the help overlay's search, and
// what this type adds is the view plumbing — where matches come from, what
// scrolling to one means — plus the search bar.
//
// The view is a per-call parameter rather than a field. Model hands in a fresh
// one each time, so storing it would mean holding a reference across refreshes
// that replace it.
type searchOverlay struct {
	searchInput
}

func newSearchOverlay() *searchOverlay { return &searchOverlay{} }

// searchHooks binds the input to a main-pane view for the duration of one
// call. A nil view still yields usable hooks so that clearing a search without
// a view attached stays a no-op rather than a panic.
func searchHooks(view searchView) searchInputHooks {
	if view == nil {
		return searchInputHooks{Find: func(string) []int { return nil }}
	}
	return searchInputHooks{
		Find:      view.FindMatches,
		AfterEdit: view.SetSearchQuery,
		// FindMatches returns 0-indexed content lines; ScrollToSourceLine
		// takes 1-indexed source lines and maps them through formatting +
		// wrapping.
		Navigate: func(line int) { view.ScrollToSourceLine(line + 1) },
		OnReset:  func() { view.SetSearchQuery("") },
	}
}

// Open begins a fresh input.
func (s *searchOverlay) Open() { s.begin() }

// Clear cancels search entirely and tells view to drop its highlight query.
func (s *searchOverlay) Clear(view searchView) { s.reset(searchHooks(view)) }

// RecomputeMatches re-runs the current query against the view's content and
// clamps matchIdx back into range, without re-highlighting or scrolling. See
// searchInput.recompute for why a refresh keeps the query.
func (s *searchOverlay) RecomputeMatches(view searchView) { s.recompute(searchHooks(view)) }

// HandleInputKey processes a key while the user is typing into the search
// input. Every key is consumed while searching.
func (s *searchOverlay) HandleInputKey(msg tea.KeyPressMsg, view searchView) {
	// Unlike the help overlay's search, the global one takes ctrl+c as a
	// cancel rather than passing it through as text.
	if key.Matches(msg, keys.QuitImmediate) {
		s.Clear(view)
		return
	}
	s.applyEditKey(msg, searchHooks(view))
}

// HandleNavKey processes a key while in confirmed (n/p navigation) mode.
// Returns true when the key was handled here. On unhandled keys the overlay
// clears so the caller can re-dispatch the same key as a normal mode key.
func (s *searchOverlay) HandleNavKey(msg tea.KeyPressMsg, view searchView) bool {
	return s.applyNavKey(msg, searchHooks(view))
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
