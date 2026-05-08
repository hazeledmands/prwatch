package ui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/git"
)

// === Title content helpers ===

func TestShortstatFromDiff_Basic(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 keep
+added
 keep
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,2 +1,1 @@
 keep
-removed
`
	got := shortstatFromDiff(diff)
	want := "2 files changed, 1 insertion(+), 1 deletion(-)"
	if got != want {
		t.Fatalf("shortstat: got %q want %q", got, want)
	}
}

func TestShortstatFromDiff_EmptyAndSingular(t *testing.T) {
	if got := shortstatFromDiff(""); got != "" {
		t.Fatalf("empty diff should be empty, got %q", got)
	}
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1 +1,2 @@
 keep
+added
`
	got := shortstatFromDiff(diff)
	want := "1 file changed, 1 insertion(+)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestShortstatFromDiff_IgnoresFileHeaderLines(t *testing.T) {
	// +++ and --- header lines must not be counted as additions/deletions.
	diff := `diff --git a/x b/x
--- a/x
+++ b/x
@@ -1 +1 @@
-old
+new
`
	got := shortstatFromDiff(diff)
	want := "1 file changed, 1 insertion(+), 1 deletion(-)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCommitTitleLeft(t *testing.T) {
	c := git.Commit{SHA: "abcdef0123456789", Subject: "fix flake"}
	got := commitTitleLeft(c)
	want := "abcdef0 · fix flake"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// Subject-less commit: just the SHA.
	if got := commitTitleLeft(git.Commit{SHA: "1234567"}); got != "1234567" {
		t.Fatalf("subject-less: got %q", got)
	}
}

func TestFormatAuthorAndTime(t *testing.T) {
	tt := time.Now().Add(-2 * time.Hour)
	got := formatAuthorAndTime("hazel", tt)
	if !strings.HasPrefix(got, "@hazel · ") {
		t.Fatalf("expected '@hazel · …', got %q", got)
	}
	if got := formatAuthorAndTime("", time.Time{}); got != "" {
		t.Fatalf("missing both should be empty, got %q", got)
	}
	if got := formatAuthorAndTime("hazel", time.Time{}); got != "@hazel" {
		t.Fatalf("missing time: got %q", got)
	}
}

// === Hunk parser ===

func TestParseHunkHeader(t *testing.T) {
	cases := []struct {
		in    string
		start int
		count int
	}{
		{"@@ -1,3 +1,4 @@", 1, 4},
		{"@@ -1,3 +1,4 @@ func foo() {", 1, 4},
		{"@@ -10 +20 @@", 20, 1},
		{"@@ -10 +20,5 @@ class Bar:", 20, 5},
		{"@@ -1,0 +1,3 @@", 1, 3},
		{"@@ -5,3 +0,0 @@", 0, 0}, // pure deletion → start=0
		{"not a header", 0, 0},
		{"@@ malformed", 0, 0},
	}
	for _, c := range cases {
		s, n := parseHunkHeader(c.in)
		if s != c.start || n != c.count {
			t.Errorf("parseHunkHeader(%q) = (%d, %d), want (%d, %d)",
				c.in, s, n, c.start, c.count)
		}
	}
}

func TestParseDiffHunks(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,4 @@ func one()
 keep
+added
 keep
 keep
@@ -10,2 +11,3 @@ func two()
 keep
+added
 keep
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -5,3 +0,0 @@ func gone()
-removed
-removed
-removed
`
	got := parseDiffHunks(diff)
	want := []diffHunk{
		{StartLine: 1, EndLine: 4},
		{StartLine: 11, EndLine: 13},
		// pure-deletion hunk dropped (count == 0)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDiffHunks mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParseDiffHunks_Empty(t *testing.T) {
	if h := parseDiffHunks(""); h != nil {
		t.Errorf("empty diff should be nil, got %+v", h)
	}
}

// === Hunk position classification ===

func TestHunkPositionForLine(t *testing.T) {
	hunks := []diffHunk{
		{StartLine: 5, EndLine: 7},
		{StartLine: 20, EndLine: 25},
		{StartLine: 40, EndLine: 41},
	}
	cases := []struct {
		line      int
		desc      string
		insideIdx int
		beforeIdx int
		afterIdx  int
	}{
		{1, "before all", -1, 0, -1},
		{4, "before all (just before)", -1, 0, -1},
		{5, "inside first start", 0, -1, -1},
		{6, "inside first middle", 0, -1, -1},
		{7, "inside first end", 0, -1, -1},
		{8, "between first and second", -1, 1, 0},
		{19, "between (just before second)", -1, 1, 0},
		{22, "inside second", 1, -1, -1},
		{30, "between second and third", -1, 2, 1},
		{40, "inside third start", 2, -1, -1},
		{50, "after all", -1, -1, 2},
	}
	for _, c := range cases {
		got := hunkPositionForLine(hunks, c.line)
		if got.total != len(hunks) {
			t.Errorf("[%s] total: got %d, want %d", c.desc, got.total, len(hunks))
		}
		if got.insideIdx != c.insideIdx || got.beforeIdx != c.beforeIdx || got.afterIdx != c.afterIdx {
			t.Errorf("[%s] line %d: got (inside=%d, before=%d, after=%d), want (inside=%d, before=%d, after=%d)",
				c.desc, c.line, got.insideIdx, got.beforeIdx, got.afterIdx,
				c.insideIdx, c.beforeIdx, c.afterIdx)
		}
	}
}

func TestHunkPositionForLine_Empty(t *testing.T) {
	got := hunkPositionForLine(nil, 5)
	if got.total != 0 || got.insideIdx != -1 || got.beforeIdx != -1 || got.afterIdx != -1 {
		t.Errorf("empty hunks: got %+v", got)
	}
}

func TestReviewStateLabel(t *testing.T) {
	cases := map[string]string{
		"APPROVED":          "approved",
		"CHANGES_REQUESTED": "changes requested",
		"COMMENTED":         "commented",
		"":                  "pending",
		"DISMISSED":         "dismissed",
	}
	for in, want := range cases {
		if got := reviewStateLabel(in); got != want {
			t.Errorf("reviewStateLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// === Title wiring per mode ===

func newTitleTestModel(t *testing.T, mock *mockGit, mode Mode) *Model {
	t.Helper()
	m := NewModel("/tmp", mock)
	m.width = 100
	m.height = 30
	m.base = "origin/main"
	m.mode = mode
	m.updateLayout()
	return m
}

func TestTitle_FilesMode_NoDiff_FallsBackToUntracked(t *testing.T) {
	// No commit history and no on-disk mtime → just "untracked" (no time component).
	mock := &mockGit{
		repoInfo:             git.RepoInfoResult{Branch: "main"},
		base:                 "origin/main",
		changedFiles:         git.ChangedFilesResult{},
		allFiles:             []string{"missing.go"},
		fileContent:          "package main\n",
		fileDiff:             "",
		lastCommitForFileErr: errors.New("no commits"),
	}
	m := newTitleTestModel(t, mock, FilesMode)
	m.allFiles = []string{"missing.go"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "missing.go" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	if got := m.mainPane.titleLeft; got != "missing.go" {
		t.Errorf("FilesMode left: got %q, want %q", got, "missing.go")
	}
	if got := m.mainPane.hunkTitleRight(); got != "untracked" {
		t.Errorf("FilesMode right (no commit, no file): got %q, want %q", got, "untracked")
	}
}

func TestTitle_FilesMode_NoDiff_TrackedShowsLastCommit(t *testing.T) {
	authoredAt := time.Now().Add(-2 * time.Hour)
	mock := &mockGit{
		repoInfo:          git.RepoInfoResult{Branch: "main"},
		base:              "origin/main",
		changedFiles:      git.ChangedFilesResult{},
		allFiles:          []string{"tracked.go"},
		fileContent:       "package main\n",
		fileDiff:          "",
		lastCommitForFile: git.Commit{SHA: "abcdef0123456789", AuthorDate: authoredAt, Subject: "x", Author: "hazel"},
	}
	m := newTitleTestModel(t, mock, FilesMode)
	m.allFiles = []string{"tracked.go"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "tracked.go" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	got := m.mainPane.hunkTitleRight()
	if !strings.HasPrefix(got, "abcdef0 · ") {
		t.Errorf("tracked file: got %q, want 'abcdef0 · <relative-time>'", got)
	}
}

func TestTitle_FilesMode_NoDiff_UntrackedShowsMtime(t *testing.T) {
	// No commit history → fall back to filesystem mtime, prefixed "untracked".
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mock := &mockGit{
		repoInfo:             git.RepoInfoResult{Branch: "main"},
		base:                 "origin/main",
		changedFiles:         git.ChangedFilesResult{},
		allFiles:             []string{"fresh.txt"},
		fileContent:          "hi\n",
		fileDiff:             "",
		lastCommitForFileErr: errors.New("untracked"),
	}
	m := NewModel(dir, mock)
	m.width = 100
	m.height = 30
	m.base = "origin/main"
	m.mode = FilesMode
	m.updateLayout()
	m.allFiles = []string{"fresh.txt"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "fresh.txt" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	got := m.mainPane.hunkTitleRight()
	if !strings.HasPrefix(got, "untracked · ") {
		t.Errorf("untracked file: got %q, want 'untracked · <relative-time>'", got)
	}
}

func TestTitle_FilesMode_BinaryUntracked(t *testing.T) {
	// Binary content + no commit history → "binary · untracked · <time>".
	dir := t.TempDir()
	path := filepath.Join(dir, "logo.png")
	binaryPayload := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02, 0x03}
	if err := os.WriteFile(path, binaryPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	mock := &mockGit{
		repoInfo:             git.RepoInfoResult{Branch: "main"},
		base:                 "origin/main",
		changedFiles:         git.ChangedFilesResult{},
		allFiles:             []string{"logo.png"},
		fileContent:          string(binaryPayload),
		fileDiff:             "",
		lastCommitForFileErr: errors.New("untracked"),
	}
	m := NewModel(dir, mock)
	m.width = 100
	m.height = 30
	m.base = "origin/main"
	m.mode = FilesMode
	m.updateLayout()
	m.allFiles = []string{"logo.png"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "logo.png" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	got := m.mainPane.hunkTitleRight()
	if !strings.HasPrefix(got, "binary · untracked · ") {
		t.Errorf("binary untracked: got %q, want 'binary · untracked · <relative-time>'", got)
	}
}

func TestTitle_FilesMode_BinaryTracked(t *testing.T) {
	// Binary content + tracked → "binary · <sha7> · <time>".
	authoredAt := time.Now().Add(-1 * time.Hour)
	binaryPayload := []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02, 0x03}
	mock := &mockGit{
		repoInfo:          git.RepoInfoResult{Branch: "main"},
		base:              "origin/main",
		changedFiles:      git.ChangedFilesResult{},
		allFiles:          []string{"logo.png"},
		fileContent:       string(binaryPayload),
		fileDiff:          "",
		lastCommitForFile: git.Commit{SHA: "deadbee0000000", AuthorDate: authoredAt, Author: "hazel", Subject: "add logo"},
	}
	m := newTitleTestModel(t, mock, FilesMode)
	m.allFiles = []string{"logo.png"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "logo.png" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	got := m.mainPane.hunkTitleRight()
	if !strings.HasPrefix(got, "binary · deadbee · ") {
		t.Errorf("binary tracked: got %q, want 'binary · deadbee · <relative-time>'", got)
	}
}

func TestTitle_CommitsMode_NewChangesShortstat(t *testing.T) {
	mock := &mockGit{
		repoInfo:     git.RepoInfoResult{Branch: "main"},
		base:         "origin/main",
		changedFiles: git.ChangedFilesResult{Uncommitted: []string{"a.go"}},
		fileDiff: `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1 +1,2 @@
 keep
+added
`,
	}
	m := newTitleTestModel(t, mock, CommitsMode)
	m.uncommittedFiles = []string{"a.go"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.label == "new changes" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	if got := m.mainPane.titleLeft; got != "new changes" {
		t.Errorf("left: got %q want %q", got, "new changes")
	}
	if got := m.mainPane.titleRight; got != "1 file changed, 1 insertion(+)" {
		t.Errorf("right: got %q", got)
	}
}

func TestTitle_CommitsMode_CommitSelected(t *testing.T) {
	authoredAt := time.Now().Add(-3 * time.Hour)
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "main"},
		base:     "origin/main",
		commits: []git.Commit{
			{SHA: "deadbee0000000000", Subject: "Add widget", Author: "hazel", AuthorDate: authoredAt},
		},
		commitPatch: "commit deadbee\n\ndiff --git a/x b/x\n",
	}
	m := newTitleTestModel(t, mock, CommitsMode)
	m.commits = mock.commits
	m.updateSidebarItems()
	// Select the only commit.
	for i, item := range m.sidebar.items {
		if strings.HasPrefix(item.label, "deadbee") {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	if got, want := m.mainPane.titleLeft, "deadbee · Add widget"; got != want {
		t.Errorf("left: got %q want %q", got, want)
	}
	if got := m.mainPane.titleRight; !strings.HasPrefix(got, "@hazel · ") {
		t.Errorf("right: got %q, want '@hazel · <time>'", got)
	}
}

// === Files mode: diff prefix (mtime / sha7 + commit time) ===

func TestTitle_FilesMode_Diff_Uncommitted_PrependsMtime(t *testing.T) {
	// Uncommitted file with a diff → prefix is the working-tree mtime.
	dir := t.TempDir()
	path := filepath.Join(dir, "edited.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate mtime so relativeTime renders "<N> ago" instead of "now".
	mtime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	diff := `diff --git a/edited.go b/edited.go
--- a/edited.go
+++ b/edited.go
@@ -1,1 +1,2 @@
 package main
+// comment
`
	mock := &mockGit{
		repoInfo:     git.RepoInfoResult{Branch: "main"},
		base:         "origin/main",
		changedFiles: git.ChangedFilesResult{Uncommitted: []string{"edited.go"}},
		fileContent:  "package main\n// comment\n",
		fileDiff:     diff,
	}
	m := NewModel(dir, mock)
	m.width = 100
	m.height = 30
	m.base = "origin/main"
	m.mode = FilesMode
	m.updateLayout()
	m.uncommittedFiles = []string{"edited.go"}
	m.allFiles = []string{"edited.go"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "edited.go" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	titleRow := strings.Split(stripANSI(m.mainPane.View(false)), "\n")[1]
	if !regexp.MustCompile(`uncommitted · \S+ ago · hunk 1/1`).MatchString(titleRow) {
		t.Errorf("uncommitted diff: title row = %q, want 'uncommitted · <time> ago · hunk 1/1'", titleRow)
	}
}

func TestTitle_FilesMode_Diff_Committed_PrependsShaAndTime(t *testing.T) {
	// Committed file with a diff → prefix is "<sha7> · <relative-time>".
	authoredAt := time.Now().Add(-2 * time.Hour)
	diff := `diff --git a/done.go b/done.go
--- a/done.go
+++ b/done.go
@@ -1,1 +1,2 @@
 package main
+// landed
`
	mock := &mockGit{
		repoInfo:          git.RepoInfoResult{Branch: "main"},
		base:              "origin/main",
		changedFiles:      git.ChangedFilesResult{Committed: []string{"done.go"}},
		fileContent:       "package main\n// landed\n",
		fileDiff:          diff,
		lastCommitForFile: git.Commit{SHA: "feedfac0123456", AuthorDate: authoredAt, Author: "hazel", Subject: "land"},
	}
	m := newTitleTestModel(t, mock, FilesMode)
	m.committedFiles = []string{"done.go"}
	m.allFiles = []string{"done.go"}
	m.updateSidebarItems()
	for i, item := range m.sidebar.items {
		if item.filePath == "done.go" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()

	titleRow := strings.Split(stripANSI(m.mainPane.View(false)), "\n")[1]
	if !regexp.MustCompile(`feedfac · \S+ ago · hunk 1/1`).MatchString(titleRow) {
		t.Errorf("committed diff: title row = %q, want 'feedfac · <time> ago · hunk 1/1'", titleRow)
	}
}

// When the prefix is set but no hunks are present, View() must NOT prepend
// the prefix (the right side falls back to noHunkRight).
func TestMainPane_DiffPrefix_OmittedWhenNoHunks(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 6)
	mp.SetPlainContent("line\n")
	mp.SetTitleWithHunks("file.go")
	mp.SetDiffPrefix("abc1234 · 5m ago")
	// No SetDiffHunks → len(diffHunks) == 0 → prefix must not appear.
	titleRow := strings.Split(stripANSI(mp.View(false)), "\n")[1]
	if strings.Contains(titleRow, "abc1234") {
		t.Errorf("no hunks: prefix should not appear: %q", titleRow)
	}
}

// === Files mode: sticky title tracks scroll ===

// hunkTitleHarness builds a minimal mainPane with a known hunk list and a
// content string long enough to cover the largest hunk's source line.
func hunkTitleHarness(hunks []diffHunk, contentLines int) *mainPane {
	mp := newMainPane()
	// Use a tall pane so scrolling is allowed; viewport.SetYOffset clamps to
	// content height otherwise.
	mp.SetSize(80, 6)
	var body strings.Builder
	for i := 0; i < contentLines; i++ {
		body.WriteString("line\n")
	}
	mp.SetPlainContent(body.String())
	mp.SetDiffHunks(hunks)
	mp.SetTitleWithHunks("file.go")
	return mp
}

func TestHunkTitleRight_NoHunks(t *testing.T) {
	mp := hunkTitleHarness(nil, 50)
	if got := mp.hunkTitleRight(); got != "no changes" {
		t.Errorf("no hunks: got %q, want 'no changes'", got)
	}
}

func TestHunkTitleRight_BeforeFirst(t *testing.T) {
	hunks := []diffHunk{{StartLine: 10, EndLine: 12}}
	mp := hunkTitleHarness(hunks, 50)
	mp.viewport.SetYOffset(0) // top of file → source line 1 (before hunk)
	if got := mp.hunkTitleRight(); got != "before hunk 1" {
		t.Errorf("before first: got %q", got)
	}
}

func TestHunkTitleRight_InsideHunk(t *testing.T) {
	hunks := []diffHunk{
		{StartLine: 5, EndLine: 9},
		{StartLine: 30, EndLine: 32},
	}
	mp := hunkTitleHarness(hunks, 60)
	// Scroll so source line ≈ 6 is at top; viewport offset == source - 1
	// (source-to-format mapping is identity when no annotations).
	mp.viewport.SetYOffset(5)
	if got := mp.hunkTitleRight(); got != "hunk 1/2" {
		t.Errorf("inside hunk 1: got %q", got)
	}
	mp.viewport.SetYOffset(30)
	if got := mp.hunkTitleRight(); got != "hunk 2/2" {
		t.Errorf("inside hunk 2: got %q", got)
	}
}

func TestHunkTitleRight_BetweenHunks(t *testing.T) {
	hunks := []diffHunk{
		{StartLine: 5, EndLine: 9},
		{StartLine: 30, EndLine: 32},
		{StartLine: 50, EndLine: 51},
	}
	mp := hunkTitleHarness(hunks, 80)
	// Scroll between hunk 1 and hunk 2 → should report (1–2).
	mp.viewport.SetYOffset(15) // source line 16
	got := mp.hunkTitleRight()
	if got != "between hunks (1–2)" {
		t.Errorf("between 1 and 2: got %q", got)
	}
	// Scroll between hunk 2 and hunk 3 → (2–3).
	mp.viewport.SetYOffset(40) // source line 41
	got = mp.hunkTitleRight()
	if got != "between hunks (2–3)" {
		t.Errorf("between 2 and 3: got %q", got)
	}
}

func TestHunkTitleRight_MultipleVisible(t *testing.T) {
	hunks := []diffHunk{
		{StartLine: 5, EndLine: 6},
		{StartLine: 10, EndLine: 11},
		{StartLine: 14, EndLine: 15},
	}
	mp := newMainPane()
	// Viewport height = pane height − 1 = 19 → visible source range [1..19]
	// when scrolled to offset 0, covering all three hunks.
	mp.SetSize(80, 20)
	var body strings.Builder
	for i := 0; i < 30; i++ {
		body.WriteString("line\n")
	}
	mp.SetPlainContent(body.String())
	mp.SetDiffHunks(hunks)
	mp.viewport.SetYOffset(0)

	got := mp.hunkTitleRight()
	if got != "viewing hunks 1 through 3" {
		t.Errorf("multiple visible: got %q", got)
	}
}

func TestHunkTitleRight_AfterLast(t *testing.T) {
	hunks := []diffHunk{
		{StartLine: 5, EndLine: 6},
	}
	mp := hunkTitleHarness(hunks, 50)
	mp.viewport.SetYOffset(40) // far past hunk 1
	got := mp.hunkTitleRight()
	if got != "after hunk 1" {
		t.Errorf("after last: got %q", got)
	}
}

// === Files mode: progress percent ===

// progressHarness builds a mainPane with N source lines (no trailing newline,
// so total source lines == N exactly). Viewport height is height-1.
func progressHarness(sourceLines, paneHeight int) *mainPane {
	mp := newMainPane()
	mp.SetSize(80, paneHeight)
	if sourceLines <= 0 {
		mp.SetPlainContent("")
		return mp
	}
	content := strings.Repeat("line\n", sourceLines-1) + "line"
	mp.SetPlainContent(content)
	return mp
}

func TestProgressPercent_AtTop_SmallFraction(t *testing.T) {
	// 50 source lines, viewport height = 5 → bottom = line 5 → 10%.
	mp := progressHarness(50, 6)
	mp.viewport.SetYOffset(0)
	if got := mp.progressPercent(); got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

func TestProgressPercent_AtBottom_Is100(t *testing.T) {
	mp := progressHarness(50, 6)
	mp.viewport.GotoBottom()
	if got := mp.progressPercent(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}

func TestProgressPercent_ContentFitsInViewport(t *testing.T) {
	// 3 source lines in a 10-line viewport → bottom is past EOF → 100%.
	mp := progressHarness(3, 11)
	if got := mp.progressPercent(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}

func TestProgressPercent_EmptyContent(t *testing.T) {
	mp := progressHarness(0, 6)
	if got := mp.progressPercent(); got != 100 {
		t.Errorf("expected 100, got %d", got)
	}
}

// Dynamic title in View() output should include the progress suffix alongside
// the hunk position, separated by " · ".
func TestMainPane_DynamicTitle_IncludesProgressSuffix(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 6)                                // viewport height = 5
	content := strings.Repeat("line\n", 79) + "line" // 80 source lines
	mp.SetPlainContent(content)
	mp.SetDiffHunks([]diffHunk{
		{StartLine: 30, EndLine: 32},
	})
	mp.SetTitleWithHunks("file.go")

	suffixRE := regexp.MustCompile(` · \d+%`)

	mp.viewport.SetYOffset(0)
	out := stripANSI(mp.View(false))
	titleRow := strings.Split(out, "\n")[1]
	if !strings.Contains(titleRow, "before hunk 1") {
		t.Errorf("title row missing hunk text: %q", titleRow)
	}
	if !suffixRE.MatchString(titleRow) {
		t.Errorf("title row missing progress suffix: %q", titleRow)
	}

	mp.viewport.GotoBottom()
	out = stripANSI(mp.View(false))
	titleRow = strings.Split(out, "\n")[1]
	if !strings.Contains(titleRow, "100%") {
		t.Errorf("at bottom, title row should contain 100%%: %q", titleRow)
	}
}

// Static-title mode (SetTitle, not SetTitleWithHunks) should NOT have the
// progress suffix appended — that's reserved for dynamic mode.
func TestMainPane_StaticTitle_NoProgressSuffix(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(80, 6)
	content := strings.Repeat("line\n", 79) + "line"
	mp.SetPlainContent(content)
	mp.SetTitle("file.go", "@hazel · 5m ago")

	out := stripANSI(mp.View(false))
	titleRow := strings.Split(out, "\n")[1]
	if regexp.MustCompile(`\d+%`).MatchString(titleRow) {
		t.Errorf("static-title row should not include progress percent: %q", titleRow)
	}
}

// Dynamic title is reflected in mainPane.View output and updates with scroll.
func TestMainPane_DynamicTitle_RendersFromScroll(t *testing.T) {
	mp := newMainPane()
	mp.SetSize(60, 6) // small viewport so only a handful of source lines are visible
	var body strings.Builder
	for i := 0; i < 80; i++ {
		body.WriteString("line\n")
	}
	mp.SetPlainContent(body.String())
	mp.SetDiffHunks([]diffHunk{
		{StartLine: 30, EndLine: 32},
		{StartLine: 60, EndLine: 62},
	})
	mp.SetTitleWithHunks("file.go")

	// At top of file, no hunks are visible — should report "before hunk 1".
	mp.viewport.SetYOffset(0)
	out := stripANSI(mp.View(false))
	titleRow := strings.Split(out, "\n")[1]
	if !strings.Contains(titleRow, "before hunk 1") {
		t.Errorf("before-hunk-1 frame: title row = %q", titleRow)
	}

	// Scroll so hunk 1 is in view (top line near 30).
	mp.viewport.SetYOffset(29)
	out = stripANSI(mp.View(false))
	titleRow = strings.Split(out, "\n")[1]
	if !strings.Contains(titleRow, "hunk 1/2") {
		t.Errorf("inside-hunk-1 frame: title row = %q", titleRow)
	}
}

func TestTitle_PRMode_DescriptionAndComment(t *testing.T) {
	created := time.Now().Add(-10 * time.Minute)
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "main"},
		base:     "origin/main",
		prInfo:   git.PRInfoResult{Number: 42, Title: "Add auth"},
		prComments: []git.PRComment{
			{Author: "alice", Body: "lgtm", CreatedAt: created},
		},
	}
	m := newTitleTestModel(t, mock, PRMode)
	m.prInfo = mock.prInfo
	m.prComments = mock.prComments
	m.updateSidebarItems()

	// Description (sidebar default).
	for i, item := range m.sidebar.items {
		if item.label == "Description" {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()
	if got, want := m.mainPane.titleLeft, "Description"; got != want {
		t.Errorf("Description left: got %q want %q", got, want)
	}
	if got := m.mainPane.titleRight; got != "" {
		t.Errorf("Description right: got %q want empty", got)
	}

	// Now the comment.
	for i, item := range m.sidebar.items {
		if strings.Contains(item.label, "@alice") {
			m.sidebar.SelectIndex(i)
			break
		}
	}
	m.updateMainContent()
	if got, want := m.mainPane.titleLeft, "comment #1"; got != want {
		t.Errorf("comment left: got %q want %q", got, want)
	}
	if got := m.mainPane.titleRight; !strings.HasPrefix(got, "@alice · ") {
		t.Errorf("comment right: got %q, want '@alice · <time>'", got)
	}
}
