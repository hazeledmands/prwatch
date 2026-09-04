package ui

import (
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestWrapLines_ContinuationRowsCarryTheirOwnStyle is the "styled run spanning a
// wrap boundary" fix: a continuation row must open the styling that was active
// at the break rather than inheriting it by SGR bleed from the row above. Bleed
// is not a rendering model the pane can rely on — the viewport renders a window
// of rows, so when a continuation row is the top visible row its opening
// sequence sits in a row that is never written to the terminal and the row
// renders unstyled.
//
// The assertions are the two halves of self-containment: every row that leaves
// styling open closes it, and every continuation row re-opens it after its
// indent.
func TestWrapLines_ContinuationRowsCarryTheirOwnStyle(t *testing.T) {
	t.Parallel()

	longWords := strings.TrimSpace(strings.Repeat("word ", 10))

	tests := []struct {
		name   string
		in     string
		width  int
		indent int
		// wantOpen is the sequence every continuation row must carry directly
		// after its indent. Empty means the row must carry no escape there:
		// nothing was open at the break.
		wantOpen string
	}{
		{
			name:     "single foreground run",
			in:       diffAddStyle.Render(longWords),
			width:    20,
			wantOpen: styleOpenSeq(diffAddStyle),
		},
		{
			name:     "reopened after the continuation indent",
			in:       "  12 " + diffAddStyle.Render(longWords),
			width:    20,
			indent:   5,
			wantOpen: styleOpenSeq(diffAddStyle),
		},
		{
			name:     "several concurrent attributes",
			in:       diffAddStyle.Background(diffAddBg).Bold(true).Render(longWords),
			width:    20,
			wantOpen: styleOpenSeq(diffAddStyle.Background(diffAddBg).Bold(true)),
		},
		{
			name: "style closed before the break stays closed",
			// The styled run ends, with its own reset, well inside row 0.
			in:       diffAddStyle.Render("aa") + " " + longWords,
			width:    20,
			wantOpen: "",
		},
		{
			name: "full reset mid-line drops the earlier run",
			// An explicit reset with no following open: nothing is active at
			// any later break, so no continuation row may re-open the red.
			in:       "\x1b[38;2;243;139;168m" + "aa" + "\x1b[0m" + " " + longWords,
			width:    20,
			wantOpen: "",
		},
		{
			name: "partial attribute change replays as switched off",
			// Bold is switched off again mid-line. The re-opened sequence
			// replays the switch-off rather than dropping the bold that it
			// cancels — the tracker replays sequences instead of modelling what
			// each parameter means, so the terminal, not this code, decides
			// that 22 undoes 1. TestProperty_WrapRowsAreStyleSelfContained is
			// what checks the rendered result.
			in:       "\x1b[1m\x1b[38;2;166;227;161m" + "aa " + "\x1b[22m" + longWords,
			width:    20,
			wantOpen: "\x1b[1m\x1b[38;2;166;227;161m\x1b[22m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, cont, _, _ := wrapLinesWithBreaks(tt.in, tt.width, tt.indent)
			rows := strings.Split(out, "\n")
			if len(rows) < 2 {
				t.Fatalf("input did not wrap: rows = %q", rows)
			}
			indentStr := strings.Repeat(" ", tt.indent)
			sawContinuation := false
			for i, row := range rows {
				if !cont[i] {
					continue
				}
				sawContinuation = true
				want := indentStr + tt.wantOpen
				if !strings.HasPrefix(row, want) {
					t.Errorf("row %d = %q, want prefix %q", i, row, want)
				}
				if tt.wantOpen == "" {
					continue
				}
				// The row above must have closed what this row re-opens,
				// otherwise the two rows double up when rendered in sequence.
				if !strings.HasSuffix(rows[i-1], sgrReset) {
					t.Errorf("row %d = %q, want it to close with a reset", i-1, rows[i-1])
				}
			}
			if !sawContinuation {
				t.Fatalf("no continuation rows in %q", rows)
			}
		})
	}
}

// TestMainPane_ContinuationRowIsStyledAsTopVisibleRow is the symptom as a user
// meets it: scroll a wrapped diff line so its continuation row is the top
// visible row, and that row must still be green. The viewport writes only the
// rows in its window, so styling opened by a row above the window never reaches
// the terminal.
func TestMainPane_ContinuationRowIsStyledAsTopVisibleRow(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(30, 4)
	mp.SetWordWrap(true)
	mp.SetContent("@@ -1 +1 @@\n+" + strings.TrimSpace(strings.Repeat("added ", 20)))

	// The added line is the only one long enough to wrap, so its continuation
	// rows are the only continuation rows.
	cont := -1
	for i, isCont := range mp.wrapContinuation {
		if isCont {
			cont = i
			break
		}
	}
	if cont < 0 {
		t.Fatalf("content did not wrap; rows = %q", strings.Split(mp.viewport.GetContent(), "\n"))
	}

	mp.viewport.SetYOffset(cont)
	top := strings.Split(mp.viewport.View(), "\n")[0]

	cells, _ := modelRow(sgrModel{}, top)
	if len(cells) == 0 {
		t.Fatalf("top visible row %q has no visible cells", top)
	}
	wantCells, _ := modelRow(sgrModel{}, diffAddStyle.Render("x"))
	if cells[0] != wantCells[0] {
		t.Errorf("top visible row %q renders its first cell as %+v, want the added-line style %+v",
			top, cells[0], wantCells[0])
	}
}

// TestWrapLines_SearchHighlightDoesNotPaintContinuationIndent pins the second
// symptom of the same bug. A continuation indent is padding the wrapper
// invented, not source text: it stands in for the gutter, which is never
// highlighted. So a search highlight that spans a wrap break must resume
// *after* the indent, leaving those columns unstyled — with the styling bled
// from the row above, the highlight background painted the indent instead.
func TestWrapLines_SearchHighlightDoesNotPaintContinuationIndent(t *testing.T) {
	t.Parallel()

	const gutter = "  12 " // 5 columns, the shape applyFileViewFormatting emits
	const indent = len(gutter)
	body := strings.Repeat("x", 40)

	highlighted := highlightSearch(gutter+body, body)
	out, cont, _, _ := wrapLinesWithBreaks(highlighted, 20, indent)
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("input did not wrap: rows = %q", rows)
	}

	for i, row := range rows {
		if !cont[i] {
			continue
		}
		if !strings.HasPrefix(row, strings.Repeat(" ", indent)) {
			t.Errorf("row %d = %q: continuation indent is not bare spaces", i, row)
		}
		want := strings.Repeat(" ", indent) + searchHighlightOpen
		if !strings.HasPrefix(row, want) {
			t.Errorf("row %d = %q, want the highlight to resume after the indent (prefix %q)", i, row, want)
		}
	}
}

// TestWrapLines_StylingDoesNotChangeGeometry is the regression that closing and
// re-opening rows first introduced: styling must not change how many rows a
// line wraps to, where the breaks land, or the space accounting.
//
// It did, in the one shape where the wrapper's row buffer holds no content: a
// source line whose trailing space run overflows its last row is flushed and
// then discarded, and the re-opened styling written into that buffer read as
// content, emitting a spurious blank row. That also quietly moved the line's
// trailing-space count out of the fourth slice (its own run) into the third (a
// break's), the two ce53460 went to some trouble to keep disjoint.
func TestWrapLines_StylingDoesNotChangeGeometry(t *testing.T) {
	t.Parallel()

	longWords := strings.Repeat("word ", 32)

	tests := []struct {
		name   string
		plain  string
		styled string
		width  int
		indent int
	}{
		{
			// The reviewer's case: a styled diff line whose trailing run
			// overflows the last row, with no gutter (diff mode).
			name:   "trailing run overflows the last row",
			plain:  "+" + longWords + "  ",
			styled: diffAddStyle.Render("+" + longWords + "  "),
			width:  20,
		},
		{
			name:   "same, behind a gutter",
			plain:  "  12 " + longWords + "  ",
			styled: "  12 " + diffAddStyle.Render(longWords+"  "),
			width:  20,
			indent: 5,
		},
		{
			name:   "trailing run fits on the last row",
			plain:  "+" + longWords + "x  ",
			styled: diffAddStyle.Render("+" + longWords + "x  "),
			width:  20,
		},
		{
			name:   "no trailing run",
			plain:  "+" + strings.TrimSpace(longWords),
			styled: diffAddStyle.Render("+" + strings.TrimSpace(longWords)),
			width:  20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The plain line is the same content with the styling taken out, so
			// the two must wrap to the same shape.
			if got := stripANSIForWidth(tt.styled); got != tt.plain {
				t.Fatalf("styled input strips to %q, want the plain input %q", got, tt.plain)
			}
			assertSameWrapGeometry(t, tt.plain, tt.styled, tt.width, tt.indent)
		})
	}
}

// assertSameWrapGeometry wraps a styled line and its unstyled equivalent and
// requires everything but the escapes to match: the rows' visible text and all
// three accounting slices.
func assertSameWrapGeometry(t testingT, plain, styled string, width, indent int) {
	plainOut, plainCont, plainBreaks, plainOwn := wrapLinesWithBreaks(plain, width, indent)
	styledOut, styledCont, styledBreaks, styledOwn := wrapLinesWithBreaks(styled, width, indent)

	plainRows := strings.Split(plainOut, "\n")
	styledRows := strings.Split(styledOut, "\n")
	if len(plainRows) != len(styledRows) {
		t.Fatalf("styled wrapped to %d rows %q, plain to %d rows %q",
			len(styledRows), styledRows, len(plainRows), plainRows)
	}
	for i := range plainRows {
		if got, want := stripANSIForWidth(styledRows[i]), plainRows[i]; got != want {
			t.Fatalf("row %d: styled renders %q, plain renders %q", i, got, want)
		}
		if styledCont[i] != plainCont[i] {
			t.Fatalf("row %d: cont %v styled, %v plain", i, styledCont[i], plainCont[i])
		}
		if styledBreaks[i] != plainBreaks[i] {
			t.Fatalf("row %d: breaks %d styled, %d plain (breaks=%v vs %v)",
				i, styledBreaks[i], plainBreaks[i], styledBreaks, plainBreaks)
		}
		if styledOwn[i] != plainOwn[i] {
			t.Fatalf("row %d: own trailing %d styled, %d plain (own=%v vs %v)",
				i, styledOwn[i], plainOwn[i], styledOwn, plainOwn)
		}
	}
}

// testingT is the slice of *testing.T and *rapid.T that assertSameWrapGeometry
// needs, so the table test and the property can share it.
type testingT interface {
	Fatalf(format string, args ...any)
}

// TestProperty_WrapGeometryIgnoresStyling generalizes it: for any content the
// pane's producers could emit, wrapping is blind to the styling. Escapes are
// zero-width to the oracle and never sit between a space and a non-space — every
// producer attaches an opening sequence to the text it styles — so they cannot
// move a token boundary, a break, or a space count.
func TestProperty_WrapGeometryIgnoresStyling(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		styled := styledContent(t)
		width := rapid.IntRange(1, 40).Draw(t, "width")
		indent := rapid.IntRange(0, 6).Draw(t, "indent")
		assertSameWrapGeometry(t, stripANSIForWidth(styled), styled, width, indent)
	})
}

// TestWrapLines_ReopenedStylingDoesNotPaintTheNextLinesGutter is the gutter rule
// applied to the other kind of row that can inherit styling: not a continuation
// row, but the first row of the *next* source line, when a line leaves styling
// open. Its gutter is a real one — a line number — and it must stay unpainted
// for the same reason a continuation row's blank stand-in must.
func TestWrapLines_ReopenedStylingDoesNotPaintTheNextLinesGutter(t *testing.T) {
	t.Parallel()

	const indent = 5
	open := styleOpenSeq(diffAddStyle)
	// The first line opens styling and never closes it.
	content := "  12 " + open + "code\n" + "  13 " + "more code"

	out, _, _, _ := wrapLinesWithBreaks(content, 40, indent)
	rows := strings.Split(out, "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %q", rows)
	}
	if want := "  13 " + open; !strings.HasPrefix(rows[1], want) {
		t.Errorf("row 1 = %q, want the carried styling to resume after the gutter (prefix %q)",
			rows[1], want)
	}
}

// TestWrapLines_LineOwnResetBeatsCarriedStyling is the "code block bleeds into
// the next comment's separator" regression (BUG_REPORTS.md, 2026-09-04). When a
// line leaves styling open, the wrapper re-opens it on following rows — but a
// following line's own reset must win over the carry, exactly as it would have
// in the original stream. The broken shape: openStyleAfterGutter stepped over
// escape clusters when looking for the insertion point, so on a line whose
// post-gutter content began with (or consisted only of) a reset, the carried
// styling was inserted *after* that reset and survived it — and the tracker,
// fed the composed row, kept the styling alive for every later line until the
// next reset. renderMarkdown produces exactly this shape: a fenced code block
// ends with a reset on its own line.
func TestWrapLines_LineOwnResetBeatsCarriedStyling(t *testing.T) {
	t.Parallel()

	open := "\x1b[48;2;40;40;40m\x1b[38;2;166;227;161m" // codeBg+codeFg, as renderMarkdown emits

	tests := []struct {
		name   string
		in     string
		indent int
		// unstyledRows are the row indices whose every visible cell must render
		// with no attributes at all, given all preceding rows were written.
		unstyledRows []int
	}{
		{
			name:         "reset-only line stops the carry",
			in:           open + "code\n\x1b[0m\n--- b.go:2 ---",
			unstyledRows: []int{1, 2},
		},
		{
			name:         "reset-only line behind a gutter",
			in:           " 1  " + open + "code\n 2  \x1b[0m\n 3  --- b.go:2 ---",
			indent:       4,
			unstyledRows: []int{1, 2},
		},
		{
			name:         "leading reset on a content line",
			in:           open + "code\n\x1b[0mplain text",
			unstyledRows: []int{1},
		},
		{
			name:         "leading reset behind a gutter",
			in:           " 1  " + open + "code\n 2  \x1b[0mplain text",
			indent:       4,
			unstyledRows: []int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _, _, _ := wrapLinesWithBreaks(tt.in, 40, tt.indent)
			rows := strings.Split(out, "\n")

			var carried sgrModel
			var cells [][]sgrModel
			for _, row := range rows {
				rowCells, after := modelRow(carried, row)
				cells = append(cells, rowCells)
				carried = after
			}
			for _, i := range tt.unstyledRows {
				if i >= len(rows) {
					t.Fatalf("want row %d, only %d rows: %q", i, len(rows), rows)
				}
				for j, cell := range cells[i] {
					if cell != (sgrModel{}) {
						t.Errorf("row %d %q cell %d renders styled as %+v; the line's own reset must beat the carried styling",
							i, rows[i], j, cell)
						break
					}
				}
			}
		})
	}
}

// TestProperty_WrapRowsAreStyleSelfContained is the invariant behind both
// symptoms: every row the wrapper emits renders identically whether it is
// written to the terminal on its own or after all of its predecessors.
//
// "Renders identically" is checked against sgrModel — an independent
// attribute-parsing terminal model, deliberately not the production tracker's
// replay-the-sequences approach — by comparing the attribute state at every
// visible cluster of a row, once with the model starting clean and once with it
// carrying the state the preceding rows left it in.
func TestProperty_WrapRowsAreStyleSelfContained(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		content := styledContent(t)
		width := rapid.IntRange(1, 40).Draw(t, "width")
		indent := rapid.IntRange(0, 6).Draw(t, "indent")

		out, _, _, _ := wrapLinesWithBreaks(content, width, indent)
		rows := strings.Split(out, "\n")

		var carried sgrModel
		for i, row := range rows {
			alone, _ := modelRow(sgrModel{}, row)
			inContext, after := modelRow(carried, row)
			if len(alone) != len(inContext) {
				t.Fatalf("row %d %q: %d cells alone, %d in context", i, row, len(alone), len(inContext))
			}
			for j := range alone {
				if alone[j] != inContext[j] {
					t.Fatalf("row %d %q cell %d: renders %+v alone but %+v after its predecessors",
						i, row, j, alone[j], inContext[j])
				}
			}
			carried = after
		}
	})
}

// styledContent draws content shaped like what the pane actually wraps: styled
// runs (closed and unclosed), explicit resets, partial attribute changes, and
// several source lines.
func styledContent(t *rapid.T) string {
	opens := []string{
		"\x1b[38;2;166;227;161m",             // truecolor fg, as chroma emits
		"\x1b[48;2;31;45;36m",                // truecolor bg, as applyDiffBg emits
		"\x1b[1m",                            // bold
		"\x1b[3m",                            // italic
		"\x1b[31m",                           // basic fg
		"\x1b[38;5;204m",                     // 256-color fg
		"\x1b[1m\x1b[38;2;243;139;168m",      // two attributes at once
		"\x1b[22m",                           // partial off: intensity
		"\x1b[39m",                           // partial off: default fg
		"\x1b[0m",                            // full reset
		"\x1b[4m\x1b[48;5;17m\x1b[38;5;226m", // three at once
	}
	words := []string{"a", "ab", "hello", "café", "日本語", "//", strings.Repeat("y", 17), "🔥"}

	nLines := rapid.IntRange(1, 3).Draw(t, "nLines")
	var lines []string
	for l := range nLines {
		nSeg := rapid.IntRange(1, 8).Draw(t, drawKey("nSeg", l))
		var b strings.Builder
		for s := range nSeg {
			if s > 0 {
				b.WriteString(strings.Repeat(" ", rapid.IntRange(1, 3).Draw(t, drawKey("gap", l, s))))
			}
			if rapid.Bool().Draw(t, drawKey("styled", l, s)) {
				b.WriteString(rapid.SampledFrom(opens).Draw(t, drawKey("open", l, s)))
			}
			b.WriteString(rapid.SampledFrom(words).Draw(t, drawKey("word", l, s)))
			if rapid.Bool().Draw(t, drawKey("close", l, s)) {
				b.WriteString(sgrReset)
			}
		}
		// A trailing space run, so lines can end the way the wrapper's lossy
		// cases need them to: the run overflowing the last row is what used to
		// leave an escapes-only buffer behind and emit a blank row. Keyed on
		// values already drawn rather than drawn afresh, so the committed .fail
		// seeds keep replaying against an unchanged draw sequence — the same
		// reason TestProperty_WrapLines_JoinWithBreaksRestoresSource does it.
		lines = append(lines, b.String()+strings.Repeat(" ", (nSeg+l)%4))
	}
	return strings.Join(lines, "\n")
}

func drawKey(name string, parts ...int) string {
	var b strings.Builder
	b.WriteString(name)
	for _, p := range parts {
		b.WriteString("_")
		b.WriteString(strconv.Itoa(p))
	}
	return b.String()
}
