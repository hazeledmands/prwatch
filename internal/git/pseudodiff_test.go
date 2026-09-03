package git_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// setupPseudoRepo builds a repo with one of each kind of pending change:
//
//	staged.go    — modified and staged (index vs HEAD)
//	feature.go   — modified in the working tree only (working tree vs index)
//	untracked.go — untracked text file
//
// The three pseudo-entry diffs must each see only their own slice of that
// state; the bug this file guards against rendered one `git diff HEAD` for
// all of them.
func setupPseudoRepo(t *testing.T) (string, *git.Git) {
	t.Helper()
	dir := setupTestRepo(t)

	writeFile(t, dir, "staged.go", "package staged\n")
	runGit(t, dir, "add", "staged.go")
	runGit(t, dir, "commit", "-m", "add staged.go")

	// staged: index differs from HEAD
	writeFile(t, dir, "staged.go", "package staged\n\nvar onlyStaged = 1\n")
	runGit(t, dir, "add", "staged.go")

	// unstaged: working tree differs from index
	writeFile(t, dir, "feature.go", "package feature\n\nvar onlyUnstaged = 2\n")

	// untracked
	writeFile(t, dir, "untracked.go", "package untracked\n\nvar onlyUntracked = 3\n")

	return dir, noGH(dir)
}

func TestStagedDiff(t *testing.T) {
	_, g := setupPseudoRepo(t)

	diff, err := g.StagedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+var onlyStaged = 1") {
		t.Errorf("StagedDiff should contain the staged change, got:\n%s", diff)
	}
	if strings.Contains(diff, "onlyUnstaged") {
		t.Errorf("StagedDiff must not contain unstaged changes, got:\n%s", diff)
	}
	if strings.Contains(diff, "onlyUntracked") {
		t.Errorf("StagedDiff must not contain untracked files, got:\n%s", diff)
	}
}

func TestUnstagedDiff(t *testing.T) {
	_, g := setupPseudoRepo(t)

	diff, err := g.UnstagedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+var onlyUnstaged = 2") {
		t.Errorf("UnstagedDiff should contain the working-tree change, got:\n%s", diff)
	}
	if strings.Contains(diff, "onlyStaged") {
		t.Errorf("UnstagedDiff must not contain staged changes, got:\n%s", diff)
	}
	if strings.Contains(diff, "onlyUntracked") {
		t.Errorf("UnstagedDiff must not contain untracked files, got:\n%s", diff)
	}
}

func TestUntrackedFiles(t *testing.T) {
	dir, g := setupPseudoRepo(t)

	// A gitignored file must not be reported.
	writeFile(t, dir, ".gitignore", "ignored.txt\n")
	writeFile(t, dir, "ignored.txt", "nope\n")

	// A path git would quote under core.quotePath=true (which setupTestRepo
	// pins on) — this is the -z discipline check.
	writeFile(t, dir, "ünïcode.go", "package u\n")

	files, err := g.UntrackedFiles()
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f] = true
	}
	if !got["untracked.go"] {
		t.Errorf("UntrackedFiles missing untracked.go, got %v", files)
	}
	if !got["ünïcode.go"] {
		t.Errorf("UntrackedFiles should decode NUL-delimited non-ASCII paths, got %v", files)
	}
	if got["ignored.txt"] {
		t.Errorf("UntrackedFiles must exclude ignored files, got %v", files)
	}
	if got["feature.go"] || got["staged.go"] {
		t.Errorf("UntrackedFiles must exclude tracked files, got %v", files)
	}
}

func TestUntrackedDiff_RendersAsNewFileDiff(t *testing.T) {
	_, g := setupPseudoRepo(t)

	diff, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"diff --git a/untracked.go b/untracked.go",
		"new file mode",
		"--- /dev/null",
		"+++ b/untracked.go",
		"+var onlyUntracked = 3",
	} {
		if !strings.Contains(diff, want) {
			t.Errorf("UntrackedDiff missing %q, got:\n%s", want, diff)
		}
	}
	if strings.Contains(diff, "onlyStaged") || strings.Contains(diff, "onlyUnstaged") {
		t.Errorf("UntrackedDiff must contain only untracked files, got:\n%s", diff)
	}
}

// Every body line of an untracked text file's diff is an addition — there is
// no old side to remove from.
func TestUntrackedDiff_AllBodyLinesAreAdditions(t *testing.T) {
	dir, g := setupPseudoRepo(t)
	writeFile(t, dir, "second.txt", "alpha\nbeta\ngamma\n")

	diff, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			continue
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "\\") {
			t.Errorf("untracked hunk body line is not an addition: %q\nfull diff:\n%s", line, diff)
		}
	}
}

func TestUntrackedDiff_BinaryFileShowsNoContent(t *testing.T) {
	dir, g := setupPseudoRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 'x', 0x00}, 0644); err != nil {
		t.Fatal(err)
	}

	diff, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "blob.bin") {
		t.Errorf("UntrackedDiff should mention the binary file, got:\n%s", diff)
	}
	if strings.Contains(diff, "\x00") {
		t.Errorf("UntrackedDiff must never emit binary content, got:\n%q", diff)
	}
	if !strings.Contains(diff, "Binary files") {
		t.Errorf("UntrackedDiff should use git's binary placeholder, got:\n%s", diff)
	}
}

func TestUntrackedDiff_NoUntrackedFiles(t *testing.T) {
	dir := setupTestRepo(t)
	g := noGH(dir)

	diff, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("UntrackedDiff with no untracked files = %q, want empty", diff)
	}
}

// NewChangesDiff is what the "new changes" pseudo-entry renders: the
// working-tree diff against the index plus each untracked file as a new-file
// diff. Staged-only content must not leak in.
func TestNewChangesDiff(t *testing.T) {
	_, g := setupPseudoRepo(t)

	diff, err := g.NewChangesDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+var onlyUnstaged = 2") {
		t.Errorf("NewChangesDiff should contain unstaged changes, got:\n%s", diff)
	}
	if !strings.Contains(diff, "+var onlyUntracked = 3") {
		t.Errorf("NewChangesDiff should contain untracked file contents, got:\n%s", diff)
	}
	if strings.Contains(diff, "onlyStaged") {
		t.Errorf("NewChangesDiff must not contain staged changes, got:\n%s", diff)
	}
}

// The three pseudo-entry sources must be pairwise distinct whenever the repo
// has all three kinds of pending change. This is the regression guard for the
// original bug, where all three rendered one `git diff HEAD`.
func TestPseudoEntryDiffsAreDistinct(t *testing.T) {
	_, g := setupPseudoRepo(t)

	staged, err := g.StagedDiff()
	if err != nil {
		t.Fatal(err)
	}
	newChanges, err := g.NewChangesDiff()
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		nameA, nameB string
		a, b         string
	}{
		{"staged", "new changes", staged, newChanges},
		{"staged", "untracked", staged, untracked},
		{"new changes", "untracked", newChanges, untracked},
	}
	for _, tc := range cases {
		if tc.a == "" || tc.b == "" {
			t.Errorf("%s / %s: one side empty (a=%d b=%d bytes)", tc.nameA, tc.nameB, len(tc.a), len(tc.b))
			continue
		}
		if tc.a == tc.b {
			t.Errorf("%s and %s render identical diffs:\n%s", tc.nameA, tc.nameB, tc.a)
		}
	}
}

// ---------------------------------------------------------------------------
// Failure paths: a file that can't be read, and a listing that fails
// ---------------------------------------------------------------------------

// interceptGit returns a factory that delegates to real git except for the
// invocations matched by pick, which are answered from the stub. args excludes
// the program name.
func interceptGit(pick func(args []string) (stdout string, err error, handled bool)) command.Factory {
	return func(name string, args ...string) command.Command {
		if name == "git" {
			if stdout, err, handled := pick(subcommandArgs(args)); handled {
				return command.StubCommand(stdout, err)
			}
		}
		return command.DefaultFactory(name, args...)
	}
}

// subcommandArgs drops the leading global options every git invocation carries
// (--no-optional-locks, and -c <name>=<value> on the diff producers) so a
// matcher can index from the subcommand. Without this the pick functions here
// would be matching against args[0] == "--no-optional-locks" for every call.
func subcommandArgs(args []string) []string {
	for len(args) > 0 {
		switch {
		case args[0] == "-c" && len(args) > 1:
			args = args[2:]
		case strings.HasPrefix(args[0], "-"):
			args = args[1:]
		default:
			return args
		}
	}
	return args
}

// A file listed as untracked but unreadable by the time it is diffed (deleted
// in between, or permission-denied) must not silently vanish from the body: it
// is still counted in the sidebar's "New Changes (N files)" header.
//
// `git diff --no-index` reports this as exit 1 with empty stdout and a message
// on stderr — the same exit code as the ordinary "differences found" case, so
// only stderr distinguishes them.
func TestUntrackedDiff_UnreadableFileGetsPlaceholder(t *testing.T) {
	dir, _ := setupPseudoRepo(t)
	// ls-files reports a file that does not exist on disk.
	g := git.NewWithFactory(dir, interceptGit(func(args []string) (string, error, bool) {
		if len(args) > 0 && args[0] == "ls-files" {
			return "ghost.go\x00untracked.go\x00", nil, true
		}
		return "", nil, false
	}))

	diff, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "[could not read ghost.go]") {
		t.Errorf("unreadable file should get a placeholder, got:\n%s", diff)
	}
	// The readable file alongside it still renders normally.
	if !strings.Contains(diff, "+var onlyUntracked = 3") {
		t.Errorf("readable untracked file should still render, got:\n%s", diff)
	}
}

// A `git diff --no-index` that fails without writing anywhere — killed by its
// deadline, or never started at all — is indistinguishable from "no
// differences" if only the output is consulted. That would drop the file from
// the body while the sidebar header still counts it, the very failure the
// placeholder exists to prevent, so any outputless failure has to be treated
// as a read failure.
func TestUntrackedDiff_OutputlessFailureGetsPlaceholder(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "killed by its deadline",
			err:  fmt.Errorf("git timed out after 45s: %w", context.DeadlineExceeded),
		},
		{
			// Cmd.Start returns this before it ever consults the context, so
			// it carries no deadline and cannot be caught by looking for one.
			name: "never started",
			err:  errors.New(`exec: "git": executable file not found in $PATH`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _ := setupPseudoRepo(t)
			g := git.NewWithFactory(dir, interceptGit(func(args []string) (string, error, bool) {
				if len(args) > 0 && args[0] == "ls-files" {
					return "untracked.go\x00", nil, true
				}
				for _, a := range args {
					if a == "--no-index" {
						// No stdout, no stderr: what both failures leave.
						return "", tt.err, true
					}
				}
				return "", nil, false
			}))

			diff, err := g.UntrackedDiff()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(diff, "[could not read untracked.go]") {
				t.Errorf("an outputless failure should leave a placeholder, got:\n%s", diff)
			}
		})
	}
}

// When the untracked listing fails outright, NewChangesDiff must not pass the
// result off as "no untracked files" — the sidebar header counts them.
func TestNewChangesDiff_UntrackedListingFailure(t *testing.T) {
	lsFailed := func(args []string) (string, error, bool) {
		if len(args) > 0 && args[0] == "ls-files" {
			return "", errors.New("ls-files exploded"), true
		}
		return "", nil, false
	}

	t.Run("keeps unstaged content and says what is missing", func(t *testing.T) {
		dir, _ := setupPseudoRepo(t) // has an unstaged change
		g := git.NewWithFactory(dir, interceptGit(lsFailed))

		diff, err := g.NewChangesDiff()
		if err != nil {
			t.Fatalf("unexpected error with unstaged content present: %v", err)
		}
		if !strings.Contains(diff, "+var onlyUnstaged = 2") {
			t.Errorf("unstaged content should survive a listing failure, got:\n%s", diff)
		}
		if !strings.Contains(diff, "[error listing untracked files:") {
			t.Errorf("listing failure should be visible in the body, got:\n%s", diff)
		}
	})

	t.Run("propagates when there is nothing else to show", func(t *testing.T) {
		dir := setupTestRepo(t) // clean working tree: no unstaged diff
		g := git.NewWithFactory(dir, interceptGit(lsFailed))

		diff, err := g.NewChangesDiff()
		if err == nil {
			t.Fatalf("total failure must propagate, got body %q and nil error", diff)
		}
		if diff != "" {
			t.Errorf("error case should return no body, got %q", diff)
		}
	})
}

// ---------------------------------------------------------------------------
// Displayed paths
// ---------------------------------------------------------------------------

// A diff body carries paths inside its own headers, and those are subject to
// core.quotePath just like the listing side was. setupTestRepo pins
// core.quotePath=true, so without -c core.quotePath=false the header reads
// `diff --git "a/\303\274n\303\257.go" ...` on screen.
func TestUntrackedDiff_NonASCIIPathIsReadable(t *testing.T) {
	dir, g := setupPseudoRepo(t)
	writeFile(t, dir, "ünï.go", "package u\n")

	diff, err := g.UntrackedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "diff --git a/ünï.go b/ünï.go") {
		t.Errorf("non-ASCII path should render readably, got:\n%s", diff)
	}
	if strings.Contains(diff, `\303\274`) {
		t.Errorf("non-ASCII path came back octal-escaped, got:\n%s", diff)
	}
}

func TestStagedDiff_NonASCIIPathIsReadable(t *testing.T) {
	dir, g := setupPseudoRepo(t)
	writeFile(t, dir, "stäged.go", "package s\n")
	runGit(t, dir, "add", "stäged.go")

	diff, err := g.StagedDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "b/stäged.go") {
		t.Errorf("non-ASCII staged path should render readably, got:\n%s", diff)
	}
	if strings.Contains(diff, `\303\244`) {
		t.Errorf("non-ASCII staged path came back octal-escaped, got:\n%s", diff)
	}
}
