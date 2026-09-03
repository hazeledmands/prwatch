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
		lines = append(lines, b.String())
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
