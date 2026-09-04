package ui

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

// firstRoleIndex returns the index of the first row standing for the given
// kind of thing.
func firstRoleIndex(items []sidebarItem, role sidebarItemRole) int {
	return firstSidebarMatch(items, func(it sidebarItem) bool {
		return it.role == role
	})
}

// firstCommentIndex finds the first PR-comment row.
func firstCommentIndex(items []sidebarItem) int {
	return firstRoleIndex(items, rolePRComment)
}

// firstReviewIndex finds the first PR-review row.
func firstReviewIndex(items []sidebarItem) int {
	return firstRoleIndex(items, rolePRReview)
}

// firstCIFailureIndex finds the sidebar index of the first failing CI check,
// or the first CI check overall when there are no failures. Returns -1 when
// there are no CI check rows.
//
// The row carries the check it was built from, so this reads the bucket off
// the referent rather than pairing a name found in the check list back to a
// row by label — which let check "build" select the "build-arm" row, and gave
// two same-named checks the same row.
func firstCIFailureIndex(items []sidebarItem) int {
	if i := firstSidebarMatch(items, func(it sidebarItem) bool {
		check := it.ciCheck()
		if check == nil {
			return false
		}
		return check.Bucket == "fail" || check.Bucket == "cancel"
	}); i >= 0 {
		return i
	}
	return firstRoleIndex(items, roleCICheck)
}
