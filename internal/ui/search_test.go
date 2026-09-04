package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"pgregory.net/rapid"
)

// fakeSearchView is a minimal searchView: it holds a fixed set of content
// lines, matches case-insensitively against them the way mainPane.FindMatches
// does, and records the scroll targets it was asked for.
type fakeSearchView struct {
	lines    []string
	query    string
	scrolled []int
}

func (v *fakeSearchView) FindMatches(query string) []int {
	if query == "" {
		return nil
	}
	var out []int
	needle := strings.ToLower(query)
	for i, line := range v.lines {
		if strings.Contains(strings.ToLower(line), needle) {
			out = append(out, i)
		}
	}
	return out
}

func (v *fakeSearchView) SetSearchQuery(query string) { v.query = query }
func (v *fakeSearchView) ScrollToSourceLine(line int) { v.scrolled = append(v.scrolled, line) }

var _ searchView = (*fakeSearchView)(nil)

func typeText(s *searchOverlay, view searchView, text string) {
	if text == "" {
		return
	}
	first, _ := utf8.DecodeRuneInString(text)
	s.HandleInputKey(tea.KeyPressMsg{Code: first, Text: text}, view)
}

func pressBackspace(s *searchOverlay, view searchView) {
	s.HandleInputKey(tea.KeyPressMsg{Code: tea.KeyBackspace}, view)
}

// TestSearchBackspaceDeletesWholeCluster pins that backspace removes one
// user-perceived character. Slicing bytes off the end left invalid UTF-8 in
// the query for any multibyte input, which then corrupted both the rendered
// search bar and every subsequent match.
func TestSearchBackspaceDeletesWholeCluster(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		typed string
		want  string
	}{
		{"ascii", "abc", "ab"},
		{"two-byte accent", "aé", "a"},
		{"cjk", "日本", "日"},
		{"combining mark", "aé", "a"},
		{"zwj emoji family", "x👩‍👩‍👦", "x"},
		{"regional indicator pair", "x🇯🇵", "x"},
		{"emoji with skin tone", "x👍\U0001f3fd", "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			view := &fakeSearchView{lines: []string{"nothing here"}}
			s := newSearchOverlay()
			s.Open()
			typeText(s, view, tc.typed)
			pressBackspace(s, view)
			if got := s.Query(); got != tc.want {
				t.Errorf("after backspace query = %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(s.Query()) {
				t.Errorf("query %q is not valid UTF-8", s.Query())
			}
		})
	}
}

// TestProperty_SearchQueryStaysValidUTF8 drives the input state machine with
// arbitrary interleavings of typed graphemes and backspaces and requires the
// query to remain valid UTF-8 throughout — never a byte-truncated fragment of
// a multibyte sequence, which is what the old byte-slicing backspace produced
// on the first press against any non-ASCII input.
//
// Validity is the whole assertion here. An earlier version also re-walked the
// query with eachDisplayCluster and checked the atoms concatenated back to it,
// which reads like a whole-cluster check but cannot fail: the walk tiles any
// string exactly by contract (TestClusterWalkAgreesWithOracle pins that), so it
// reproduces byte-truncated garbage just as faithfully. Whole-cluster deletion
// is covered by TestSearchBackspaceDeletesWholeCluster's table, which carries
// independent expected values.
func TestProperty_SearchQueryStaysValidUTF8(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		view := &fakeSearchView{lines: []string{"日本 abc 👩‍👩‍👦 é"}}
		s := newSearchOverlay()
		s.Open()

		steps := rapid.IntRange(0, 30).Draw(t, "steps")
		for i := 0; i < steps; i++ {
			if rapid.Bool().Draw(t, "backspace") {
				pressBackspace(s, view)
			} else {
				typeText(s, view, widthyString(t, "typed"))
			}
			if !utf8.ValidString(s.Query()) {
				t.Fatalf("query is not valid UTF-8: %q", s.Query())
			}
			// Backspace on an empty query closes search; re-open so the
			// remaining steps keep exercising the input state machine.
			if !s.IsSearching() {
				s.Open()
			}
		}
	})
}

// TestHelpSearchBackspaceDeletesWholeCluster is the same invariant for the help
// overlay, which carries its own parallel search input rather than sharing
// searchOverlay — and so carried its own copy of the byte-slicing backspace.
func TestHelpSearchBackspaceDeletesWholeCluster(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		typed string
		want  string
	}{
		{"ascii", "quit", "qui"},
		{"cjk", "q日本", "q日"},
		{"combining mark", "qé", "q"},
		{"zwj emoji family", "q👩‍👩‍👦", "q"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHelpOverlay()
			h.Open()
			h.search.searching = true
			for _, r := range []rune(tc.typed) {
				h.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)}, 20)
			}
			h.HandleKey(tea.KeyPressMsg{Code: tea.KeyBackspace}, 20)
			if got := h.search.query; got != tc.want {
				t.Errorf("after backspace searchQuery = %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(h.search.query) {
				t.Errorf("searchQuery %q is not valid UTF-8", h.search.query)
			}
		})
	}
}

// TestProperty_SearchMatchesInBoundsAfterContentSwap: a refresh can replace the
// main pane's content under a confirmed search. Recomputing against the new
// content must leave every match index addressable in that content and
// matchIdx addressable in the match list — otherwise n/p navigate to lines that
// no longer exist.
func TestProperty_SearchMatchesInBoundsAfterContentSwap(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		lineGen := rapid.SampledFrom([]string{"alpha", "beta target", "gamma", "target", "delta", ""})
		view := &fakeSearchView{lines: rapid.SliceOfN(lineGen, 0, 20).Draw(t, "lines")}

		s := newSearchOverlay()
		s.Open()
		typeText(s, view, rapid.SampledFrom([]string{"target", "alpha", "zzz"}).Draw(t, "query"))
		s.HandleInputKey(tea.KeyPressMsg{Code: tea.KeyEnter}, view)

		// Advance through matches an arbitrary number of times, then swap the
		// content out from under the search the way a refresh does.
		for range rapid.IntRange(0, 5).Draw(t, "advances") {
			s.HandleNavKey(tea.KeyPressMsg{Text: "n", Code: 'n'}, view)
		}

		swaps := rapid.IntRange(1, 4).Draw(t, "swaps")
		for range swaps {
			view.lines = rapid.SliceOfN(lineGen, 0, 20).Draw(t, "newLines")
			s.RecomputeMatches(view)

			for _, idx := range s.matches {
				if idx < 0 || idx >= len(view.lines) {
					t.Fatalf("match index %d out of bounds for %d content lines", idx, len(view.lines))
				}
			}
			if len(s.matches) == 0 {
				if s.matchIdx != 0 {
					t.Fatalf("matchIdx = %d with no matches, want 0", s.matchIdx)
				}
			} else if s.matchIdx < 0 || s.matchIdx >= len(s.matches) {
				t.Fatalf("matchIdx %d out of bounds for %d matches", s.matchIdx, len(s.matches))
			}
			// Recomputing is against the same query, so the search itself
			// survives the refresh rather than being silently dropped.
			if s.IsActive() && s.Query() == "" {
				t.Fatal("active search lost its query across a content swap")
			}
		}
	})
}

// TestSearchMatchesRecomputedOnMainContentRefresh is the end-to-end version:
// drive a real confirmed search, then let updateMainContent install shorter
// content the way a git/PR refresh does. The stale matches pointed past the end
// of the new content, so n/p scrolled to lines that no longer existed.
func TestSearchMatchesRecomputedOnMainContentRefresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	long := "a\nb\ntarget\nc\nd\ne\ntarget\nf\ng\nh\ni\ntarget\n"
	if err := os.WriteFile(path, []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(dir, nil)
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()
	m.sidebar.SetItems([]sidebarItem{{label: "f.txt", kind: itemNormal, filePath: "f.txt"}})
	m.updateMainContent()

	// Confirm a search over the long content.
	result, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = result.(*Model)
	for _, r := range "target" {
		result, _ = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = result.(*Model)
	}
	result, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = result.(*Model)
	if !m.search.IsConfirmed() {
		t.Fatalf("search should be confirmed; matches = %v", m.search.matches)
	}
	if len(m.search.matches) != 3 {
		t.Fatalf("matches = %v, want 3 of them", m.search.matches)
	}

	// A refresh replaces the content with something much shorter.
	if err := os.WriteFile(path, []byte("a\ntarget\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.lastMainItem = mainItemKey{}
	m.updateMainContent()

	nLines := len(strings.Split(m.mainPane.content, "\n"))
	for _, idx := range m.search.matches {
		if idx < 0 || idx >= nLines {
			t.Errorf("stale match index %d out of bounds for %d new content lines (matches = %v)",
				idx, nLines, m.search.matches)
		}
	}
	if len(m.search.matches) != 1 {
		t.Errorf("matches = %v, want exactly 1 after the swap", m.search.matches)
	}
	if m.search.Query() != "target" {
		t.Errorf("query = %q, want it to survive the refresh", m.search.Query())
	}
}

// TestProperty_TrimLastClusterShrinksToEmpty checks the primitive's structural
// contract: each application strictly shrinks the string, leaves a prefix of
// it, keeps it valid UTF-8, and repeated application converges on "" and stays
// there. Those four are what termination and no-corruption rest on, and the
// convergence loop is the guard against a trim that stalls on some cluster
// class and would hang a backspace.
//
// It deliberately does NOT assert cluster-wiseness, which it cannot honestly
// check: a rune-wise trim satisfies all four assertions, and the only way to
// name the expected boundary here is to ask eachDisplayCluster — the same
// helper trimLastCluster is built on, so the two would agree by construction
// whatever either did. Cluster-wiseness is owned by
// TestSearchBackspaceDeletesWholeCluster, whose table states the expected
// result for each cluster class as a literal, independent of the oracle.
func TestProperty_TrimLastClusterShrinksToEmpty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := widthyString(t, "s")
		for range 40 {
			trimmed := trimLastCluster(s)
			if s == "" {
				if trimmed != "" {
					t.Fatalf("trimLastCluster(%q) = %q, want %q", s, trimmed, "")
				}
				break
			}
			if len(trimmed) >= len(s) {
				t.Fatalf("trimLastCluster(%q) = %q did not shrink", s, trimmed)
			}
			if !strings.HasPrefix(s, trimmed) {
				t.Fatalf("trimLastCluster(%q) = %q is not a prefix", s, trimmed)
			}
			if !utf8.ValidString(trimmed) {
				t.Fatalf("trimLastCluster(%q) = %q is not valid UTF-8", s, trimmed)
			}
			s = trimmed
		}
		if s != "" {
			t.Fatalf("repeated trimming did not reach empty: %q", s)
		}
	})
}
