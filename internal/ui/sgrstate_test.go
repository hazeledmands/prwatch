package ui

import (
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// sgrModel is a terminal's styling state, as an independent check on sgrState.
// Where sgrState keeps the raw sequences and replays them, this parses each
// sequence's parameters into the attributes they set — so a test comparing the
// two is comparing two implementations, not the production one against itself.
//
// Comparable by design: every field is a value, so two models are == when they
// would render identically.
type sgrModel struct {
	fg, bg, ul                      string
	bold, faint, italic, underline  bool
	blink, reverse, conceal, strike bool
	unknownSeen                     bool
}

// apply folds one escape sequence into the model. Non-SGR escapes (anything
// not a CSI ... m) set no attributes and are ignored, as in a real terminal.
func (m sgrModel) apply(seq string) sgrModel {
	body, ok := strings.CutPrefix(seq, "\x1b[")
	if !ok {
		return m
	}
	body, ok = strings.CutSuffix(body, "m")
	if !ok {
		return m
	}
	params := strings.Split(body, ";")
	for i := 0; i < len(params); i++ {
		p := params[i]
		if p == "" {
			p = "0"
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			m.unknownSeen = true
			continue
		}
		switch {
		case n == 0:
			m = sgrModel{}
		case n == 1:
			m.bold = true
		case n == 2:
			m.faint = true
		case n == 3:
			m.italic = true
		case n == 4:
			m.underline = true
		case n == 5 || n == 6:
			m.blink = true
		case n == 7:
			m.reverse = true
		case n == 8:
			m.conceal = true
		case n == 9:
			m.strike = true
		case n == 21 || n == 22:
			m.bold, m.faint = false, false
		case n == 23:
			m.italic = false
		case n == 24:
			m.underline = false
		case n == 25 || n == 26:
			m.blink = false
		case n == 27:
			m.reverse = false
		case n == 28:
			m.conceal = false
		case n == 29:
			m.strike = false
		case n >= 30 && n <= 37, n == 39, n >= 90 && n <= 97:
			m.fg = p
		case n >= 40 && n <= 47, n == 49, n >= 100 && n <= 107:
			m.bg = p
		case n == 38 || n == 48 || n == 58:
			color, consumed := readExtendedColor(params[i+1:])
			i += consumed
			switch n {
			case 38:
				m.fg = color
			case 48:
				m.bg = color
			case 58:
				m.ul = color
			}
		default:
			m.unknownSeen = true
		}
	}
	return m
}

// readExtendedColor reads the argument form of an SGR 38/48/58 parameter —
// "5;<idx>" for 256-color, "2;<r>;<g>;<b>" for truecolor — returning a
// canonical string for the color and how many parameters it consumed.
func readExtendedColor(rest []string) (string, int) {
	if len(rest) == 0 {
		return "", 0
	}
	switch rest[0] {
	case "5":
		if len(rest) < 2 {
			return "5", 1
		}
		return "5:" + rest[1], 2
	case "2":
		if len(rest) < 4 {
			return "2", len(rest)
		}
		return "2:" + strings.Join(rest[1:4], ":"), 4
	default:
		return rest[0], 1
	}
}

// modelRow renders row through the model: the attribute state in force at each
// visible (non-escape) cluster, plus the state the row leaves behind.
func modelRow(start sgrModel, row string) ([]sgrModel, sgrModel) {
	cur := start
	var cells []sgrModel
	eachDisplayCluster(row, func(c displayCluster) bool {
		if c.IsEscape {
			cur = cur.apply(c.Text)
			return true
		}
		cells = append(cells, cur)
		return true
	})
	return cells, cur
}

// TestSGRState_ReplayMatchesTheTerminalState is sgrState's contract: replaying
// openSeq() from a clean terminal must leave it in the same state as writing
// the whole text it was fed. That is the property the wrapper depends on — a
// continuation row opens with openSeq() and must render as if everything above
// it had been written.
func TestProperty_SGRState_ReplayMatchesTerminalState(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		text := styledContent(t)

		var st sgrState
		st.feed(text)

		_, direct := modelRow(sgrModel{}, text)
		_, replayed := modelRow(sgrModel{}, st.openSeq())
		if direct != replayed {
			t.Fatalf("openSeq() %q replays as %+v, but the text itself leaves the terminal in %+v",
				st.openSeq(), replayed, direct)
		}
	})
}

// TestProperty_SGRState_ActiveMatchesNonEmptyOpen keeps the two accessors from
// disagreeing: the wrapper closes a row iff active(), and re-opens with
// openSeq(), so "active" and "openSeq is non-empty" must be the same claim.
func TestProperty_SGRState_ActiveMatchesNonEmptyOpen(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		text := styledContent(t)
		var st sgrState
		st.feed(text)
		if st.active() != (st.openSeq() != "") {
			t.Fatalf("active() = %v but openSeq() = %q", st.active(), st.openSeq())
		}
	})
}

// TestProperty_SGRState_FeedIsIdempotent: feeding the same text twice is the
// same state as feeding it once. The wrapper relies on this — emit() feeds a
// row that already begins with the openSeq() the tracker just handed out.
func TestProperty_SGRState_FeedIsIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		text := styledContent(t)
		var once, twice sgrState
		once.feed(text)
		twice.feed(text)
		twice.feed(text)
		if once.openSeq() != twice.openSeq() {
			t.Fatalf("feeding twice gives %q, once gives %q", twice.openSeq(), once.openSeq())
		}
	})
}

// TestProperty_SGRSeqAtAgreesWithTheEscapeOracle keeps sgrState's hand-rolled
// scan tied to ansiStripRE, the package's definition of an escape sequence:
// walking a string with sgrSeqAt must find exactly the sequences the oracle
// finds that are SGR, in the same order. Without this the fast scan could
// quietly disagree with everything else about where an escape starts and ends.
func TestProperty_SGRSeqAtAgreesWithTheEscapeOracle(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Well-formed styled content, plus fragments designed to look like
		// escapes without being SGR ones.
		text := styledContent(t)
		noise := rapid.SampledFrom([]string{
			"", "\x1b[2J", "\x1b[10;5H", "\x1b]8;;http://example.com/m\x1b\\",
			"\x1b[", "\x1b", "\x1b[1;", "\x1b[<0;1;2M", "\x1b[38;2;1;2;3",
		}).Draw(t, "noise")
		at := rapid.IntRange(0, len(text)).Draw(t, "at")
		text = text[:at] + noise + text[at:]

		var got []string
		for i := 0; i < len(text); {
			j := strings.IndexByte(text[i:], 0x1b)
			if j < 0 {
				break
			}
			i += j
			seq, ok := sgrSeqAt(text, i)
			if !ok {
				i++
				continue
			}
			got = append(got, seq)
			i += len(seq)
		}

		var want []string
		for _, seq := range ansiStripRE.FindAllString(text, -1) {
			if _, isSGR := sgrParams(seq); isSGR {
				want = append(want, seq)
			}
		}

		if len(got) != len(want) {
			t.Fatalf("scan of %q found %q, oracle found %q", text, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("scan of %q found %q, oracle found %q", text, got, want)
			}
		}
	})
}

func TestSGRState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		feed []string
		want string
	}{
		{name: "empty", want: ""},
		{name: "plain text", feed: []string{"hello"}, want: ""},
		{name: "single open", feed: []string{"\x1b[1mbold"}, want: "\x1b[1m"},
		{
			name: "reset clears",
			feed: []string{"\x1b[1mbold\x1b[0m plain"},
			want: "",
		},
		{
			name: "bare reset clears",
			feed: []string{"\x1b[1mbold\x1b[m plain"},
			want: "",
		},
		{
			name: "concurrent attributes accumulate in order",
			feed: []string{"\x1b[1m\x1b[38;2;1;2;3m\x1b[48;5;17mx"},
			want: "\x1b[1m\x1b[38;2;1;2;3m\x1b[48;5;17m",
		},
		{
			name: "repeat moves to the end rather than duplicating",
			feed: []string{"\x1b[31mx\x1b[32my\x1b[31mz"},
			want: "\x1b[32m\x1b[31m",
		},
		{
			name: "reset inside a compound sequence drops what came before",
			feed: []string{"\x1b[1mx\x1b[0;31my"},
			want: "\x1b[0;31m",
		},
		{
			name: "non-SGR escapes are not styling",
			feed: []string{"\x1b[2J\x1b[10;5H\x1b]8;;http://x\x1b\\y"},
			want: "",
		},
		{
			name: "feeding row by row is feeding the whole",
			feed: []string{"\x1b[1mrow one", "\x1b[38;5;9mrow two"},
			want: "\x1b[1m\x1b[38;5;9m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var st sgrState
			for _, f := range tt.feed {
				st.feed(f)
			}
			if got := st.openSeq(); got != tt.want {
				t.Errorf("openSeq() = %q, want %q", got, tt.want)
			}
			if st.active() != (tt.want != "") {
				t.Errorf("active() = %v, want %v", st.active(), tt.want != "")
			}
		})
	}
}
