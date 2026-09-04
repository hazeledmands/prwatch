package ui

import (
	"strings"
	"testing"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

const rwxURLB = "https://cloud.rwx.com/mint/org/runs/bbb"

// A permanently broken `rwx` (missing binary, revoked token) fails every time
// it is asked. Errors are dropped on every PR poll, so before backoff a broken
// binary cost a full `rwx runs` + per-task `rwx logs` subprocess chain every
// ~30s for as long as a failing check stayed selected. The poll-driven
// invalidation must respect a growing retry delay.
func TestRWXFetcher_PollInvalidationRespectsBackoff(t *testing.T) {
	f := newRWXFetcher()
	t0 := time.Unix(1_700_000_000, 0)

	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("rwx: command not found")}, t0)

	// The poll that arrives right behind the failure must not retry.
	f.InvalidateReadyErrors(t0.Add(time.Second))
	if _, cached := f.Lookup(rwxCheck(rwxURL)); !cached {
		t.Fatal("a poll inside the backoff window must leave the cached error in place")
	}

	// Once the window has passed, the same poll path retries.
	f.InvalidateReadyErrors(t0.Add(rwxRetryBase))
	if _, cached := f.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatal("a poll past the backoff window must invalidate the error so it refetches")
	}
}

// The delay schedule: exponential from the base, monotonic, and clamped at the
// cap so a check that has been failing all afternoon is still retried
// occasionally rather than never.
func TestRWXFetcher_RetryDelaySchedule(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 1, want: 30 * time.Second},
		{failures: 2, want: 1 * time.Minute},
		{failures: 3, want: 2 * time.Minute},
		{failures: 4, want: 4 * time.Minute},
		{failures: 5, want: 8 * time.Minute},
		{failures: 6, want: 10 * time.Minute}, // clamped
		{failures: 30, want: 10 * time.Minute},
		{failures: 500, want: 10 * time.Minute}, // no shift overflow
	}
	for _, tt := range tests {
		if got := rwxRetryDelay(tt.failures); got != tt.want {
			t.Errorf("rwxRetryDelay(%d) = %s, want %s", tt.failures, got, tt.want)
		}
	}

	// Monotonic non-decreasing, and never past the cap.
	prev := time.Duration(0)
	for n := 1; n <= 40; n++ {
		got := rwxRetryDelay(n)
		if got < prev {
			t.Fatalf("rwxRetryDelay(%d) = %s, less than rwxRetryDelay(%d) = %s", n, got, n-1, prev)
		}
		if got > rwxRetryCap {
			t.Fatalf("rwxRetryDelay(%d) = %s, past the cap %s", n, got, rwxRetryCap)
		}
		prev = got
	}
}

// Repeated failures escalate: each one pushes the next eligible retry further
// out, so a broken binary is asked less and less often.
func TestRWXFetcher_ConsecutiveFailuresEscalate(t *testing.T) {
	f := newRWXFetcher()
	now := time.Unix(1_700_000_000, 0)

	for attempt := 1; attempt <= 4; attempt++ {
		f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")}, now)
		want := rwxRetryDelay(attempt)

		// One tick short of the window: still suppressed.
		f.InvalidateReadyErrors(now.Add(want - time.Millisecond))
		if _, cached := f.Lookup(rwxCheck(rwxURL)); !cached {
			t.Fatalf("attempt %d: retried after %s, want a %s wait", attempt, want-time.Millisecond, want)
		}

		// At the window: eligible.
		now = now.Add(want)
		f.InvalidateReadyErrors(now)
		if _, cached := f.Lookup(rwxCheck(rwxURL)); cached {
			t.Fatalf("attempt %d: still suppressed after the full %s window", attempt, want)
		}
	}
}

// A success clears the schedule: the next failure starts over at the base
// delay rather than inheriting the escalation from an outage that has ended.
func TestRWXFetcher_SuccessResetsBackoff(t *testing.T) {
	f := newRWXFetcher()
	now := time.Unix(1_700_000_000, 0)

	for range 4 {
		f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")}, now)
	}
	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "log body"}, now)

	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")}, now)
	f.InvalidateReadyErrors(now.Add(rwxRetryBase))
	if _, cached := f.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatalf("after a success the schedule must restart at %s, not stay escalated", rwxRetryBase)
	}
}

// Backoff throttles the automatic retries only. The refresh key is the user
// asking for a retry now, and it has to win — the clear-on-refresh design is
// what the backoff composes with, not what it replaces.
func TestRWXFetcher_ManualRefreshOverridesBackoff(t *testing.T) {
	f := newRWXFetcher()
	now := time.Unix(1_700_000_000, 0)

	for range 5 { // deep into the escalation, well past the cap's doorstep
		f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")}, now)
	}

	f.ForceRetryErrors()
	if _, cached := f.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatal("an explicit refresh must clear the cached error regardless of the backoff window")
	}

	// And it resets the schedule, so the next failure waits only the base.
	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")}, now)
	f.InvalidateReadyErrors(now.Add(rwxRetryBase))
	if _, cached := f.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatalf("an explicit refresh must reset the escalation, so the next wait is %s", rwxRetryBase)
	}
}

// A successful log is expensive and stays valid for the run it describes, so
// backoff must not touch it.
func TestRWXFetcher_BackoffLeavesGoodLogsAlone(t *testing.T) {
	f := newRWXFetcher()
	now := time.Unix(1_700_000_000, 0)

	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "good log"}, now)
	f.InvalidateReadyErrors(now.Add(time.Hour))
	f.ForceRetryErrors()

	if content, cached := f.Lookup(rwxCheck(rwxURL)); !cached || content != "good log" {
		t.Fatalf("content=%q cached=%v, want the cached log to survive", content, cached)
	}
}

// Every push mints new RWX run URLs, and nothing ever removed the old ones:
// full log bodies for runs that are no longer on the PR accumulated for the
// life of the session.
func TestRWXFetcher_PruneDropsVanishedURLs(t *testing.T) {
	f := newRWXFetcher()
	now := time.Unix(1_700_000_000, 0)
	const gone = "https://cloud.rwx.com/mint/org/runs/stale"

	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "kept log"}, now)
	f.Apply(rwxLogMsg{checkURL: gone, log: "stale log"}, now)
	f.Apply(rwxLogMsg{checkURL: rwxURLB, err: errFake("boom")}, now)

	// A fresh push: only rwxURL is still on the PR.
	f.Prune([]gitpkg.CICheck{rwxCheck(rwxURL), {Name: "lint", URL: "https://example.com/x"}})

	if content, cached := f.Lookup(rwxCheck(rwxURL)); !cached || content != "kept log" {
		t.Errorf("a URL still in ciChecks was evicted: content=%q cached=%v", content, cached)
	}
	if _, ok := f.cache[gone]; ok {
		t.Error("the vanished run's log body is still cached")
	}
	if _, ok := f.cache[rwxURLB]; ok {
		t.Error("the vanished run's error entry is still cached")
	}
	if f.failed[rwxURLB] {
		t.Error("the vanished run is still marked failed")
	}
	if _, ok := f.backoff[rwxURLB]; ok {
		t.Error("the vanished run still carries a backoff schedule")
	}
}

// Pruning must not strand the pending slot on a check that is no longer on the
// PR: Cmd would dispatch a fetch for a run nothing can display.
func TestRWXFetcher_PruneClearsStalePending(t *testing.T) {
	f := newRWXFetcher()

	f.Lookup(rwxCheck(rwxURL))
	if f.pending == nil {
		t.Fatal("setup: the miss should have staged a pending fetch")
	}
	f.Prune([]gitpkg.CICheck{rwxCheck(rwxURLB)})
	if f.pending != nil {
		t.Error("pending still points at a check that has left ciChecks")
	}
	if cmd := f.Cmd(&rwxStubGit{}); cmd != nil {
		t.Error("a pruned pending check must not dispatch")
	}
}

// In-flight marks are pruned too, so the set cannot grow without bound across
// a long session of pushes. The result that lands afterwards is discarded
// rather than resurrecting a cache entry for a run that has left the PR.
func TestRWXFetcher_PruneDiscardsLateResult(t *testing.T) {
	f := newRWXFetcher()
	now := time.Unix(1_700_000_000, 0)

	f.Lookup(rwxCheck(rwxURL))
	f.Cmd(&rwxStubGit{})
	if !f.InFlight(rwxURL) {
		t.Fatal("setup: the dispatch should have marked the URL in flight")
	}

	f.Prune([]gitpkg.CICheck{rwxCheck(rwxURLB)})
	if f.InFlight(rwxURL) {
		t.Error("a pruned URL must not stay marked in flight")
	}

	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "log body"}, now)
	if _, ok := f.cache[rwxURL]; ok {
		t.Error("a result for a pruned URL must be discarded, not cached")
	}
}

// Fresh PR check data is the moment stale runs can be identified, so the poll
// arms have to prune as they adopt it.
func TestPRRefresh_PrunesVanishedRWXRuns(t *testing.T) {
	m := NewModel("/tmp", testGit())
	now := time.Unix(1_700_000_000, 0)

	m.rwxFetcher.Apply(rwxLogMsg{checkURL: rwxURL, log: "log body"}, now)

	res, _ := m.Update(prRefreshMsg{
		prInfo:   gitpkg.PRInfoResult{Number: 10, Title: "test"},
		ciChecks: []gitpkg.CICheck{{Name: "test", URL: rwxURLB}},
	})
	m = res.(*Model)

	if _, ok := m.rwxFetcher.cache[rwxURL]; ok {
		t.Error("fresh CI check data must prune cached logs for runs no longer on the PR")
	}
}

// The refresh key stays the retry affordance, now against a backoff that would
// otherwise suppress the retry.
func TestRefreshKey_OverridesRWXBackoff(t *testing.T) {
	m := NewModel("/tmp", testGit())
	now := time.Unix(1_700_000_000, 0)

	for range 5 {
		m.rwxFetcher.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("connection reset")}, now)
	}
	if content, cached := m.rwxFetcher.Lookup(rwxCheck(rwxURL)); !cached || !strings.Contains(content, "connection reset") {
		t.Fatalf("setup: content=%q cached=%v", content, cached)
	}

	res, _ := m.Update(keyMsg("r"))
	m = res.(*Model)

	if _, cached := m.rwxFetcher.Lookup(rwxCheck(rwxURL)); cached {
		t.Error("the refresh key must clear cached RWX errors even deep into the backoff schedule")
	}
}
