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
}

// rwxLogMsg is the result of an async RWX log fetch.
type rwxLogMsg struct {
	checkURL string
	log      string
	err      error
}

const rwxLoadingPlaceholder = "Loading RWX logs..."

func newRWXFetcher() *rwxFetcher {
	return &rwxFetcher{cache: make(map[string]string)}
}

// Lookup returns the additional content for an RWX CI check.
//   - For checks without an RWX URL, returns "" — callers should skip.
//   - On cache hit, returns the cached log.
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
	return func() tea.Msg { return fetchRWXLog(git, check) }
}

// Apply stores a fetched log (or an error message) in the cache.
func (f *rwxFetcher) Apply(msg rwxLogMsg) {
	if msg.err != nil {
		f.cache[msg.checkURL] = fmt.Sprintf("Error fetching RWX logs: %v", msg.err)
		return
	}
	f.cache[msg.checkURL] = msg.log
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
