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
// thousands of sequences per keystroke. Routing it through the regexp instead
// costs 45% on BenchmarkRefreshViewportStyled (25ms to 37ms). It needs only the
// SGR shape, which sgrParams already spells out, and
// TestProperty_SGRSeqAtAgreesWithTheEscapeOracle pins it to ansiStripRE so the
// two cannot drift on what an escape is.
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
			// Not an SGR sequence — step past this ESC and keep looking. Only
			// ESC-anchored positions are ever examined, so a byte sequence that
			// looks like SGR can only be read as styling if it genuinely starts
			// with ESC [; the body of a non-SGR escape is otherwise skipped.
			i++
			continue
		}
		s.feedSeq(seq)
		i += len(seq)
	}
}

// sgrSeqAt returns the SGR sequence (CSI, numeric parameters, final 'm')
// starting at i, reporting false when what starts there is anything else.
//
// Colon sub-parameter forms (`ESC[4:3m` for a curly underline, `ESC[38:2::r:g:bm`)
// are deliberately out of scope, matching ansiStripRE, which does not accept a
// colon either: neither chroma nor lipgloss emits them, and treating one as
// styling here while the rest of the package does not count it as an escape at
// all would be worse than ignoring it.
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
func (s *sgrState) openSeq() string { return strings.Join(s.seqs, "") }

// openStyleAfterGutter inserts open into row just past its first gutterCols
// display columns, so re-established styling never paints the gutter.
//
// A rendered row starts with a gutter: the real one (line number, diff mark) on
// a source line's first row, and on a continuation row the blank stand-in the
// wrapper invents for it — PROMPT.md's "wrapped text does not wrap into the
// gutter — continuation lines have an empty gutter". Styling carried over from
// the row above belongs to the content, so it resumes after those columns; put
// it first and a search highlight's background paints the gutter.
//
// The gutter region can carry the row's own escapes — a styled diff gutter,
// or a line whose content begins with a reset (a fenced code block's closing
// reset lands on its own line). What is inserted is therefore not the raw
// carried sequence but the carry *folded with those escapes*: the state the
// source stream would be in at the insertion point. Inserting the raw carry
// after a reset the row had already written re-opened styling the source had
// closed — a code block's background painted every following line, or was left
// dangling past the row's end (see TestWrapLines_LineOwnResetBeatsCarriedStyling
// and the TestProperty_WrapRowsAreStyleSelfContained seeds).
//
// A row with fewer than gutterCols columns of its own is all gutter: there is
// nothing past the gutter to style, so nothing is inserted.
func openStyleAfterGutter(row, open string, gutterCols int) string {
	if open == "" {
		return row
	}
	if gutterCols <= 0 {
		return open + row
	}
	off := -1
	eachDisplayCluster(row, func(c displayCluster) bool {
		if c.IsEscape {
			return true
		}
		if c.Col >= gutterCols {
			off = c.ByteOff
			return false
		}
		return true
	})
	if off < 0 {
		return row
	}
	var st sgrState
	st.feed(open)
	st.feed(row[:off])
	seq := st.openSeq()
	if seq == "" {
		return row
	}
	return row[:off] + seq + row[off:]
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
