package ui

import "strings"

// sgrReset is the full SGR reset: it clears every styling attribute.
const sgrReset = "\x1b[0m"

// sgrState tracks the styling a stretch of text leaves the terminal in, so a
// row can be closed at its end and re-opened at the start of the next one.
//
// It stores the SGR sequences themselves rather than the attributes they mean.
// Replaying the sequences in the order they were seen reproduces the terminal's
// state exactly, whatever the sequences do — one attribute or several at once, a
// truecolor foreground, an attribute switched back off (`ESC[22m`), a vendor
// parameter this code has never heard of. Parsing them into a model of
// foreground/background/bold/... would mean re-deriving that meaning, and being
// wrong about any parameter would silently mis-render.
//
// A full reset drops everything before it, which is what keeps the state small:
// both chroma and lipgloss close every span they open, so in practice the state
// holds one or two sequences.
type sgrState struct {
	// seqs are the SGR sequences seen since the last full reset, in the order
	// they must be replayed. Deduplicated: a sequence seen again moves to the
	// end rather than being appended twice. Removing the earlier copy cannot
	// change the state the whole slice replays to — anything between the two
	// copies that the later one overrides is overridden either way — and it
	// bounds the slice by the number of *distinct* sequences on a line.
	seqs []string
}

// feed folds every SGR sequence in text into the state, in order.
//
// The scan is by hand rather than through ansiStripRE because this runs on
// every wrapped row of every refresh, and on a syntax-highlighted file that is
// thousands of sequences per keystroke — the regexp walk cost more than the
// rest of the wrapper put together. It needs only the SGR shape, which
// sgrParams already spells out. TestProperty_SGRSeqAtAgreesWithTheEscapeOracle
// pins it to ansiStripRE so the two cannot drift on what an escape is.
func (s *sgrState) feed(text string) {
	// Shortcut, and the reason a styled file is not measurably slower to wrap
	// than an unstyled one: when the last escape in text is a bare reset,
	// nothing before it can survive, so the state is empty and the rest of the
	// text never has to be read. Both chroma and lipgloss close every span they
	// open, so this is the case for very nearly every row they produce.
	last := strings.LastIndexByte(text, 0x1b)
	if last < 0 {
		return
	}
	if seq, ok := sgrSeqAt(text, last); ok {
		if params, _ := sgrParams(seq); lastResetParam(params) == len(params)-1 {
			s.seqs = s.seqs[:0]
			return
		}
	}
	for i := 0; i <= last; {
		j := strings.IndexByte(text[i:], 0x1b)
		if j < 0 {
			return
		}
		i += j
		seq, ok := sgrSeqAt(text, i)
		if !ok {
			// Not an SGR sequence. Step past this ESC and keep looking; an OSC
			// 8 hyperlink's body cannot contain one, so nothing inside another
			// escape can be mistaken for styling.
			i++
			continue
		}
		s.feedSeq(seq)
		i += len(seq)
	}
}

// sgrSeqAt returns the SGR sequence (CSI, numeric parameters, final 'm')
// starting at i, reporting false when what starts there is anything else.
func sgrSeqAt(s string, i int) (string, bool) {
	if !strings.HasPrefix(s[i:], "\x1b[") {
		return "", false
	}
	for k := i + 2; k < len(s); k++ {
		switch c := s[k]; {
		case c >= '0' && c <= '9', c == ';':
			continue
		case c == 'm':
			return s[i : k+1], true
		default:
			return "", false // some other CSI sequence, or malformed
		}
	}
	return "", false // unterminated
}

// feedSeq folds one escape sequence into the state. Anything that is not an
// SGR sequence sets no styling and is ignored — a cursor move or an OSC 8
// hyperlink is not state a continuation row should re-open.
func (s *sgrState) feedSeq(seq string) {
	params, ok := sgrParams(seq)
	if !ok {
		return
	}
	// A reset anywhere in the sequence discards everything before it. The
	// sequence itself is still recorded when it sets something afterwards
	// (`ESC[0;31m`), so replaying it re-does the reset and then the attribute.
	if i := lastResetParam(params); i >= 0 {
		s.seqs = s.seqs[:0]
		if i == len(params)-1 {
			return
		}
	}
	for i, existing := range s.seqs {
		if existing == seq {
			s.seqs = append(s.seqs[:i], s.seqs[i+1:]...)
			break
		}
	}
	s.seqs = append(s.seqs, seq)
}

// active reports whether any styling is open.
func (s *sgrState) active() bool { return len(s.seqs) > 0 }

// openSeq returns the sequences that re-establish the current styling from a
// clean terminal. Empty when nothing is open.
func (s *sgrState) openSeq() string {
	switch len(s.seqs) {
	case 0:
		return ""
	case 1:
		return s.seqs[0]
	default:
		return strings.Join(s.seqs, "")
	}
}

// sgrParams splits an escape sequence into its SGR parameters, reporting false
// if the sequence is not an SGR sequence (CSI ... m) at all.
func sgrParams(seq string) ([]string, bool) {
	body, ok := strings.CutPrefix(seq, "\x1b[")
	if !ok {
		return nil, false
	}
	body, ok = strings.CutSuffix(body, "m")
	if !ok {
		return nil, false
	}
	return strings.Split(body, ";"), true
}

// lastResetParam returns the index of the last reset parameter — "0", any
// zero-padded spelling of it, or the empty parameter, which SGR defines as 0
// (so `ESC[m` is a reset). Returns -1 when the sequence contains none.
func lastResetParam(params []string) int {
	found := -1
	for i, p := range params {
		if p == "" || strings.Trim(p, "0") == "" {
			found = i
		}
	}
	return found
}
