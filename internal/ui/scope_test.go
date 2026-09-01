package ui

import (
	"fmt"
	"testing"

	"github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// linearGit is a GitDataSource implementing only the methods scope needs
// (Parent, FirstChildToward). It models a linear history HEAD = c0, c1, c2, ...
// with first-parent links and no merge commits, which is enough to exercise
// every scope state transition.
type linearGit struct{}

func (linearGit) RepoInfo() (git.RepoInfoResult, error)    { return git.RepoInfoResult{}, nil }
func (linearGit) PRAll() (git.PRAllResult, error)          { return git.PRAllResult{}, nil }
func (linearGit) PRChecksAll() (git.PRChecksResult, error) { return git.PRChecksResult{}, nil }
func (linearGit) DetectBaseLocal() (string, error)         { return "", nil }
func (linearGit) DetectBaseFromPR(string) (string, error)  { return "", nil }
func (linearGit) ChangedFiles(string) (git.ChangedFilesResult, error) {
	return git.ChangedFilesResult{}, nil
}
func (linearGit) Commits(string, int, int) ([]git.Commit, error)     { return nil, nil }
func (linearGit) CommitCountRange(string) (int, error)               { return 0, nil }
func (linearGit) FileDiffCommitted(string, string) (string, error)   { return "", nil }
func (linearGit) FileDiffUncommitted(string) (string, error)         { return "", nil }
func (linearGit) FileContent(string) (string, error)                 { return "", nil }
func (linearGit) LastCommitForFile(string) (git.Commit, error)       { return git.Commit{}, nil }
func (linearGit) CommitPatch(string) (string, error)                 { return "", nil }
func (linearGit) AllFiles() ([]string, error)                        { return nil, nil }
func (linearGit) IgnoredEntries() ([]git.IgnoredEntry, error)        { return nil, nil }
func (linearGit) IgnoredFilesInDir(string) ([]string, error)         { return nil, nil }
func (linearGit) BaseCommits(string, int) ([]git.Commit, error)      { return nil, nil }
func (linearGit) BehindCount(string) (int, error)                    { return 0, nil }
func (linearGit) RWXResults(string) (*git.RWXResult, error)          { return nil, nil }
func (linearGit) RWXTaskLog(string) (string, error)                  { return "", nil }
func (linearGit) RWXTestResults(string) ([]git.RWXFailedTest, error) { return nil, nil }

// Parent: "c<n>" → "c<n+1>", representing "one commit older". The
// root commit is "c<rootN>" and has no parent.
const linearRoot = 100

func (linearGit) Parent(sha string) (string, error) {
	var n int
	if _, err := fmt.Sscanf(sha, "c%d", &n); err != nil {
		return "", fmt.Errorf("linearGit: cannot parse sha %q", sha)
	}
	if n >= linearRoot {
		return "", fmt.Errorf("linearGit: %q is root", sha)
	}
	return fmt.Sprintf("c%d", n+1), nil
}

// FirstChildToward: "c<n>" → "c<n-1>" — one commit newer. "c0" is HEAD and
// has no child toward HEAD; ContractForward will hit the Len()==0 floor
// before reaching that anyway, but the method models it correctly.
func (linearGit) FirstChildToward(base, _ string) (string, error) {
	var n int
	if _, err := fmt.Sscanf(base, "c%d", &n); err != nil {
		return "", fmt.Errorf("linearGit: cannot parse sha %q", base)
	}
	if n <= 0 {
		return "", fmt.Errorf("linearGit: %q is HEAD, no child toward HEAD", base)
	}
	return fmt.Sprintf("c%d", n-1), nil
}

// Property: Len() always equals oldOffset - newOffset, and is never negative.
func TestScope_LenInvariant(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		if s.Len() != s.oldOffset-s.newOffset {
			t.Fatalf("Len()=%d, want oldOffset-newOffset=%d", s.Len(), s.oldOffset-s.newOffset)
		}
		if s.Len() < 0 {
			t.Fatalf("Len()=%d is negative; old=%d new=%d", s.Len(), s.oldOffset, s.newOffset)
		}
	})
}

// Property: scrubbed-ness is anchored to the endpoint SHAs, not to their
// distance from HEAD. The user pins a *commit*; a distance is a derived,
// HEAD-relative view of it that changes every time a commit lands.
//
// This test used to assert the offset-based disjunction, which is the bug:
// scrub back one, make a commit, and naturalOldOffset catches up to
// oldOffset, flipping IsScrubbed() to false with no user action.
//
// Scope note: this is a *consistency* check between `repin` and `IsScrubbed`
// over generated states — it restates the same predicate `repin` computes, so
// it would not catch a wrong choice of predicate on its own. The behavioral
// weight sits in the deterministic tests below, which drive real endpoint
// movement and assert what the user sees: `TestScope_ScrubSurvivesNewCommit`,
// `TestScope_PinSurvivesCommitsAndIndicatorTracksSHA`,
// `TestScope_ContractBackToNaturalUnpins`,
// `TestScope_NaturalCatchingUpToPinUnpins`. Its job here is to guarantee
// `arbitraryScope`-generated states are reachable ones, so the properties
// built on that generator describe the real state space.
func TestScope_IsScrubbedConditions(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		want := s.oldBase != s.naturalOldBase || s.newBase != s.naturalNewBase
		if got := s.IsScrubbed(); got != want {
			t.Fatalf("IsScrubbed()=%v, want %v (old=%q/%q new=%q/%q)",
				got, want, s.oldBase, s.naturalOldBase, s.newBase, s.naturalNewBase)
		}
	})
}

// Regression: scrub back one commit, then make a commit. The natural
// endpoint's *offset* from HEAD catches up with the scrubbed one, but the
// pinned commit is unchanged, so the scope must stay scrubbed and keep
// querying the pinned SHA.
func TestScope_ScrubSurvivesNewCommit(t *testing.T) {
	// HEAD is c0; the natural base is c0 itself (e.g. on main).
	s := scope{}
	s.SyncFromLoad("c0", "", 0, 0, "", -1)
	if err := s.ExtendBack(linearGit{}); err != nil {
		t.Fatalf("ExtendBack: %v", err)
	}
	if !s.IsScrubbed() {
		t.Fatal("scope should be scrubbed after ExtendBack")
	}
	pinned := s.oldBase // "c1"

	// A commit lands. HEAD moves, so every existing commit is one further
	// back: the natural base c0 is now HEAD~1, and the pinned c1 is HEAD~2.
	s.SyncFromLoad("c0", "", 1, 0, pinned, 2)

	if !s.IsScrubbed() {
		t.Fatalf("new commit un-scrubbed the scope (oldBase=%q natural=%q)", s.oldBase, s.naturalOldBase)
	}
	if s.OldBase() != pinned {
		t.Fatalf("pinned base moved: got %q, want %q", s.OldBase(), pinned)
	}
	h := s.Handle()
	if h == nil {
		t.Fatal("Handle() nil while scrubbed")
	}
	if h.headOffset != 2 {
		t.Fatalf("indicator says HEAD~%d, want HEAD~2 (the pinned commit's actual distance)", h.headOffset)
	}
}

// Property: a pin survives any number of new commits (each of which advances
// both the natural and the pinned endpoint's distance from HEAD), and the
// indicator always reports the pinned commit's actual distance.
func TestScope_PinSurvivesCommitsAndIndicatorTracksSHA(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		natOff := rapid.IntRange(0, 20).Draw(t, "natOff")
		var s scope
		s.SyncFromLoad(fmt.Sprintf("c%d", natOff), "", natOff, 0, "", -1)

		backs := rapid.IntRange(1, 10).Draw(t, "backs")
		for range backs {
			if err := s.ExtendBack(linearGit{}); err != nil {
				t.Fatalf("ExtendBack: %v", err)
			}
		}
		pinned := s.OldBase()
		pinnedDist := natOff + backs

		commits := rapid.IntRange(0, 15).Draw(t, "commits")
		for i := 1; i <= commits; i++ {
			// Each new commit pushes everything one further from HEAD. The
			// natural base SHA is unchanged.
			s.SyncFromLoad(fmt.Sprintf("c%d", natOff), "", natOff+i, 0, pinned, pinnedDist+i)
			if !s.IsScrubbed() {
				t.Fatalf("un-scrubbed after commit %d/%d", i, commits)
			}
			if s.OldBase() != pinned {
				t.Fatalf("pinned base changed after commit %d: %q != %q", i, s.OldBase(), pinned)
			}
			h := s.Handle()
			if h == nil || h.headOffset != pinnedDist+i {
				t.Fatalf("after commit %d indicator = %+v, want headOffset=%d", i, h, pinnedDist+i)
			}
		}

		// Un-scrub returns to natural, whatever HEAD has done since.
		s.Reset()
		if s.IsScrubbed() {
			t.Fatal("Reset left the scope scrubbed")
		}
		if s.OldBase() != s.naturalOldBase {
			t.Fatalf("Reset left oldBase=%q, natural=%q", s.OldBase(), s.naturalOldBase)
		}
		if s.Handle() != nil {
			t.Fatalf("Handle() non-nil after Reset: %+v", s.Handle())
		}
	})
}

// Property: contracting all the way forward onto the natural endpoint
// un-scrubs — the pin no longer names anything different from the default.
func TestScope_ContractBackToNaturalUnpins(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		natOff := rapid.IntRange(1, 20).Draw(t, "natOff")
		var s scope
		s.SyncFromLoad(fmt.Sprintf("c%d", natOff), "", natOff, 0, "", -1)
		backs := rapid.IntRange(1, 10).Draw(t, "backs")
		for range backs {
			if err := s.ExtendBack(linearGit{}); err != nil {
				t.Fatalf("ExtendBack: %v", err)
			}
		}
		if !s.IsScrubbed() {
			t.Fatal("expected scrubbed after ExtendBack")
		}
		for range backs {
			if err := s.ContractForward(linearGit{}); err != nil {
				t.Fatalf("ContractForward: %v", err)
			}
		}
		if s.IsScrubbed() {
			t.Fatalf("back at the natural endpoint but still scrubbed: %+v", s)
		}
	})
}

// Property: a natural endpoint that moves *onto* the pinned commit (rebase,
// base branch advanced) drops the pin rather than leaving a scrub indicator
// that describes the default position.
func TestScope_NaturalCatchingUpToPinUnpins(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		natOff := rapid.IntRange(0, 20).Draw(t, "natOff")
		var s scope
		s.SyncFromLoad(fmt.Sprintf("c%d", natOff), "", natOff, 0, "", -1)
		if err := s.ExtendBack(linearGit{}); err != nil {
			t.Fatalf("ExtendBack: %v", err)
		}
		pinned := s.OldBase()
		// A load reports the natural base is now the very commit we pinned.
		s.SyncFromLoad(pinned, "", natOff+1, 0, pinned, natOff+1)
		if s.IsScrubbed() {
			t.Fatalf("pin survived the natural endpoint catching up: %+v", s)
		}
		if s.Handle() != nil {
			t.Fatalf("indicator shown at the default position: %+v", s.Handle())
		}
	})
}

// Property: Handle() is non-nil iff IsScrubbed() and oldBase != "".
func TestScope_HandleNilCondition(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		want := s.IsScrubbed() && s.oldBase != ""
		got := s.Handle() != nil
		if got != want {
			t.Fatalf("Handle()!=nil = %v, want %v (scrubbed=%v old=%q)",
				got, want, s.IsScrubbed(), s.oldBase)
		}
	})
}

// Property: Handle().sha7 is the first 7 chars of oldBase (or all of it).
func TestScope_HandleSha7Truncation(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		h := s.Handle()
		if h == nil {
			return
		}
		want := len(s.oldBase)
		if want > 7 {
			want = 7
		}
		if len(h.sha7) != want {
			t.Fatalf("sha7=%q has len %d, want %d", h.sha7, len(h.sha7), want)
		}
		if h.sha7 != s.oldBase[:want] {
			t.Fatalf("sha7=%q, want prefix of oldBase=%q", h.sha7, s.oldBase)
		}
	})
}

// Property: Handle().headOffset is oldOffset.
func TestScope_HandleHeadOffset(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		h := s.Handle()
		if h == nil {
			return
		}
		if h.headOffset != s.oldOffset {
			t.Fatalf("headOffset=%d, want oldOffset=%d", h.headOffset, s.oldOffset)
		}
	})
}

// Property: ContractForward on an empty range is a no-op (the working tree
// is the floor and can't be crossed by the outer endpoint).
func TestScope_ContractForwardEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Build a scope with Len() == 0: oldOffset == newOffset.
		off := rapid.IntRange(0, 20).Draw(t, "off")
		s := scope{
			oldBase:          fmt.Sprintf("c%d", off),
			newBase:          fmt.Sprintf("c%d", off),
			oldOffset:        off,
			newOffset:        off,
			naturalOldBase:   fmt.Sprintf("c%d", off),
			naturalOldOffset: off,
		}
		before := s
		if err := s.ContractForward(linearGit{}); err != nil {
			t.Fatalf("ContractForward returned error on empty range: %v", err)
		}
		if s != before {
			t.Fatalf("ContractForward mutated empty scope: before=%+v after=%+v", before, s)
		}
	})
}

// Property: ExtendBack followed by ContractForward returns to the original
// state (on a linear history, with the range non-empty after the back).
func TestScope_ExtendBackThenContractForwardIsIdentity(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Start anywhere on the linear chain with a non-empty range so
		// ContractForward isn't a no-op.
		oldOff := rapid.IntRange(1, linearRoot-1).Draw(t, "oldOff")
		newOff := rapid.IntRange(0, oldOff-1).Draw(t, "newOff")
		s := scope{
			oldBase:          fmt.Sprintf("c%d", oldOff),
			newBase:          fmt.Sprintf("c%d", newOff),
			oldOffset:        oldOff,
			newOffset:        newOff,
			naturalOldBase:   fmt.Sprintf("c%d", oldOff),
			naturalNewBase:   fmt.Sprintf("c%d", newOff),
			naturalOldOffset: oldOff,
			naturalNewOffset: newOff,
		}
		before := s
		if err := s.ExtendBack(linearGit{}); err != nil {
			t.Fatalf("ExtendBack: %v", err)
		}
		if err := s.ContractForward(linearGit{}); err != nil {
			t.Fatalf("ContractForward: %v", err)
		}
		if s != before {
			t.Fatalf("Extend then Contract changed state:\nbefore=%+v\nafter =%+v", before, s)
		}
	})
}

// Property: ExtendBack always increments oldOffset by 1 and walks oldBase.
func TestScope_ExtendBackIncrementsOffset(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		oldOff := rapid.IntRange(0, linearRoot-1).Draw(t, "oldOff")
		s := scope{
			oldBase:          fmt.Sprintf("c%d", oldOff),
			oldOffset:        oldOff,
			naturalOldBase:   fmt.Sprintf("c%d", oldOff),
			naturalOldOffset: oldOff,
		}
		if err := s.ExtendBack(linearGit{}); err != nil {
			t.Fatalf("ExtendBack: %v", err)
		}
		if s.oldOffset != oldOff+1 {
			t.Fatalf("oldOffset after ExtendBack: %d, want %d", s.oldOffset, oldOff+1)
		}
		if s.oldBase != fmt.Sprintf("c%d", oldOff+1) {
			t.Fatalf("oldBase after ExtendBack: %q, want c%d", s.oldBase, oldOff+1)
		}
	})
}

// Property: Reset is idempotent and snaps to the natural endpoints.
func TestScope_ResetIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		s.Reset()
		want := s
		s.Reset()
		if s != want {
			t.Fatalf("Reset not idempotent:\nfirst =%+v\nsecond=%+v", want, s)
		}
		if s.oldBase != s.naturalOldBase || s.newBase != s.naturalNewBase {
			t.Fatalf("Reset did not snap to natural: scope=%+v", s)
		}
		if s.oldOffset != s.naturalOldOffset || s.newOffset != s.naturalNewOffset {
			t.Fatalf("Reset did not snap offsets: scope=%+v", s)
		}
		if s.IsScrubbed() {
			t.Fatalf("Reset left scope scrubbed: %+v", s)
		}
	})
}

// Property: ExtendBack at the root commit returns an error and does not
// mutate the scope.
func TestScope_ExtendBackAtRootErrors(t *testing.T) {
	s := scope{
		oldBase:          fmt.Sprintf("c%d", linearRoot),
		oldOffset:        linearRoot,
		naturalOldBase:   fmt.Sprintf("c%d", linearRoot),
		naturalOldOffset: linearRoot,
	}
	before := s
	if err := s.ExtendBack(linearGit{}); err == nil {
		t.Fatalf("ExtendBack at root returned nil error")
	}
	if s != before {
		t.Fatalf("ExtendBack at root mutated state: before=%+v after=%+v", before, s)
	}
}

// Property: ExtendBack on an unloaded scope (oldBase == "") returns
// errScopeUnloaded and does not mutate.
func TestScope_ExtendBackUnloaded(t *testing.T) {
	var s scope
	if err := s.ExtendBack(linearGit{}); err != errScopeUnloaded {
		t.Fatalf("ExtendBack unloaded: got %v, want errScopeUnloaded", err)
	}
	if s != (scope{}) {
		t.Fatalf("ExtendBack unloaded mutated state: %+v", s)
	}
}

// Property: SyncFromLoad at the natural position adopts loaded values;
// when scrubbed, preserves the scrubbed endpoints.
func TestScope_SyncFromLoadPreservesScrub(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		wasScrubbed := s.IsScrubbed()
		preScrubOld := s.oldBase
		preScrubNew := s.newBase
		preScrubOldOff := s.oldOffset
		preScrubNewOff := s.newOffset

		// Generate fresh natural endpoints (possibly different from current).
		nob := fmt.Sprintf("c%d", rapid.IntRange(0, linearRoot).Draw(t, "nob"))
		newOff := rapid.IntRange(0, linearRoot/2).Draw(t, "newOff")
		oldOff := rapid.IntRange(newOff, linearRoot).Draw(t, "oldOff")
		// -1 is in the domain so the "load carried no pin" branch gets direct
		// coverage: the cached distance must then be left alone rather than
		// stamped with a nonsense value.
		pinnedOff := rapid.IntRange(-1, linearRoot).Draw(t, "pinnedOff")
		// The SHA the measurement was taken against: the commit that is
		// actually pinned, some *other* commit (a load measured against an
		// earlier pin — the double-scrub case), or none at all.
		pinnedBase := rapid.SampledFrom([]string{preScrubOld, "some-other-sha", ""}).
			Draw(t, "pinnedBase")
		s.SyncFromLoad(nob, "", oldOff, newOff, pinnedBase, pinnedOff)

		// Natural fields must always update.
		if s.naturalOldBase != nob || s.naturalNewBase != "" {
			t.Fatalf("naturalBase fields not updated: %+v", s)
		}
		if s.naturalOldOffset != oldOff || s.naturalNewOffset != newOff {
			t.Fatalf("natural offsets not updated: %+v", s)
		}

		if wasScrubbed {
			// The pinned *commit* is preserved (that is what the user chose);
			// its distance from HEAD is refreshed from this load's own
			// measurement, since HEAD may have moved.
			if s.oldBase != preScrubOld || s.newBase != preScrubNew {
				t.Fatalf("scrubbed base fields changed: scope=%+v", s)
			}
			stillPinned := preScrubOld != nob || preScrubNew != ""
			if s.IsScrubbed() != stillPinned {
				t.Fatalf("IsScrubbed()=%v, want %v after sync (old=%q natural=%q)",
					s.IsScrubbed(), stillPinned, s.oldBase, nob)
			}
			// The load's measurement is adopted only when it was taken
			// against the commit that is still pinned. A measurement against
			// an earlier pin (or none) leaves the cached distance alone —
			// stamping it on would make the HEAD~N indicator read wrong.
			measurementApplies := pinnedOff >= 0 && pinnedBase != "" && pinnedBase == preScrubOld
			switch {
			case !stillPinned:
				if s.oldOffset != oldOff {
					t.Fatalf("un-pinned oldOffset=%d, want natural %d", s.oldOffset, oldOff)
				}
			case measurementApplies:
				if s.oldOffset != pinnedOff {
					t.Fatalf("pinned oldOffset=%d, want the load's measurement %d", s.oldOffset, pinnedOff)
				}
			default:
				if s.oldOffset != preScrubOldOff {
					t.Fatalf("oldOffset=%d changed to a distance measured against %q, not the pin %q; want the cached %d",
						s.oldOffset, pinnedBase, preScrubOld, preScrubOldOff)
				}
			}
			_ = preScrubNewOff
		} else {
			// Adopted natural values.
			if s.oldBase != nob || s.newBase != "" {
				t.Fatalf("unscrubbed: oldBase=%q newBase=%q, want %q/\"\"", s.oldBase, s.newBase, nob)
			}
			if s.oldOffset != oldOff || s.newOffset != newOff {
				t.Fatalf("unscrubbed: offsets=%d/%d, want %d/%d", s.oldOffset, s.newOffset, oldOff, newOff)
			}
		}
	})
}

// arbitraryScope draws a scope with internally-consistent offsets:
// oldOffset >= newOffset >= 0 for both current and natural endpoints.
// Bases are stable labels derived from offsets so equality is meaningful.
func arbitraryScope(t *rapid.T) scope {
	newOff := rapid.IntRange(0, 20).Draw(t, "newOff")
	oldOff := rapid.IntRange(newOff, 40).Draw(t, "oldOff")
	natNewOff := rapid.IntRange(0, 20).Draw(t, "natNewOff")
	natOldOff := rapid.IntRange(natNewOff, 40).Draw(t, "natOldOff")
	sc := scope{
		oldBase:          fmt.Sprintf("c%d", oldOff),
		newBase:          baseLabel(newOff),
		oldOffset:        oldOff,
		newOffset:        newOff,
		naturalOldBase:   fmt.Sprintf("c%d", natOldOff),
		naturalNewBase:   baseLabel(natNewOff),
		naturalOldOffset: natOldOff,
		naturalNewOffset: natNewOff,
	}
	// pinned is maintained by the endpoint movers as "an endpoint SHA differs
	// from its natural SHA"; keep the generator consistent with that so
	// properties describe reachable states.
	sc.repin()
	return sc
}

func baseLabel(off int) string {
	if off == 0 {
		return "" // newBase=="" means HEAD
	}
	return fmt.Sprintf("c%d", off)
}
