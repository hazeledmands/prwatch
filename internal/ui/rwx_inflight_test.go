package ui

import (
	"strings"
	"testing"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

const rwxURL = "https://cloud.rwx.com/mint/org/runs/abc123"

// rwxCheck builds a CI check pointing at an RWX run.
func rwxCheck(url string) gitpkg.CICheck {
	return gitpkg.CICheck{Name: "test", URL: url}
}

// stubGit satisfies enough of GitDataSource for Cmd to be non-nil. The fetch
// itself is never run in these tests — only the dispatch bookkeeping matters.
type rwxStubGit struct{ GitDataSource }

// Lookup runs on every render of a selected CI check, so while a fetch is in
// flight it kept re-staging pending, and each render dispatched another full
// `rwx runs` + per-task `rwx logs` subprocess chain over the same run.
func TestRWXFetcher_NoDuplicateDispatchWhileInFlight(t *testing.T) {
	f := newRWXFetcher()
	git := &rwxStubGit{}

	if _, cached := f.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatal("first Lookup should miss")
	}
	if cmd := f.Cmd(git); cmd == nil {
		t.Fatal("first Cmd should dispatch")
	}

	// Every subsequent render while the fetch is outstanding.
	for i := range 5 {
		content, cached := f.Lookup(rwxCheck(rwxURL))
		if cached {
			t.Fatalf("render %d: nothing is cached yet", i)
		}
		if content != rwxLoadingPlaceholder {
			t.Fatalf("render %d: content = %q, want the loading placeholder", i, content)
		}
		if cmd := f.Cmd(git); cmd != nil {
			t.Fatalf("render %d dispatched a second fetch for a URL already in flight", i)
		}
	}

	// The result clears the in-flight mark and caches the log.
	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "log body"}, time.Unix(1_700_000_000, 0))
	if f.InFlight(rwxURL) {
		t.Fatal("Apply must clear the in-flight mark")
	}
	content, cached := f.Lookup(rwxCheck(rwxURL))
	if !cached || content != "log body" {
		t.Fatalf("after Apply: content=%q cached=%v", content, cached)
	}
	if cmd := f.Cmd(git); cmd != nil {
		t.Fatal("a cache hit must not dispatch")
	}
}

// A transient network failure used to stick for the entire session: Apply
// cached the error string under the check URL and nothing ever invalidated it,
// so the pane showed "Error fetching RWX logs: ..." until prwatch was
// restarted. An explicit refresh has to clear it and let the fetch retry.
func TestRWXFetcher_ErrorsAreRefetchableAfterInvalidation(t *testing.T) {
	f := newRWXFetcher()
	git := &rwxStubGit{}

	f.Lookup(rwxCheck(rwxURL))
	f.Cmd(git)
	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("connection reset")}, time.Unix(1_700_000_000, 0))

	// The error is cached and displayed — no refetch storm in the meantime.
	content, cached := f.Lookup(rwxCheck(rwxURL))
	if !cached || !strings.Contains(content, "connection reset") {
		t.Fatalf("error should be cached and shown: content=%q cached=%v", content, cached)
	}
	if cmd := f.Cmd(git); cmd != nil {
		t.Fatal("a cached error must not refetch on every render")
	}

	f.ForceRetryErrors()

	// Now it retries.
	content, cached = f.Lookup(rwxCheck(rwxURL))
	if cached {
		t.Fatal("an invalidated error must read as a cache miss")
	}
	if content != rwxLoadingPlaceholder {
		t.Fatalf("content = %q, want the loading placeholder", content)
	}
	if cmd := f.Cmd(git); cmd == nil {
		t.Fatal("an invalidated error must be refetchable")
	}
}

// Invalidation is scoped to failures: a good log is expensive to fetch and
// stays valid for the run it describes.
func TestRWXFetcher_InvalidateErrorsKeepsGoodLogs(t *testing.T) {
	f := newRWXFetcher()
	const other = "https://cloud.rwx.com/mint/org/runs/def456"

	now := time.Unix(1_700_000_000, 0)
	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "good log"}, now)
	f.Apply(rwxLogMsg{checkURL: other, err: errFake("boom")}, now)
	f.InvalidateReadyErrors(now.Add(rwxRetryCap))

	if content, cached := f.Lookup(rwxCheck(rwxURL)); !cached || content != "good log" {
		t.Fatalf("successful log was dropped: content=%q cached=%v", content, cached)
	}
	if _, cached := f.Lookup(rwxCheck(other)); cached {
		t.Fatal("errored entry should have been invalidated")
	}
}

// An errored fetch must not leave the URL marked in flight, or it can never be
// retried at all.
func TestRWXFetcher_ErrorClearsInFlight(t *testing.T) {
	f := newRWXFetcher()
	f.Lookup(rwxCheck(rwxURL))
	f.Cmd(&rwxStubGit{})
	if !f.InFlight(rwxURL) {
		t.Fatal("dispatch should mark the URL in flight")
	}
	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")}, time.Unix(1_700_000_000, 0))
	if f.InFlight(rwxURL) {
		t.Fatal("an errored Apply must clear the in-flight mark")
	}
}

// Property: across any interleaving of renders, dispatches, results, polls,
// forced retries and prunes, at most one fetch per URL is ever outstanding,
// the in-flight set drains once results arrive, a poll inside the backoff
// window changes nothing, and no map ever holds a URL that has left ciChecks.
//
// Time is advanced only by zero or by more than the cap, so the model never has
// to reproduce the delay formula — the schedule itself is pinned by
// TestRWXFetcher_RetryDelaySchedule.
func TestProperty_RWXFetcher(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := newRWXFetcher()
		git := &rwxStubGit{}
		urls := []string{
			"https://cloud.rwx.com/mint/org/runs/a",
			"https://cloud.rwx.com/mint/org/runs/b",
			"https://cloud.rwx.com/mint/org/runs/c",
		}
		now := time.Unix(1_700_000_000, 0)

		// outstanding tracks dispatches we have not yet fed a result for.
		outstanding := map[string]int{}
		// cached/failedURL mirror what we expect the fetcher to be holding, and
		// keep is the current ciChecks URL set once anything has been pruned.
		cached := map[string]bool{}
		failedURL := map[string]bool{}
		// escalating is the URLs carrying a retry schedule. It outlives the
		// cached error on purpose: a poll clears the error to allow one retry
		// but keeps the failure count, so a URL that fails again waits the next
		// step up rather than the base again. Only a success, a forced retry or
		// a prune ends the escalation.
		escalating := map[string]bool{}
		keep := map[string]bool{}
		everPruned := false

		accepts := func(u string) bool { return !everPruned || keep[u] }

		n := rapid.IntRange(1, 60).Draw(t, "steps")
		for i := 0; i < n; i++ {
			url := rapid.SampledFrom(urls).Draw(t, "url")
			op := rapid.SampledFrom([]string{
				"render", "result", "poll", "poll-later", "force", "prune",
			}).Draw(t, "op")

			switch op {
			case "render":
				// One render: Lookup then Cmd, exactly as the Update wrapper does.
				f.Lookup(rwxCheck(url))
				if cmd := f.Cmd(git); cmd != nil {
					outstanding[url]++
				}
			case "result":
				if outstanding[url] == 0 {
					continue
				}
				outstanding[url]--
				failed := rapid.Bool().Draw(t, "failed")
				if failed {
					f.Apply(rwxLogMsg{checkURL: url, err: errFake("boom")}, now)
				} else {
					f.Apply(rwxLogMsg{checkURL: url, log: "body"}, now)
				}
				if accepts(url) {
					cached[url] = true
					failedURL[url] = failed
					escalating[url] = failed
				}
			case "poll":
				// Inside every open window: the base delay is positive, so a
				// poll at the same instant as the failure can retry nothing.
				f.InvalidateReadyErrors(now)
			case "poll-later":
				now = now.Add(rwxRetryCap + time.Second)
				f.InvalidateReadyErrors(now)
				for u := range failedURL {
					if failedURL[u] {
						cached[u] = false
						failedURL[u] = false
					}
				}
			case "force":
				f.ForceRetryErrors()
				// The contract, not the implementation: an explicit refresh
				// resets *all* escalation, including a URL whose cached error a
				// poll has already cleared while keeping its failure count.
				// Modelling this as "clear escalation only where an error is
				// currently cached" would just restate whatever the code does.
				for _, u := range urls {
					if failedURL[u] {
						cached[u] = false
						failedURL[u] = false
					}
					escalating[u] = false
				}
			case "prune":
				var checks []gitpkg.CICheck
				keep = map[string]bool{}
				for _, u := range urls {
					if rapid.Bool().Draw(t, "keep") {
						keep[u] = true
						checks = append(checks, rwxCheck(u))
					}
				}
				everPruned = true
				f.Prune(checks)
				for _, u := range urls {
					if !keep[u] {
						cached[u] = false
						failedURL[u] = false
						escalating[u] = false
						// outstanding is untouched: a prune evicts stored
						// entries but cannot cancel a running fetch, so the
						// in-flight mark stays until its result lands.
					}
				}
			}

			for _, u := range urls {
				if outstanding[u] > 1 {
					t.Fatalf("%s has %d concurrent fetches outstanding", u, outstanding[u])
				}
				if f.InFlight(u) != (outstanding[u] == 1) {
					t.Fatalf("%s: InFlight()=%v but %d outstanding", u, f.InFlight(u), outstanding[u])
				}
				if _, ok := f.cache[u]; ok != cached[u] {
					t.Fatalf("after %s: %s cached=%v, want %v", op, u, ok, cached[u])
				}
				if f.failed[u] != failedURL[u] {
					t.Fatalf("after %s: %s failed=%v, want %v", op, u, f.failed[u], failedURL[u])
				}
				// A URL that has succeeded, been forced or been pruned carries
				// no schedule: a stale window would otherwise suppress a retry
				// of something that is no longer failing.
				if _, ok := f.backoff[u]; ok != escalating[u] {
					t.Fatalf("after %s: %s backoff present=%v, want %v", op, u, ok, escalating[u])
				}
			}
			if everPruned {
				// Stored state only. inFlight is exempt by contract: it tracks
				// live fetches, which a prune cannot cancel.
				for u := range f.failed {
					if !keep[u] {
						t.Fatalf("after %s: %s survived a prune that dropped it", op, u)
					}
				}
				for u := range f.cache {
					if !keep[u] {
						t.Fatalf("after %s: %s cache entry survived a prune that dropped it", op, u)
					}
				}
				if f.pending != nil && !keep[f.pending.URL] {
					t.Fatalf("after %s: pending %s survived a prune that dropped it", op, f.pending.URL)
				}
			}
		}

		// Drain: feeding the remaining results must empty the in-flight set.
		for _, u := range urls {
			for outstanding[u] > 0 {
				outstanding[u]--
				f.Apply(rwxLogMsg{checkURL: u, log: "body"}, now)
			}
		}
		for _, u := range urls {
			if f.InFlight(u) {
				t.Fatalf("%s still marked in flight after draining", u)
			}
		}
	})
}

// The explicit refresh key is the retry affordance: it must clear cached RWX
// errors so a transient failure isn't permanent.
func TestRefreshKey_InvalidatesRWXErrors(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.rwxFetcher.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("connection reset")}, time.Now())

	if _, cached := m.rwxFetcher.Lookup(rwxCheck(rwxURL)); !cached {
		t.Fatal("setup: the error should be cached")
	}

	res, _ := m.Update(keyMsg("r"))
	m = res.(*Model)

	if _, cached := m.rwxFetcher.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatal("the refresh key must clear cached RWX errors so the fetch retries")
	}
}
