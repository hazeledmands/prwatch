package git

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

// fakeStats is a statFunc backed by a map, so a test can move a file's stat
// identity without touching a filesystem.
type fakeStats map[string]fileStat

func (f fakeStats) statFunc() statFunc {
	return func(path string) (fileStat, bool) {
		st, ok := f[path]
		return st, ok
	}
}

// recordingHasher is a hashFunc that returns a canned hash per path and
// records every batch it was asked to compute.
type recordingHasher struct {
	hashes map[string]string
	err    error
	calls  [][]string
}

func (r *recordingHasher) hashFunc() hashFunc {
	return func(paths []string) (map[string]string, error) {
		batch := append([]string(nil), paths...)
		sort.Strings(batch)
		r.calls = append(r.calls, batch)
		if r.err != nil {
			return nil, r.err
		}
		out := make(map[string]string, len(paths))
		for _, p := range paths {
			if h, ok := r.hashes[p]; ok {
				out[p] = h
			}
		}
		return out, nil
	}
}

func TestHashCache_Hashes(t *testing.T) {
	tests := []struct {
		name string
		// stats/hashes describe the world; paths is what we ask for.
		stats  fakeStats
		hashes map[string]string
		paths  []string
		want   map[string]string
		// wantHashed is the single batch we expect to be handed to the hasher
		// (nil means the hasher must not be called at all).
		wantHashed []string
	}{
		{
			name:       "hashes every nonempty path on a cold cache",
			stats:      fakeStats{"a": {size: 3, mtimeNano: 1}, "b": {size: 4, mtimeNano: 1}},
			hashes:     map[string]string{"a": "ha", "b": "hb"},
			paths:      []string{"a", "b"},
			want:       map[string]string{"a": "ha", "b": "hb"},
			wantHashed: []string{"a", "b"},
		},
		{
			name:       "an empty file is never hashed and never returned",
			stats:      fakeStats{"empty": {size: 0, mtimeNano: 1}, "a": {size: 3, mtimeNano: 1}},
			hashes:     map[string]string{"empty": "he", "a": "ha"},
			paths:      []string{"empty", "a"},
			want:       map[string]string{"a": "ha"},
			wantHashed: []string{"a"},
		},
		{
			name:       "a path that will not stat is skipped",
			stats:      fakeStats{"a": {size: 3, mtimeNano: 1}},
			hashes:     map[string]string{"a": "ha", "gone": "hg"},
			paths:      []string{"a", "gone"},
			want:       map[string]string{"a": "ha"},
			wantHashed: []string{"a"},
		},
		{
			name:       "no candidate paths means no subprocess",
			stats:      fakeStats{"empty": {size: 0, mtimeNano: 1}},
			hashes:     map[string]string{"empty": "he"},
			paths:      []string{"empty"},
			want:       map[string]string{},
			wantHashed: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &recordingHasher{hashes: tc.hashes}
			var c hashCache
			got := c.hashes(tc.paths, tc.stats.statFunc(), h.hashFunc())
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hashes = %v, want %v", got, tc.want)
			}
			if tc.wantHashed == nil {
				if len(h.calls) != 0 {
					t.Errorf("hasher called %v, want no call", h.calls)
				}
				return
			}
			if len(h.calls) != 1 {
				t.Fatalf("hasher calls = %v, want exactly 1", h.calls)
			}
			if !reflect.DeepEqual(h.calls[0], tc.wantHashed) {
				t.Errorf("hashed batch = %v, want %v", h.calls[0], tc.wantHashed)
			}
		})
	}
}

func TestHashCache_ReuseAndInvalidation(t *testing.T) {
	tests := []struct {
		name string
		// mutate rewrites the world between the two calls.
		mutate func(fakeStats)
		// wantSecondBatch is the batch the second call should hash; nil for
		// a pure cache hit.
		wantSecondBatch []string
	}{
		{
			name:            "unchanged stat is a cache hit",
			mutate:          func(fakeStats) {},
			wantSecondBatch: nil,
		},
		{
			name:            "a changed size invalidates",
			mutate:          func(s fakeStats) { s["a"] = fileStat{size: 99, mtimeNano: 1} },
			wantSecondBatch: []string{"a"},
		},
		{
			name:            "a changed mtime invalidates",
			mutate:          func(s fakeStats) { s["a"] = fileStat{size: 3, mtimeNano: 2} },
			wantSecondBatch: []string{"a"},
		},
		{
			name: "a rewritten file with identical size and mtime stays cached",
			// The stat identity is the whole contract: this is the documented
			// blind spot, asserted so a change of key is a deliberate one.
			mutate:          func(fakeStats) {},
			wantSecondBatch: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stats := fakeStats{"a": {size: 3, mtimeNano: 1}, "b": {size: 4, mtimeNano: 1}}
			h := &recordingHasher{hashes: map[string]string{"a": "ha", "b": "hb"}}
			var c hashCache

			first := c.hashes([]string{"a", "b"}, stats.statFunc(), h.hashFunc())
			if len(first) != 2 {
				t.Fatalf("first call = %v, want 2 hashes", first)
			}
			if len(h.calls) != 1 {
				t.Fatalf("first call hashed %v, want 1 batch", h.calls)
			}

			tc.mutate(stats)
			second := c.hashes([]string{"a", "b"}, stats.statFunc(), h.hashFunc())
			if len(second) != 2 {
				t.Fatalf("second call = %v, want 2 hashes", second)
			}

			if tc.wantSecondBatch == nil {
				if len(h.calls) != 1 {
					t.Errorf("hasher calls = %v, want the second call to hash nothing", h.calls)
				}
				return
			}
			if len(h.calls) != 2 {
				t.Fatalf("hasher calls = %v, want 2", h.calls)
			}
			if !reflect.DeepEqual(h.calls[1], tc.wantSecondBatch) {
				t.Errorf("second batch = %v, want %v", h.calls[1], tc.wantSecondBatch)
			}
		})
	}
}

func TestHashCache_EvictsPathsNoLongerAsked(t *testing.T) {
	stats := fakeStats{"a": {size: 3, mtimeNano: 1}, "b": {size: 4, mtimeNano: 1}}
	h := &recordingHasher{hashes: map[string]string{"a": "ha", "b": "hb"}}
	var c hashCache

	c.hashes([]string{"a", "b"}, stats.statFunc(), h.hashFunc())
	// A refresh where "a" is no longer a candidate must drop its entry...
	c.hashes([]string{"b"}, stats.statFunc(), h.hashFunc())
	// ...so its reappearance is a miss, not a stale hit.
	c.hashes([]string{"a", "b"}, stats.statFunc(), h.hashFunc())

	// Two batches: the cold one, then "a" alone. The middle call is a pure hit
	// on "b" and makes no subprocess at all.
	if len(h.calls) != 2 {
		t.Fatalf("hasher calls = %v, want 2 (cold [a b], then [a] re-hashed)", h.calls)
	}
	if !reflect.DeepEqual(h.calls[1], []string{"a"}) {
		t.Errorf("second batch = %v, want [a] — the evicted path must re-hash", h.calls[1])
	}
}

func TestHashCache_HasherErrorIsNotCached(t *testing.T) {
	stats := fakeStats{"a": {size: 3, mtimeNano: 1}}
	h := &recordingHasher{hashes: map[string]string{"a": "ha"}, err: errors.New("boom")}
	var c hashCache

	if got := c.hashes([]string{"a"}, stats.statFunc(), h.hashFunc()); len(got) != 0 {
		t.Errorf("hashes on hasher error = %v, want empty", got)
	}
	h.err = nil
	if got := c.hashes([]string{"a"}, stats.statFunc(), h.hashFunc()); got["a"] != "ha" {
		t.Errorf("hashes after recovery = %v, want a=ha", got)
	}
	if len(h.calls) != 2 {
		t.Errorf("hasher calls = %v, want 2 (the failure must not be cached)", h.calls)
	}
}
