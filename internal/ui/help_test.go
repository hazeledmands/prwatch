package ui

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// unlistedBindings names keyMap fields deliberately kept out of the help
// listing. It is empty: every command the app binds is documented, which is
// what PROMPT.md's help section asks for ("a help page showing all commands
// and their bindings").
//
// An omission has to be spelled out here to pass TestHelpListing_MatchesKeyMap,
// so dropping a command from the listing is a visible decision rather than a
// silent gap.
var unlistedBindings = map[string]string{}

// helpListingLines is the number of rows helpContentLines produces: the
// "Keybindings:" header, a blank, 44 command rows in 11 sections separated by
// 10 blank lines, then a blank and the footer.
//
// Pinned as a literal so assertions about the listing's extent are anchored to
// a committed fact rather than re-derived from helpContentLines itself.
// TestHelpListing_Golden fails if the real count drifts from this.
const helpListingLines = 58

// TestHelpListing_Golden pins the full listing — every row, every word.
//
// testdata/golden/help_overlay.txt renders the overlay at a terminal height
// that cuts off after section 4 of 11, so rows 31-58 had no wording coverage
// at all. And now that each description has exactly one definition, a typo in
// a withDesc string changes the keymap and the listing in lockstep, which is
// precisely the kind of edit a correspondence test cannot see. This fixture is
// height-independent: it asserts helpContentLines directly.
//
// Regenerate with: go test ./internal/ui -run TestHelpListing_Golden -update
func TestHelpListing_Golden(t *testing.T) {
	lines := helpContentLines()
	if len(lines) != helpListingLines {
		t.Errorf("helpContentLines() returned %d lines, want helpListingLines=%d; "+
			"update the constant if the listing legitimately grew",
			len(lines), helpListingLines)
	}
	assertGolden(t, "help_listing", strings.Join(lines, "\n"))
}

// bindingID identifies a binding by the two facts the help screen states about
// it. Keys alone are not unique — `n` is both SearchNext and ToggleLineNums,
// `N` is both SearchPrev and NextLeaf — so the description disambiguates.
type bindingID struct {
	keys string
	desc string
}

func idOf(b key.Binding) bindingID {
	return bindingID{keys: strings.Join(b.Keys(), ","), desc: b.Help().Desc}
}

// TestHelpListing_MatchesKeyMap is the divergence guard for the help overlay.
//
// The listing (helpSections) and the keymap (keys) are separate declarations
// over the same set of commands, and the repo has been bitten before by two
// hand-maintained copies of one fact. This asserts they are in exact
// correspondence, in both directions:
//
//   - every keyMap field appears in the listing exactly once (or is named in
//     unlistedBindings);
//   - every listing entry is a real keyMap field, and carries a description;
//   - the rendered help has exactly one row per listed binding, keyed by that
//     binding's own keys.
func TestHelpListing_MatchesKeyMap(t *testing.T) {
	// Index the keymap by identity, and assert the identity is actually
	// unique — two fields sharing keys *and* description would be a duplicate
	// command, and would make the counting below meaningless.
	fieldOf := map[bindingID]string{}
	v := reflect.ValueOf(keys)
	tp := v.Type()
	for i := range v.NumField() {
		name := tp.Field(i).Name
		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			t.Fatalf("keys.%s is not a key.Binding", name)
		}
		if b.Help().Desc == "" {
			t.Errorf("keys.%s has no description; add withDesc(...) to its binding", name)
			continue
		}
		if prev, dup := fieldOf[idOf(b)]; dup {
			t.Errorf("keys.%s and keys.%s are indistinguishable (same keys %v and "+
				"same description %q)", prev, name, b.Keys(), b.Help().Desc)
			continue
		}
		fieldOf[idOf(b)] = name
	}

	// Direction 1: every listing entry is a real binding.
	listed := map[string]int{}
	var listedBindings []key.Binding
	for si, section := range helpSections {
		for _, b := range section {
			listedBindings = append(listedBindings, b)
			name, ok := fieldOf[idOf(b)]
			if !ok {
				t.Errorf("helpSections[%d] lists a binding (keys %v, desc %q) that is "+
					"not a field of keyMap", si, b.Keys(), b.Help().Desc)
				continue
			}
			listed[name]++
		}
	}

	// Direction 2: every binding is listed exactly once, or excluded on purpose.
	for _, name := range fieldOf {
		reason, excluded := unlistedBindings[name]
		switch {
		case excluded && listed[name] > 0:
			t.Errorf("keys.%s is in unlistedBindings (%q) but appears in helpSections; "+
				"remove the exclusion or the listing entry", name, reason)
		case excluded:
			// Deliberately undocumented.
		case listed[name] == 0:
			t.Errorf("keys.%s is not documented in helpSections; add it to a section, "+
				"or add it to unlistedBindings with a reason", name)
		case listed[name] > 1:
			t.Errorf("keys.%s appears in helpSections %d times, want 1", name, listed[name])
		}
	}

	// Direction 3: the rendered listing has one row per listed binding, and
	// each row's key column is that binding's own keys.
	var rows []string
	for _, line := range helpContentLines() {
		if strings.HasPrefix(line, "  [") {
			rows = append(rows, line)
		}
	}
	if len(rows) != len(listedBindings) {
		t.Fatalf("rendered help has %d key rows, want %d (one per listed binding)",
			len(rows), len(listedBindings))
	}
	for i, b := range listedBindings {
		wantKeys, wantDesc := keyList(b), b.Help().Desc
		if !strings.HasPrefix(rows[i], "  "+wantKeys+" ") {
			t.Errorf("row %d = %q, want its key column to be %q", i, rows[i], wantKeys)
		}
		if !strings.HasSuffix(rows[i], wantDesc) {
			t.Errorf("row %d = %q, want it to end with description %q", i, rows[i], wantDesc)
		}
	}
}

// TestHelpOverlay_AdvertisedKeys pins the help overlay's own key contract:
// which keys do something inside help, and which dismiss it.
//
// The listing and the footer ("Press q/esc to dismiss. Use j/k or mouse to
// scroll. / to search.") are a promise to the user about the screen they are
// looking at. HandleKey's default arm closes the overlay, so a promised key
// that isn't matched dismisses the screen documenting it — which is how
// go-top's `g`/`home` behaved.
func TestHelpOverlay_AdvertisedKeys(t *testing.T) {
	const visibleHeight = 10

	tests := []struct {
		name          string
		msg           tea.KeyPressMsg
		wantOpen      bool // overlay still open afterwards
		wantQuit      bool // HandleKey returned a command (tea.Quit)
		wantSearching bool // overlay entered its content search
	}{
		// Dismissal, per PROMPT.md's `quit`: "when help is open, it closes help".
		{name: "q dismisses", msg: tea.KeyPressMsg{Text: "q", Code: 'q'}},
		{name: "esc dismisses", msg: tea.KeyPressMsg{Code: tea.KeyEscape}},
		{name: "? toggles back off", msg: tea.KeyPressMsg{Text: "?", Code: '?'}},

		// quit-immediate exits the app rather than the overlay.
		{
			name: "ctrl+c quits",
			// No Text: KeyPressMsg.String() returns Text verbatim when it is
			// set, so a ctrl-modified rune only stringifies as "ctrl+c" the
			// way a terminal actually delivers it.
			msg:      tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl},
			wantOpen: true,
			wantQuit: true,
		},
		{
			name:     "Q quits",
			msg:      tea.KeyPressMsg{Text: "Q", Code: 'Q'},
			wantOpen: true,
			wantQuit: true,
		},

		// Search over help content stays inside the overlay.
		{
			name:          "/ opens help search",
			msg:           tea.KeyPressMsg{Text: "/", Code: '/'},
			wantOpen:      true,
			wantSearching: true,
		},

		// Keys that belong to other views are not help commands; closing is
		// the documented fall-through (see TestHelp_ShowAndDismiss).
		{name: "m is not a help command", msg: tea.KeyPressMsg{Text: "m", Code: 'm'}},
		{name: "w is not a help command", msg: tea.KeyPressMsg{Text: "w", Code: 'w'}},
		{name: "1 is not a help command", msg: tea.KeyPressMsg{Text: "1", Code: '1'}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHelpOverlay()
			h.Open()

			cmd := h.HandleKey(tc.msg, visibleHeight)

			if got := h.IsOpen(); got != tc.wantOpen {
				t.Errorf("IsOpen() = %v, want %v", got, tc.wantOpen)
			}
			if got := cmd != nil; got != tc.wantQuit {
				t.Errorf("returned a command = %v, want %v", got, tc.wantQuit)
			}
			if h.search.searching != tc.wantSearching {
				t.Errorf("searching = %v, want %v", h.search.searching, tc.wantSearching)
			}
		})
	}
}

// TestHelpOverlay_ScrollCommands covers PROMPT.md's help section: "help should
// be scrollable by mouse and by the same scrolling commands (page-up,
// page-down, go-top, go-bottom, up, down) as other views."
//
// Every one of those keys must be consumed by the overlay. A key that falls
// through to HandleKey's default arm closes help — so a scroll command the
// listing advertises would dismiss the screen documenting it.
func TestHelpOverlay_ScrollCommands(t *testing.T) {
	const visibleHeight = 10

	// Far enough down that go-top has somewhere to travel from, and short of
	// the bottom so page-down and go-bottom differ.
	const startOffset = 20

	// Literal arithmetic, not len(helpContentLines())-visibleHeight: that is
	// the very expression the GoBottom arm evaluates, so deriving the
	// expectation from it asserts nothing. helpListingLines is pinned against
	// the committed fixture by TestHelpListing_Golden, which is what makes 48
	// a fact about the help screen rather than a restatement of the code.
	const maxOffset = helpListingLines - visibleHeight // 58 - 10

	if maxOffset <= startOffset {
		t.Fatalf("help content (%d lines) too short for this fixture; "+
			"maxOffset=%d must exceed startOffset=%d",
			helpListingLines, maxOffset, startOffset)
	}

	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want int
	}{
		{"go-top g", tea.KeyPressMsg{Text: "g", Code: 'g'}, 0},
		{"go-top home", tea.KeyPressMsg{Code: tea.KeyHome}, 0},
		{"go-bottom G", tea.KeyPressMsg{Text: "G", Code: 'G'}, maxOffset},
		{"go-bottom end", tea.KeyPressMsg{Code: tea.KeyEnd}, maxOffset},
		{"down j", tea.KeyPressMsg{Text: "j", Code: 'j'}, startOffset + 1},
		{"down arrow", tea.KeyPressMsg{Code: tea.KeyDown}, startOffset + 1},
		{"up k", tea.KeyPressMsg{Text: "k", Code: 'k'}, startOffset - 1},
		{"up arrow", tea.KeyPressMsg{Code: tea.KeyUp}, startOffset - 1},
		{"page-down", tea.KeyPressMsg{Code: tea.KeyPgDown}, startOffset + visibleHeight},
		{"page-up", tea.KeyPressMsg{Code: tea.KeyPgUp}, startOffset - visibleHeight},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHelpOverlay()
			h.Open()
			h.scrollOffset = startOffset

			h.HandleKey(tc.msg, visibleHeight)

			if !h.IsOpen() {
				t.Fatalf("%q dismissed the help overlay; it is a scroll command "+
					"and must be consumed by HandleKey", tc.name)
			}
			if h.scrollOffset != tc.want {
				t.Errorf("scrollOffset = %d, want %d", h.scrollOffset, tc.want)
			}
		})
	}
}
