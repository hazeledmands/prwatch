package ui

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Property: a fresh tracker starts at active intervals.
func TestActivityInitialActive(t *testing.T) {
	a := newActivityTracker(time.Now())
	if a.PRInterval() != prRefreshActive {
		t.Errorf("PRInterval=%v want %v", a.PRInterval(), prRefreshActive)
	}
	if a.GitInterval() != gitRefreshActive {
		t.Errorf("GitInterval=%v want %v", a.GitInterval(), gitRefreshActive)
	}
}

// Property: when UI is recent and server is recent, PR interval is active.
// When either is past its threshold, interval is idle.
func TestActivityComputePRInterval(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		now := time.Now()
		uiAgo := time.Duration(rapid.IntRange(0, 30).Draw(t, "uiAgoMin")) * time.Minute
		serverAgo := time.Duration(rapid.IntRange(0, 48).Draw(t, "serverAgoH")) * time.Hour

		a := &activityTracker{
			lastUIEvent:      now.Add(-uiAgo),
			lastServerChange: now.Add(-serverAgo),
		}

		got := a.ComputePRInterval(now)
		idle := uiAgo >= prIdleThreshold
		stale := serverAgo >= prStaleThreshold
		want := prRefreshActive
		if idle || stale {
			want = prRefreshIdle
		}
		if got != want {
			t.Fatalf("uiAgo=%v serverAgo=%v got=%v want=%v", uiAgo, serverAgo, got, want)
		}
	})
}

// Property: git interval is active when EITHER UI or FS is recent.
func TestActivityComputeGitInterval(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		now := time.Now()
		uiAgo := time.Duration(rapid.IntRange(0, 30).Draw(t, "uiAgoMin")) * time.Minute
		fsAgo := time.Duration(rapid.IntRange(0, 10).Draw(t, "fsAgoMin")) * time.Minute

		a := &activityTracker{
			lastUIEvent:   now.Add(-uiAgo),
			lastGitChange: now.Add(-fsAgo),
		}

		got := a.ComputeGitInterval(now)
		uiIdle := uiAgo >= prIdleThreshold
		fsQuiet := fsAgo >= gitActiveWindow
		want := gitRefreshActive
		if uiIdle && fsQuiet {
			want = gitRefreshIdle
		}
		if got != want {
			t.Fatalf("uiAgo=%v fsAgo=%v got=%v want=%v", uiAgo, fsAgo, got, want)
		}
	})
}

// Property: BumpRateLimited at most doubles, capped at prRefreshMax.
func TestActivityBumpRateLimited(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		startMult := rapid.IntRange(1, 32).Draw(t, "startMult")
		start := time.Duration(startMult) * prRefreshActive
		if start > prRefreshMax {
			start = prRefreshMax
		}
		a := &activityTracker{prInterval: start}
		a.BumpRateLimited()

		want := start * 2
		if want > prRefreshMax {
			want = prRefreshMax
		}
		if a.PRInterval() != want {
			t.Fatalf("start=%v after bump %v want %v", start, a.PRInterval(), want)
		}
	})
}

// Property: any MarkUIEvent followed by Compute returns active intervals.
func TestActivityMarkUIEventResets(t *testing.T) {
	now := time.Now()
	a := &activityTracker{
		lastUIEvent:      now.Add(-time.Hour),
		lastServerChange: now.Add(-time.Hour),
		lastGitChange:    now.Add(-time.Hour),
	}
	a.MarkUIEvent(now)
	if got := a.ComputeGitInterval(now); got != gitRefreshActive {
		t.Errorf("after MarkUIEvent: GitInterval=%v want %v", got, gitRefreshActive)
	}
}
