package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// errHashObjectMismatch reports that `git hash-object` returned a number of
// hashes that does not match the number of paths it was given.
var errHashObjectMismatch = errors.New("git hash-object: hash count does not match path count")

// splitLines splits newline-delimited command output into non-empty records.
func splitLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// fileStat is the identity a cached blob hash is bound to: change either half
// and the cached hash is presumed stale. Size is the cheap discriminator;
// mtime catches same-length edits. mtime is held as Unix nanoseconds rather
// than a time.Time so the struct stays comparable with == (time.Time carries a
// monotonic reading and a location pointer, neither of which belongs in an
// equality test).
type fileStat struct {
	size      int64
	mtimeNano int64
}

// statFunc reports the stat identity of a repo-relative path, and whether it
// could be read at all.
type statFunc func(path string) (fileStat, bool)

// hashFunc computes blob hashes for a batch of repo-relative paths, keyed by
// path. A path the hasher could not read is simply absent from the result.
type hashFunc func(paths []string) (map[string]string, error)

// hashCache memoizes `git hash-object` output across refreshes.
//
// Rename detection needs the working-tree hash of every untracked file, and
// the refresh that needs it runs every few seconds. Recomputing those hashes
// each time is one subprocess-and-full-file-read per untracked file per poll,
// for an answer that only changes when the file does — so the hash is keyed on
// (path, size, mtime) and recomputed only when that identity moves.
//
// Safe for concurrent use: one *Git is shared by every refresh goroutine.
type hashCache struct {
	mu      sync.Mutex
	entries map[string]hashCacheEntry
}

type hashCacheEntry struct {
	stat fileStat
	hash string
}

// hashes returns blob hashes for the given repo-relative paths, hashing only
// those whose stat identity has changed since the last call.
//
// Two kinds of path are dropped rather than hashed: one that will not stat (it
// vanished between the listing and now), and one of zero length. Empty files
// are excluded on purpose — every empty file in a repo shares one blob hash,
// so hashing them only manufactures collisions for a caller whose whole
// question is "does this hash identify exactly one file".
//
// Paths not passed in are evicted, so the cache tracks the current candidate
// set instead of growing without bound across the process's lifetime.
func (c *hashCache) hashes(paths []string, statOf statFunc, hashOf hashFunc) map[string]string {
	live := make(map[string]fileStat, len(paths))
	for _, p := range paths {
		st, ok := statOf(p)
		if !ok || st.size == 0 {
			continue
		}
		live[p] = st
	}

	out := make(map[string]string, len(live))
	var misses []string

	c.mu.Lock()
	for _, p := range paths {
		st, ok := live[p]
		if !ok {
			continue
		}
		if e, ok := c.entries[p]; ok && e.stat == st {
			out[p] = e.hash
			continue
		}
		misses = append(misses, p)
	}
	for p := range c.entries {
		if _, ok := live[p]; !ok {
			delete(c.entries, p)
		}
	}
	c.mu.Unlock()

	if len(misses) == 0 {
		return out
	}

	// Deliberately outside the lock: hashOf shells out to git, and a refresh
	// holding the mutex across a subprocess would stall every other refresh.
	fresh, err := hashOf(misses)
	if err != nil {
		return out
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]hashCacheEntry, len(fresh))
	}
	for p, h := range fresh {
		st, ok := live[p]
		if !ok {
			continue
		}
		out[p] = h
		c.entries[p] = hashCacheEntry{stat: st, hash: h}
	}
	c.mu.Unlock()

	return out
}

// statInDir returns a statFunc reading repo-relative paths under dir.
func statInDir(dir string) statFunc {
	return func(path string) (fileStat, bool) {
		info, err := os.Stat(filepath.Join(dir, path))
		if err != nil || info.IsDir() {
			return fileStat{}, false
		}
		return fileStat{size: info.Size(), mtimeNano: info.ModTime().UnixNano()}, true
	}
}

// hashObjectBatchSize bounds how many paths ride on one `git hash-object`
// argv. Batching is the point — one subprocess for N files instead of N — but
// an unbounded argv would blow ARG_MAX on a repo with thousands of untracked
// files, so the batch is chunked.
//
// Paths go as arguments rather than through --stdin-paths because that flag is
// newline-delimited with no -z equivalent, and a path may legitimately contain
// a newline.
const hashObjectBatchSize = 256

// hashObjects is the production hashFunc: it asks git for the blob hashes of
// paths, in batches. `git hash-object` emits one hash per line in argument
// order, which is what pairs the answers back to their paths.
func (g *Git) hashObjects(paths []string) (map[string]string, error) {
	out := make(map[string]string, len(paths))
	for start := 0; start < len(paths); start += hashObjectBatchSize {
		end := min(start+hashObjectBatchSize, len(paths))
		batch := paths[start:end]

		args := append([]string{"hash-object", "--"}, batch...)
		res, err := g.run(args...)
		if err != nil {
			return out, err
		}
		lines := splitLines(res)
		if len(lines) != len(batch) {
			// A short or over-long answer means the line-to-argument
			// correspondence is broken; guessing which hash belongs to which
			// path is how a false rename gets reported.
			return out, errHashObjectMismatch
		}
		for i, h := range lines {
			out[batch[i]] = h
		}
	}
	return out, nil
}
