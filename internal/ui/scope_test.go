package ui

import (
	"testing"

	"pgregory.net/rapid"
)

// Property: returns nil iff base equals naturalBase or either is empty.
func TestScopeHandleNilCondition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := rapid.SampledFrom([]string{"", "abc1234567890", "deadbeefdeadbeef", "natural"}).Draw(t, "base")
		natural := rapid.SampledFrom([]string{"", "abc1234567890", "natural", "another"}).Draw(t, "natural")
		commitCount := rapid.IntRange(0, 1000).Draw(t, "commitCount")

		got := scopeHandleFromBase(base, natural, commitCount)
		shouldBeNil := base == "" || natural == "" || base == natural
		if (got == nil) != shouldBeNil {
			t.Fatalf("base=%q natural=%q: got=%v want-nil=%v", base, natural, got, shouldBeNil)
		}
	})
}

// Property: sha7 is min(7, len(base)) when non-nil.
func TestScopeHandleSha7Length(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := rapid.StringMatching(`[a-f0-9]{1,40}`).Draw(t, "base")
		natural := "different-natural-base"
		got := scopeHandleFromBase(base, natural, 0)
		if got == nil {
			return
		}
		want := len(base)
		if want > 7 {
			want = 7
		}
		if len(got.sha7) != want {
			t.Fatalf("base=%q sha7=%q want len %d", base, got.sha7, want)
		}
	})
}

// Property: headOffset equals commitCount when non-nil.
func TestScopeHandleHeadOffset(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		commitCount := rapid.IntRange(0, 10000).Draw(t, "commitCount")
		got := scopeHandleFromBase("scrubbed-base", "natural-base", commitCount)
		if got == nil {
			t.Fatalf("expected non-nil for distinct bases")
		}
		if got.headOffset != commitCount {
			t.Fatalf("headOffset=%d want %d", got.headOffset, commitCount)
		}
	})
}
