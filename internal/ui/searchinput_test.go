package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"pgregory.net/rapid"
)

// The properties below drive searchInput directly. searchInput is what the two
// "/" searches in the app — the global search overlay (search.go) and the help
// overlay's own search (help.go) — both delegate their key handling to, so the
// only thing left for a caller to disagree about is the searchInputHooks it
// passes: where matches come from, and what bringing one into view means.
// A property that holds against an arbitrary hook wiring therefore holds for
// both overlays.

// searchInputProbe is a hook wiring that records what the input asked its
// caller to do, so a property can assert the input never navigates to a line
// it holds no match for.
type searchInputProbe struct {
	lines  []string
	edited []string
	// badNav records any navigate target that was out of bounds for the
	// content in force when the input asked for it. Checked at call time
	// rather than afterwards: the content is deliberately replaced mid-test,
	// and a target that was valid when asked for stays valid history.
	badNav []string
	resets int
}

// find matches case-insensitively and reports nothing for an empty query,
// which is what both real sources (mainPane.FindMatches, helpSearchMatches) do.
func (p *searchInputProbe) find(query string) []int {
	if query == "" {
		return nil
	}
	needle := strings.ToLower(query)
	var out []int
	for i, line := range p.lines {
		if strings.Contains(strings.ToLower(line), needle) {
			out = append(out, i)
		}
	}
	return out
}

func (p *searchInputProbe) hooks() searchInputHooks {
	return searchInputHooks{
		Find:      p.find,
		AfterEdit: func(q string) { p.edited = append(p.edited, q) },
		Navigate: func(line int) {
			if line < 0 || line >= len(p.lines) {
				p.badNav = append(p.badNav,
					fmt.Sprintf("line %d of %d", line, len(p.lines)))
			}
		},
		OnReset: func() { p.resets++ },
	}
}

// searchInputContent is the content the probe searches. It mixes multi-byte
// runes and a combining sequence in so a query built from them can match.
var searchInputContent = []string{
	"Keybindings:",
	"  j/k        move",
	"  /          search",
	"naïve café",
	"emoji \U0001F44D\U0001F3FD row",
	"café combining",
	"MiXeD CaSe",
	"",
}

// searchInputText is what one keystroke can contribute to the query: plain
// ASCII, multi-byte runes, a combining sequence, and an emoji with a skin-tone
// modifier — the cases a byte-sliced backspace corrupted.
var searchInputText = []string{
	"a", "e", "n", "p", "q", "/", " ", "C",
	"ï", "é", "\U0001F44D\U0001F3FD", "é",
	"naïve", "café", "日本",
}

// genSearchInputKey draws a keystroke from the alphabet the input understands
// plus two it does not, so the properties also cover the fall-through paths:
// an inert key while typing must not touch the query, and an unhandled key
// while confirmed must dismiss and hand the key back.
func genSearchInputKey(t *rapid.T) (string, tea.KeyPressMsg) {
	switch rapid.SampledFrom([]string{
		"text", "backspace", "enter", "escape", "next", "prev", "quit", "inert",
	}).Draw(t, "key") {
	case "backspace":
		return "backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "enter":
		return "enter", tea.KeyPressMsg{Code: tea.KeyEnter}
	case "escape":
		return "escape", tea.KeyPressMsg{Code: tea.KeyEscape}
	case "next":
		return "next", keyMsg("n")
	case "prev":
		return "prev", keyMsg("p")
	case "quit":
		return "quit", keyMsg("q")
	case "inert":
		return "inert", tea.KeyPressMsg{Code: tea.KeyTab}
	default:
		text := rapid.SampledFrom(searchInputText).Draw(t, "text")
		first, _ := utf8.DecodeRuneInString(text)
		return "text " + text, tea.KeyPressMsg{Code: first, Text: text}
	}
}

// routeSearchInputKey routes one keystroke the way both overlays route it: to
// the edit handler while typing, to the nav handler once confirmed, and
// nowhere at all once the input has dismissed itself.
func routeSearchInputKey(s *searchInput, msg tea.KeyPressMsg, h searchInputHooks) {
	switch {
	case s.IsSearching():
		s.applyEditKey(msg, h)
	case s.IsConfirmed():
		s.applyNavKey(msg, h)
	}
}

// sameSearchInput compares two inputs field by field. searchInput holds a
// slice, so it isn't comparable with ==.
func sameSearchInput(a, b searchInput) bool {
	if a.searching != b.searching || a.confirmed != b.confirmed ||
		a.query != b.query || a.matchIdx != b.matchIdx || len(a.matches) != len(b.matches) {
		return false
	}
	for i := range a.matches {
		if a.matches[i] != b.matches[i] {
			return false
		}
	}
	return true
}

// checkSearchInputInvariants asserts what must hold in every reachable state,
// whatever sequence of keys got us there.
func checkSearchInputInvariants(t *rapid.T, s *searchInput, p *searchInputProbe, after string) {
	// The query is always valid UTF-8. Backspace used to slice one byte off
	// the end, leaving a half-rune that corrupted the rendered search bar and
	// made every subsequent match test meaningless.
	if !utf8.ValidString(s.query) {
		t.Fatalf("after %s: query is not valid UTF-8: %q", after, s.query)
	}

	// The match index always addresses a real match.
	if len(s.matches) == 0 {
		if s.matchIdx != 0 {
			t.Fatalf("after %s: matchIdx=%d with no matches", after, s.matchIdx)
		}
	} else if s.matchIdx < 0 || s.matchIdx >= len(s.matches) {
		t.Fatalf("after %s: matchIdx=%d out of range for %d matches",
			after, s.matchIdx, len(s.matches))
	}

	// Every stored match addresses a line the content actually has, and the
	// input only ever asked to navigate to a line it held a match for.
	for _, line := range s.matches {
		if line < 0 || line >= len(p.lines) {
			t.Fatalf("after %s: match line %d outside content of %d lines",
				after, line, len(p.lines))
		}
	}
	if len(p.badNav) > 0 {
		t.Fatalf("after %s: navigated outside the content in force: %v", after, p.badNav)
	}

	// searching and confirmed are the two active states, and are exclusive.
	if s.searching && s.confirmed {
		t.Fatalf("after %s: both searching and confirmed", after)
	}

	// A dismissed input holds no matches. It can still hold a query: pressing
	// enter on a query that matches nothing closes the input without
	// confirming, which is the behavior both overlays have always had.
	if !s.IsActive() && len(s.matches) != 0 {
		t.Fatalf("after %s: dismissed input still holds matches %v", after, s.matches)
	}

}

// checkConfirmedHasMatches asserts that entering navigation always means there
// is something to navigate. It holds on every key-driven path, because confirm
// is the only route into the confirmed state and it refuses to confirm a query
// that matched nothing.
//
// It deliberately does NOT hold after recompute: a confirmed search whose
// query stops matching when the content is swapped stays confirmed and reads
// "0/0", which searchInput.recompute documents and argues for. That is why
// this is a separate check rather than part of the universal set.
func checkConfirmedHasMatches(t *rapid.T, s *searchInput, after string) {
	if s.confirmed && len(s.matches) == 0 {
		t.Fatalf("after %s: confirmed with no matches", after)
	}
}

// TestProperty_SearchInputInvariants drives an arbitrary keystroke sequence
// through the input, checking the invariants after every key rather than only
// at the end: a state machine that passes through an illegal state and
// recovers is still broken.
func TestProperty_SearchInputInvariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := &searchInputProbe{lines: searchInputContent}
		h := p.hooks()

		var s searchInput
		s.begin()
		checkSearchInputInvariants(t, &s, p, "begin")

		for i := range rapid.IntRange(1, 30).Draw(t, "keys") {
			// Reopen a dismissed input so the sequence keeps exercising the
			// machine instead of dead-ending on the first escape.
			if !s.IsActive() {
				s.begin()
				checkSearchInputInvariants(t, &s, p, "reopen")
			}
			name, msg := genSearchInputKey(t)
			wasSearching, wasConfirmed := s.IsSearching(), s.IsConfirmed()
			before := s.query

			routeSearchInputKey(&s, msg, h)
			where := fmt.Sprintf("key %d (%s)", i, name)
			checkSearchInputInvariants(t, &s, p, where)
			checkConfirmedHasMatches(t, &s, where)

			// An inert key — carrying no text and matching no binding — must
			// leave a query being typed exactly as it was, and must not
			// dismiss the input. This is the one key class the edit handler
			// deliberately does nothing for, and nothing else pins it.
			if name == "inert" && wasSearching {
				if s.query != before {
					t.Fatalf("%s: inert key changed the query %q -> %q", where, before, s.query)
				}
				if !s.IsSearching() {
					t.Fatalf("%s: inert key ended query editing", where)
				}
			}
			// The mirror contract while navigating: an unhandled key dismisses
			// the search so the caller can re-dispatch it as a normal key.
			if name == "inert" && wasConfirmed && s.IsActive() {
				t.Fatalf("%s: unhandled key left a confirmed search active", where)
			}
		}
	})
}

// drawSearchInputSequence begins an input and drives an arbitrary keystroke
// sequence through it, reopening it whenever it dismisses itself.
func drawSearchInputSequence(t *rapid.T, s *searchInput, h searchInputHooks, maxKeys int) {
	s.begin()
	for range rapid.IntRange(0, maxKeys).Draw(t, "keys") {
		if !s.IsActive() {
			s.begin()
		}
		_, msg := genSearchInputKey(t)
		routeSearchInputKey(s, msg, h)
	}
}

// TestProperty_SearchInputEscapeAlwaysDismisses pins that every reachable
// state has a dismiss path: escape from anywhere returns the input to its zero
// value and tells the caller once to drop whatever it was showing.
func TestProperty_SearchInputEscapeAlwaysDismisses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := &searchInputProbe{lines: searchInputContent}
		h := p.hooks()

		var s searchInput
		drawSearchInputSequence(t, &s, h, 20)
		if !s.IsActive() {
			s.begin()
		}

		before := p.resets
		routeSearchInputKey(&s, tea.KeyPressMsg{Code: tea.KeyEscape}, h)

		if s.IsActive() {
			t.Fatalf("escape left the input active: %+v", s)
		}
		if !sameSearchInput(s, searchInput{}) {
			t.Fatalf("escape left residue: %+v", s)
		}
		if p.resets != before+1 {
			t.Fatalf("escape fired %d resets, want exactly 1", p.resets-before)
		}
	})
}

// TestProperty_SearchInputResetIsIdempotent pins that dismissing an already
// dismissed input leaves the state alone.
func TestProperty_SearchInputResetIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := &searchInputProbe{lines: searchInputContent}
		h := p.hooks()

		var s searchInput
		drawSearchInputSequence(t, &s, h, 20)

		s.reset(h)
		once := s
		s.reset(h)
		if !sameSearchInput(s, once) {
			t.Fatalf("second reset changed state: %+v then %+v", once, s)
		}
	})
}

// TestProperty_SearchInputMatchesAgreeWithSource pins that while the user is
// typing, the stored matches are exactly what the source reports for the
// stored query. A stale match list is how n/p ends up scrolling to a line that
// no longer contains the query.
func TestProperty_SearchInputMatchesAgreeWithSource(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := &searchInputProbe{lines: searchInputContent}
		h := p.hooks()

		var s searchInput
		s.begin()
		for i := range rapid.IntRange(1, 30).Draw(t, "keys") {
			if !s.IsActive() {
				s.begin()
			}
			name, msg := genSearchInputKey(t)
			routeSearchInputKey(&s, msg, h)

			// Only edits refresh the list, and only while typing — once
			// confirmed, n/p must not silently re-run the query.
			if !s.IsSearching() {
				continue
			}
			want := p.find(s.query)
			if len(want) != len(s.matches) {
				t.Fatalf("key %d (%s): holding %d matches for %q, source reports %d",
					i, name, len(s.matches), s.query, len(want))
			}
			for j := range want {
				if want[j] != s.matches[j] {
					t.Fatalf("key %d (%s): matches %v disagree with source %v for %q",
						i, name, s.matches, want, s.query)
				}
			}
		}
	})
}

// TestProperty_SearchInputRecomputeClampsIndex covers the refresh path: when
// the content changes under a confirmed search, the matches are re-run against
// the new content and the index is pulled back in bounds rather than left
// addressing a line that is gone.
func TestProperty_SearchInputRecomputeClampsIndex(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := &searchInputProbe{lines: searchInputContent}
		h := p.hooks()

		var s searchInput
		s.begin()
		query := rapid.SampledFrom([]string{"e", "a", "café", "\U0001F44D\U0001F3FD", "zzz"}).Draw(t, "query")
		for _, r := range query {
			s.applyEditKey(tea.KeyPressMsg{Code: r, Text: string(r)}, h)
		}
		s.applyEditKey(tea.KeyPressMsg{Code: tea.KeyEnter}, h)

		// Walk the index somewhere non-zero before the content shrinks.
		for range rapid.IntRange(0, 5).Draw(t, "advances") {
			if s.IsConfirmed() {
				s.applyNavKey(keyMsg("n"), h)
			}
		}

		// The content is replaced wholesale, as a background refresh does.
		keep := rapid.IntRange(0, len(searchInputContent)).Draw(t, "keep")
		p.lines = searchInputContent[:keep]
		s.recompute(h)

		checkSearchInputInvariants(t, &s, p, "recompute")

		// recompute never re-highlights and never scrolls: it runs on every
		// background poll, and scrolling there would yank the viewport away
		// from wherever the user is.
		if s.IsActive() && s.query != "" {
			want := p.find(s.query)
			if len(want) != len(s.matches) {
				t.Fatalf("recompute left %d matches, source reports %d for %q",
					len(s.matches), len(want), s.query)
			}
		}
	})
}

// TestSearchInputBackspaceDeletesWholeCluster is the table-driven pin for the
// bug that shipped in both overlays and had to be fixed in both: backspace
// removes one user-perceived character, never one byte.
func TestSearchInputBackspaceDeletesWholeCluster(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"ascii", "abc", "ab"},
		{"precomposed rune", "café", "caf"},
		{"combining sequence", "café", "caf"},
		{"emoji with modifier", "ok\U0001F44D\U0001F3FD", "ok"},
		{"cjk", "a日本", "a日"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &searchInputProbe{lines: searchInputContent}
			s := searchInput{searching: true, query: tc.query}
			s.applyEditKey(tea.KeyPressMsg{Code: tea.KeyBackspace}, p.hooks())
			if s.query != tc.want {
				t.Errorf("query = %q, want %q", s.query, tc.want)
			}
			if !utf8.ValidString(s.query) {
				t.Errorf("query %q is not valid UTF-8", s.query)
			}
		})
	}
}
