package ui

import (
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// TestPRSidebarRows_CarryTheirReferent covers CODE_REVIEW A1 sub-item 6:
// openPRItemURL matched sidebar items by label, so two comments from one
// author both resolved to the newest one, a review row could match a comment,
// and CI check "build" matched the "build-arm" row. Each row now records what
// it stands for, and the URL/body paths read it back off the row.
func TestPRSidebarRows_CarryTheirReferent(t *testing.T) {
	comments := []gitpkg.PRComment{
		{Author: "alice", URL: "https://gh/pr/9#c-newest"},
		{Author: "alice", URL: "https://gh/pr/9#c-oldest"},
	}
	reviews := []gitpkg.PRReview{
		{Author: "alice", State: "APPROVED", URL: "https://gh/pr/9#r-1"},
		{Author: "bob", State: "COMMENTED", URL: "https://gh/pr/9#r-2"},
	}
	checks := []gitpkg.CICheck{
		// "build" first so a substring matcher resolves the "build-arm"
		// row to it.
		{Name: "build", Bucket: "fail", URL: "https://ci/build"},
		{Name: "build-arm", Bucket: "pass", URL: "https://ci/build-arm"},
	}

	items := buildPRSidebar(comments, reviews, checks, len(reviews))

	// referentURL reads back what a row stands for, the same way prItemURL
	// and updatePRModeContent do.
	referentURL := func(it sidebarItem) string {
		switch it.role {
		case rolePRComment:
			return it.pr.comment.URL
		case rolePRReview:
			return it.pr.review.URL
		case roleCICheck:
			return it.pr.check.URL
		}
		return ""
	}

	tests := []struct {
		row        string // prefix+label, as rendered
		wantRole   sidebarItemRole
		wantNumber int
		wantURL    string
	}{
		{"Description", rolePRDescription, 0, ""},
		{"#2 @alice", rolePRComment, 2, "https://gh/pr/9#c-newest"},
		{"#1 @alice", rolePRComment, 1, "https://gh/pr/9#c-oldest"},
		{"#2 ✓ @alice", rolePRReview, 2, "https://gh/pr/9#r-1"},
		{"#1 c @bob", rolePRReview, 1, "https://gh/pr/9#r-2"},
		{"[✗] build", roleCICheck, 0, "https://ci/build"},
		{"[✓] build-arm", roleCICheck, 0, "https://ci/build-arm"},
	}
	for _, tc := range tests {
		t.Run(tc.row, func(t *testing.T) {
			var found *sidebarItem
			for i := range items {
				if items[i].prefix+items[i].label == tc.row {
					found = &items[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("sidebar has no row %q; items: %+v", tc.row, items)
			}
			if found.role != tc.wantRole {
				t.Errorf("role = %v, want %v", found.role, tc.wantRole)
			}
			if got := referentURL(*found); got != tc.wantURL {
				t.Errorf("referent URL = %q, want %q", got, tc.wantURL)
			}
			if tc.wantNumber != 0 && found.pr.number != tc.wantNumber {
				t.Errorf("number = %d, want %d", found.pr.number, tc.wantNumber)
			}
		})
	}

	// Rows that stand for nothing carry no referent. Skip the Description row
	// by role, not by label: a label is precisely what must not be load
	// bearing here. The full one-of rule over both builders lives in
	// sidebarreferents_test.go.
	for _, it := range buildPRSidebar(nil, nil, nil, 0) {
		if it.role == rolePRDescription {
			continue
		}
		if it.pr != nil {
			t.Errorf("row %q carries a referent, want none", it.prefix+it.label)
		}
	}

	// Same identity rule for "jump to the first failing CI check": "build"
	// (failing) must select its own row, not the "build-arm" row.
	idx := firstCIFailureIndex(items)
	if idx < 0 {
		t.Fatalf("firstCIFailureIndex found nothing in %+v", items)
	}
	if got := items[idx].prefix + items[idx].label; got != "[✗] build" {
		t.Errorf("firstCIFailureIndex selected %q, want %q", got, "[✗] build")
	}
	// …including when the substring-superset row is listed first.
	reordered := []gitpkg.CICheck{
		{Name: "build-arm", Bucket: "pass", URL: "https://ci/build-arm"},
		{Name: "build", Bucket: "fail", URL: "https://ci/build"},
	}
	ritems := buildPRSidebar(nil, nil, reordered, 0)
	ridx := firstCIFailureIndex(ritems)
	if ridx < 0 {
		t.Fatalf("firstCIFailureIndex found nothing in %+v", ritems)
	}
	if got := ritems[ridx].prefix + ritems[ridx].label; got != "[✗] build" {
		t.Errorf("firstCIFailureIndex selected %q, want %q", got, "[✗] build")
	}
}
