package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

func genPRInfo(t *rapid.T) gitpkg.PRInfoResult {
	pr := gitpkg.PRInfoResult{
		Number:  rapid.IntRange(1, 9999).Draw(t, "number"),
		Title:   rapid.StringMatching(`[a-zA-Z0-9 ]{1,40}`).Draw(t, "title"),
		IsDraft: rapid.Bool().Draw(t, "draft"),
		State:   rapid.SampledFrom([]string{"OPEN", "MERGED", "CLOSED"}).Draw(t, "state"),
		Body:    rapid.SampledFrom([]string{"", "hello world", "# header\n\nbody"}).Draw(t, "body"),
	}
	if rapid.Bool().Draw(t, "hasCreated") {
		pr.CreatedAt = time.Now().Add(-time.Duration(rapid.IntRange(1, 1000).Draw(t, "createdMin")) * time.Minute)
	}
	if rapid.Bool().Draw(t, "hasUpdated") {
		pr.UpdatedAt = time.Now().Add(-time.Duration(rapid.IntRange(1, 1000).Draw(t, "updatedMin")) * time.Minute)
	}
	if pr.State == "MERGED" && rapid.Bool().Draw(t, "hasMerged") {
		pr.MergedAt = time.Now().Add(-time.Duration(rapid.IntRange(1, 1000).Draw(t, "mergedMin")) * time.Minute)
	}
	if pr.State == "CLOSED" && rapid.Bool().Draw(t, "hasClosed") {
		pr.ClosedAt = time.Now().Add(-time.Duration(rapid.IntRange(1, 1000).Draw(t, "closedMin")) * time.Minute)
	}
	nLabels := rapid.IntRange(0, 3).Draw(t, "nLabels")
	for i := 0; i < nLabels; i++ {
		pr.Labels = append(pr.Labels, gitpkg.PRLabel{Name: fmt.Sprintf("label%d", i)})
	}
	nAssignees := rapid.IntRange(0, 3).Draw(t, "nAssignees")
	for i := 0; i < nAssignees; i++ {
		pr.Assignees = append(pr.Assignees, gitpkg.PRUser{Login: fmt.Sprintf("user%d", i)})
	}
	if rapid.Bool().Draw(t, "hasMilestone") {
		pr.Milestone.Title = "v1.0"
	}
	return pr
}

func genReviews(t *rapid.T) []gitpkg.PRReview {
	n := rapid.IntRange(0, 5).Draw(t, "nReviews")
	out := make([]gitpkg.PRReview, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, gitpkg.PRReview{
			Author:      fmt.Sprintf("reviewer%d", i),
			State:       rapid.SampledFrom([]string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED", "PENDING"}).Draw(t, fmt.Sprintf("rstate%d", i)),
			SubmittedAt: time.Now().Add(-time.Hour),
		})
	}
	return out
}

func genDeployments(t *rapid.T) []gitpkg.PRDeployment {
	n := rapid.IntRange(0, 3).Draw(t, "nDeploy")
	out := make([]gitpkg.PRDeployment, 0, n)
	for i := 0; i < n; i++ {
		d := gitpkg.PRDeployment{
			Environment: fmt.Sprintf("env%d", i),
			State:       rapid.SampledFrom([]string{"ACTIVE", "INACTIVE", "ERROR"}).Draw(t, fmt.Sprintf("dstate%d", i)),
		}
		if rapid.Bool().Draw(t, fmt.Sprintf("dhasURL%d", i)) {
			d.URL = fmt.Sprintf("https://example.com/deploy/%d", i)
		}
		out = append(out, d)
	}
	return out
}

// Property: output is idempotent (same inputs → same output).
func TestPRDescriptionIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pr := genPRInfo(t)
		reviews := genReviews(t)
		deployments := genDeployments(t)
		width := rapid.IntRange(20, 200).Draw(t, "width")

		a := renderPRDescription(pr, reviews, deployments, width)
		b := renderPRDescription(pr, reviews, deployments, width)
		if a != b {
			t.Fatalf("renderPRDescription not idempotent")
		}
	})
}

// Property: header line always contains "PR #N: title".
func TestPRDescriptionHeader(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pr := genPRInfo(t)
		out := renderPRDescription(pr, genReviews(t), genDeployments(t), 80)
		want := fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title)
		if !strings.HasPrefix(out, want) {
			t.Fatalf("header missing: got %q, want prefix %q", out[:min(len(out), 80)], want)
		}
	})
}

// Property: Reviewers: line appears iff reviews non-empty.
func TestPRDescriptionReviewersLine(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pr := genPRInfo(t)
		reviews := genReviews(t)
		out := renderPRDescription(pr, reviews, nil, 80)
		hasLine := strings.Contains(out, "Reviewers:")
		if hasLine != (len(reviews) > 0) {
			t.Fatalf("Reviewers: line present=%v, reviews=%d", hasLine, len(reviews))
		}
	})
}

// Property: Deployments: section appears iff deployments non-empty.
func TestPRDescriptionDeploymentsSection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pr := genPRInfo(t)
		deployments := genDeployments(t)
		out := renderPRDescription(pr, nil, deployments, 80)
		hasSection := strings.Contains(out, "Deployments:\n")
		if hasSection != (len(deployments) > 0) {
			t.Fatalf("Deployments: section present=%v, deployments=%d", hasSection, len(deployments))
		}
	})
}

// Property: [DRAFT] and [MERGED]/[CLOSED] tags reflect pr state.
func TestPRDescriptionStateTags(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pr := genPRInfo(t)
		out := renderPRDescription(pr, nil, nil, 80)
		firstLine := strings.SplitN(out, "\n", 2)[0]
		if pr.IsDraft != strings.Contains(firstLine, "[DRAFT]") {
			t.Fatalf("DRAFT tag mismatch: isDraft=%v line=%q", pr.IsDraft, firstLine)
		}
		if (pr.State == "MERGED") != strings.Contains(firstLine, "[MERGED]") {
			t.Fatalf("MERGED tag mismatch: state=%s line=%q", pr.State, firstLine)
		}
		if (pr.State == "CLOSED") != strings.Contains(firstLine, "[CLOSED]") {
			t.Fatalf("CLOSED tag mismatch: state=%s line=%q", pr.State, firstLine)
		}
	})
}
