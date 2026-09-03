package git

import (
	"fmt"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

func TestPairUniqueHashes(t *testing.T) {
	tests := []struct {
		name     string
		oldPaths []string
		oldHash  map[string]string
		newHash  map[string]string
		want     []Rename
	}{
		{
			name:     "a hash unique on both sides pairs",
			oldPaths: []string{"old.go"},
			oldHash:  map[string]string{"old.go": "h1"},
			newHash:  map[string]string{"new.go": "h1"},
			want:     []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name:     "two untracked files with identical content pair with neither",
			oldPaths: []string{"old.go"},
			oldHash:  map[string]string{"old.go": "h1"},
			newHash:  map[string]string{"copy-a.go": "h1", "copy-b.go": "h1"},
			want:     nil,
		},
		{
			name:     "two deleted files with identical content pair with neither",
			oldPaths: []string{"old-a.go", "old-b.go"},
			oldHash:  map[string]string{"old-a.go": "h1", "old-b.go": "h1"},
			newHash:  map[string]string{"new.go": "h1"},
			want:     nil,
		},
		{
			name:     "ambiguity is local to its hash",
			oldPaths: []string{"amb.go", "uniq.go"},
			oldHash:  map[string]string{"amb.go": "h1", "uniq.go": "h2"},
			newHash:  map[string]string{"c1.go": "h1", "c2.go": "h1", "moved.go": "h2"},
			want:     []Rename{{Old: "uniq.go", New: "moved.go", Pure: true}},
		},
		{
			name:     "an unmatched hash on either side yields nothing",
			oldPaths: []string{"old.go"},
			oldHash:  map[string]string{"old.go": "h1"},
			newHash:  map[string]string{"unrelated.go": "h9"},
			want:     nil,
		},
		{
			name:     "a deleted path with no known hash is skipped",
			oldPaths: []string{"unknown.go", "old.go"},
			oldHash:  map[string]string{"old.go": "h1"},
			newHash:  map[string]string{"new.go": "h1"},
			want:     []Rename{{Old: "old.go", New: "new.go", Pure: true}},
		},
		{
			name:     "output follows the deleted-path order it was given",
			oldPaths: []string{"b.go", "a.go"},
			oldHash:  map[string]string{"a.go": "h1", "b.go": "h2"},
			newHash:  map[string]string{"a-new.go": "h1", "b-new.go": "h2"},
			want: []Rename{
				{Old: "b.go", New: "b-new.go", Pure: true},
				{Old: "a.go", New: "a-new.go", Pure: true},
			},
		},
		{
			name:     "no candidates at all",
			oldPaths: nil,
			oldHash:  nil,
			newHash:  nil,
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pairUniqueHashes(tc.oldPaths, tc.oldHash, tc.newHash)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("pairUniqueHashes = %#v, want %#v", got, tc.want)
			}
			// Repeating the call must produce the identical answer: the inputs
			// include maps, and a result that depends on their iteration order
			// would flip the reported rename target from one refresh to the
			// next.
			for i := 0; i < 20; i++ {
				again := pairUniqueHashes(tc.oldPaths, tc.oldHash, tc.newHash)
				if !reflect.DeepEqual(again, got) {
					t.Fatalf("repeat %d = %#v, want %#v", i, again, got)
				}
			}
		})
	}
}

// TestProperty_PairUniqueHashes_PartialBijection asserts the shape of the
// pairing regardless of input: no path may appear in two pairs on either side,
// and every pair must be justified by a hash that is unique on both sides.
func TestProperty_PairUniqueHashes_PartialBijection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// A deliberately small hash alphabet, so collisions — the whole
		// hazard — are common rather than rare.
		hashOf := rapid.SampledFrom([]string{"h0", "h1", "h2"})

		nOld := rapid.IntRange(0, 8).Draw(t, "nOld")
		oldPaths := make([]string, 0, nOld)
		oldHash := make(map[string]string, nOld)
		for i := range nOld {
			p := fmt.Sprintf("old%d", i)
			oldPaths = append(oldPaths, p)
			// Some deleted paths have no index hash at all.
			if rapid.Bool().Draw(t, fmt.Sprintf("oldKnown%d", i)) {
				oldHash[p] = hashOf.Draw(t, fmt.Sprintf("oldHash%d", i))
			}
		}

		nNew := rapid.IntRange(0, 8).Draw(t, "nNew")
		newHash := make(map[string]string, nNew)
		for i := range nNew {
			newHash[fmt.Sprintf("new%d", i)] = hashOf.Draw(t, fmt.Sprintf("newHash%d", i))
		}

		got := pairUniqueHashes(oldPaths, oldHash, newHash)

		// Count occurrences per hash on each side, independently of the
		// production code, so the uniqueness claim is checked rather than
		// restated.
		oldCount := map[string]int{}
		for _, p := range oldPaths {
			if h, ok := oldHash[p]; ok {
				oldCount[h]++
			}
		}
		newCount := map[string]int{}
		for _, h := range newHash {
			newCount[h]++
		}

		// Completeness, checked before soundness because it is what a
		// `return nil` would sail through: every hash unique on both sides
		// must actually be paired.
		wantOld := map[string]bool{}
		for _, p := range oldPaths {
			h, ok := oldHash[p]
			if !ok {
				continue
			}
			if oldCount[h] == 1 && newCount[h] == 1 {
				wantOld[p] = true
			}
		}
		if len(got) != len(wantOld) {
			t.Fatalf("got %d pairs %#v, want exactly the %d unambiguous ones %v",
				len(got), got, len(wantOld), wantOld)
		}
		for _, r := range got {
			if !wantOld[r.Old] {
				t.Fatalf("pair %#v is not one of the unambiguous ones %v", r, wantOld)
			}
		}

		seenOld := map[string]bool{}
		seenNew := map[string]bool{}
		for _, r := range got {
			if seenOld[r.Old] {
				t.Fatalf("old path %q appears in two pairs: %#v", r.Old, got)
			}
			if seenNew[r.New] {
				t.Fatalf("new path %q appears in two pairs: %#v", r.New, got)
			}
			seenOld[r.Old] = true
			seenNew[r.New] = true

			h, ok := oldHash[r.Old]
			if !ok {
				t.Fatalf("paired old path %q has no hash: %#v", r.Old, got)
			}
			if newHash[r.New] != h {
				t.Fatalf("pair %#v joins mismatched hashes %q/%q", r, h, newHash[r.New])
			}
			if oldCount[h] != 1 || newCount[h] != 1 {
				t.Fatalf("pair %#v rests on hash %q with counts old=%d new=%d, want 1/1",
					r, h, oldCount[h], newCount[h])
			}
			if !r.Pure {
				t.Fatalf("pair %#v must be marked Pure", r)
			}
		}

		// Determinism: same inputs, same answer, however the maps happen to
		// iterate this time.
		for range 5 {
			if again := pairUniqueHashes(oldPaths, oldHash, newHash); !reflect.DeepEqual(again, got) {
				t.Fatalf("nondeterministic: %#v then %#v", got, again)
			}
		}
	})
}
