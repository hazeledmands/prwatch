package git

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
)

// blobSHA1 computes a git blob hash independently of the code under test,
// straight from the object format: sha1 over "blob <len>\x00<content>". This is
// the oracle that makes the batch-boundary test meaningful — checking hashes
// against another call to the same helper would only prove it is consistent
// with itself, not that each path got *its own* hash.
func blobSHA1(content string) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// requireSHA1Repo skips a test whose oracle assumes the default object format.
func requireSHA1Repo(t *testing.T, g *Git) {
	t.Helper()
	format, err := g.run("rev-parse", "--show-object-format")
	if err != nil {
		t.Fatal(err)
	}
	if format != "sha1" {
		t.Skipf("repo object format is %q; the blob oracle in this test assumes sha1", format)
	}
}

// TestHashObjects_CrossesBatchBoundary walks past hashObjectBatchSize so the
// argv chunking is actually exercised, and checks every path against the
// independent oracle. A batch boundary is exactly where a line-to-argument
// misalignment would hide: every hash would still be a real hash, just
// attributed to the wrong file — which is a false rename, silently.
func TestHashObjects_CrossesBatchBoundary(t *testing.T) {
	dir := setupPureMvRepo(t)
	g := New(dir)
	requireSHA1Repo(t, g)

	const n = hashObjectBatchSize + 17 // spills into a second, partial batch
	paths := make([]string, 0, n)
	want := make(map[string]string, n)
	for i := range n {
		name := fmt.Sprintf("batch/f%03d.txt", i)
		content := fmt.Sprintf("content number %d\n", i)
		if err := os.MkdirAll(filepath.Join(dir, "batch"), 0o755); err != nil {
			t.Fatal(err)
		}
		mvWrite(t, dir, name, content)
		paths = append(paths, name)
		want[name] = blobSHA1(content)
	}

	got, err := g.hashObjects(paths)
	if err != nil {
		t.Fatalf("hashObjects: %v", err)
	}
	if len(got) != n {
		t.Fatalf("hashObjects returned %d hashes, want %d", len(got), n)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("hash for %s = %s, want %s", p, got[p], w)
		}
	}
}

func TestHashObjects_BatchesRatherThanOneCallPerPath(t *testing.T) {
	dir := setupPureMvRepo(t)
	cf := &countingFactory{}
	g := NewWithFactory(dir, cf.factory())

	const n = hashObjectBatchSize + 17
	paths := make([]string, 0, n)
	if err := os.MkdirAll(filepath.Join(dir, "batch"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		name := fmt.Sprintf("batch/f%03d.txt", i)
		mvWrite(t, dir, name, fmt.Sprintf("content %d\n", i))
		paths = append(paths, name)
	}

	if _, err := g.hashObjects(paths); err != nil {
		t.Fatal(err)
	}
	// Two chunks for n paths, and nothing per-path: the fallback must not fire
	// when every batch succeeds.
	if got := cf.countMatching("hash-object"); got != 2 {
		t.Errorf("hash-object invocations = %d, want 2 chunks for %d paths", got, n)
	}
}

// TestHashObjects_UnreadablePathLosesOnlyItself is the all-or-nothing
// regression. `git hash-object` exits nonzero for the whole invocation if one
// path is unreadable, and run() discards stdout on a nonzero exit, so before
// the per-path fallback a single file that vanished between the listing and the
// hashing took every other hash in its batch — and rename detection for the
// whole refresh — down with it.
func TestHashObjects_UnreadablePathLosesOnlyItself(t *testing.T) {
	dir := setupPureMvRepo(t)
	g := New(dir)
	requireSHA1Repo(t, g)

	good1, good2 := "alive-a.txt", "alive-b.txt"
	mvWrite(t, dir, good1, "first\n")
	mvWrite(t, dir, good2, "second\n")
	// A path that no longer exists, sitting between two readable ones — the
	// shape of a file deleted after `ls-files --others` listed it.
	const vanished = "vanished.txt"

	got, err := g.hashObjects([]string{good1, vanished, good2})
	if err != nil {
		t.Fatalf("hashObjects should absorb the failure, got %v", err)
	}
	if got[good1] != blobSHA1("first\n") {
		t.Errorf("hash for %s = %q, want %q", good1, got[good1], blobSHA1("first\n"))
	}
	if got[good2] != blobSHA1("second\n") {
		t.Errorf("hash for %s = %q, want %q", good2, got[good2], blobSHA1("second\n"))
	}
	if h, ok := got[vanished]; ok {
		t.Errorf("hash for the missing path = %q, want it absent", h)
	}
}

// TestDetectPureMvRenames_SurvivesAnUnreadableUntrackedFile is the same
// failure seen from the feature's own altitude: one untracked file that git
// cannot read must not cost the rename sitting next to it in the batch.
//
// Unreadable rather than missing, deliberately. A missing file is filtered out
// by statInDir before any hashing, so it would never reach the batch and the
// test would pass with or without the fallback. A file that stats fine and
// then fails to open is the case that actually gets that far.
func TestDetectPureMvRenames_SurvivesAnUnreadableUntrackedFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a 0000 mode would still be readable")
	}
	dir := setupPureMvRepo(t)
	g := New(dir)

	if err := os.Rename(filepath.Join(dir, "unique.go"), filepath.Join(dir, "moved.go")); err != nil {
		t.Fatal(err)
	}
	// Nonempty, so it is not skipped as an empty file, and unreadable, so
	// `git hash-object` fails the invocation it lands in.
	mvWrite(t, dir, "secret.txt", "cannot read this\n")
	if err := os.Chmod(filepath.Join(dir, "secret.txt"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "secret.txt"), 0o644) })

	untracked := untrackedIn(t, g)
	if !untracked["secret.txt"] {
		t.Fatalf("fixture is wrong: secret.txt is not in the untracked set %v", untracked)
	}

	want := []Rename{{Old: "unique.go", New: "moved.go", Pure: true}}
	got := g.detectPureMvRenames(nil, untracked)
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("detectPureMvRenames = %#v, want %#v", got, want)
	}
}

func TestHashObjectBatch_MismatchedLineCount(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		paths   []string
		wantErr error
	}{
		{
			name:    "fewer hashes than paths",
			stdout:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
			paths:   []string{"a", "b"},
			wantErr: errHashObjectMismatch,
		},
		{
			name:    "more hashes than paths",
			stdout:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
			paths:   []string{"a"},
			wantErr: errHashObjectMismatch,
		},
		{
			name:    "no output at all",
			stdout:  "",
			paths:   []string{"a"},
			wantErr: errHashObjectMismatch,
		},
		{
			name:    "one hash per path is accepted",
			stdout:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n",
			paths:   []string{"a", "b"},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithFactory("", func(name string, args ...string) command.Command {
				return command.StubCommand(tc.stdout, nil)
			})
			got, err := g.hashObjectBatch(tc.paths)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("hashObjectBatch error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Errorf("hashObjectBatch = %v on error, want nil", got)
				}
				return
			}
			if len(got) != len(tc.paths) {
				t.Errorf("hashObjectBatch = %v, want %d entries", got, len(tc.paths))
			}
		})
	}
}

// TestHashObjects_FallsBackPerPathOnMismatch pins that the fallback covers a
// mismatched answer too, not just a nonzero exit. The stub fails any
// multi-path invocation and answers single-path ones, which is what the
// per-path retry issues.
func TestHashObjects_FallsBackPerPathOnMismatch(t *testing.T) {
	var mu sync.Mutex
	var multiCalls, singleCalls int

	g := NewWithFactory("", func(name string, args ...string) command.Command {
		// args: --no-optional-locks hash-object -- p1 [p2...]
		var paths []string
		for i, a := range args {
			if a == "--" {
				paths = args[i+1:]
				break
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if len(paths) > 1 {
			multiCalls++
			// One line for many paths: a mismatch, not an exit failure.
			return command.StubCommand("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", nil)
		}
		singleCalls++
		return command.StubCommand(strings.Repeat("b", 40)+"\n", nil)
	})

	got, err := g.hashObjects([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("hashObjects: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("hashObjects = %v, want all 3 paths recovered per-path", got)
	}
	if multiCalls != 1 || singleCalls != 3 {
		t.Errorf("calls: multi=%d single=%d, want 1 and 3", multiCalls, singleCalls)
	}
}
