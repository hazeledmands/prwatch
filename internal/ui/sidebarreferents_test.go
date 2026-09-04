package ui

import (
	"fmt"
	"testing"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
	"pgregory.net/rapid"
)

// errorReporter is the slice of *testing.T / *rapid.T that the shared
// referent checker needs, so one checker serves both the table tests and the
// property tests below.
type errorReporter interface {
	Errorf(format string, args ...any)
}

// referentRoleNames maps each role that promises a PR referent to the one
// prRow pointer it promises. A role absent from this map promises none.
var referentRoleNames = map[sidebarItemRole]string{
	rolePRComment: "comment",
	rolePRReview:  "review",
	roleCICheck:   "check",
}

// checkSidebarReferents asserts the one-of rule every sidebar row obeys:
// exactly the referent the row's role names is present, and nothing else is.
//
// Nothing asserted this before. It is the invariant the whole role/referent
// design rests on — every consumer switches on the role and dereferences the
// pointer that role implies — so a builder that set one without the other
// would produce either an empty pane or, before the accessors were guarded, a
// panic mid-render.
func checkSidebarReferents(t errorReporter, items []sidebarItem) {
	for i, it := range items {
		where := fmt.Sprintf("row %d %q (role %d, kind %d)", i, it.prefix+it.label, it.role, it.kind)

		wantName, wantPR := referentRoleNames[it.role]
		if (it.pr != nil) != wantPR {
			t.Errorf("%s: has pr referent = %v, want %v", where, it.pr != nil, wantPR)
			continue
		}

		if it.pr != nil {
			var got []string
			if it.pr.comment != nil {
				got = append(got, "comment")
			}
			if it.pr.review != nil {
				got = append(got, "review")
			}
			if it.pr.check != nil {
				got = append(got, "check")
			}
			if len(got) != 1 || got[0] != wantName {
				t.Errorf("%s: carries %v, want exactly [%s]", where, got, wantName)
			}
		}

		// The guarded accessors are the only way consumers read a referent,
		// so they must agree with the role — and at most one may answer.
		accessors := map[string]bool{
			"comment": it.prComment() != nil,
			"review":  it.prReview() != nil,
			"check":   it.ciCheck() != nil,
		}
		for name, nonNil := range accessors {
			if nonNil != (wantPR && name == wantName) {
				t.Errorf("%s: %s() non-nil = %v, want %v",
					where, name, nonNil, wantPR && name == wantName)
			}
		}

		// A commit row and a PR row are never the same row, and a commit is
		// only ever carried by a row that claims no other role.
		if it.commit != nil && wantPR {
			t.Errorf("%s: carries both a commit and a %s referent", where, wantName)
		}
		if it.commit != nil && it.role != roleNone {
			t.Errorf("%s: commit row also claims role %d", where, it.role)
		}
	}
}

// ---------------------------------------------------------------------------
// Property: both builders produce role/referent-consistent rows
// ---------------------------------------------------------------------------

func genPRComments(t *rapid.T) []gitpkg.PRComment {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) gitpkg.PRComment {
		return gitpkg.PRComment{
			Author:    rapid.SampledFrom([]string{"alice", "bob", ""}).Draw(t, "author"),
			Body:      rapid.SampledFrom([]string{"", "looks good", "nit"}).Draw(t, "body"),
			URL:       rapid.SampledFrom([]string{"", "https://gh/c1", "https://gh/c2"}).Draw(t, "url"),
			CreatedAt: time.Unix(rapid.Int64Range(0, 1_800_000_000).Draw(t, "created"), 0),
		}
	}), 0, 4).Draw(t, "comments")
}

func genPRReviews(t *rapid.T) []gitpkg.PRReview {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) gitpkg.PRReview {
		return gitpkg.PRReview{
			Author:      rapid.SampledFrom([]string{"alice", "bob", ""}).Draw(t, "author"),
			State:       rapid.SampledFrom([]string{"APPROVED", "CHANGES_REQUESTED", "COMMENTED", "PENDING", ""}).Draw(t, "state"),
			Body:        rapid.SampledFrom([]string{"", "ship it"}).Draw(t, "body"),
			URL:         rapid.SampledFrom([]string{"", "https://gh/r1"}).Draw(t, "url"),
			SubmittedAt: time.Unix(rapid.Int64Range(0, 1_800_000_000).Draw(t, "submitted"), 0),
		}
	}), 0, 4).Draw(t, "reviews")
}

func genCIChecks(t *rapid.T) []gitpkg.CICheck {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) gitpkg.CICheck {
		return gitpkg.CICheck{
			// Deliberately drawn from a tiny name pool so duplicate and
			// prefix-of-another names come up often.
			Name:   rapid.SampledFrom([]string{"build", "build-arm", "test", ""}).Draw(t, "name"),
			Bucket: rapid.SampledFrom([]string{"pass", "fail", "cancel", "pending", ""}).Draw(t, "bucket"),
			State:  rapid.SampledFrom([]string{"COMPLETED", "IN_PROGRESS", ""}).Draw(t, "state"),
			URL:    rapid.SampledFrom([]string{"", "https://ci/a", "https://ci/b"}).Draw(t, "url"),
		}
	}), 0, 4).Draw(t, "checks")
}

func genCommitList(t *rapid.T, label string) []gitpkg.Commit {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) gitpkg.Commit {
		return gitpkg.Commit{
			SHA:     rapid.SampledFrom([]string{"abc1234", "def5678", "abc1234"}).Draw(t, "sha"),
			Subject: rapid.SampledFrom([]string{"fix", "feat", ""}).Draw(t, "subject"),
		}
	}), 0, 5).Draw(t, label)
}

// TestProperty_PRSidebarReferentsConsistent pins the one-of rule over
// arbitrary PR data, including the duplicate and prefix-shadowing check names
// that broke label matching.
func TestProperty_PRSidebarReferentsConsistent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		comments := genPRComments(t)
		reviews := genPRReviews(t)
		checks := genCIChecks(t)
		total := rapid.IntRange(0, 10).Draw(t, "reviewsTotal")

		checkSidebarReferents(t, buildPRSidebar(comments, reviews, checks, total))
	})
}

// TestProperty_CommitsSidebarReferentsConsistent covers the builder that had
// no referent coverage at all. Its rows carry commits and the two pseudo-entry
// roles, and must never carry a PR referent.
func TestProperty_CommitsSidebarReferentsConsistent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		commits := genCommitList(t, "commits")
		baseCommits := genCommitList(t, "baseCommits")
		uncommitted := rapid.SliceOfN(rapid.SampledFrom([]string{"a.go", "b.go"}), 0, 2).Draw(t, "uncommitted")
		staged := rapid.SliceOfN(rapid.SampledFrom([]string{"c.go", "d.go"}), 0, 2).Draw(t, "staged")
		ahead := rapid.IntRange(0, 6).Draw(t, "ahead")
		loaded := rapid.IntRange(0, 6).Draw(t, "commitsLoaded")
		count := rapid.IntRange(0, 12).Draw(t, "commitCount")

		items := buildCommitsSidebar(commits, baseCommits, uncommitted, staged, ahead, loaded, count)
		checkSidebarReferents(t, items)

		// Every commit row resolves to a commit, which is what lets the Base
		// section render: its rows come from baseCommits, a list no consumer
		// searches.
		for i, it := range items {
			if it.commit == nil {
				continue
			}
			if it.commit.SHA == "" && it.commit.Subject == "" {
				t.Fatalf("row %d %q carries an empty commit", i, it.label)
			}
		}
	})
}

// TestPRSidebarPseudoRowsCarryNoReferent pins that the "(no comments)"-style
// rows, the section headers, and the separators stand for nothing — matched by
// role rather than by label, since a label is exactly what must not be load
// bearing here.
func TestPRSidebarPseudoRowsCarryNoReferent(t *testing.T) {
	items := buildPRSidebar(nil, nil, nil, 0)
	checkSidebarReferents(t, items)

	for _, it := range items {
		if it.role == rolePRDescription {
			continue
		}
		if it.role != roleNone {
			t.Errorf("row %q in an empty PR sidebar claims role %d, want roleNone",
				it.prefix+it.label, it.role)
		}
	}
}

// ---------------------------------------------------------------------------
// Line-3 jump targets
// ---------------------------------------------------------------------------

// TestFirstCommentIndex_NoFallthroughToReviews pins the behavior change that
// came with role-based resolution. firstCommentIndex used to match any row
// whose label began with "@", and a review row's label does too — so clicking
// the line-3 comments label on a PR with no comments jumped to the first
// review.
//
// PROMPT.md's line-3 section says clicking the comment count "should jump
// straight to the comments list"; it promises no fallthrough, and the reviews
// bullet beside it is explicitly hedged "(if any)". With no comments there is
// no comments list to jump to, so the selection is left alone.
func TestFirstCommentIndex_NoFallthroughToReviews(t *testing.T) {
	reviews := []gitpkg.PRReview{
		{Author: "alice", State: "APPROVED", URL: "https://gh/r1"},
		{Author: "bob", State: "COMMENTED", URL: "https://gh/r2"},
	}
	items := buildPRSidebar(nil, reviews, nil, len(reviews))

	if got := firstCommentIndex(items); got != -1 {
		t.Errorf("firstCommentIndex = %d for a PR with no comments, want -1 (row %q)",
			got, items[got].prefix+items[got].label)
	}

	// The reviews are still reachable by their own jump target.
	idx := firstReviewIndex(items)
	if idx < 0 {
		t.Fatalf("firstReviewIndex found nothing in %+v", items)
	}
	if items[idx].role != rolePRReview {
		t.Errorf("firstReviewIndex selected role %d, want rolePRReview", items[idx].role)
	}
	if got := items[idx].prReview(); got == nil || got.URL != "https://gh/r1" {
		t.Errorf("firstReviewIndex selected %+v, want the newest review", got)
	}
}

// TestFirstCommentIndex_SelectsNewestComment is the normal case: comments are
// listed newest-first, so the first comment row is the newest one.
func TestFirstCommentIndex_SelectsNewestComment(t *testing.T) {
	comments := []gitpkg.PRComment{
		{Author: "alice", URL: "https://gh/c-newest"},
		{Author: "alice", URL: "https://gh/c-oldest"},
	}
	reviews := []gitpkg.PRReview{{Author: "bob", State: "APPROVED", URL: "https://gh/r1"}}
	items := buildPRSidebar(comments, reviews, nil, len(reviews))

	idx := firstCommentIndex(items)
	if idx < 0 {
		t.Fatalf("firstCommentIndex found nothing in %+v", items)
	}
	if items[idx].role != rolePRComment {
		t.Errorf("firstCommentIndex selected role %d, want rolePRComment", items[idx].role)
	}
	if got := items[idx].prComment(); got == nil || got.URL != "https://gh/c-newest" {
		t.Errorf("firstCommentIndex selected %+v, want the newest comment", got)
	}
}

// TestFirstReviewIndex_NoReviews pins the mirror case: no review rows means no
// jump target, not the first comment row.
func TestFirstReviewIndex_NoReviews(t *testing.T) {
	comments := []gitpkg.PRComment{{Author: "alice", URL: "https://gh/c1"}}
	items := buildPRSidebar(comments, nil, nil, 0)

	if got := firstReviewIndex(items); got != -1 {
		t.Errorf("firstReviewIndex = %d for a PR with no reviews, want -1 (row %q)",
			got, items[got].prefix+items[got].label)
	}
}
