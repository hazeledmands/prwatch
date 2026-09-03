package ui

import (
	"strings"
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// TestPRSidebar_ReviewCountShowsTruncation covers the user-visible half of
// the reviews pagination cap. A fetch that stopped short must not render as a
// complete list, or a PR with 500 reviews looks like a PR with 250.
func TestPRSidebar_ReviewCountShowsTruncation(t *testing.T) {
	reviews := []gitpkg.PRReview{
		{Author: "alice", State: "APPROVED"},
		{Author: "bob", State: "COMMENTED"},
	}

	tests := []struct {
		name         string
		reviewsTotal int
		wantHeader   string
	}{
		{name: "complete fetch counts plainly", reviewsTotal: 2, wantHeader: "Reviews (2)"},
		{name: "unknown total counts plainly", reviewsTotal: 0, wantHeader: "Reviews (2)"},
		{name: "truncated fetch says N of M", reviewsTotal: 500, wantHeader: "Reviews (2 of 500)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := buildPRSidebar(nil, reviews, nil, tt.reviewsTotal)
			var headers []string
			for _, it := range items {
				if it.kind == itemHeader {
					headers = append(headers, it.label)
				}
			}
			found := false
			for _, h := range headers {
				if h == tt.wantHeader {
					found = true
				}
			}
			if !found {
				t.Errorf("no %q header; headers = %v", tt.wantHeader, headers)
			}
		})
	}
}

// TestBuildReviewContent_InlineCommentTruncation covers the nested cap: a
// review whose inline comments were capped at one page has to say so in the
// body, since the sidebar row counts reviews, not comments.
func TestBuildReviewContent_InlineCommentTruncation(t *testing.T) {
	tests := []struct {
		name          string
		comments      []gitpkg.PRReviewComment
		commentsTotal int
		wantNote      bool
	}{
		{
			name:          "complete inline comments carry no note",
			comments:      []gitpkg.PRReviewComment{{Path: "a.go", Line: 1, Body: "nit"}},
			commentsTotal: 1,
			wantNote:      false,
		},
		{
			name:          "truncated inline comments say how many are missing",
			comments:      []gitpkg.PRReviewComment{{Path: "a.go", Line: 1, Body: "nit"}},
			commentsTotal: 240,
			wantNote:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gitpkg.PRReview{
				Author:        "alice",
				State:         "COMMENTED",
				Comments:      tt.comments,
				CommentsTotal: tt.commentsTotal,
			}
			got := buildReviewContent(r, 80)
			note := "showing 1 of 240 inline comments"
			if strings.Contains(got, note) != tt.wantNote {
				t.Errorf("contains %q = %v, want %v; content:\n%s", note, !tt.wantNote, tt.wantNote, got)
			}
			// The comments themselves must still render either way.
			if !strings.Contains(got, "nit") {
				t.Errorf("inline comment body missing from content:\n%s", got)
			}
		})
	}
}

// TestPRRefresh_CarriesReviewsTotal checks the plumbing: the truncation
// signal has to survive the trip from PRAllResult through the refresh message
// into the model, or the sidebar can never see it.
func TestPRRefresh_CarriesReviewsTotal(t *testing.T) {
	m := NewModel("/tmp", testGit())
	result, _ := m.Update(prRefreshMsg{
		prInfo:       gitpkg.PRInfoResult{Number: 7, Title: "t"},
		reviews:      []gitpkg.PRReview{{Author: "alice", State: "APPROVED"}},
		reviewsTotal: 500,
	})
	m = result.(*Model)
	if m.prReviewsTotal != 500 {
		t.Fatalf("m.prReviewsTotal = %d, want 500", m.prReviewsTotal)
	}
}
