package ui

import (
	"cmp"
	"slices"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// sortPRData sorts comments, reviews, and CI checks for display. Comments and
// reviews are ordered most-recent-first. CI checks order: failures first, then
// pending, then passing — secondary order preserves the input order via stable
// sort.
func sortPRData(comments []gitpkg.PRComment, reviews []gitpkg.PRReview, checks []gitpkg.CICheck) {
	slices.SortFunc(comments, func(a, b gitpkg.PRComment) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	slices.SortFunc(reviews, func(a, b gitpkg.PRReview) int {
		return b.SubmittedAt.Compare(a.SubmittedAt)
	})
	slices.SortStableFunc(checks, func(a, b gitpkg.CICheck) int {
		return cmp.Compare(ciBucketOrder(a.Bucket), ciBucketOrder(b.Bucket))
	})
}

// ciBucketOrder returns a sort key for CI check buckets: failures first, then
// pending, then passing.
func ciBucketOrder(bucket string) int {
	switch bucket {
	case "fail", "cancel":
		return 0
	case "pending":
		return 1
	case "pass":
		return 2
	case "skipping":
		return 3
	default:
		return 4
	}
}
