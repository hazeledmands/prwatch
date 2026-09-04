package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// rwxFetcher owns the per-check log cache and the single-slot pending fetch
// state. Lookup returns cached content if present, or stages a fetch by
// returning the loading placeholder and setting pending. Cmd then turns the
// pending check into a tea.Cmd that fetches asynchronously and posts an
// rwxLogMsg. Apply stores the result.
//
// One pending slot is sufficient — only one CI check can be selected at a
// time, and a newer selection replaces any earlier still-loading fetch.
type rwxFetcher struct {
	cache   map[string]string
	pending *gitpkg.CICheck
	// inFlight holds the check URLs with an outstanding fetch.
	//
	// The pending slot alone was not enough. Lookup runs on every render of the
	// selected check, and a miss re-staged pending every time, so each redraw
	// while a fetch was outstanding dispatched another full `rwx runs` +
	// per-task `rwx logs` subprocess chain over the same run. The cache is only
	// populated when the *first* one finally lands, so nothing upstream could
	// stop it. Keyed by URL, like the cache, so an in-flight fetch for one
	// check does not block a different one the user selects meanwhile.
	inFlight map[string]bool
	// failed marks the cache entries that hold an error message rather than a
	// log. Those entries are display state, not results: without this set a
	// single transient network failure was cached under the check URL and never
	// invalidated, so the pane showed the error for the rest of the session.
	// InvalidateReadyErrors drops exactly these.
	failed map[string]bool
	// backoff holds the retry schedule for URLs whose last fetch failed.
	// Present only while an entry is failing: a success deletes it.
	backoff map[string]rwxBackoff
	// known is the URL set from the most recently adopted ciChecks, or nil
	// before the first Prune. It is what makes a late result identifiable as
	// stale — see Apply.
	known map[string]bool
}

// rwxBackoff is one URL's retry schedule: how many times in a row its fetch
// has failed, and the earliest moment a poll may retry it.
type rwxBackoff struct {
	failures int
	retryAt  time.Time
}

// rwxRetryBase and rwxRetryCap bound the automatic retry schedule.
//
// The base matches the PR poll interval: one failure costs at most one extra
// poll's worth of delay, which is the smallest step that actually removes work
// rather than reshuffling it. The cap keeps a long outage from becoming
// permanent — after ten minutes a retry costs one subprocess chain and might
// find a re-authenticated `rwx`, which is cheap enough to keep doing forever.
const (
	rwxRetryBase = 30 * time.Second
	rwxRetryCap  = 10 * time.Minute
)

// rwxRetryDelay is the wait after the nth consecutive failure: the base
// doubled n-1 times, clamped at the cap. Clamping inside the loop rather than
// shifting and comparing afterwards keeps a long-lived failing check from
// overflowing the duration.
func rwxRetryDelay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := rwxRetryBase
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= rwxRetryCap {
			return rwxRetryCap
		}
	}
	return d
}

// rwxLogMsg is the result of an async RWX log fetch.
type rwxLogMsg struct {
	checkURL string
	log      string
	err      error
}

const rwxLoadingPlaceholder = "Loading RWX logs..."

func newRWXFetcher() *rwxFetcher {
	return &rwxFetcher{
		cache:    make(map[string]string),
		inFlight: make(map[string]bool),
		failed:   make(map[string]bool),
		backoff:  make(map[string]rwxBackoff),
	}
}

// Lookup returns the additional content for an RWX CI check.
//   - For checks without an RWX URL, returns "" — callers should skip.
//   - On cache hit, returns the cached log.
//   - While a fetch for the URL is already outstanding, returns the loading
//     placeholder and stages nothing.
//   - On cache miss, marks check as pending and returns the loading placeholder.
//
// The cached bool distinguishes a real result from a placeholder.
func (f *rwxFetcher) Lookup(check gitpkg.CICheck) (content string, cached bool) {
	if !gitpkg.IsRWXURL(check.URL) {
		return "", false
	}
	// A check that has left ciChecks is not fetchable. In practice callers only
	// ever pass checks drawn from ciChecks (applyCICheckContent matches against
	// it), so this changes no display; it is what makes "nothing outside
	// ciChecks is cached or in flight" hold unconditionally rather than by the
	// grace of the caller. known is nil until the first Prune.
	if f.known != nil && !f.known[check.URL] {
		return "", false
	}
	if c, ok := f.cache[check.URL]; ok {
		return c, true
	}
	// Already fetching this URL: show the placeholder but stage nothing, so the
	// render loop can't pile up duplicate fetches behind one outstanding one.
	if f.inFlight[check.URL] {
		return rwxLoadingPlaceholder, false
	}
	c := check
	f.pending = &c
	return rwxLoadingPlaceholder, false
}

// Cmd returns a tea.Cmd that fetches the pending check, if any. Clears the
// pending state. Returns nil when there's nothing to fetch.
func (f *rwxFetcher) Cmd(git GitDataSource) tea.Cmd {
	if f.pending == nil || git == nil {
		return nil
	}
	check := *f.pending
	f.pending = nil
	// Claimed before returning, not when the closure runs: Cmd is called on the
	// Update goroutine and the closure is not, so marking it here is what makes
	// the very next Lookup see the fetch as outstanding.
	f.inFlight[check.URL] = true
	return func() tea.Msg { return fetchRWXLog(git, check) }
}

// Apply stores a fetched log (or an error message) in the cache and releases
// the URL's in-flight mark. Both outcomes release it — an error that left the
// mark set would make the check permanently unfetchable. now is when the
// result landed, and dates the retry schedule an error opens.
//
// A result whose URL has left ciChecks since the fetch was dispatched is
// dropped: caching it would undo the Prune that just evicted it, and nothing
// can display it anyway. known is nil until the first Prune, so a fetcher that
// has never seen check data accepts everything.
func (f *rwxFetcher) Apply(msg rwxLogMsg, now time.Time) {
	delete(f.inFlight, msg.checkURL)
	if f.known != nil && !f.known[msg.checkURL] {
		return
	}
	if msg.err != nil {
		f.cache[msg.checkURL] = fmt.Sprintf("Error fetching RWX logs: %v", msg.err)
		f.failed[msg.checkURL] = true
		b := f.backoff[msg.checkURL]
		b.failures++
		b.retryAt = now.Add(rwxRetryDelay(b.failures))
		f.backoff[msg.checkURL] = b
		return
	}
	f.cache[msg.checkURL] = msg.log
	delete(f.failed, msg.checkURL)
	// A success ends the outage: the next failure starts over at the base delay
	// instead of inheriting an escalation that no longer describes anything.
	delete(f.backoff, msg.checkURL)
}

// InFlight reports whether a fetch for this check URL is outstanding.
func (f *rwxFetcher) InFlight(url string) bool { return f.inFlight[url] }

// InvalidateReadyErrors drops the cached errors whose backoff window has
// closed, so the next Lookup misses and refetches them. Successful logs are
// kept: they are expensive to fetch and remain accurate for the run they
// describe.
//
// Event-driven invalidation was chosen over expiring errors on a TTL, and the
// backoff composes with that rather than replacing it. The cache is read from
// render, which gives a TTL no natural moment to fire — the pane would silently
// swap an error for a spinner on an unrelated redraw. Invalidation instead
// rides the arrival of fresh PR check data, which already means "the CI picture
// may have changed" and needs no timer of its own.
//
// What the poll alone could not do is stop asking. It fires every ~30s whether
// or not a retry has any chance of succeeding, so a permanently broken `rwx`
// (missing binary, expired token) cost an `rwx runs` plus a per-task `rwx logs`
// chain every poll for as long as a failing check stayed selected. The
// schedule in backoff gates exactly that: the poll still drives the retry, it
// just skips the URLs whose window is still open. now is the poll's moment; no
// clock lives in this type, matching activityTracker.
func (f *rwxFetcher) InvalidateReadyErrors(now time.Time) {
	for url := range f.failed {
		if now.Before(f.backoff[url].retryAt) {
			continue
		}
		delete(f.cache, url)
		delete(f.failed, url)
		// The failure count deliberately survives: this is a retry of a URL
		// still known to be failing, and if it fails again the wait should be
		// the next step up, not the base again.
	}
}

// ForceRetryErrors drops every cached error and resets the retry schedule,
// whatever the backoff windows say. This is the refresh key: the user asking
// for a retry now outranks a throttle whose whole purpose is to stand in for
// them not having asked.
//
// The schedule is cleared wholesale rather than per cached error, because the
// two sets do not coincide. InvalidateReadyErrors clears an error while keeping
// its failure count, so between that poll and the retry's result a URL has a
// schedule and no `failed` entry — and iterating `failed` walked straight past
// exactly those, leaving the escalation this promises to reset.
func (f *rwxFetcher) ForceRetryErrors() {
	for url := range f.failed {
		delete(f.cache, url)
		delete(f.failed, url)
	}
	clear(f.backoff)
}

// Prune drops every entry for a URL absent from checks, and is called wherever
// fresh ciChecks are adopted — the only moment a run can be known to have left
// the PR.
//
// Every push mints new run URLs, so without this the cache accumulated a full
// log body per superseded run for the life of the session, and inFlight and
// backoff grew alongside it. The pending slot is cleared too: a fetch for a
// check that has left ciChecks can never be displayed.
func (f *rwxFetcher) Prune(checks []gitpkg.CICheck) {
	known := make(map[string]bool, len(checks))
	for _, c := range checks {
		known[c.URL] = true
	}
	f.known = known

	for url := range f.cache {
		if !known[url] {
			delete(f.cache, url)
		}
	}
	for url := range f.failed {
		if !known[url] {
			delete(f.failed, url)
		}
	}
	for url := range f.backoff {
		if !known[url] {
			delete(f.backoff, url)
		}
	}
	// inFlight is deliberately not pruned. It does not describe stored data; it
	// describes a subprocess chain that is already running and cannot be
	// cancelled from here, and every dispatch posts exactly one result whose
	// Apply releases the mark — so the set is bounded by live fetches, not by
	// the session, and there is nothing to reclaim.
	//
	// Dropping the mark did cost something: a check can leave ciChecks and come
	// back (a flapping check, or a checks fetch that failed and then
	// succeeded), and the returning URL then re-staged a second full fetch over
	// the same run while the first was still outstanding. Apply still discards
	// the result for a URL that is gone, so nothing stale is cached either way.
	if f.pending != nil && !known[f.pending.URL] {
		f.pending = nil
	}
}

// fetchRWXLog does the actual fetch + log assembly synchronously, intended to
// run inside a tea.Cmd goroutine.
func fetchRWXLog(git GitDataSource, check gitpkg.CICheck) rwxLogMsg {
	runID := gitpkg.ExtractRWXRunID(check.URL)
	if runID == "" {
		return rwxLogMsg{checkURL: check.URL, err: fmt.Errorf("could not extract run ID from URL")}
	}

	results, err := git.RWXResults(runID)
	if err != nil {
		return rwxLogMsg{checkURL: check.URL, err: err}
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("RWX Run: %s\nStatus: %s\n", runID, results.Status))

	if len(results.FailedTasks) > 0 {
		content.WriteString(fmt.Sprintf("\nFailed tasks: %d\n", len(results.FailedTasks)))
		for _, task := range results.FailedTasks {
			content.WriteString(fmt.Sprintf("\n--- %s ---\n\n", task.Key))

			// Try test-results artifacts first for structured failure output.
			if task.HasArtifacts {
				failedTests, err := git.RWXTestResults(task.TaskID)
				if err == nil && len(failedTests) > 0 {
					for _, ft := range failedTests {
						content.WriteString(fmt.Sprintf("FAIL: %s (%s)\n\n", ft.Name, ft.Scope))
						if ft.Stdout != "" {
							content.WriteString(ft.Stdout)
							content.WriteString("\n")
						}
					}
					continue
				}
			}

			// Fall back to raw logs.
			log, err := git.RWXTaskLog(task.TaskID)
			if err != nil {
				content.WriteString(fmt.Sprintf("Error fetching log: %v\n", err))
			} else {
				content.WriteString(log)
			}
		}
	} else {
		content.WriteString("\nNo failed tasks.")
	}

	return rwxLogMsg{checkURL: check.URL, log: content.String()}
}
