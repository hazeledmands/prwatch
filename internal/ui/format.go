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

// sanitizeDisplayText makes a filename safe to put on screen by replacing
// every C0 control character and DEL with a visible escape.
//
// This is the filename→display-text boundary, and it exists because of the
// `-z` conversion: git's default core.quotePath used to escape these for us,
// so a filename containing a tab or a newline arrived pre-neutered. Reading
// raw NUL-delimited output is correct — it is what lets café.txt display as
// café.txt — but it also means a literal tab or newline in a filename now
// reaches display text directly. A raw tab hits the runewidth-0 / lipgloss-4
// disagreement that expandTabs exists to prevent, and a raw newline in a
// sidebar label breaks row math and mouse hit-testing outright, since the
// label is assumed to occupy exactly one row.
//
// Representation is git's own: \t, \n, \r by name and \xNN for the rest. That
// is what these filenames displayed as before the -z change, so it is already
// familiar in this context; it is pure ASCII, so width math stays trivially
// correct; and it renders in every terminal, unlike the Control Pictures block
// (␉/␊), whose font coverage is patchy.
//
// A literal backslash is deliberately NOT escaped. Doing so would rewrite
// every path containing one for the sake of an ambiguity ("a\tb" as six
// characters vs. a real tab) that is vanishingly rare on the platforms this
// runs on, and the cost — every normal path changing appearance — is worse
// than the ambiguity.
//
// Only display text goes through here. Paths used as identity — git arguments,
// map keys, sidebarItem.filePath — must stay raw, or they stop naming a file.
func sanitizeDisplayText(s string) string {
	if strings.IndexFunc(s, isDisplayControl) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case isDisplayControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isDisplayControl reports whether r is a C0 control character or DEL — the
// characters that have no width of their own but move the cursor anyway.
func isDisplayControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}
