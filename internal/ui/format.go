package ui

import (
	"fmt"
	"strings"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// relativeTime returns a short human-readable relative timestamp like "2h ago"
// or "3d ago". Returns "" for the zero time.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// pluralize formats "<n> <word>[s]". Word gets pluralized with "s" when n != 1.
func pluralize(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// formatAuthorAndTime joins "@author" and a relative timestamp with " · ",
// omitting either part if missing.
func formatAuthorAndTime(author string, t time.Time) string {
	var parts []string
	if author != "" {
		parts = append(parts, "@"+author)
	}
	if rel := relativeTime(t); rel != "" {
		parts = append(parts, rel)
	}
	return strings.Join(parts, " · ")
}

// commitTitleLeft formats the left-side of the title bar for a commit:
// "<short-sha> · <subject>".
func commitTitleLeft(c gitpkg.Commit) string {
	short := c.SHA
	if len(short) > 7 {
		short = short[:7]
	}
	if c.Subject == "" {
		return short
	}
	return short + " · " + c.Subject
}

// reviewStateLabel returns a short human-readable label for a PR review state.
func reviewStateLabel(state string) string {
	switch state {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes requested"
	case "COMMENTED":
		return "commented"
	default:
		if state == "" {
			return "pending"
		}
		return strings.ToLower(state)
	}
}

// matchNumberedItem checks if selected matches any item's expected label (built
// by labelFn). Returns (true, index) on the first match, (false, 0) otherwise.
func matchNumberedItem[T any](selected string, items []T, labelFn func(int, T) string) (bool, int) {
	for i, item := range items {
		if selected == labelFn(i, item) {
			return true, i
		}
	}
	return false, 0
}
