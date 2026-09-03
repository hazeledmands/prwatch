package git

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
)

// countingFactory wraps the real command factory and records the git
// subcommand of every invocation, so a test can assert that a subprocess was
// (or was not) spawned.
type countingFactory struct {
	mu   sync.Mutex
	seen []string
}

func (c *countingFactory) factory() command.Factory {
	return func(name string, args ...string) command.Command {
		c.mu.Lock()
		c.seen = append(c.seen, strings.Join(append([]string{name}, args...), " "))
		c.mu.Unlock()
		return command.DefaultFactory(name, args...)
	}
}

// countMatching returns how many recorded invocations contain sub.
func (c *countingFactory) countMatching(sub string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, s := range c.seen {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func mvRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := command.DefaultFactory("git", args...)
	cmd.SetDir(dir)
	var stderr bytes.Buffer
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v: %s %v", args, stderr.String(), err)
	}
}

func mvWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	uniqueBody = "package a\n\nfunc A() int { return 1 }\n"
	dupBody    = "package dup\n\nfunc D() int { return 2 }\n"
)

// setupPureMvRepo builds a repo whose committed content deliberately includes
// an empty file and a pair of byte-identical files — the two shapes that make
// a content hash ambiguous.
func setupPureMvRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mvRunGit(t, dir, "init", "--initial-branch=main")
	mvRunGit(t, dir, "config", "user.email", "test@test.com")
	mvRunGit(t, dir, "config", "user.name", "Test")

	mvWrite(t, dir, "unique.go", uniqueBody)
	mvWrite(t, dir, "empty.txt", "")
	mvWrite(t, dir, "dup1.go", dupBody)
	mvWrite(t, dir, "dup2.go", dupBody)
	mvRunGit(t, dir, "add", ".")
	mvRunGit(t, dir, "commit", "-m", "initial")
	return dir
}

// untrackedIn lists the untracked paths the way ChangedFiles does, so the unit
// under test sees exactly the input it gets in production.
func untrackedIn(t *testing.T, g *Git) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	paths, err := g.runZ("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		out[p] = true
	}
	return out
}

func TestDetectPureMvRenames(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  []Rename
	}{
		{
			name: "a content-identical move with a unique hash pairs",
			setup: func(t *testing.T, dir string) {
				if err := os.Rename(filepath.Join(dir, "unique.go"), filepath.Join(dir, "moved.go")); err != nil {
					t.Fatal(err)
				}
			},
			want: []Rename{{Old: "unique.go", New: "moved.go", Pure: true}},
		},
		{
			name: "an empty untracked file never pairs with a deleted empty file",
			setup: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "empty.txt")); err != nil {
					t.Fatal(err)
				}
				mvWrite(t, dir, "scratch.txt", "")
			},
			want: nil,
		},
		{
			name: "an unrelated empty untracked file does not pair with a deleted file",
			setup: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "empty.txt")); err != nil {
					t.Fatal(err)
				}
				// A brand new empty file, plus a real move alongside it: the
				// move must still be found and the empty file must stay out
				// of it.
				if err := os.Rename(filepath.Join(dir, "unique.go"), filepath.Join(dir, "moved.go")); err != nil {
					t.Fatal(err)
				}
				mvWrite(t, dir, "notes.txt", "")
			},
			want: []Rename{{Old: "unique.go", New: "moved.go", Pure: true}},
		},
		{
			name: "two untracked files sharing the deleted file's content pair with neither",
			setup: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "unique.go")); err != nil {
					t.Fatal(err)
				}
				mvWrite(t, dir, "copy-a.go", uniqueBody)
				mvWrite(t, dir, "copy-b.go", uniqueBody)
			},
			want: nil,
		},
		{
			name: "two deleted files sharing content pair with neither",
			setup: func(t *testing.T, dir string) {
				for _, f := range []string{"dup1.go", "dup2.go"} {
					if err := os.Remove(filepath.Join(dir, f)); err != nil {
						t.Fatal(err)
					}
				}
				mvWrite(t, dir, "moved-dup.go", dupBody)
			},
			want: nil,
		},
		{
			name: "an untracked file with unrelated content does not pair",
			setup: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "unique.go")); err != nil {
					t.Fatal(err)
				}
				mvWrite(t, dir, "unrelated.go", "package unrelated\n")
			},
			want: nil,
		},
		{
			name: "an untracked file with no deletion anywhere does not pair",
			setup: func(t *testing.T, dir string) {
				mvWrite(t, dir, "brand-new.go", uniqueBody)
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupPureMvRepo(t)
			g := New(dir)
			tc.setup(t, dir)

			untracked := untrackedIn(t, g)
			got := g.detectPureMvRenames(nil, untracked)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("detectPureMvRenames = %#v, want %#v", got, tc.want)
			}

			// Repeated refreshes must agree. Before the double-uniqueness
			// rule, an ambiguous hash was resolved by whichever candidate map
			// iteration happened to write last, so the reported target could
			// flip from one poll to the next.
			for i := range 20 {
				again := g.detectPureMvRenames(nil, untrackedIn(t, g))
				if !reflect.DeepEqual(again, got) {
					t.Fatalf("refresh %d = %#v, want the stable %#v", i, again, got)
				}
			}
		})
	}
}

func TestDetectPureMvRenames_SkipsHashingWithoutDeletions(t *testing.T) {
	dir := setupPureMvRepo(t)
	cf := &countingFactory{}
	g := NewWithFactory(dir, cf.factory())

	// Untracked files but no deletion: there is nothing to pair against, so
	// the expensive half must not run at all.
	mvWrite(t, dir, "scratch1.go", "package s1\n")
	mvWrite(t, dir, "scratch2.go", "package s2\n")

	if got := g.detectPureMvRenames(nil, untrackedIn(t, g)); got != nil {
		t.Fatalf("detectPureMvRenames = %#v, want nil", got)
	}
	if n := cf.countMatching("hash-object"); n != 0 {
		t.Errorf("hash-object invocations = %d, want 0 when nothing is deleted", n)
	}
}

func TestDetectPureMvRenames_HashesOnceAcrossRefreshes(t *testing.T) {
	dir := setupPureMvRepo(t)
	cf := &countingFactory{}
	g := NewWithFactory(dir, cf.factory())

	if err := os.Rename(filepath.Join(dir, "unique.go"), filepath.Join(dir, "moved.go")); err != nil {
		t.Fatal(err)
	}
	mvWrite(t, dir, "scratch.go", "package s\n")

	want := []Rename{{Old: "unique.go", New: "moved.go", Pure: true}}
	for i := range 5 {
		if got := g.detectPureMvRenames(nil, untrackedIn(t, g)); !reflect.DeepEqual(got, want) {
			t.Fatalf("refresh %d = %#v, want %#v", i, got, want)
		}
	}
	// One batched invocation covers both untracked files, and the cache keeps
	// the four later refreshes from spawning anything.
	if n := cf.countMatching("hash-object"); n != 1 {
		t.Errorf("hash-object invocations = %d over 5 refreshes, want 1", n)
	}

	// Editing an untracked file moves its stat identity, so it must be
	// re-hashed — and the rename must survive.
	mvWrite(t, dir, "scratch.go", "package s\n\nfunc S() {}\n")
	if got := g.detectPureMvRenames(nil, untrackedIn(t, g)); !reflect.DeepEqual(got, want) {
		t.Fatalf("after edit = %#v, want %#v", got, want)
	}
	if n := cf.countMatching("hash-object"); n != 2 {
		t.Errorf("hash-object invocations = %d after an edit, want 2", n)
	}
}
