package git_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// Reading the repo must never write to it. `git status` (and `git diff`)
// refresh `.git/index` when they find stat-dirty entries, and prwatch watches
// `.git` — so a load's own status call fired the watcher, which triggered
// another load, which fired the watcher again. With the single-flight gate in
// place that loop is capped rather than cured: each cycle's trailing rerun
// re-arms the next. `--no-optional-locks` cuts it at the source.
//
// Asserted at the command-construction seam so it covers every read-only call
// in the load path, not just the one that happens to be stat-dirty today.
func TestGitCommands_NeverTakeOptionalLocks(t *testing.T) {
	dir := setupTestRepo(t)

	var invocations [][]string
	factory := func(name string, args ...string) command.Command {
		if name == "gh" || name == "rwx" {
			return command.StubCommand("", errStubbed)
		}
		if name == "git" {
			invocations = append(invocations, args)
		}
		return command.DefaultFactory(name, args...)
	}
	g := git.NewWithFactory(dir, factory)

	// Everything the load path touches, plus the pseudo-diff producers.
	info, err := g.RepoInfo()
	if err != nil {
		t.Fatal(err)
	}
	base, err := g.DetectBaseLocal()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = g.ChangedFiles(base)
	_, _ = g.Commits(base, 0, 10)
	_, _ = g.CommitCountRange(base)
	_, _ = g.BaseCommits(base, 10)
	_, _ = g.BehindCount("origin/main")
	_, _ = g.AllFiles()
	_, _ = g.IgnoredEntries()
	_, _ = g.UntrackedFiles()
	_, _ = g.StagedDiff()
	_, _ = g.UnstagedDiff()
	_, _ = g.UntrackedDiff()
	_, _ = g.NewChangesDiff()
	_, _ = g.FileDiffCommitted(base, "README.md")
	_, _ = g.FileDiffUncommitted(base, "README.md")
	_, _ = g.FileContent("README.md")
	_, _ = g.LastCommitForFile("README.md")
	_, _ = g.CommitPatch(info.HeadSHA)
	_, _ = g.Parent(info.HeadSHA)

	if len(invocations) == 0 {
		t.Fatal("no git invocations recorded — the test is vacuous")
	}
	for _, args := range invocations {
		if len(args) == 0 || args[0] != "--no-optional-locks" {
			t.Errorf("git invocation without --no-optional-locks: %v", args)
		}
	}
}

// The end-to-end property that actually kills the feedback loop: repeatedly
// reading an unchanged worktree must stop producing `.git` writes. It cannot be
// stated as "a load never writes the index", because it can:
// `--no-optional-locks` suppresses the refresh in `git status` but NOT in
// `git diff` (verified against git 2.53 — `GIT_OPTIONAL_LOCKS=0` behaves the
// same, and only the `diff-files` plumbing avoids the write, at the cost of
// reporting stat-dirty-but-identical files as modified, which would put
// phantom entries in the sidebar).
//
// What makes that survivable is that the diff refresh is self-limiting: it
// writes only when the worktree changed since the last load, and the write
// leaves the index clean, so the next load writes nothing. So a real edit costs
// one extra watcher event, not an endless stream — and the single-flight gate
// collapses that extra event into at most one trailing load. This test pins the
// fixed point that argument depends on.
func TestChangedFiles_ReadLoopQuiesces(t *testing.T) {
	// Pin the git environment before creating the repo. Index-refresh behaviour
	// is configurable — core.fsmonitor, core.untrackedCache and feature.manyFiles
	// all change when and whether git writes the index — so inheriting the
	// developer's global config makes this test's result a property of their
	// machine. Both the control commands and prwatch's own git calls are child
	// processes of this one and inherit the env, so this covers both without
	// needing per-command env plumbing.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := setupTestRepo(t)
	index := filepath.Join(dir, ".git", "index")

	// Rewrite a tracked file with identical content and a fresh mtime. The
	// entry is now stat-dirty: git must open it to discover the content is
	// unchanged, and normally records that discovery back into the index.
	dirtyIndex := func() {
		body, err := os.ReadFile(filepath.Join(dir, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		// A visibly newer mtime, so a coarse-grained filesystem timestamp
		// can't make the entry look clean.
		if err := os.WriteFile(filepath.Join(dir, "README.md"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		future := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(filepath.Join(dir, "README.md"), future, future); err != nil {
			t.Fatal(err)
		}
	}
	stamp := func() time.Time {
		st, err := os.Stat(index)
		if err != nil {
			t.Fatal(err)
		}
		return st.ModTime()
	}

	// Control: a plain `git status` over a stat-dirty index rewrites it. This is
	// the write prwatch's own flag removes, and confirming it happens here is
	// what keeps the rest of the test from passing vacuously on a repo git
	// would never have refreshed.
	// With the environment pinned this is a hard requirement, not a skip: if an
	// un-flagged status stops rewriting a stat-dirty index, either dirtyIndex no
	// longer makes it stat-dirty or git changed, and in both cases the
	// quiescence assertion below has become vacuous and needs a human.
	dirtyIndex()
	before := stamp()
	runGit(t, dir, "status", "--porcelain=v2", "-z", "-M", "--untracked-files=all")
	if stamp().Equal(before) {
		t.Fatalf("control: an un-flagged `git status` left .git/index untouched (%v), so this "+
			"test can no longer tell a suppressed refresh from no refresh at all", before)
	}

	g := noGH(dir)
	base, err := g.DetectBaseLocal()
	if err != nil {
		t.Fatal(err)
	}

	// First load after a worktree change is allowed to refresh the index.
	dirtyIndex()
	if _, err := g.ChangedFiles(base); err != nil {
		t.Fatal(err)
	}

	// Every load after that, with the worktree untouched, must write nothing —
	// otherwise each load feeds the `.git` watcher another event and the
	// refresh loop never settles.
	settled := stamp()
	for i := range 3 {
		if _, err := g.ChangedFiles(base); err != nil {
			t.Fatal(err)
		}
		if got := stamp(); !got.Equal(settled) {
			t.Fatalf("load %d rewrote .git/index over an unchanged worktree: %v → %v; "+
				"the read loop does not quiesce and will keep re-triggering the watcher",
				i+2, settled, got)
		}
	}
}

var errStubbed = errStubbedType("stubbed out in tests")

type errStubbedType string

func (e errStubbedType) Error() string { return string(e) }
