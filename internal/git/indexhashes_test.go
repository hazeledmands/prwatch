package git

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestIndexHashes_ChunksPastTheBatchWidth covers a large `git rm -r`: the path
// list is unbounded, and an unbounded argv would exceed ARG_MAX (1MB on
// darwin), fail the exec, and hand back an empty map — switching rename
// detection off for the refresh without any sign that it happened.
func TestIndexHashes_ChunksPastTheBatchWidth(t *testing.T) {
	dir := setupPureMvRepo(t)
	g := New(dir)
	requireSHA1Repo(t, g)

	const n = hashObjectBatchSize*2 + 5
	if err := os.MkdirAll(filepath.Join(dir, "bulk"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, n)
	want := make(map[string]string, n)
	for i := range n {
		name := fmt.Sprintf("bulk/f%04d.txt", i)
		content := fmt.Sprintf("bulk content %d\n", i)
		mvWrite(t, dir, name, content)
		paths = append(paths, name)
		want[name] = blobSHA1(content)
	}
	mvRunGit(t, dir, "add", ".")
	mvRunGit(t, dir, "commit", "-m", "bulk")

	got := g.indexHashes(paths)
	if len(got) != n {
		t.Fatalf("indexHashes returned %d entries, want %d", len(got), n)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("index hash for %s = %s, want %s", p, got[p], w)
		}
	}
}

// TestIndexHashes_SkipsConflictedPaths pins the unmerged-stage rule. A path in
// conflict has three index entries (base, ours, theirs) and therefore no single
// content identity; the previous code let whichever record git printed last
// win, which silently meant "theirs".
func TestIndexHashes_SkipsConflictedPaths(t *testing.T) {
	dir := setupPureMvRepo(t)
	g := New(dir)

	// A content conflict on conflicted.txt, with clean.txt alongside it.
	mvWrite(t, dir, "conflicted.txt", "base\n")
	mvWrite(t, dir, "clean.txt", "untouched\n")
	mvRunGit(t, dir, "add", ".")
	mvRunGit(t, dir, "commit", "-m", "add both")

	mvRunGit(t, dir, "checkout", "-b", "other")
	mvWrite(t, dir, "conflicted.txt", "theirs\n")
	mvRunGit(t, dir, "commit", "-am", "theirs")

	mvRunGit(t, dir, "checkout", "main")
	mvWrite(t, dir, "conflicted.txt", "ours\n")
	mvRunGit(t, dir, "commit", "-am", "ours")

	// Expected to fail: that is the point.
	cmd := g.cmdFactory("git", "merge", "other")
	cmd.SetDir(dir)
	_ = cmd.Run()

	stages, err := g.runZ("ls-files", "--stage", "-z", "--", "conflicted.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 3 {
		t.Fatalf("fixture is wrong: conflicted.txt has %d index entries, want 3: %v", len(stages), stages)
	}

	got := g.indexHashes([]string{"conflicted.txt", "clean.txt"})
	if h, ok := got["conflicted.txt"]; ok {
		t.Errorf("index hash for a conflicted path = %q, want it absent", h)
	}
	if got["clean.txt"] == "" {
		t.Errorf("clean.txt lost its hash: %v", got)
	}
}

// TestIndexHashes_UnknownPathsAreAbsent guards the ordinary case: a path git
// does not track simply has no entry, rather than an empty-string hash that
// would then be matched against something.
func TestIndexHashes_UnknownPathsAreAbsent(t *testing.T) {
	dir := setupPureMvRepo(t)
	g := New(dir)

	got := g.indexHashes([]string{"unique.go", "not-in-the-index.txt"})
	if got["unique.go"] == "" {
		t.Errorf("unique.go should have an index hash: %v", got)
	}
	if h, ok := got["not-in-the-index.txt"]; ok {
		t.Errorf("untracked path has index hash %q, want it absent", h)
	}
}
