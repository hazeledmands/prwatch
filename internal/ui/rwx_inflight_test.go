package ui

import (
	"strings"
	"testing"

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
	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "log body"})
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
	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("connection reset")})

	// The error is cached and displayed — no refetch storm in the meantime.
	content, cached := f.Lookup(rwxCheck(rwxURL))
	if !cached || !strings.Contains(content, "connection reset") {
		t.Fatalf("error should be cached and shown: content=%q cached=%v", content, cached)
	}
	if cmd := f.Cmd(git); cmd != nil {
		t.Fatal("a cached error must not refetch on every render")
	}

	f.InvalidateErrors()

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

	f.Apply(rwxLogMsg{checkURL: rwxURL, log: "good log"})
	f.Apply(rwxLogMsg{checkURL: other, err: errFake("boom")})
	f.InvalidateErrors()

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
	f.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("boom")})
	if f.InFlight(rwxURL) {
		t.Fatal("an errored Apply must clear the in-flight mark")
	}
}

// Property: across any interleaving of renders, dispatches, results and
// invalidations, at most one fetch per URL is ever outstanding, and the
// in-flight set always drains once results arrive.
func TestProperty_RWXFetcher(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := newRWXFetcher()
		git := &rwxStubGit{}
		urls := []string{
			"https://cloud.rwx.com/mint/org/runs/a",
			"https://cloud.rwx.com/mint/org/runs/b",
			"https://cloud.rwx.com/mint/org/runs/c",
		}
		// outstanding tracks dispatches we have not yet fed a result for.
		outstanding := map[string]int{}

		n := rapid.IntRange(1, 50).Draw(t, "steps")
		for i := 0; i < n; i++ {
			url := rapid.SampledFrom(urls).Draw(t, "url")
			switch rapid.SampledFrom([]string{"render", "result", "invalidate"}).Draw(t, "op") {
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
				if rapid.Bool().Draw(t, "failed") {
					f.Apply(rwxLogMsg{checkURL: url, err: errFake("boom")})
				} else {
					f.Apply(rwxLogMsg{checkURL: url, log: "body"})
				}
			case "invalidate":
				f.InvalidateErrors()
			}

			for _, u := range urls {
				if outstanding[u] > 1 {
					t.Fatalf("%s has %d concurrent fetches outstanding", u, outstanding[u])
				}
				if f.InFlight(u) != (outstanding[u] == 1) {
					t.Fatalf("%s: InFlight()=%v but %d outstanding", u, f.InFlight(u), outstanding[u])
				}
			}
		}

		// Drain: feeding the remaining results must empty the in-flight set.
		for _, u := range urls {
			for outstanding[u] > 0 {
				outstanding[u]--
				f.Apply(rwxLogMsg{checkURL: u, log: "body"})
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
	m.rwxFetcher.Apply(rwxLogMsg{checkURL: rwxURL, err: errFake("connection reset")})

	if _, cached := m.rwxFetcher.Lookup(rwxCheck(rwxURL)); !cached {
		t.Fatal("setup: the error should be cached")
	}

	res, _ := m.Update(keyMsg("r"))
	m = res.(*Model)

	if _, cached := m.rwxFetcher.Lookup(rwxCheck(rwxURL)); cached {
		t.Fatal("the refresh key must clear cached RWX errors so the fetch retries")
	}
}
