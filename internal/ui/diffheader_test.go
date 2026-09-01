package ui

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// trickyDiff is a one-file diff whose hunk body contains lines that *look*
// like file headers: `+++i;` (adding `++i;`) and `--- comment` (removing a
// SQL/Lua `-- comment`). Both are ordinary body lines; only the `---`/`+++`
// pair before the first `@@` are headers.
const trickyDiff = `diff --git a/main.c b/main.c
index 1111111..2222222 100644
--- a/main.c
+++ b/main.c
@@ -1,4 +1,4 @@
 int main(void) {
-  i++;
-- comment
+++i;
+  return i;
 }
`

// TestDiffHeaderDetection_BodyLines covers CODE_REVIEW A1 sub-item 5: the
// four independent header predicates skipped `+++`/`---`-prefixed body
// lines, shifting every later annotation in the hunk.
func TestDiffHeaderDetection_BodyLines(t *testing.T) {
	t.Run("shortstat counts body lines", func(t *testing.T) {
		got := shortstatFromDiff(trickyDiff)
		want := "1 file changed, 2 insertions(+), 2 deletions(-)"
		if got != want {
			t.Errorf("shortstatFromDiff = %q, want %q", got, want)
		}
	})

	t.Run("hunks span the real changed lines", func(t *testing.T) {
		hunks := parseDiffHunks(trickyDiff)
		if len(hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %+v", hunks)
		}
		// New file:
		//   1 int main(void) {
		//   2 ++i;          <- added (from "+++i;")
		//   3   return i;   <- added
		//   4 }
		// Removals ("  i++;" and "-- comment") attach at line 2.
		if hunks[0].StartLine != 2 || hunks[0].EndLine != 3 {
			t.Errorf("hunk = %+v, want StartLine 2 EndLine 3", hunks[0])
		}
	})

	t.Run("annotations land on the right lines", func(t *testing.T) {
		ann := parseDiffAnnotations(trickyDiff)
		if a, ok := ann[2]; !ok {
			t.Errorf("no annotation for new line 2 (the `++i;` addition); got %+v", ann)
		} else if len(a.removedLines) != 2 {
			t.Errorf("line 2: removedLines = %q, want the two removed lines", a.removedLines)
		}
		if a, ok := ann[3]; !ok || a.kind != diffLineAdded {
			t.Errorf("new line 3 should be a plain addition; got %+v (present=%v)", a, ok)
		}
		if _, ok := ann[4]; ok {
			t.Errorf("new line 4 is context, should have no annotation; got %+v", ann)
		}
	})

	t.Run("colorDiff styles body lines as add/remove", func(t *testing.T) {
		out := colorDiff(trickyDiff)
		lines := strings.Split(out, "\n")
		if lines[2] != diffHeaderStyle.Render("--- a/main.c") {
			t.Errorf("the pre-hunk `--- a/main.c` should be styled as a header, got %q", stripANSI(lines[2]))
		}
		if lines[3] != diffHeaderStyle.Render("+++ b/main.c") {
			t.Errorf("the pre-hunk `+++ b/main.c` should be styled as a header, got %q", stripANSI(lines[3]))
		}
		if lines[7] != diffRemoveStyle.Render("-- comment") {
			t.Errorf("`-- comment` inside a hunk should render as a removal, got %q", stripANSI(lines[7]))
		}
		if lines[8] != diffAddStyle.Render("+++i;") {
			t.Errorf("`+++i;` inside a hunk should render as an addition, got %q", stripANSI(lines[8]))
		}
	})
}

// TestDiffHeaderDetection_MultiFile checks the positional rule still holds:
// `---`/`+++` between files are headers, not body lines.
func TestDiffHeaderDetection_MultiFile(t *testing.T) {
	diff := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-old
+new
diff --git a/b.txt b/b.txt
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-old
+new
`
	got := shortstatFromDiff(diff)
	want := "2 files changed, 2 insertions(+), 2 deletions(-)"
	if got != want {
		t.Errorf("shortstatFromDiff = %q, want %q", got, want)
	}
}

// TestProperty_DiffBodyLinesAreNeverHeaders fuzzes hunk bodies containing
// header-shaped text and checks that the shared classifier and every
// consumer agree on the counts.
func TestProperty_DiffBodyLinesAreNeverHeaders(t *testing.T) {
	bodyTexts := []string{"plain", "++i;", "-- comment", "--- three", "+++ three", "@ at", " leading space"}

	rapid.Check(t, func(t *rapid.T) {
		nOps := rapid.IntRange(1, 8).Draw(t, "nOps")
		var body strings.Builder
		wantIns, wantDel := 0, 0
		oldCount, newCount := 0, 0
		for i := range nOps {
			kind := rapid.SampledFrom([]string{"context", "added", "removed"}).Draw(t, fmt.Sprintf("kind%d", i))
			text := rapid.SampledFrom(bodyTexts).Draw(t, fmt.Sprintf("text%d", i))
			switch kind {
			case "context":
				body.WriteString(" " + text + "\n")
				oldCount++
				newCount++
			case "added":
				body.WriteString("+" + text + "\n")
				wantIns++
				newCount++
			case "removed":
				body.WriteString("-" + text + "\n")
				wantDel++
				oldCount++
			}
		}
		diff := fmt.Sprintf("diff --git a/f b/f\nindex 111..222 100644\n--- a/f\n+++ b/f\n@@ -1,%d +1,%d @@\n%s",
			oldCount, newCount, body.String())

		// Build the expected string independently of the production
		// formatter — deriving it from `pluralize` would let a formatting
		// bug agree with itself.
		countPhrase := func(n int, word, suffix string) string {
			if n == 1 {
				return fmt.Sprintf("%d %s%s", n, word, suffix)
			}
			return fmt.Sprintf("%d %ss%s", n, word, suffix)
		}
		got := shortstatFromDiff(diff)
		parts := []string{"1 file changed"}
		if wantIns > 0 {
			parts = append(parts, countPhrase(wantIns, "insertion", "(+)"))
		}
		if wantDel > 0 {
			parts = append(parts, countPhrase(wantDel, "deletion", "(-)"))
		}
		want := strings.Join(parts, ", ")
		if got != want {
			t.Fatalf("shortstatFromDiff(%q) = %q, want %q", diff, got, want)
		}

		// Annotations: every added line must have an annotation, and the
		// number of annotated added lines must equal wantIns.
		ann := parseDiffAnnotations(diff)
		added := 0
		for _, a := range ann {
			switch a.kind {
			case diffLineAdded, diffLineChanged:
				added++
			}
		}
		if added != wantIns {
			t.Fatalf("parseDiffAnnotations(%q): %d annotated added lines, want %d (%+v)", diff, added, wantIns, ann)
		}
	})
}
