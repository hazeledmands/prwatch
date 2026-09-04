package ui

import (
	"fmt"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// PR-mode sidebar rows are built here, and each row is built together with the
// thing it stands for (sidebarItem.role + sidebarItem.pr). Consumers that have
// to answer "which item is this?" — main-pane content, open-in-browser — read
// the referent off the row.
//
// Nothing re-derives the answer from the rendered label. A label is built for
// humans and cannot carry identity: two CI checks may report the same name
// (`[✓] build` twice), and one check's name can be a prefix of another's
// ("build" vs "build-arm"). Matching regenerated labels against the source
// list gave every duplicate row the first match's body and URL.

// prCommentPrefix returns the "#N " prefix for the idx-th comment of n.
// Comments are listed newest-first, so the displayed number counts down.
func prCommentPrefix(idx, n int) string { return fmt.Sprintf("#%d ", n-idx) }

// prCommentLabel returns the label body for a comment row.
func prCommentLabel(c gitpkg.PRComment) string { return "@" + c.Author }

// prReviewPrefix returns the "#N <emoji> " prefix for the idx-th review of n.
func prReviewPrefix(idx, n int, r gitpkg.PRReview) string {
	return fmt.Sprintf("#%d %s", n-idx, reviewStateEmoji(r.State))
}

// prReviewLabel returns the label body for a review row.
func prReviewLabel(r gitpkg.PRReview) string { return "@" + r.Author }

// ciCheckPrefix returns the status-indicator prefix for a CI check row.
func ciCheckPrefix(c gitpkg.CICheck) string {
	switch c.Bucket {
	case "pass":
		return "[✓] "
	case "fail", "cancel":
		return "[✗] "
	case "pending":
		return "[…] "
	case "skipping":
		return "[-] "
	default:
		return "    "
	}
}

// ciCheckLabel returns the label body for a CI check row.
func ciCheckLabel(c gitpkg.CICheck) string { return c.Name }

// prCommentSidebarItem builds the row for the idx-th comment of n. c is a
// by-value parameter, so the pointer stored on the row survives the source
// slice being replaced by the next PR refresh.
func prCommentSidebarItem(idx, n int, c gitpkg.PRComment) sidebarItem {
	return sidebarItem{
		prefix: prCommentPrefix(idx, n),
		label:  prCommentLabel(c),
		suffix: " " + relativeTime(c.CreatedAt),
		kind:   itemNormal,
		role:   rolePRComment,
		pr:     &prRow{number: n - idx, comment: &c},
	}
}

// prReviewSidebarItem builds the row for the idx-th review of n.
func prReviewSidebarItem(idx, n int, r gitpkg.PRReview) sidebarItem {
	return sidebarItem{
		prefix: prReviewPrefix(idx, n, r),
		label:  prReviewLabel(r),
		suffix: " " + relativeTime(r.SubmittedAt),
		kind:   itemNormal,
		role:   rolePRReview,
		pr:     &prRow{number: n - idx, review: &r},
	}
}

// ciCheckSidebarItem builds the row for one CI check. The timestamp shown is
// the completion time, falling back to the start time for a check still
// running.
func ciCheckSidebarItem(c gitpkg.CICheck) sidebarItem {
	ts := c.CompletedAt
	if ts.IsZero() {
		ts = c.StartedAt
	}
	return sidebarItem{
		prefix: ciCheckPrefix(c),
		label:  ciCheckLabel(c),
		suffix: " " + relativeTime(ts),
		kind:   itemNormal,
		role:   roleCICheck,
		pr:     &prRow{check: &c},
	}
}
