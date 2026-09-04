package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// searchInput is the query-editing and match-navigation state machine behind
// both "/" searches in the app: the global search overlay (search.go) and the
// help overlay's own search (help.go).
//
// The two used to carry their own copy of these five fields and their own copy
// of the key handling. The copies did not merely duplicate, they drifted — and
// the backspace that sliced the query by bytes rather than by grapheme
// clusters shipped in both and had to be found and fixed twice.
//
// What legitimately differs between the two callers is where matches come from
// and what bringing one into view means. Those arrive as searchInputHooks on
// each call rather than being stored on the input: the global search's view is
// handed in fresh by Model every time, so holding onto one would mean holding
// a reference that a later refresh can invalidate.
//
// State diagram:
//
//	closed --begin()--> searching --enter, matches > 0--> confirmed
//	   ↑                    │                                 │
//	   │                    ╰── enter, no matches ──╮          │
//	   ╰──────── reset() ───────────────────────────┴──────────╯
type searchInput struct {
	searching bool
	confirmed bool
	query     string
	matches   []int
	matchIdx  int
}

// searchInputHooks is everything a caller has to supply for the input to do
// its job. Only Find is required; a nil hook means "nothing to do".
type searchInputHooks struct {
	// Find returns the content-line indices the query matches. Both real
	// sources report nothing for an empty query.
	Find func(query string) []int
	// AfterEdit runs after each query edit, with the edited query, before the
	// new current match is navigated to. The global search re-applies the main
	// pane's highlight here; the help overlay highlights at render time and
	// leaves this nil.
	AfterEdit func(query string)
	// Navigate runs whenever the current match changes and one exists, with
	// the content line it now points at. The global search scrolls the
	// viewport there; the help overlay snaps its scroll offset.
	Navigate func(line int)
	// OnReset runs when the input returns to its dismissed state, for callers
	// with something to tear down — the global search drops the pane's
	// highlight query.
	OnReset func()
}

func (s *searchInput) IsSearching() bool { return s.searching }
func (s *searchInput) IsConfirmed() bool { return s.confirmed }
func (s *searchInput) IsActive() bool    { return s.searching || s.confirmed }
func (s *searchInput) Query() string     { return s.query }

// begin starts a fresh input, discarding any previous query and matches.
func (s *searchInput) begin() {
	*s = searchInput{searching: true}
}

// reset returns the input to its dismissed state and notifies the caller.
func (s *searchInput) reset(h searchInputHooks) {
	*s = searchInput{}
	if h.OnReset != nil {
		h.OnReset()
	}
}

// refresh re-runs the query against the match source, points the index at the
// first match, and brings it into view. This is the path every query edit
// takes.
func (s *searchInput) refresh(h searchInputHooks) {
	s.matches = h.Find(s.query)
	s.matchIdx = 0
	if h.AfterEdit != nil {
		h.AfterEdit(s.query)
	}
	s.navigate(h)
}

// recompute re-runs the current query against the match source and pulls the
// index back into range, without re-highlighting or navigating.
//
// It is what an active search needs when a refresh swaps the content out from
// under it: matches are content-line indices, so against new content the old
// ones address the wrong lines — or lines that no longer exist, which n/p
// would then try to scroll to.
//
// The query is deliberately kept and re-run rather than the search being
// dropped: PROMPT.md's "### search" says nothing about refreshes, and silently
// cancelling a search because a background poll landed is the more surprising
// of the two. A query with no matches in the new content stays active and
// reads "0/0" in the bar, exactly as typing a non-matching query does.
//
// Not navigating is the point of the separate path: this runs on every poll,
// and scrolling here would yank the viewport away from wherever the user is.
func (s *searchInput) recompute(h searchInputHooks) {
	if !s.IsActive() || s.query == "" {
		return
	}
	s.matches = h.Find(s.query)
	if s.matchIdx >= len(s.matches) {
		s.matchIdx = 0
	}
}

// confirm ends query editing. An empty query dismisses the input; a query that
// matched nothing closes it without entering navigation, so there is never a
// confirmed input with no matches to navigate.
func (s *searchInput) confirm(h searchInputHooks) {
	if s.query == "" {
		s.reset(h)
		return
	}
	s.searching = false
	if len(s.matches) > 0 {
		s.confirmed = true
	}
}

// navigate brings whichever match matchIdx points at into view.
func (s *searchInput) navigate(h searchInputHooks) {
	if len(s.matches) == 0 || h.Navigate == nil {
		return
	}
	h.Navigate(s.matches[s.matchIdx])
}

// advance moves the current match by delta, wrapping in both directions.
func (s *searchInput) advance(delta int, h searchInputHooks) {
	if len(s.matches) == 0 {
		return
	}
	n := len(s.matches)
	s.matchIdx = ((s.matchIdx+delta)%n + n) % n
	s.navigate(h)
}

// applyEditKey applies one key press while the user is typing a query. It
// covers the four editing keys both callers agree on: escape dismisses, enter
// confirms, backspace deletes one grapheme cluster, and anything carrying text
// appends it. A key carrying no text and matching none of those leaves the
// query alone.
//
// A caller that binds an extra key while typing — the global search treats
// ctrl+c as a cancel, the help overlay does not — pre-empts it before
// delegating here, so the difference stays visible at the call site.
func (s *searchInput) applyEditKey(msg tea.KeyPressMsg, h searchInputHooks) {
	switch {
	case msg.Code == tea.KeyEscape:
		s.reset(h)
	case msg.Code == tea.KeyEnter:
		s.confirm(h)
	case msg.Code == tea.KeyBackspace:
		// One grapheme cluster, not one byte: slicing bytes off a query
		// ending in a multi-byte rune leaves invalid UTF-8 behind.
		s.query = trimLastCluster(s.query)
		if s.query == "" {
			s.reset(h)
			return
		}
		s.refresh(h)
	default:
		if msg.Text != "" {
			s.query += msg.Text
		}
		s.refresh(h)
	}
}

// applyNavKey applies one key press while a confirmed search is being
// navigated with n/p. It reports whether the key belonged to the search:
// false means the input dismissed itself and the caller should re-dispatch the
// same key as a normal one.
func (s *searchInput) applyNavKey(msg tea.KeyPressMsg, h searchInputHooks) bool {
	switch {
	case key.Matches(msg, keys.SearchNext):
		s.advance(1, h)
		return true
	case key.Matches(msg, keys.SearchPrev):
		s.advance(-1, h)
		return true
	case msg.Code == tea.KeyEscape, key.Matches(msg, keys.QuitConfirm):
		s.reset(h)
		return true
	default:
		s.reset(h)
		return false
	}
}
