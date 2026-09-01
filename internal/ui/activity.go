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
// Rate-limit backoff is a third, sticky input to the PR interval: BumpRateLimited
// doubles it (up to prRefreshMax) and latches the result in rateLimitBackoff, so
// the per-tick ResetPRInterval cannot recompute it away from activity state
// (PROMPT.md:21, "respond to rate limits appropriately, backing off as needed").
// Only MarkPRSuccess — a fetch that actually came back — clears the latch.
type activityTracker struct {
	lastUIEvent      time.Time
	lastServerChange time.Time
	lastGitChange    time.Time
	prInterval       time.Duration
	gitInterval      time.Duration
	// rateLimitBackoff is the latched backoff interval while GitHub is rate
	// limiting us; zero when it isn't.
	rateLimitBackoff time.Duration
	// lastPRFetch is when the last PR fetch was dispatched. Only read while
	// backed off, to hold off a tick that was already in flight at the old
	// interval when the rate limit came back.
	lastPRFetch time.Time
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

// BumpRateLimited doubles the latched rate-limit backoff, up to prRefreshMax.
// The first 403 doubles the activity-derived interval; consecutive ones double
// the previous backoff. The latch survives ResetPRInterval, so the doubled
// interval actually governs the next scheduled fetch instead of being
// recomputed away.
func (a *activityTracker) BumpRateLimited(now time.Time) {
	base := a.rateLimitBackoff
	if base == 0 {
		base = a.ComputePRInterval(now)
	}
	a.rateLimitBackoff = min(base*2, prRefreshMax)
	a.prInterval = a.effectivePRInterval(now)
}

// effectivePRInterval is the interval the poll actually runs at: the
// activity-derived one, raised to the rate-limit backoff when one is latched.
// The backoff is a *floor*, never a replacement — treating it as a replacement
// made a rate-limited app poll faster than an idle healthy one (latch 60s,
// user goes idle, every tick resets 10m back down to 60s).
func (a *activityTracker) effectivePRInterval(now time.Time) time.Duration {
	return max(a.ComputePRInterval(now), a.rateLimitBackoff)
}

// MarkPRSuccess clears the rate-limit backoff and returns the PR interval to
// whatever activity implies. Called when a PR fetch comes back successfully.
func (a *activityTracker) MarkPRSuccess(now time.Time) {
	a.rateLimitBackoff = 0
	a.prInterval = a.ComputePRInterval(now)
}

// MarkPRFetch records that a PR fetch was just dispatched.
func (a *activityTracker) MarkPRFetch(now time.Time) { a.lastPRFetch = now }

// PRFetchDue reports whether a PR fetch may be issued now. It only holds
// anything back while a rate-limit backoff is latched: the tick that delivers
// a 403 was scheduled before the bump existed, so without this gate the very
// next fetch still went out at the old interval and the backoff first took
// effect a full cycle late.
func (a *activityTracker) PRFetchDue(now time.Time) bool {
	if a.rateLimitBackoff == 0 {
		return true
	}
	return !now.Before(a.lastPRFetch.Add(a.effectivePRInterval(now)))
}

// PRTickDelay returns how long the next PR tick should wait: the current
// interval normally, or — when a fetch is being held back by the backoff —
// just the remainder of it, so the held-back fetch goes out as soon as the
// backoff allows rather than a whole interval later.
func (a *activityTracker) PRTickDelay(now time.Time) time.Duration {
	if a.rateLimitBackoff > 0 {
		if remaining := a.lastPRFetch.Add(a.effectivePRInterval(now)).Sub(now); remaining > 0 {
			return min(remaining, a.prInterval)
		}
	}
	return a.prInterval
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

// ResetPRInterval recomputes and stores the PR interval, floored at any latched
// rate-limit backoff. Without the floor the per-tick reset overwrote every bump
// before any schedule read it, and the app kept polling at the active rate
// straight through sustained rate limiting; with the backoff *replacing* the
// computed interval instead of flooring it, going idle mid-backoff sped the
// poll back up.
func (a *activityTracker) ResetPRInterval(now time.Time) {
	a.prInterval = a.effectivePRInterval(now)
}

// ResetGitInterval recomputes and stores the git poll interval.
func (a *activityTracker) ResetGitInterval(now time.Time) {
	a.gitInterval = a.ComputeGitInterval(now)
}
