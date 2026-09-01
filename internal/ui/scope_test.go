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

// Property: IsScrubbed is the disjunction of offset-mismatches.
func TestScope_IsScrubbedConditions(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := arbitraryScope(t)
		want := s.oldOffset != s.naturalOldOffset || s.newOffset != s.naturalNewOffset
		if got := s.IsScrubbed(); got != want {
			t.Fatalf("IsScrubbed()=%v, want %v (old=%d/%d new=%d/%d)",
				got, want, s.oldOffset, s.naturalOldOffset, s.newOffset, s.naturalNewOffset)
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
		s.SyncFromLoad(nob, "", oldOff, newOff)

		// Natural fields must always update.
		if s.naturalOldBase != nob || s.naturalNewBase != "" {
			t.Fatalf("naturalBase fields not updated: %+v", s)
		}
		if s.naturalOldOffset != oldOff || s.naturalNewOffset != newOff {
			t.Fatalf("natural offsets not updated: %+v", s)
		}

		if wasScrubbed {
			// Scrub state preserved.
			if s.oldBase != preScrubOld || s.newBase != preScrubNew {
				t.Fatalf("scrubbed base fields changed: scope=%+v", s)
			}
			if s.oldOffset != preScrubOldOff || s.newOffset != preScrubNewOff {
				t.Fatalf("scrubbed offsets changed: scope=%+v", s)
			}
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
	return scope{
		oldBase:          fmt.Sprintf("c%d", oldOff),
		newBase:          baseLabel(newOff),
		oldOffset:        oldOff,
		newOffset:        newOff,
		naturalOldBase:   fmt.Sprintf("c%d", natOldOff),
		naturalNewBase:   baseLabel(natNewOff),
		naturalOldOffset: natOldOff,
		naturalNewOffset: natNewOff,
	}
}

func baseLabel(off int) string {
	if off == 0 {
		return "" // newBase=="" means HEAD
	}
	return fmt.Sprintf("c%d", off)
}
