package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// setupPseudoEntryRepo builds a real repo carrying one of each kind of pending
// change, so the commits-mode pseudo-entries have genuinely different content
// to render:
//
//	staged.go    — staged (index vs HEAD)
//	feature.go   — unstaged (working tree vs index)
//	untracked.go — untracked
func setupPseudoEntryRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := command.DefaultFactory("git", args...)
		cmd.SetDir(dir)
		var stderr bytes.Buffer
		cmd.SetStderr(&stderr)
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v: %s %v", args, stderr.String(), err)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--initial-branch=main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	write("feature.go", "package feature\n")
	write("staged.go", "package staged\n")
	run("add", ".")
	run("commit", "-m", "initial")

	write("staged.go", "package staged\n\nvar onlyStaged = 1\n")
	run("add", "staged.go")
	write("feature.go", "package feature\n\nvar onlyUnstaged = 2\n")
	write("untracked.go", "package untracked\n\nvar onlyUntracked = 3\n")

	return dir
}

// pseudoEntryModel wires a Model to a real git repo and puts commits mode's
// two pseudo-entries in the sidebar, without going near the network (no gh).
func pseudoEntryModel(t *testing.T, dir string) *Model {
	t.Helper()
	m := NewModel(dir, gitpkg.New(dir))
	m.width, m.height = 100, 40
	m.updateLayout()
	m.mode = CommitsMode
	// updateMainContent bails out while the scope has no base yet; give it the
	// one a real load would have produced.
	m.scope.SyncFromLoad("HEAD", "", 0, 0, "", -1)
	m.sidebar.SetItems(buildCommitsSidebar(
		nil, nil,
		[]string{"feature.go", "untracked.go"}, // uncommitted (unstaged + untracked)
		[]string{"staged.go"},                  // staged
		0, 0, 0,
	))
	return m
}

// selectPseudoEntry moves the sidebar cursor onto the named pseudo-entry and
// renders the main pane, returning its body and title-bar halves.
func selectPseudoEntry(t *testing.T, m *Model, label string) (body, titleLeft, titleRight string) {
	t.Helper()
	found := false
	for i := range m.sidebar.items {
		if m.sidebar.items[i].label == label {
			m.sidebar.SelectIndex(i)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pseudo-entry %q not in sidebar", label)
	}
	if got := m.sidebar.SelectedItem(); got != label {
		t.Fatalf("selected %q, want %q", got, label)
	}
	m.updateMainContent()
	return m.mainPane.content, m.mainPane.titleLeft, m.mainPane.titleRight
}

// The bug: both pseudo-entries rendered one `git diff HEAD`, so their bodies
// and shortstats were byte-identical.
func TestCommitsPseudoEntries_BodiesAreDistinct(t *testing.T) {
	dir := setupPseudoEntryRepo(t)
	m := pseudoEntryModel(t, dir)

	newBody, _, newRight := selectPseudoEntry(t, m, "new changes")
	stagedBody, _, stagedRight := selectPseudoEntry(t, m, "staged changes")

	if newBody == stagedBody {
		t.Errorf("new changes and staged changes render identical bodies:\n%s", newBody)
	}
	if newRight == stagedRight {
		t.Errorf("new changes and staged changes share one shortstat: %q", newRight)
	}
}

// Each body must match its own git source and exclude the others'.
func TestCommitsPseudoEntries_BodyMatchesGitSource(t *testing.T) {
	dir := setupPseudoEntryRepo(t)
	m := pseudoEntryModel(t, dir)

	cases := []struct {
		label   string
		want    []string
		notWant []string
	}{
		{
			label:   "staged changes",
			want:    []string{"+var onlyStaged = 1"},
			notWant: []string{"onlyUnstaged", "onlyUntracked"},
		},
		{
			// "New Changes" covers unstaged *and* untracked (PROMPT.md groups
			// them under one entry), so both belong in this body.
			label:   "new changes",
			want:    []string{"+var onlyUnstaged = 2", "+var onlyUntracked = 3", "+++ b/untracked.go"},
			notWant: []string{"onlyStaged"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			body, left, right := selectPseudoEntry(t, m, tc.label)
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("%s body missing %q, got:\n%s", tc.label, w, body)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(body, nw) {
					t.Errorf("%s body must not contain %q, got:\n%s", tc.label, nw, body)
				}
			}
			if left != tc.label {
				t.Errorf("title left = %q, want %q", left, tc.label)
			}
			if !strings.Contains(right, "changed") {
				t.Errorf("title right should carry a shortstat, got %q", right)
			}
		})
	}
}

// The shortstat is per-entry: it must describe that entry's own diff.
func TestCommitsPseudoEntries_ShortstatPerEntry(t *testing.T) {
	dir := setupPseudoEntryRepo(t)
	m := pseudoEntryModel(t, dir)

	_, _, stagedRight := selectPseudoEntry(t, m, "staged changes")
	if stagedRight != "1 file changed, 2 insertions(+)" {
		t.Errorf("staged shortstat = %q, want %q", stagedRight, "1 file changed, 2 insertions(+)")
	}

	// new changes: feature.go contributes 2 insertions (its first line is
	// context); untracked.go is a new file, so all 3 of its lines are
	// insertions. `git diff --shortstat` + `git diff --no-index /dev/null
	// untracked.go` agree on 5.
	_, _, newRight := selectPseudoEntry(t, m, "new changes")
	if newRight != "2 files changed, 5 insertions(+)" {
		t.Errorf("new-changes shortstat = %q, want %q", newRight, "2 files changed, 5 insertions(+)")
	}
}

// A pseudo-entry with nothing to show gets a quiet line, not a stale body.
func TestCommitsPseudoEntries_EmptyState(t *testing.T) {
	dir := setupPseudoEntryRepo(t)
	// Unstage everything so "staged changes" has no diff, while the sidebar
	// entry is still present.
	cmd := command.DefaultFactory("git", "reset", "-q")
	cmd.SetDir(dir)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	m := pseudoEntryModel(t, dir)
	body, _, right := selectPseudoEntry(t, m, "staged changes")
	if body != "no staged changes" {
		t.Errorf("empty staged body = %q, want %q", body, "no staged changes")
	}
	if right != "" {
		t.Errorf("empty staged shortstat = %q, want empty", right)
	}
}

// ---------------------------------------------------------------------------
// Properties of the extracted pure builder
// ---------------------------------------------------------------------------

// genPseudoDiff draws a plausible unified diff over 1..3 files, sometimes
// entirely new-file (untracked) hunks.
func genPseudoDiff(t *rapid.T) string {
	n := rapid.IntRange(1, 3).Draw(t, "files")
	var b strings.Builder
	for i := 0; i < n; i++ {
		name := rapid.SampledFrom([]string{"a.go", "b/c.txt", "d e.md", "ünï.go"}).Draw(t, fmt.Sprintf("name%d", i))
		newFile := rapid.Bool().Draw(t, fmt.Sprintf("new%d", i))
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n", name, name)
		if newFile {
			b.WriteString("new file mode 100644\n--- /dev/null\n")
		} else {
			fmt.Fprintf(&b, "--- a/%s\n", name)
		}
		fmt.Fprintf(&b, "+++ b/%s\n", name)
		adds := rapid.IntRange(1, 4).Draw(t, fmt.Sprintf("adds%d", i))
		fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", adds)
		for j := 0; j < adds; j++ {
			fmt.Fprintf(&b, "+line %d\n", j)
		}
	}
	return b.String()
}

func TestProperty_PseudoEntryContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		label := rapid.SampledFrom([]string{pseudoNewChangesLabel, pseudoStagedLabel}).Draw(t, "label")
		empty := rapid.Bool().Draw(t, "empty")

		diff := ""
		if !empty {
			diff = genPseudoDiff(t)
		}
		got := buildPseudoEntryContent(label, diff)

		// Every state has something to show — the pane is never left blank.
		if got.body == "" {
			t.Fatalf("empty body for label=%q diff=%q", label, diff)
		}
		// asDiff exactly tracks "there is a diff to render".
		if got.asDiff != (diff != "") {
			t.Fatalf("asDiff=%v for diff of %d bytes", got.asDiff, len(diff))
		}
		// A shortstat is only meaningful for a real diff.
		if !got.asDiff && got.titleRight != "" {
			t.Fatalf("non-diff body carries shortstat %q", got.titleRight)
		}
		if got.asDiff {
			if got.body != diff {
				t.Fatalf("diff body was rewritten:\nwant %q\ngot  %q", diff, got.body)
			}
			if got.titleRight == "" {
				t.Fatalf("diff body has no shortstat:\n%s", diff)
			}
			// Idempotent: feeding the rendered body back yields the same result.
			if again := buildPseudoEntryContent(label, got.body); again != got {
				t.Fatalf("not idempotent:\nfirst  %+v\nsecond %+v", got, again)
			}
		} else {
			// The empty state names its own entry, never the other one.
			other := pseudoStagedLabel
			if label == pseudoStagedLabel {
				other = pseudoNewChangesLabel
			}
			if got.body != emptyPseudoEntryText(label) {
				t.Fatalf("empty body = %q, want %q", got.body, emptyPseudoEntryText(label))
			}
			if got.body == emptyPseudoEntryText(other) {
				t.Fatalf("%q showed %q's empty text", label, other)
			}
		}
	})
}

// The two entries must never produce the same content from different diffs —
// the conflation this fix removed.
func TestProperty_PseudoEntriesNeverShareContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		stagedDiff := genPseudoDiff(t)
		newDiff := genPseudoDiff(t)
		if stagedDiff == newDiff {
			t.Skip("generated identical diffs")
		}
		staged := buildPseudoEntryContent(pseudoStagedLabel, stagedDiff)
		newChanges := buildPseudoEntryContent(pseudoNewChangesLabel, newDiff)
		if staged.body == newChanges.body {
			t.Fatalf("distinct diffs rendered one body:\n%s", staged.body)
		}
	})
}

// ---------------------------------------------------------------------------
// Caching: one fetch per git-load cycle, not one per render
// ---------------------------------------------------------------------------

// countingGit wraps mockGit and counts pseudo-diff fetches.
type countingGit struct {
	*mockGit
	stagedCalls     int
	newChangesCalls int
}

func (c *countingGit) StagedDiff() (string, error) {
	c.stagedCalls++
	return c.mockGit.StagedDiff()
}

func (c *countingGit) NewChangesDiff() (string, error) {
	c.newChangesCalls++
	return c.mockGit.NewChangesDiff()
}

func countingPseudoModel(t *testing.T) (*Model, *countingGit) {
	t.Helper()
	mg := &mockGit{
		repoInfo:       gitpkg.RepoInfoResult{Branch: "main"},
		base:           "origin/main",
		changedFiles:   gitpkg.ChangedFilesResult{Uncommitted: []string{"a.go"}, Staged: []string{"b.go"}},
		newChangesDiff: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n keep\n+added\n",
		stagedDiff:     "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1,2 @@\n keep\n+staged\n",
	}
	cg := &countingGit{mockGit: mg}
	m := NewModel("/tmp", cg)
	m.width, m.height = 100, 40
	m.updateLayout()
	m.mode = CommitsMode
	m.scope.SyncFromLoad("HEAD", "", 0, 0, "", -1)
	m.sidebar.SetItems(buildCommitsSidebar(
		nil, nil, []string{"a.go"}, []string{"b.go"}, 0, 0, 0,
	))
	return m, cg
}

// The pseudo-diff fetch used to run on every updateMainContent — every sidebar
// move onto the entry and every message that refreshes the pane. For "new
// changes" that is `git diff` + `ls-files` + one subprocess per untracked file.
func TestCommitsPseudoEntries_FetchedOncePerGitLoad(t *testing.T) {
	m, cg := countingPseudoModel(t)

	selectPseudoEntry(t, m, pseudoNewChangesLabel)
	if cg.newChangesCalls != 1 {
		t.Fatalf("first render: newChangesCalls = %d, want 1", cg.newChangesCalls)
	}

	// Repeated renders of the same entry — a PR refresh landing, an RWX log
	// arriving, the user moving away and back — must not re-spawn git.
	for i := 0; i < 5; i++ {
		m.updateMainContent()
	}
	selectPseudoEntry(t, m, pseudoStagedLabel)
	selectPseudoEntry(t, m, pseudoNewChangesLabel)

	if cg.newChangesCalls != 1 {
		t.Errorf("steady-state selection re-fetched: newChangesCalls = %d, want 1", cg.newChangesCalls)
	}
	if cg.stagedCalls != 1 {
		t.Errorf("steady-state selection re-fetched: stagedCalls = %d, want 1", cg.stagedCalls)
	}
}

// A git load is what refreshes the sidebar counts, so it must also refresh the
// bodies behind them.
func TestCommitsPseudoEntries_RefetchedAfterGitLoad(t *testing.T) {
	m, cg := countingPseudoModel(t)

	selectPseudoEntry(t, m, pseudoNewChangesLabel)
	if cg.newChangesCalls != 1 {
		t.Fatalf("setup: newChangesCalls = %d, want 1", cg.newChangesCalls)
	}

	m.pseudoDiffs.Invalidate()
	selectPseudoEntry(t, m, pseudoNewChangesLabel)
	if cg.newChangesCalls != 2 {
		t.Errorf("after invalidation: newChangesCalls = %d, want 2", cg.newChangesCalls)
	}
}

// The invalidation is wired to the message, not just available as a method.
func TestCommitsPseudoEntries_GitDataMsgInvalidates(t *testing.T) {
	m, cg := countingPseudoModel(t)

	selectPseudoEntry(t, m, pseudoNewChangesLabel)
	before := cg.newChangesCalls

	// naturalOldBase must be set: SyncFromLoad would otherwise leave the scope
	// with no base, and updateMainContent bails before rendering anything.
	m.Update(gitDataMsg{
		repoInfo:       gitpkg.RepoInfoResult{Branch: "main"},
		localOnly:      true,
		naturalOldBase: "HEAD",
	})
	m.sidebar.SetItems(buildCommitsSidebar(
		nil, nil, []string{"a.go"}, []string{"b.go"}, 0, 0, 0,
	))
	selectPseudoEntry(t, m, pseudoNewChangesLabel)

	if cg.newChangesCalls != before+1 {
		t.Errorf("gitDataMsg did not invalidate the cache: newChangesCalls = %d, want %d",
			cg.newChangesCalls, before+1)
	}
}

// A failing fetch is cached too — a persistent git failure must not re-spawn a
// subprocess on every render.
func TestCommitsPseudoEntries_ErrorIsCachedAndShown(t *testing.T) {
	m, cg := countingPseudoModel(t)
	cg.mockGit.newChangesDiffErr = errors.New("git exploded")

	body, _, right := selectPseudoEntry(t, m, pseudoNewChangesLabel)
	if !strings.Contains(body, "git exploded") {
		t.Errorf("fetch error should be shown, got %q", body)
	}
	if right != "" {
		t.Errorf("error state should have no shortstat, got %q", right)
	}

	for i := 0; i < 3; i++ {
		m.updateMainContent()
	}
	if cg.newChangesCalls != 1 {
		t.Errorf("failing fetch re-spawned: newChangesCalls = %d, want 1", cg.newChangesCalls)
	}
}

// pseudoDiffCache is an extracted state machine, so it gets unit-level
// properties of its own rather than relying on the end-to-end tests above.
func TestProperty_PseudoDiffCache(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var c pseudoDiffCache
		// Distinct per-label payloads, so a cross-label leak is visible.
		payload := map[string]string{
			pseudoStagedLabel:     "diff --git a/s b/s\n@@ -0,0 +1 @@\n+staged\n",
			pseudoNewChangesLabel: "diff --git a/n b/n\n@@ -0,0 +1 @@\n+new\n",
		}
		calls := map[string]int{}
		get := func(label string) pseudoEntryContent {
			got, err := c.Get(label, func() (string, error) {
				calls[label]++
				return payload[label], nil
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			return got
		}

		// A run of operations: mostly reads, occasionally an invalidation.
		steps := rapid.IntRange(1, 12).Draw(t, "steps")
		cycles := 1
		for i := 0; i < steps; i++ {
			if rapid.Float64Range(0, 1).Draw(t, fmt.Sprintf("inv%d", i)) < 0.25 {
				c.Invalidate()
				cycles++
				continue
			}
			label := rapid.SampledFrom([]string{pseudoStagedLabel, pseudoNewChangesLabel}).Draw(t, fmt.Sprintf("label%d", i))
			got := get(label)

			// The body is always that label's own diff — never the other's.
			if got.body != payload[label] {
				t.Fatalf("label %q returned %q", label, got.body)
			}
			// Idempotent within a cycle: a second Get is free and equal.
			before := calls[label]
			if again := get(label); again != got {
				t.Fatalf("Get not idempotent for %q:\n%+v\n%+v", label, got, again)
			}
			if calls[label] != before {
				t.Fatalf("repeat Get re-fetched %q", label)
			}
		}

		// Fetches per label never exceed the number of cycles it was read in —
		// i.e. no cycle ever fetches the same label twice.
		for label, n := range calls {
			if n > cycles {
				t.Fatalf("label %q fetched %d times across %d cycles", label, n, cycles)
			}
		}
	})
}
