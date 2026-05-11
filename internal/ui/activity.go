package ui

import "time"

// activityTracker tracks user/server/filesystem activity timestamps and the
// resulting adaptive refresh intervals for PR polling and local git polling.
//
// Three event sources feed in: MarkUIEvent (any user interaction), MarkServerChange
// (server-side PR/CI data actually changed), MarkFSEvent (filesystem watcher fired).
// Two readers consume the timestamps: PRInterval (active if UI recent AND server
// recent; idle otherwise) and GitInterval (active if UI OR FS recent; idle when
// both quiet).
//
// BumpRateLimited doubles the PR interval (up to prRefreshMax) when GitHub
// returns a rate limit, distinct from idle backoff.
type activityTracker struct {
	lastUIEvent      time.Time
	lastServerChange time.Time
	lastGitChange    time.Time
	prInterval       time.Duration
	gitInterval      time.Duration
}

// newActivityTracker returns a tracker initialized to "everything just happened"
// with active-rate intervals.
func newActivityTracker(now time.Time) *activityTracker {
	return &activityTracker{
		lastUIEvent:      now,
		lastServerChange: now,
		lastGitChange:    now,
		prInterval:       prRefreshActive,
		gitInterval:      gitRefreshActive,
	}
}

func (a *activityTracker) MarkUIEvent(now time.Time)      { a.lastUIEvent = now }
func (a *activityTracker) MarkServerChange(now time.Time) { a.lastServerChange = now }
func (a *activityTracker) MarkFSEvent(now time.Time)      { a.lastGitChange = now }

func (a *activityTracker) PRInterval() time.Duration  { return a.prInterval }
func (a *activityTracker) GitInterval() time.Duration { return a.gitInterval }

// BumpRateLimited doubles the PR interval up to prRefreshMax. Called when the
// GitHub API returns a rate-limit error.
func (a *activityTracker) BumpRateLimited() {
	a.prInterval = min(a.prInterval*2, prRefreshMax)
}

// ComputePRInterval returns the PR interval implied by current activity state
// without mutating the tracker. idle OR stale → prRefreshIdle; otherwise
// prRefreshActive.
func (a *activityTracker) ComputePRInterval(now time.Time) time.Duration {
	idle := now.Sub(a.lastUIEvent) >= prIdleThreshold
	stale := now.Sub(a.lastServerChange) >= prStaleThreshold
	if idle || stale {
		return prRefreshIdle
	}
	return prRefreshActive
}

// ComputeGitInterval returns the git poll interval implied by current activity
// state without mutating the tracker. EITHER recent UI event OR recent FS event
// keeps the active rate.
func (a *activityTracker) ComputeGitInterval(now time.Time) time.Duration {
	uiIdle := now.Sub(a.lastUIEvent) >= prIdleThreshold
	fsQuiet := now.Sub(a.lastGitChange) >= gitActiveWindow
	if uiIdle && fsQuiet {
		return gitRefreshIdle
	}
	return gitRefreshActive
}

// ResetPRInterval recomputes and stores the PR interval.
func (a *activityTracker) ResetPRInterval(now time.Time) {
	a.prInterval = a.ComputePRInterval(now)
}

// ResetGitInterval recomputes and stores the git poll interval.
func (a *activityTracker) ResetGitInterval(now time.Time) {
	a.gitInterval = a.ComputeGitInterval(now)
}
