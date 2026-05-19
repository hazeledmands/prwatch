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

// isRenamed reports whether file is the new path of any rename.
func isRenamed(file string, renamed []gitpkg.Rename) bool {
	for _, r := range renamed {
		if r.New == file {
			return true
		}
	}
	return false
}

// changeBadgeFor returns the right-aligned change-type badge ([→], [-], [+],
// [±]) for a file appearing in a changed section of the sidebar. Returns empty
// string if file is not in any changed section. Rename takes precedence over
// every other badge — a rename plus edits is still primarily a rename.
func changeBadgeFor(file string, deleted, added, committed, uncommitted, staged []string, renamed []gitpkg.Rename) string {
	switch {
	case isRenamed(file, renamed):
		return "[→]"
	case containsString(deleted, file):
		return "[-]"
	case containsString(added, file):
		return "[+]"
	case containsString(committed, file), containsString(uncommitted, file), containsString(staged, file):
		return "[±]"
	}
	return ""
}

// applyChangeBadges sets the change-type suffix on leaf file items in the
// New Changes, Staged, and Committed sections. Directory entries, headers,
// separators, and items in the All Files section are left untouched.
func applyChangeBadges(items []sidebarItem, deleted, added, committed, uncommitted, staged []string, renamed []gitpkg.Rename) []sidebarItem {
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
		items[i].suffix = changeBadgeFor(items[i].filePath, deleted, added, committed, uncommitted, staged, renamed)
	}
	return items
}
