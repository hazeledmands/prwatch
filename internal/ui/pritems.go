package ui

import (
	"fmt"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// PR-mode sidebar rows are identified by their rendered label
// (prefix+label, what sidebar.SelectedItem returns). These builders are the
// single definition of those labels: buildPRSidebar renders from them, and
// every consumer that has to answer "which item is this?" — main-pane
// content, open-in-browser — matches against them exactly.
//
// Matching by substring instead (`strings.Contains(label, author)`) makes
// two comments by one author indistinguishable and lets CI check "build"
// claim the "build-arm" row.

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

// prCommentItemLabel / prReviewItemLabel / ciCheckItemLabel return the full
// rendered row identity (prefix+label) that sidebar.SelectedItem yields.
func prCommentItemLabel(idx, n int, c gitpkg.PRComment) string {
	return prCommentPrefix(idx, n) + prCommentLabel(c)
}

func prReviewItemLabel(idx, n int, r gitpkg.PRReview) string {
	return prReviewPrefix(idx, n, r) + prReviewLabel(r)
}

func ciCheckItemLabel(c gitpkg.CICheck) string {
	return ciCheckPrefix(c) + ciCheckLabel(c)
}

// matchPRComment / matchPRReview / matchCICheck resolve a selected sidebar
// label to an index, exactly.
func matchPRComment(selected string, comments []gitpkg.PRComment) (bool, int) {
	return matchNumberedItem(selected, comments, func(i int, c gitpkg.PRComment) string {
		return prCommentItemLabel(i, len(comments), c)
	})
}

func matchPRReview(selected string, reviews []gitpkg.PRReview) (bool, int) {
	return matchNumberedItem(selected, reviews, func(i int, r gitpkg.PRReview) string {
		return prReviewItemLabel(i, len(reviews), r)
	})
}

func matchCICheck(selected string, checks []gitpkg.CICheck) (bool, int) {
	return matchNumberedItem(selected, checks, func(_ int, c gitpkg.CICheck) string {
		return ciCheckItemLabel(c)
	})
}
