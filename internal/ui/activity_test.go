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

// Property: consecutive rate limits double the backoff, capped at prRefreshMax.
// For an active user the effective interval is the backoff itself.
func TestActivityBumpRateLimited(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		now := time.Now()
		bumps := rapid.IntRange(1, 8).Draw(t, "bumps")
		a := newActivityTracker(now)

		want := prRefreshActive
		for i := range bumps {
			want = min(want*2, prRefreshMax)
			a.BumpRateLimited(now)
			if a.PRInterval() != want {
				t.Fatalf("after bump %d: interval %v want %v", i+1, a.PRInterval(), want)
			}
		}
	})
}

// The backoff is a floor on the poll interval, not a replacement for it: a
// rate-limited app must never poll *faster* than an idle healthy one. Before
// the fix, latching 60s and then going idle made every tick reset the interval
// to 60s — 10x more requests than the idle rate, while rate limited.
func TestActivityBackoffNeverBelowActivityInterval(t *testing.T) {
	now := time.Now()
	a := newActivityTracker(now)

	a.BumpRateLimited(now) // active user, one 403 → 60s
	if got := a.PRInterval(); got != prRefreshActive*2 {
		t.Fatalf("interval after 403 = %v, want %v", got, prRefreshActive*2)
	}

	// The user walks away. Ticks keep firing.
	now = now.Add(prIdleThreshold + time.Minute)
	a.ResetPRInterval(now)
	if got := a.PRInterval(); got != prRefreshIdle {
		t.Errorf("idle interval while backed off = %v, want the idle rate %v", got, prRefreshIdle)
	}
	if d := a.PRTickDelay(now); d != prRefreshIdle {
		t.Errorf("PRTickDelay while idle and backed off = %v, want %v", d, prRefreshIdle)
	}

	// Backing off further from the idle rate still only goes up.
	a.BumpRateLimited(now)
	if got := a.PRInterval(); got < prRefreshIdle {
		t.Errorf("interval after a 403 while idle = %v, want >= %v", got, prRefreshIdle)
	}

	// Coming back clears the latch and returns to the active rate.
	a.MarkUIEvent(now)
	a.MarkPRSuccess(now)
	if got := a.PRInterval(); got != prRefreshActive {
		t.Errorf("interval after success = %v, want %v", got, prRefreshActive)
	}
}

// Properties of the rate-limit backoff state machine (CODE_REVIEW A3), driven
// through an arbitrary interleaving of the operations the tick loop performs:
//
//   - the interval never decreases while consecutive rate limits arrive,
//   - it never exceeds prRefreshMax and never drops below the activity-derived
//     interval while backed off — in particular, going idle mid-backoff must
//     still slow the poll to the idle rate, never speed it back up,
//   - only a successful fetch clears the backoff,
//   - a tick's ResetPRInterval cannot recompute an active backoff away.
//
// Time is an explicit op ("advance"), so a run can go idle or stale between
// operations; without that the idle-vs-backoff interaction is unreachable.
func TestActivityRateLimitBackoffProperties(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		now := time.Now()
		a := newActivityTracker(now)

		ops := rapid.SliceOfN(
			rapid.SampledFrom([]string{"bump", "tick", "success", "ui", "fetch", "advance", "goIdle"}),
			1, 40,
		).Draw(t, "ops")
		// Durations that land on both sides of every threshold: sub-interval,
		// one active interval, past the 10m idle threshold, past 24h stale.
		steps := []time.Duration{time.Second, prRefreshActive, 11 * time.Minute, 25 * time.Hour}
		backedOff := false

		for i, op := range ops {
			switch op {
			case "bump":
				// The backoff itself grows with every consecutive 403 until it
				// hits the cap. (The *interval* can legitimately fall across a
				// bump — a UI event since the last one moves the activity
				// interval from idle back to active — so the monotonicity
				// invariant belongs to the backoff, not the interval.)
				beforeBackoff := a.rateLimitBackoff
				a.BumpRateLimited(now)
				backedOff = true
				if a.rateLimitBackoff <= beforeBackoff && a.rateLimitBackoff != prRefreshMax {
					t.Fatalf("op %d (bump): backoff %v → %v, want growth (or the %v cap)",
						i, beforeBackoff, a.rateLimitBackoff, prRefreshMax)
				}
				if got, want := a.PRInterval(), a.ComputePRInterval(now); got < want {
					t.Fatalf("op %d (bump): interval %v below the activity interval %v", i, got, want)
				}
			case "tick":
				a.ResetPRInterval(now)
			case "success":
				a.MarkPRSuccess(now)
				backedOff = false
				if want := a.ComputePRInterval(now); a.PRInterval() != want {
					t.Fatalf("op %d (success): interval %v, want the activity-derived %v", i, a.PRInterval(), want)
				}
			case "ui":
				a.MarkUIEvent(now)
			case "fetch":
				a.MarkPRFetch(now)
				if backedOff && a.PRFetchDue(now) {
					t.Fatalf("op %d (fetch): another fetch is due immediately while backed off", i)
				}
			case "advance":
				now = now.Add(rapid.SampledFrom(steps).Draw(t, "step"))
			case "goIdle":
				// Long enough with no UI event that ComputePRInterval flips idle.
				now = now.Add(prIdleThreshold + time.Minute)
			}

			got := a.PRInterval()
			if got > prRefreshMax {
				t.Fatalf("op %d (%s): interval %v exceeds cap %v", i, op, got, prRefreshMax)
			}
			if got <= 0 {
				t.Fatalf("op %d (%s): non-positive interval %v", i, op, got)
			}
			// A tick always leaves the interval at the effective one: never
			// below the latched backoff, and never below what activity implies
			// — a rate-limited app must not poll faster than an idle healthy one.
			if op == "tick" {
				want := a.ComputePRInterval(now)
				if backedOff && got < want {
					t.Fatalf("op %d (tick): backed-off interval %v below the activity interval %v", i, got, want)
				}
				if !backedOff && got != want {
					t.Fatalf("op %d (tick): interval %v, want the activity-derived %v", i, got, want)
				}
			}
			// The next tick never waits longer than the interval, and never
			// schedules for the past.
			if d := a.PRTickDelay(now); d <= 0 || d > got {
				t.Fatalf("op %d (%s): PRTickDelay %v outside (0, %v]", i, op, d, got)
			}
			if !backedOff && !a.PRFetchDue(now) {
				t.Fatalf("op %d (%s): fetch withheld while not rate limited", i, op)
			}
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
