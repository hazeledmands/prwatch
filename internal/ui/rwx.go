package ui

import (
	"fmt"
	"strings"

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
	// InvalidateErrors drops exactly these.
	failed map[string]bool
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
// mark set would make the check permanently unfetchable.
func (f *rwxFetcher) Apply(msg rwxLogMsg) {
	delete(f.inFlight, msg.checkURL)
	if msg.err != nil {
		f.cache[msg.checkURL] = fmt.Sprintf("Error fetching RWX logs: %v", msg.err)
		f.failed[msg.checkURL] = true
		return
	}
	f.cache[msg.checkURL] = msg.log
	delete(f.failed, msg.checkURL)
}

// InFlight reports whether a fetch for this check URL is outstanding.
func (f *rwxFetcher) InFlight(url string) bool { return f.inFlight[url] }

// InvalidateErrors drops every cached error so the next Lookup misses and
// refetches. Successful logs are kept: they are expensive to fetch and remain
// accurate for the run they describe.
//
// Chosen over expiring errors on a TTL. The cache is read from render, which
// gives a TTL no natural moment to fire — the pane would silently swap an error
// for a spinner on an unrelated redraw, and testing it means injecting a clock.
// Invalidation instead rides the two events that already mean "the CI picture
// may have changed": the explicit refresh key, which is the user asking for a
// retry, and the arrival of fresh PR check data, which bounds the retry rate to
// the PR poll interval without any timer of its own.
func (f *rwxFetcher) InvalidateErrors() {
	for url := range f.failed {
		delete(f.cache, url)
		delete(f.failed, url)
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
