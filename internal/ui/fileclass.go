package ui

import (
	"strings"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// containsString returns true iff slice contains s. Linear scan; callers that
// hot-loop this should switch to set membership instead.
func containsString(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

// changeBadgeFor returns the right-aligned change-type badge ([→], [-], [+],
// [±]) for a file appearing in a changed section of the sidebar. Returns
// empty string if file is not in the change set. Rename takes precedence
// over every other badge — a rename plus edits is still primarily a rename.
func changeBadgeFor(file string, cf *gitpkg.ChangedFiles) string {
	f, ok := cf.Get(file)
	if !ok {
		return ""
	}
	switch f.Class {
	case gitpkg.ClassRenamed:
		return "[→]"
	case gitpkg.ClassDeleted:
		return "[-]"
	case gitpkg.ClassAdded:
		return "[+]"
	default:
		return "[±]"
	}
}

// applyChangeBadges sets the change-type suffix on leaf file items in the
// New Changes, Staged, and Committed sections. Directory entries, headers,
// separators, and items in the All Files section are left untouched.
func applyChangeBadges(items []sidebarItem, cf *gitpkg.ChangedFiles) []sidebarItem {
	inChangedSection := false
	for i := range items {
		if items[i].kind == itemHeader {
			label := items[i].label
			inChangedSection = strings.HasPrefix(label, "New Changes") ||
				strings.HasPrefix(label, "Staged") ||
				strings.HasPrefix(label, "Committed")
			continue
		}
		if !inChangedSection || !items[i].kind.selectable() || items[i].isDir || items[i].filePath == "" {
			continue
		}
		items[i].suffix = changeBadgeFor(items[i].filePath, cf)
	}
	return items
}
