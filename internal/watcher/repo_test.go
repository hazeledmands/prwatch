package watcher_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/watcher"
)

func repoGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := command.DefaultFactory("git", args...)
	cmd.SetDir(dir)
	var stderr bytes.Buffer
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %s %v", args, stderr.String(), err)
	}
}

// resolved resolves a path the way RepoDirs does. t.TempDir hands back
// /var/folders/... on macOS while git reports the /private/var/... it resolves
// to, so an unresolved expectation would compare two names for one directory.
func resolved(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return r
}

// newRepo builds a repo with one commit on main.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repoGit(t, dir, "init", "-q", "--initial-branch=main")
	repoGit(t, dir, "config", "user.email", "test@test.com")
	repoGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repoGit(t, dir, "add", ".")
	repoGit(t, dir, "commit", "-q", "-m", "initial")
	return dir
}

// newLinkedWorktree adds a linked worktree beside main and returns its path.
// This is the case the old startup gate skipped outright: the worktree's `.git`
// is a *file* holding a `gitdir:` pointer, not a directory.
func newLinkedWorktree(t *testing.T, main string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt")
	repoGit(t, main, "worktree", "add", "-q", wt, "-b", "feature")

	info, err := os.Lstat(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatalf("fixture is wrong: %s/.git is a directory, wanted the gitdir-pointer file", wt)
	}
	return wt
}

func TestRepoDirs_MainWorktree(t *testing.T) {
	dir := newRepo(t)
	got := watcher.RepoDirs(dir, command.DefaultFactory)

	for _, want := range []string{
		resolved(t, dir),
		resolved(t, filepath.Join(dir, ".git")),
		resolved(t, filepath.Join(dir, ".git", "refs", "heads")),
	} {
		if !slices.Contains(got, want) {
			t.Errorf("RepoDirs missing %q; got %v", want, got)
		}
	}
}

func TestRepoDirs_LinkedWorktree(t *testing.T) {
	main := newRepo(t)
	wt := newLinkedWorktree(t, main)

	got := watcher.RepoDirs(wt, command.DefaultFactory)

	// The worktree's own tree, its private gitdir (where its HEAD lives), and
	// the common dir's refs (where its branch lives) all have to be covered.
	priv := filepath.Join(main, ".git", "worktrees", "wt")
	for _, want := range []string{
		resolved(t, wt),
		resolved(t, priv),
		resolved(t, filepath.Join(main, ".git", "refs", "heads")),
	} {
		if !slices.Contains(got, want) {
			t.Errorf("RepoDirs missing %q; got %v", want, got)
		}
	}

	// And nothing that does not exist: `<wt>/.git` is a file, and
	// `<wt>/.git/refs/heads` is nothing at all.
	for _, unwanted := range []string{
		filepath.Join(resolved(t, wt), ".git"),
		filepath.Join(resolved(t, wt), ".git", "refs", "heads"),
	} {
		if slices.Contains(got, unwanted) {
			t.Errorf("RepoDirs includes non-directory %q; got %v", unwanted, got)
		}
	}
	for _, d := range got {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			t.Errorf("RepoDirs returned %q which is not a watchable directory (%v)", d, err)
		}
	}
}

func TestRepoDirs_NotARepo(t *testing.T) {
	dir := t.TempDir()
	got := watcher.RepoDirs(dir, command.DefaultFactory)
	// Outside a repo there are no git locations, but the directory itself is
	// still worth watching — prwatch runs against non-repo directories too.
	if !slices.Contains(got, resolved(t, dir)) {
		t.Errorf("RepoDirs = %v, want it to contain %q", got, resolved(t, dir))
	}
}

// TestRepoDirs_LinkedWorktreeCommitIsSeen is the regression that matters: a
// commit made in a linked worktree updates a ref under the *common* dir, and
// the watcher must hear about it. The watch set deliberately excludes the
// worktree's own tree so only the git-location coverage can satisfy it.
func TestRepoDirs_LinkedWorktreeCommitIsSeen(t *testing.T) {
	main := newRepo(t)
	wt := newLinkedWorktree(t, main)

	var gitOnly []string
	for _, d := range watcher.RepoDirs(wt, command.DefaultFactory) {
		if d != resolved(t, wt) {
			gitOnly = append(gitOnly, d)
		}
	}
	if len(gitOnly) == 0 {
		t.Fatal("no git locations resolved for the linked worktree")
	}

	ch := make(chan struct{}, 10)
	w, err := watcher.NewMulti(gitOnly, func() { ch <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repoGit(t, wt, "add", ".")
	repoGit(t, wt, "commit", "-q", "-m", "in the worktree")

	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Error("no refresh after committing in a linked worktree")
	}
}

func TestNewMulti_WatchesEveryDir(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()

	ch := make(chan struct{}, 10)
	w, err := watcher.NewMulti([]string{a, b}, func() { ch <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if got := w.Watching(); len(got) != 2 {
		t.Fatalf("Watching = %v, want both dirs", got)
	}

	for _, dir := range []string{a, b} {
		time.Sleep(50 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Errorf("no refresh for a change in %s", dir)
		}
	}
}

func TestNewMulti_SkipsUnwatchableDirs(t *testing.T) {
	good := t.TempDir()

	ch := make(chan struct{}, 10)
	w, err := watcher.NewMulti(
		[]string{"/nonexistent/dir/that/should/not/exist", good},
		func() { ch <- struct{}{} },
	)
	if err != nil {
		t.Fatalf("a single bad dir must not fail the whole watcher: %v", err)
	}
	defer w.Close()

	if got := w.Watching(); !slices.Equal(got, []string{good}) {
		t.Errorf("Watching = %v, want just %q", got, good)
	}

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(good, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Error("no refresh from the dir that could be watched")
	}
}

func TestNewMulti_AllDirsUnwatchableIsAnError(t *testing.T) {
	if _, err := watcher.NewMulti([]string{"/nope/a", "/nope/b"}, func() {}); err == nil {
		t.Error("expected an error when no dir could be watched")
	}
	if _, err := watcher.NewMulti(nil, func() {}); err == nil {
		t.Error("expected an error for an empty dir list")
	}
}

// TestRepoDirs_CoversSubdirectoriesOfASmallRepo pins the subdirectory
// coverage: fsnotify is not recursive, so a dir one or more levels down is
// only watched if it was named explicitly.
func TestRepoDirs_CoversSubdirectoriesOfASmallRepo(t *testing.T) {
	dir := newRepo(t)
	deep := filepath.Join(dir, "internal", "ui")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "model.go"), []byte("package ui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repoGit(t, dir, "add", ".")
	repoGit(t, dir, "commit", "-q", "-m", "add nested source")

	got := watcher.RepoDirs(dir, command.DefaultFactory)
	for _, want := range []string{resolved(t, filepath.Join(dir, "internal")), resolved(t, deep)} {
		if !slices.Contains(got, want) {
			t.Errorf("RepoDirs missing tracked subdirectory %q; got %v", want, got)
		}
	}
}

// TestRepoDirs_SkipsSubdirectoriesOfALargeRepo pins the other half of that
// decision: past the budget, subdirectory watching is dropped wholesale and
// the poll covers it.
func TestRepoDirs_SkipsSubdirectoriesOfALargeRepo(t *testing.T) {
	dir := newRepo(t)
	sub := filepath.Join(dir, "many")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range watcher.TrackedFileBudget + 1 {
		name := filepath.Join(sub, "f"+itoa(i)+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repoGit(t, dir, "add", ".")
	repoGit(t, dir, "commit", "-q", "-m", "a great many files")

	got := watcher.RepoDirs(dir, command.DefaultFactory)
	if slices.Contains(got, resolved(t, sub)) {
		t.Errorf("RepoDirs watched %q despite exceeding the file budget; got %v", sub, got)
	}
	// The git locations and the worktree root survive regardless.
	for _, want := range []string{resolved(t, dir), resolved(t, filepath.Join(dir, ".git", "refs", "heads"))} {
		if !slices.Contains(got, want) {
			t.Errorf("RepoDirs missing %q; got %v", want, got)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
