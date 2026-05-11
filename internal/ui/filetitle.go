package ui

import (
	"strings"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// statMtimeFn returns the working-tree mtime for a file, or false when
// unavailable. Implementations should join the file with the base dir as
// appropriate.
type statMtimeFn func(file string) (time.Time, bool)

// lastCommitFn returns the most-recent commit touching file. Returns the zero
// Commit with a non-nil error when unavailable; callers also treat empty SHA
// as "no info".
type lastCommitFn func(file string) (gitpkg.Commit, error)

// fileDiffPrefix builds the right-side prefix for a file with an active diff.
//   - Uncommitted: "uncommitted · <relative-time>" using working-tree mtime,
//     or just "uncommitted" when mtime is unavailable.
//   - Committed: "<sha7> · <relative-time>" using the most recent commit
//     touching the file.
//   - "" when neither classification applies or no data is available.
func fileDiffPrefix(file string, isUncommitted, isCommitted bool, stat statMtimeFn, lastCommit lastCommitFn) string {
	if isUncommitted {
		if stat != nil {
			if mt, ok := stat(file); ok {
				return "uncommitted · " + relativeTime(mt)
			}
		}
		return "uncommitted"
	}
	if isCommitted && lastCommit != nil {
		if c, err := lastCommit(file); err == nil && c.SHA != "" {
			return shortSHA(c.SHA) + " · " + relativeTime(c.AuthorDate)
		}
	}
	return ""
}

// fileContextRight builds the right-side title text for a file with no active
// diff. Components join with " · " in this order:
//
//	[binary] [<sha7>|untracked] [<relative-time>]
//
// Tracked files show the most recent commit's short SHA + author time;
// untracked files show "untracked" + the file's mtime. Binary files prefix
// the result with "binary".
func fileContextRight(file string, binary bool, stat statMtimeFn, lastCommit lastCommitFn) string {
	var parts []string
	if binary {
		parts = append(parts, "binary")
	}

	var trackedInfo, rel string
	if lastCommit != nil {
		if c, err := lastCommit(file); err == nil && c.SHA != "" {
			trackedInfo = shortSHA(c.SHA)
			rel = relativeTime(c.AuthorDate)
		}
	}
	if trackedInfo == "" {
		trackedInfo = "untracked"
		if stat != nil {
			if mt, ok := stat(file); ok {
				rel = relativeTime(mt)
			}
		}
	}

	if trackedInfo != "" {
		parts = append(parts, trackedInfo)
	}
	if rel != "" {
		parts = append(parts, rel)
	}
	return strings.Join(parts, " · ")
}

// shortSHA returns the first 7 characters of sha (or sha unchanged if shorter).
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
