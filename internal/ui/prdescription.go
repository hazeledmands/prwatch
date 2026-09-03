package ui

import (
	"fmt"
	"strings"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// renderPRDescription builds the full PR description panel content from a
// snapshot of PR data: title, status, dates, labels, assignees, reviewers,
// milestone, deployments, and the markdown-rendered body. width controls the
// markdown reflow width.
func renderPRDescription(pr gitpkg.PRInfoResult, reviews []gitpkg.PRReview, deployments []gitpkg.PRDeployment, width int) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title))
	if pr.IsDraft {
		b.WriteString(" [DRAFT]")
	}
	if pr.State == "MERGED" {
		b.WriteString(" [MERGED]")
	} else if pr.State == "CLOSED" {
		b.WriteString(" [CLOSED]")
	}
	b.WriteString("\n")

	if !pr.CreatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Created: %s (%s)\n", pr.CreatedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.CreatedAt)))
	}
	if !pr.UpdatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Updated: %s (%s)\n", pr.UpdatedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.UpdatedAt)))
	}
	if pr.State == "MERGED" && !pr.MergedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Merged: %s (%s)\n", pr.MergedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.MergedAt)))
	}
	if pr.State == "CLOSED" && !pr.ClosedAt.IsZero() {
		b.WriteString(fmt.Sprintf("Closed: %s (%s)\n", pr.ClosedAt.Local().Format("Jan 2, 2006 3:04 PM"), relativeTime(pr.ClosedAt)))
	}

	if len(pr.Labels) > 0 {
		var names []string
		for _, l := range pr.Labels {
			names = append(names, l.Name)
		}
		b.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(names, ", ")))
	}

	if len(pr.Assignees) > 0 {
		var names []string
		for _, a := range pr.Assignees {
			names = append(names, "@"+a.Login)
		}
		b.WriteString(fmt.Sprintf("Assignees: %s\n", strings.Join(names, ", ")))
	}

	if len(reviews) > 0 {
		var items []string
		for _, r := range reviews {
			status := ""
			switch r.State {
			case "APPROVED":
				status = "✓"
			case "CHANGES_REQUESTED":
				status = "✗"
			default:
				status = "…"
			}
			items = append(items, fmt.Sprintf("@%s %s", r.Author, status))
		}
		b.WriteString(fmt.Sprintf("Reviewers: %s\n", strings.Join(items, ", ")))
	}

	if pr.Milestone.Title != "" {
		b.WriteString(fmt.Sprintf("Milestone: %s\n", pr.Milestone.Title))
	}

	if len(deployments) > 0 {
		b.WriteString("\nDeployments:\n")
		for _, d := range deployments {
			line := fmt.Sprintf("  %s: %s", d.Environment, d.State)
			if d.URL != "" {
				line += fmt.Sprintf(" (%s)", d.URL)
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")

	if pr.Body != "" {
		b.WriteString(renderMarkdown(pr.Body, width))
	}

	return b.String()
}
