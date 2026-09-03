package ui

import (
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// TestPRItemURL_ExactIdentity covers CODE_REVIEW A1 sub-item 6:
// openPRItemURL matched sidebar items by substring, so two comments from
// one author both resolved to the newest one, a review row could match a
// comment, and CI check "build" matched the "build-arm" row.
func TestPRItemURL_ExactIdentity(t *testing.T) {
	pr := gitpkg.PRInfoResult{Number: 9, URL: "https://gh/pr/9"}
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

	// The sidebar is the source of the labels the user activates: resolve
	// against exactly what buildPRSidebar renders.
	items := buildPRSidebar(comments, reviews, checks, len(reviews))
	labelOf := func(want string) string {
		for _, it := range items {
			if it.kind != itemNormal {
				continue
			}
			if it.prefix+it.label == want {
				return want
			}
		}
		t.Fatalf("sidebar has no item %q; items: %+v", want, items)
		return ""
	}

	cases := []struct {
		selected string
		want     string
	}{
		{"Description", "https://gh/pr/9"},
		{labelOf("#2 @alice"), "https://gh/pr/9#c-newest"},
		{labelOf("#1 @alice"), "https://gh/pr/9#c-oldest"},
		{labelOf("#2 ✓ @alice"), "https://gh/pr/9#r-1"},
		{labelOf("#1 c @bob"), "https://gh/pr/9#r-2"},
		{labelOf("[✓] build-arm"), "https://ci/build-arm"},
		{labelOf("[✗] build"), "https://ci/build"},
		{"(no comments)", ""},
	}

	for _, c := range cases {
		if got := prItemURL(c.selected, pr, comments, reviews, checks); got != c.want {
			t.Errorf("prItemURL(%q) = %q, want %q", c.selected, got, c.want)
		}
	}

	// Same identity rule for "jump to the first failing CI check": "build"
	// (failing) must select its own row, not the "build-arm" row.
	idx := firstCIFailureIndex(items, checks)
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
	ridx := firstCIFailureIndex(ritems, reordered)
	if ridx < 0 {
		t.Fatalf("firstCIFailureIndex found nothing in %+v", ritems)
	}
	if got := ritems[ridx].prefix + ritems[ridx].label; got != "[✗] build" {
		t.Errorf("firstCIFailureIndex selected %q, want %q", got, "[✗] build")
	}
}
