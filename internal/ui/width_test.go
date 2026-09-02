package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// widthyString draws strings from the character classes that actually break
// width math, not just ASCII. Each class is one that has caused a real bug:
//
//   - U+0903 (Mc, spacing combining mark) merges backward into padding
//   - U+0300 (Mn, non-spacing mark) composes onto whatever precedes it
//   - U+0600 (Cf, Prepend class) swallows the character that FOLLOWS it,
//     including a space appended as padding
//   - wide CJK and emoji occupy two cells and must never be split
//   - ZWJ sequences and regional indicators are multi-rune single clusters
//   - ANSI escapes occupy no cells at all
func widthyString(t *rapid.T, label string) string {
	pieces := []string{
		"a", "b", "Z", " ", "0", "_",
		"ः", "̀", "؀",
		"日", "本", "テ",
		"é", "é",
		"🔥", "👩‍👩‍👦", "🇯🇵", "👍\U0001f3fd",
		"\x1b[31m", "\x1b[0m",
	}
	n := rapid.IntRange(0, 12).Draw(t, label+"Len")
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(pieces[rapid.IntRange(0, len(pieces)-1).Draw(t, label+"Piece")])
	}
	return b.String()
}

// TestClusterWalkAgreesWithOracle is the invariant the whole width layer rests
// on: the atoms eachDisplayCluster reports must exactly reconstruct the string,
// their columns must be consistent, and their widths must sum to what the
// oracle — and therefore the renderer — says the string measures.
//
// If this fails, every geometry consumer is silently wrong somewhere.
func TestClusterWalkAgreesWithOracle(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := widthyString(t, "s")

		var rebuilt strings.Builder
		sum := 0
		prevEnd := 0
		eachDisplayCluster(s, func(c displayCluster) bool {
			if c.ByteOff != prevEnd {
				t.Fatalf("atom at %q starts at byte %d, expected %d (gap or overlap)", c.Text, c.ByteOff, prevEnd)
			}
			if c.Col != sum {
				t.Fatalf("atom %q reports Col %d, but %d columns precede it", c.Text, c.Col, sum)
			}
			if c.Text == "" {
				t.Fatalf("empty atom at byte %d would not terminate the walk", c.ByteOff)
			}
			rebuilt.WriteString(c.Text)
			sum += c.Width
			prevEnd = c.ByteOff + len(c.Text)
			return true
		})

		if rebuilt.String() != s {
			t.Fatalf("walk did not reconstruct input: got %q want %q", rebuilt.String(), s)
		}
		if want := displayWidth(s); sum != want {
			t.Fatalf("atom widths sum to %d, oracle says %d for %q", sum, want, s)
		}
	})
}

// knownRendererDivergence reports whether s contains the construct on which
// ansi.StringWidth — and therefore lipgloss — is internally inconsistent: a
// grapheme cluster that begins with an ASCII byte and continues into non-ASCII
// bytes. See width.go's file comment.
//
// That shape is exactly what ansi's ASCII fast path mishandles. The fast path
// emits the ASCII base as one cell without consulting grapheme segmentation,
// then measures the continuation as though it started a new cluster — so the
// continuation's standalone width is added on top of the base's. Concrete cases
// this catches: an ASCII letter plus a spacing mark ("Aः", U+0903, category Mc)
// and a space plus an emoji modifier (" 🏿", U+1F3FF, category Sk). Both are one
// cluster; ansi scores them 2 and 3.
//
// The continuation must have nonzero width standing alone, which is what makes
// the fast path add a cell that segmentation would not. Non-spacing marks (Mn,
// e.g. U+0301) form the same cluster shape but measure 0 alone, so both
// accountings agree and those clusters are NOT excluded — deliberately, since
// decomposed accented Latin is the case that matters most and the property must
// actually check it rather than skip it.
func knownRendererDivergence(s string) bool {
	found := false
	eachDisplayCluster(s, func(c displayCluster) bool {
		if c.IsEscape || len(c.Text) < 2 || c.Text[0] >= 0x80 {
			return true
		}
		// The base is ASCII; the fast path will score it 1 and then measure the
		// remainder independently. Divergence iff that remainder is not
		// zero-width on its own.
		if displayWidth(c.Text[1:]) > 0 {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestOracleIsTheRenderersMeasurement pins the claim the oracle's doc comment
// makes: displayWidth is what lipgloss uses to lay text out. lipgloss.Width is
// a max over lines, so this compares on single-line input, which is what a
// width-promising row is.
//
// Two documented exceptions are excluded here and pinned separately, with their
// rationale, by the tests below — so each is asserted, not quietly tolerated:
//
//  1. a cluster beginning with an ASCII base and continuing into non-ASCII
//     bytes (knownRendererDivergence), and
//  2. a cluster split across ANSI escape spans, excluded here by stripping the
//     escapes before comparing.
//
// In both, ansi.StringWidth contradicts ansi's own grapheme segmentation, and a
// real terminal follows the segmentation — an SGR sequence emits no cell, so it
// cannot split a glyph on screen. PROMPT.md makes the cell grid ground truth,
// so the oracle follows the terminal rather than the library.
func TestOracleIsTheRenderersMeasurement(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		s = stripANSIForWidth(s)
		if knownRendererDivergence(s) {
			return
		}
		if got, want := displayWidth(s), lipgloss.Width(s); got != want {
			t.Fatalf("displayWidth(%q) = %d but lipgloss.Width = %d", s, got, want)
		}
		if got, want := displayWidth(s), ansi.StringWidth(s); got != want {
			t.Fatalf("displayWidth(%q) = %d but ansi.StringWidth = %d", s, got, want)
		}
	})
}

// TestOracleDivergesOnlyWhereRendererIsSelfInconsistent pins the exception
// itself: where we disagree with ansi.StringWidth, we agree with ansi's own
// grapheme segmentation — i.e. the renderer disagrees with itself there, and we
// follow the cluster-consistent branch because PROMPT.md requires clusters to
// stay whole.
func TestOracleDivergesOnlyWhereRendererIsSelfInconsistent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		s                 string
		wantOracle        int
		wantAnsiStringWid int
	}{
		// One grapheme cluster: ASCII base + U+0903 DEVANAGARI SIGN VISARGA.
		{"ascii base plus spacing mark", "Aः", 1, 2},
		// Same mechanism with an emoji modifier (U+1F3FF, category Sk), which
		// measures 2 standing alone — so ansi scores this one-cell cluster 3.
		{"ascii base plus emoji modifier", " 🏿", 1, 3},
		// Non-spacing mark: both accountings agree, so no divergence.
		{"ascii base plus non-spacing mark", "é", 1, 1},
		// Non-ASCII base: ansi takes its cluster path, so it agrees with us.
		{"wide base plus spacing mark", "日ः", 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayWidth(tc.s); got != tc.wantOracle {
				t.Errorf("displayWidth(%q) = %d, want %d", tc.s, got, tc.wantOracle)
			}
			if got := ansi.StringWidth(tc.s); got != tc.wantAnsiStringWid {
				t.Errorf("ansi.StringWidth(%q) = %d, want %d (upstream behavior changed)",
					tc.s, got, tc.wantAnsiStringWid)
			}
			// Wherever we differ from ansi.StringWidth, ansi's own grapheme
			// segmentation must back us up.
			sum := 0
			for i := 0; i < len(tc.s); {
				cl, w := ansi.FirstGraphemeCluster(tc.s[i:], ansi.GraphemeWidth)
				sum += w
				i += len(cl)
			}
			if sum != tc.wantOracle {
				t.Errorf("ansi grapheme widths sum to %d for %q, want %d — the oracle would be alone",
					sum, tc.s, tc.wantOracle)
			}
		})
	}
}

// TestWidthIgnoresEscapePlacement pins the oracle's "ANSI escape sequences are
// ignored" contract in its strong form: a line's width must not depend on where
// the highlighter put its color spans. Stripping the escapes must not change the
// measurement.
//
// This is not academic. The syntax highlighter emits one span per token, which
// puts escapes between the parts of a ZWJ emoji sequence. When clustering broke
// at escape boundaries, such a line measured 4 cells wider than the same line
// with escapes stripped — so the drag highlight, which rewrites its region with
// escapes removed, silently changed the row's width.
func TestWidthIgnoresEscapePlacement(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := widthyString(t, "s")
		if got, want := displayWidth(s), displayWidth(stripANSIForWidth(s)); got != want {
			t.Fatalf("displayWidth(%q) = %d but %d with escapes stripped", s, got, want)
		}
	})
}

// TestWidthIgnoresEscapePlacement_ZWJ is the concrete regression case: a family
// emoji with a color span around every component, exactly as the highlighter
// emits it.
func TestWidthIgnoresEscapePlacement_ZWJ(t *testing.T) {
	t.Parallel()
	const plain = "👩‍👩‍👦"
	spanned := "\x1b[31m👩\x1b[0m\x1b[32m‍\x1b[0m\x1b[31m👩\x1b[0m\x1b[32m‍\x1b[0m\x1b[31m👦\x1b[0m"
	if got, want := displayWidth(plain), 2; got != want {
		t.Fatalf("displayWidth(plain family emoji) = %d, want %d", got, want)
	}
	if got := displayWidth(spanned); got != 2 {
		t.Fatalf("displayWidth(per-component color spans) = %d, want 2 — escapes must not split the cluster", got)
	}
	// Divergence class 2, pinned explicitly: ansi.StringWidth breaks the cluster
	// at each escape and so reports 6. A terminal renders 2 — an SGR sequence
	// emits no cell and cannot split a glyph — so the oracle follows the screen.
	if got := ansi.StringWidth(spanned); got != 6 {
		t.Fatalf("ansi.StringWidth(spanned) = %d, want 6 (upstream behavior changed)", got)
	}
}

// TestPadToWidth_ExactForAnyInput is the "width-promising rows measure exactly
// their promised width for ANY input" clause of PROMPT.md's unicode width
// accounting, at the level of the padding primitive.
func TestPadToWidth_ExactForAnyInput(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		width := rapid.IntRange(0, 40).Draw(t, "width")
		if displayWidth(s) > width {
			return // padding only grows; truncation is fitToWidth's job
		}
		got := padToWidth(s, width)
		if w := displayWidth(got); w != width {
			t.Fatalf("padToWidth(%q, %d) measures %d", s, width, w)
		}
		if !strings.HasPrefix(got, s) {
			t.Fatalf("padToWidth(%q, %d) = %q does not preserve the input", s, width, got)
		}
	})
}

// TestPadToWidth_AbsorbedSpaces is the concrete case padToWidth's comment cites:
// a Prepend-class character eats the first space appended after it, so counting
// the shortfall once and appending that many spaces comes up short.
func TestPadToWidth_AbsorbedSpaces(t *testing.T) {
	t.Parallel()
	// "ः؀" measures 1; naively appending (3-1)=2 spaces yields width 2,
	// because the first space is absorbed into the Prepend cluster.
	const s = "ः؀"
	if w := displayWidth(s); w != 1 {
		t.Fatalf("precondition: displayWidth(%q) = %d, want 1", s, w)
	}
	if w := displayWidth(s + "  "); w != 2 {
		t.Fatalf("precondition: two appended spaces should measure 2, got %d", w)
	}
	if got := padToWidth(s, 3); displayWidth(got) != 3 {
		t.Fatalf("padToWidth(%q, 3) = %q measures %d, want 3", s, got, displayWidth(got))
	}
}

// TestFitToWidth_ExactForAnyInput covers the truncating half too.
func TestFitToWidth_ExactForAnyInput(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		width := rapid.IntRange(1, 40).Draw(t, "width")
		got := fitToWidth(s, width)
		if w := displayWidth(got); w != width {
			t.Fatalf("fitToWidth(%q, %d) = %q measures %d", s, width, got, w)
		}
	})
}

// TestTruncateToWidth_NeverExceedsAndNeverSplits checks the two properties a
// truncation must have: it fits the budget, and it cuts only at atom boundaries.
func TestTruncateToWidth_NeverExceedsAndNeverSplits(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		width := rapid.IntRange(0, 40).Draw(t, "width")
		tail := rapid.SampledFrom([]string{"", "…"}).Draw(t, "tail")

		got := truncateToWidth(s, width, tail)
		if w := displayWidth(got); w > width {
			t.Fatalf("truncateToWidth(%q, %d, %q) = %q measures %d, over budget", s, width, tail, got, w)
		}
		// The result is not a byte prefix of s — escapes past the cut are
		// deliberately carried through (see TestTruncateToWidth_KeepsAllEscapes,
		// and warning 1 in BUG_REPORTS.md). The content property is that the
		// *printable* text kept is a prefix of the printable text of s, cut at
		// an atom boundary.
		keptContent := stripANSIForWidth(strings.TrimSuffix(got, tail))
		srcContent := stripANSIForWidth(s)
		if !strings.HasPrefix(srcContent, keptContent) {
			t.Fatalf("truncateToWidth(%q, %d, %q) = %q: kept content %q is not a prefix of %q",
				s, width, tail, got, keptContent, srcContent)
		}
		if !atomBoundaries(srcContent)[len(keptContent)] {
			t.Fatalf("truncateToWidth(%q, %d, %q) cut inside an atom at content byte %d",
				s, width, tail, len(keptContent))
		}
	})
}

// atomBoundaries returns the set of byte offsets in s that sit between atoms
// (including 0 and len(s)) — the only places a slice may legally land.
func atomBoundaries(s string) map[int]bool {
	b := map[int]bool{0: true, len(s): true}
	eachDisplayCluster(s, func(c displayCluster) bool {
		b[c.ByteOff] = true
		b[c.ByteOff+len(c.Text)] = true
		return true
	})
	return b
}

// TestSliceByDisplayCol_NeverSplitsAnAtom holds for both rounding policies: a
// slice may include or exclude an atom, never bisect one.
func TestSliceByDisplayCol_NeverSplitsAnAtom(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		from := rapid.IntRange(0, 30).Draw(t, "from")
		to := rapid.IntRange(0, 30).Draw(t, "to")
		mode := rapid.SampledFrom([]colRounding{roundInward, roundOutward}).Draw(t, "mode")

		got := sliceByDisplayCol(s, from, to, mode)
		if got == "" {
			return
		}
		idx := strings.Index(s, got)
		if idx < 0 {
			t.Fatalf("sliceByDisplayCol(%q, %d, %d) = %q is not a substring", s, from, to, got)
		}
		bounds := atomBoundaries(s)
		start, end := displayColByteRange(s, from, to, mode)
		if !bounds[start] || !bounds[end] {
			t.Fatalf("sliceByDisplayCol(%q, %d, %d, %v) sliced [%d,%d), not at atom boundaries",
				s, from, to, mode, start, end)
		}
	})
}

// TestSliceByDisplayCol_OutwardIsSymmetric is the fix for the wide-glyph drag
// asymmetry: whichever cell of a glyph an endpoint lands on, the glyph is in.
// Selecting [c, c+1) for any single cell c of a wide glyph must yield that whole
// glyph, at the start edge and the end edge alike.
func TestSliceByDisplayCol_OutwardIsSymmetric(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		total := displayWidth(s)
		if total == 0 {
			return
		}
		col := rapid.IntRange(0, total-1).Draw(t, "col")

		// The atom covering this cell.
		var want string
		eachDisplayCluster(s, func(c displayCluster) bool {
			if c.IsEscape || c.Width == 0 {
				return true
			}
			if col >= c.Col && col < c.Col+c.Width {
				want = c.Text
				return false
			}
			return true
		})
		if want == "" {
			return
		}
		got := sliceByDisplayCol(s, col, col+1, roundOutward)
		if !strings.Contains(got, want) {
			t.Fatalf("selecting cell %d of %q gave %q, which omits the glyph %q covering that cell",
				col, s, got, want)
		}
	})
}

// TestNoDirectRunewidthOutsideOracle keeps the width layer from re-fragmenting.
// Six independent width authorities had accumulated before this was unified:
// runewidth.StringWidth, runewidth.RuneWidth walks in four different files,
// lipgloss.Width with rune slicing in statusbar.go, and a private rune-sum
// reimplementation in the test package. Each disagreed with the renderer, and
// with the others, on some input class.
//
// The oracle (width.go) is now the only place allowed to name a width library.
func TestNoDirectRunewidthOutsideOracle(t *testing.T) {
	t.Parallel()

	// Import paths no file outside the oracle may import at all.
	forbiddenImports := map[string]string{
		"github.com/mattn/go-runewidth": "go-runewidth measures without grapheme clustering",
		"github.com/rivo/uniseg":        "segment through eachDisplayCluster, not uniseg directly",
		"github.com/charmbracelet/x/ansi": "ansi's width and truncation functions are the oracle's " +
			"business; they contradict their own grapheme segmentation in two classes",
	}
	// Package-level functions that measure width, keyed by import path. Method
	// calls of the same name (lipgloss.NewStyle().Width(n) sets a style width)
	// are not matched, because the AST distinguishes a call on a package
	// identifier from a call on a value.
	forbiddenCalls := map[string]map[string]string{
		"charm.land/lipgloss/v2": {
			"Width":  "lipgloss.Width is a max-over-lines measure that breaks clusters at escapes",
			"Height": "derive row counts from layout geometry, not by measuring rendered text",
		},
	}
	// width.go defines the oracle; width_test.go verifies it against the
	// underlying libraries, which it can only do by naming them.
	exempt := map[string]bool{
		"internal/ui/width.go":      true,
		"internal/ui/width_test.go": true,
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if exempt[rel] {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Errorf("parse %s: %v", rel, parseErr)
			return nil
		}
		checked++

		// Resolve each import to the local name it is bound to, so an alias
		// (or a dot import) cannot slip past a textual match.
		localNames := map[string]string{} // local ident -> import path
		for _, imp := range file.Imports {
			path, unqErr := strconv.Unquote(imp.Path.Value)
			if unqErr != nil {
				continue
			}
			if why, bad := forbiddenImports[path]; bad {
				t.Errorf("%s:%d: imports %s — %s; route width math through displayWidth/eachDisplayCluster (width.go)",
					rel, fset.Position(imp.Pos()).Line, path, why)
			}
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				if imp.Name.Name == "." {
					t.Errorf("%s:%d: dot-imports %s, which defeats this guard",
						rel, fset.Position(imp.Pos()).Line, path)
					continue
				}
				name = imp.Name.Name
			}
			localNames[name] = path
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true // a method call on a value, not a package function
			}
			importPath, known := localNames[ident.Name]
			if !known {
				return true
			}
			if why, bad := forbiddenCalls[importPath][sel.Sel.Name]; bad {
				t.Errorf("%s:%d: %s.%s — %s; use displayWidth (width.go). If a call site "+
					"deliberately predicts the renderer's own wrapping, exempt it here with a "+
					"comment saying so.",
					rel, fset.Position(call.Pos()).Line, ident.Name, sel.Sel.Name, why)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if checked < 10 {
		t.Fatalf("only %d Go files checked under %s; the guard is not actually scanning the repo", checked, root)
	}
}

// TestWrapNeverSplitsACluster is PROMPT.md's "no wrap break may land inside a
// cluster". The mid-token splitter is the risk: it is the one place the wrapper
// deliberately breaks inside a token, and it used to step rune by rune, so a
// wide glyph could be halved or a base character parted from its marks.
//
// Checked by reconstructing each row's atoms: if a break had split a cluster,
// the halves would appear as separate atoms on adjacent rows and the rejoined
// text would no longer segment the same way as the source.
func TestWrapNeverSplitsACluster(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		body := widthyString(t, "body")
		// Long unbroken tokens force the mid-token splitter, which is the
		// path that can break inside a cluster.
		content := strings.ReplaceAll(body, " ", "")
		if content == "" {
			return
		}
		content = strings.Repeat(content, 4)
		width := rapid.IntRange(2, 20).Draw(t, "width")

		out, _, _ := wrapLinesWithBreaks(content, width, 0)
		// The wrapper deliberately keeps a token whole rather than splitting it
		// when the token is no wider than maxWordWidth = max(10, width/8) — see
		// TestWrapLines_PreservesShortWords. So a row may legitimately exceed a
		// very narrow width; only assert the bound where the wrapper promises
		// it, and let the cluster-segmentation check below cover every width.
		if width > 10 {
			for _, row := range strings.Split(out, "\n") {
				if w := displayWidth(row); w > width {
					t.Fatalf("wrapped row %q measures %d, over width %d", row, w, width)
				}
			}
		}
		// Every atom of every row must be a whole atom of the source: joining
		// the rows back together must reproduce the source's segmentation.
		var rejoined strings.Builder
		for _, row := range strings.Split(out, "\n") {
			rejoined.WriteString(row)
		}
		if got, want := clusterList(rejoined.String()), clusterList(content); !equalStrings(got, want) {
			t.Fatalf("wrap changed cluster segmentation:\n  source rows: %v\n  wrapped:     %v", want, got)
		}
	})
}

func clusterList(s string) []string {
	var out []string
	eachDisplayCluster(s, func(c displayCluster) bool {
		if !c.IsEscape {
			out = append(out, c.Text)
		}
		return true
	})
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCursorSnapsToClusterStart is PROMPT.md's "a cursor placed by click snaps
// to the start of the cluster it lands on" (mouse behavior). Clicking the
// trailing cell of a wide glyph must put the cursor on the glyph's first cell,
// not inside it.
func TestCursorSnapsToClusterStart(t *testing.T) {
	t.Parallel()
	mp := newMainPane()
	mp.SetSize(40, 6)
	// Columns: a=0, b=1, 日=2..3, c=4, 本=5..6, d=7
	mp.SetPlainContent("ab日c本d")

	cases := []struct {
		clickCol int
		want     int
		why      string
	}{
		{0, 0, "ascii cell is its own start"},
		{1, 1, "ascii cell is its own start"},
		{2, 2, "leading cell of a wide glyph"},
		{3, 2, "trailing cell of a wide glyph snaps back"},
		{4, 4, "ascii after a wide glyph"},
		{5, 5, "leading cell of the second wide glyph"},
		{6, 5, "trailing cell of the second wide glyph snaps back"},
		{7, 7, "ascii after the second wide glyph"},
	}
	for _, tc := range cases {
		if got := mp.snapDisplayColToCluster(0, tc.clickCol); got != tc.want {
			t.Errorf("snapDisplayColToCluster(0, %d) = %d, want %d (%s)",
				tc.clickCol, got, tc.want, tc.why)
		}
	}
}

// TestCursorSnapIsIdempotent: snapping an already-snapped column is a no-op, so
// repeated clicks and re-clamps cannot walk the cursor backwards.
func TestCursorSnapIsIdempotent(t *testing.T) {
	t.Parallel()
	mp := newMainPane()
	mp.SetSize(40, 6)
	mp.SetPlainContent("ab日c本d")
	for col := 0; col < 10; col++ {
		once := mp.snapDisplayColToCluster(0, col)
		twice := mp.snapDisplayColToCluster(0, once)
		if once != twice {
			t.Errorf("snap not idempotent at col %d: %d then %d", col, once, twice)
		}
	}
}

// countOSC8 returns how many OSC 8 sequences s contains. makeHyperlink emits
// two per link: the opener carrying the URL and the empty terminator.
func countOSC8(s string) int {
	return strings.Count(s, "\x1b]8;;")
}

// TestTruncateToWidth_PreservesEscapesPastTheCut is warning 1: truncation used
// to stop walking at the first over-budget cluster, dropping every escape after
// it. For a status bar carrying a hyperlink, that left the opening OSC 8 with no
// terminator, so the "…" and everything rendered after it joined the link.
func TestTruncateToWidth_PreservesEscapesPastTheCut(t *testing.T) {
	t.Parallel()
	link := makeHyperlink("https://example.com/pull/1234", "a very long pull request title")
	if countOSC8(link) != 2 {
		t.Fatalf("precondition: makeHyperlink should emit 2 OSC 8 sequences, got %d", countOSC8(link))
	}

	// Cut well inside the link text.
	for _, width := range []int{1, 2, 5, 10, 20} {
		got := truncateToWidth(link, width, "…")
		if n := countOSC8(got); n != 2 {
			t.Errorf("truncateToWidth(link, %d) left %d OSC 8 sequences, want 2 (unbalanced link)\n\tgot %q",
				width, n, got)
		}
		if w := displayWidth(got); w > width {
			t.Errorf("truncateToWidth(link, %d) measures %d, over budget", width, w)
		}
	}
}

// TestTruncateToWidth_PreservesSGRReset is the same bug in its color form: a
// dropped trailing reset leaks styling past the truncation point.
func TestTruncateToWidth_PreservesSGRReset(t *testing.T) {
	t.Parallel()
	s := "\x1b[31mred text that is quite long\x1b[0m"
	got := truncateToWidth(s, 6, "…")
	if !strings.HasSuffix(got, "\x1b[0m…") && !strings.Contains(got, "\x1b[0m") {
		t.Errorf("truncateToWidth dropped the SGR reset: %q", got)
	}
	if w := displayWidth(got); w > 6 {
		t.Errorf("truncateToWidth measures %d, over budget 6", w)
	}
}

// TestTruncateToWidth_KeepsAllEscapes is the general property behind both cases
// above: truncation drops printable content, never escape sequences.
func TestTruncateToWidth_KeepsAllEscapes(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := strings.ReplaceAll(widthyString(t, "s"), "\n", "")
		width := rapid.IntRange(0, 30).Draw(t, "width")
		tail := rapid.SampledFrom([]string{"", "…"}).Draw(t, "tail")

		got := truncateToWidth(s, width, tail)
		if width <= 0 {
			return // documented to return ""
		}
		if want, have := ansiEscapesIn(s), ansiEscapesIn(got); want != have {
			t.Fatalf("truncateToWidth(%q, %d, %q) changed the escape stream:\n  want %q\n  got  %q",
				s, width, tail, want, have)
		}
	})
}

// TestPadToHeight_ExactWidthForAnyInput is warning 2: padToHeight padded by
// counted shortfall, so a row ending in a Prepend-class cluster absorbed the
// first space and came out a cell short of its promised width. These rows carry
// arbitrary file content through RenderOnce.
func TestPadToHeight_ExactWidthForAnyInput(t *testing.T) {
	t.Parallel()
	// "ः؀" measures 1 and absorbs the first space appended after it.
	const prependFinal = "xy ः؀"
	out := padToHeight(prependFinal, 10, 1)
	for i, line := range strings.Split(out, "\n") {
		if w := displayWidth(line); w != 10 {
			t.Errorf("padToHeight line %d measures %d, want exactly 10 (line %q)", i, w, line)
		}
	}
}

func TestPadToHeight_ExactWidthProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nLines := rapid.IntRange(1, 4).Draw(t, "nLines")
		var lines []string
		for i := 0; i < nLines; i++ {
			lines = append(lines, strings.ReplaceAll(widthyString(t, "line"), "\n", ""))
		}
		content := strings.Join(lines, "\n")
		width := rapid.IntRange(1, 30).Draw(t, "width")
		height := rapid.IntRange(1, 6).Draw(t, "height")

		out := padToHeight(content, width, height)
		got := strings.Split(out, "\n")
		if len(got) != height {
			t.Fatalf("padToHeight produced %d lines, want %d", len(got), height)
		}
		for i, line := range got {
			// Lines wider than the target are left alone (padToHeight pads,
			// it does not truncate); every other line must be exact.
			if w := displayWidth(line); w < width {
				t.Fatalf("padToHeight line %d measures %d, under width %d (line %q)", i, w, width, line)
			}
		}
	})
}

// TestStatusBarRowCountMatchesLayout is warning 3. The overflow guards used to
// MIX width authorities: they measured with lipgloss.Width but truncated with
// ellipsize, which measures with the oracle. On divergence-class input the
// oracle says the bar fits while lipgloss still measures it wider, so
// statusBarPRStyle.Width(width).Render hard-wraps the bar onto a second row —
// and renderStatusBar then returns more lines than statusBarLineCount promised.
//
// That mismatch is the statusBarLines off-by-one-click family CLAUDE.md records
// as having recurred three times: every hit region below the wrap point shifts
// by a row. Layout and render must agree for any input, so this checks the two
// halves of that geometry against each other.
func TestStatusBarRowCountMatchesLayout(t *testing.T) {
	t.Parallel()
	// Titles built from the classes where the renderer's own measure disagrees
	// with grapheme segmentation, plus ordinary content as a control.
	titles := []string{
		"ordinary pull request title",
		strings.Repeat("Aः", 40),    // ASCII base + spacing mark
		strings.Repeat(" 🏿", 30),    // ASCII base + emoji modifier
		strings.Repeat("日本語", 20),   // wide glyphs
		strings.Repeat("👩‍👩‍👦", 20), // ZWJ clusters
		strings.Repeat("é", 40),    // decomposed Latin
	}
	widths := []int{20, 40, 60, 80, 120}

	for _, title := range titles {
		for _, width := range widths {
			data := statusBarData{
				info: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
				pr:   git.PRInfoResult{Number: 42, Title: title},
			}
			bar, _, _, _ := renderStatusBar(width, data)
			gotRows := len(strings.Split(bar, "\n"))
			wantRows := statusBarLineCount(data)
			if gotRows != wantRows {
				t.Errorf("width %d, title %.20q…: renderStatusBar produced %d rows but statusBarLineCount promised %d",
					width, title, gotRows, wantRows)
			}
			// Every row must also fit the promised width, or the terminal wraps
			// it and the same row-shift happens one layer down.
			for i, row := range strings.Split(bar, "\n") {
				if w := displayWidth(row); w > width {
					t.Errorf("width %d, title %.20q…: row %d measures %d, overflowing the bar",
						width, title, i, w)
				}
			}
		}
	}
}

// BenchmarkViewEmpty renders an empty model at the configuration that started
// timing out under -race: 192x51 with a 57-column sidebar. Almost every row is
// blank padding, which is exactly the shape that made the one-space-at-a-time
// padding loops quadratic in the row width.
//
// Guarding this with a benchmark rather than only the property test means the
// next regression shows up as a number instead of a flaky 1s timeout.
func BenchmarkViewEmpty(b *testing.B) {
	m := NewModel(b.TempDir(), nil)
	m.width = 192
	m.height = 51
	m.updateLayout()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkPadToWidth covers the primitive directly, on both the common ASCII
// tail (fast path) and the Prepend-class tail that forces the incremental loop.
func BenchmarkPadToWidth(b *testing.B) {
	cases := []struct{ name, s string }{
		{"empty", ""},
		{"ascii", "some ordinary content"},
		{"prepend_tail", "content ending in ः؀"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = padToWidth(tc.s, 192)
			}
		})
	}
}

// firstVisibleCluster returns the first atom of s that occupies at least one
// terminal cell, skipping escapes and zero-width clusters. It is the
// counterpart to charAt, which reports the glyph covering a given cell.
func firstVisibleCluster(s string) string {
	first := ""
	eachDisplayCluster(s, func(c displayCluster) bool {
		if c.IsEscape || c.Width == 0 {
			return true
		}
		first = c.Text
		return false
	})
	return first
}
