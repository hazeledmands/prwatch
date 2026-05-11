package ui

import (
	"strings"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// firstSidebarMatch returns the index of the first sidebar item satisfying pred,
// or -1 if none match.
func firstSidebarMatch(items []sidebarItem, pred func(sidebarItem) bool) int {
	for i, it := range items {
		if pred(it) {
			return i
		}
	}
	return -1
}

// firstCommentIndex finds the first @-prefixed sidebar item.
func firstCommentIndex(items []sidebarItem) int {
	return firstSidebarMatch(items, func(it sidebarItem) bool {
		return strings.HasPrefix(it.label, "@")
	})
}

// firstReviewIndex finds the first @-prefixed sidebar item whose prefix carries
// a review-state indicator.
func firstReviewIndex(items []sidebarItem) int {
	return firstSidebarMatch(items, func(it sidebarItem) bool {
		if !strings.HasPrefix(it.label, "@") {
			return false
		}
		p := it.prefix
		return strings.Contains(p, "✓") ||
			strings.Contains(p, "✗") ||
			strings.Contains(p, "c ") ||
			strings.Contains(p, "…")
	})
}

// firstCIFailureIndex finds the sidebar index of the first failing CI check,
// or the first CI check overall when there are no failures. Returns -1 if
// neither ciChecks nor sidebar items contain a usable target.
func firstCIFailureIndex(items []sidebarItem, ciChecks []gitpkg.CICheck) int {
	targetName := ""
	for _, c := range ciChecks {
		if c.Bucket == "fail" || c.Bucket == "cancel" {
			targetName = c.Name
			break
		}
	}
	if targetName == "" && len(ciChecks) > 0 {
		targetName = ciChecks[0].Name
	}
	if targetName == "" {
		return -1
	}
	return firstSidebarMatch(items, func(it sidebarItem) bool {
		return strings.Contains(it.label, targetName)
	})
}
