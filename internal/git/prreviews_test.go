package git_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// reviewsPageJSON builds one `gh api graphql` reviews-page response: the
// envelope PRAll decodes, with a controllable page size, reported total, and
// next-page marker. commentsTotal is what GitHub reports for a review's
// inline comments, which can exceed the nodes actually returned — that gap is
// the nested-collection truncation the UI has to surface.
func reviewsPageJSON(t *testing.T, reviewCount, totalCount int, hasNext bool, cursor string, commentsPerReview, commentsTotal int) string {
	t.Helper()
	nodes := make([]any, 0, reviewCount)
	for i := 0; i < reviewCount; i++ {
		comments := make([]any, 0, commentsPerReview)
		for j := 0; j < commentsPerReview; j++ {
			comments = append(comments, map[string]any{
				"path": "main.go",
				"line": j + 1,
				"body": fmt.Sprintf("inline comment %d", j),
			})
		}
		nodes = append(nodes, map[string]any{
			"author":      map[string]any{"login": fmt.Sprintf("reviewer%s%d", cursor, i)},
			"state":       "COMMENTED",
			"body":        fmt.Sprintf("review body %d", i),
			"submittedAt": "2026-01-01T00:00:00Z",
			"url":         "https://example.test/review",
			"comments": map[string]any{
				"totalCount": commentsTotal,
				"nodes":      comments,
			},
		})
	}
	payload := map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
		"reviews": map[string]any{
			"totalCount": totalCount,
			"pageInfo":   map[string]any{"hasNextPage": hasNext, "endCursor": cursor},
			"nodes":      nodes,
		},
	}}}}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling reviews page: %v", err)
	}
	return string(b)
}

// pagedGH stubs every gh call PRAll makes. Reviews GraphQL queries are served
// from pages in order and recorded, so a test can assert both how many pages
// were fetched and how the cursor was threaded. The deployments GraphQL query
// gets an empty answer of its own so it cannot consume a reviews page.
type pagedGH struct {
	prView  string
	pages   []string
	queries []string
}

func (p *pagedGH) factory() command.Factory {
	return func(name string, args ...string) command.Command {
		if name != "gh" {
			return command.DefaultFactory(name, args...)
		}
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "repo view"):
			return command.StubCommand("testowner/testrepo", nil)
		case strings.HasPrefix(joined, "api graphql"):
			if !strings.Contains(joined, "reviews(first:") {
				return command.StubCommand(`{"data":{}}`, nil)
			}
			i := len(p.queries)
			p.queries = append(p.queries, joined)
			if i < len(p.pages) {
				return command.StubCommand(p.pages[i], nil)
			}
			return command.StubCommand("", fmt.Errorf("reviews query #%d past the page cap", i+1))
		case strings.HasPrefix(joined, "pr view"):
			return command.StubCommand(p.prView, nil)
		}
		return command.StubCommand("", fmt.Errorf("unexpected gh call: %s", joined))
	}
}

// TestPRAll_ReviewPagination covers the reviews collection across the page
// boundary: a single short page, a page filled exactly to the limit, several
// pages stitched together, and the hard cap that bounds how many gh
// subprocesses one refresh can spawn. When the cap truncates, the reported
// total must still reach the caller so the UI can say "showing N of M".
func TestPRAll_ReviewPagination(t *testing.T) {
	const pageSize = 50
	const maxPages = 5

	tests := []struct {
		name        string
		pages       []string
		wantReviews int
		wantTotal   int
		wantQueries int
	}{
		{
			name:        "single page under the limit",
			pages:       []string{reviewsPageJSON(t, 3, 3, false, "", 1, 1)},
			wantReviews: 3,
			wantTotal:   3,
			wantQueries: 1,
		},
		{
			name:        "one page filled exactly to the limit",
			pages:       []string{reviewsPageJSON(t, pageSize, pageSize, false, "", 0, 0)},
			wantReviews: pageSize,
			wantTotal:   pageSize,
			wantQueries: 1,
		},
		{
			name: "full page that claims a next page which comes back empty",
			pages: []string{
				reviewsPageJSON(t, pageSize, pageSize, true, "cur1", 0, 0),
				reviewsPageJSON(t, 0, pageSize, false, "", 0, 0),
			},
			wantReviews: pageSize,
			wantTotal:   pageSize,
			wantQueries: 2,
		},
		{
			name: "multiple pages stitched together",
			pages: []string{
				reviewsPageJSON(t, pageSize, 107, true, "cur1", 0, 0),
				reviewsPageJSON(t, pageSize, 107, true, "cur2", 0, 0),
				reviewsPageJSON(t, 7, 107, false, "", 0, 0),
			},
			wantReviews: 107,
			wantTotal:   107,
			wantQueries: 3,
		},
		{
			name: "cap stops paging and the total surfaces the truncation",
			pages: []string{
				reviewsPageJSON(t, pageSize, 500, true, "cur1", 0, 0),
				reviewsPageJSON(t, pageSize, 500, true, "cur2", 0, 0),
				reviewsPageJSON(t, pageSize, 500, true, "cur3", 0, 0),
				reviewsPageJSON(t, pageSize, 500, true, "cur4", 0, 0),
				reviewsPageJSON(t, pageSize, 500, true, "cur5", 0, 0),
				reviewsPageJSON(t, pageSize, 500, true, "cur6", 0, 0),
			},
			wantReviews: pageSize * maxPages,
			wantTotal:   500,
			wantQueries: maxPages,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupTestRepo(t)
			stub := &pagedGH{prView: `{"number":1}`, pages: tt.pages}
			g := git.NewWithFactory(dir, stub.factory())

			result, err := g.PRAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Reviews) != tt.wantReviews {
				t.Errorf("len(Reviews) = %d, want %d", len(result.Reviews), tt.wantReviews)
			}
			if result.ReviewsTotal != tt.wantTotal {
				t.Errorf("ReviewsTotal = %d, want %d", result.ReviewsTotal, tt.wantTotal)
			}
			if len(stub.queries) != tt.wantQueries {
				t.Fatalf("reviews queries = %d, want %d", len(stub.queries), tt.wantQueries)
			}
			// The first page must not carry a cursor; every later page must
			// carry the previous page's endCursor.
			if strings.Contains(stub.queries[0], `after: "`) {
				t.Errorf("first reviews query threaded a cursor: %s", stub.queries[0])
			}
			for i := 1; i < len(stub.queries); i++ {
				want := fmt.Sprintf(`after: "cur%d"`, i)
				if !strings.Contains(stub.queries[i], want) {
					t.Errorf("reviews query #%d does not contain %s", i+1, want)
				}
			}
		})
	}
}

// TestPRAll_ReviewsTotalUnknownOnFallback checks the non-GraphQL path. When
// the GraphQL fetch fails, reviews come from `gh pr view` and no total is
// reported — the result must claim exactly as many reviews as it carries, so
// nothing downstream renders a phantom "showing 2 of 0".
func TestPRAll_ReviewsTotalUnknownOnFallback(t *testing.T) {
	dir := setupTestRepo(t)
	prView := `{"number":1,"reviews":[{"author":{"login":"alice"},"state":"APPROVED"},{"author":{"login":"bob"},"state":"COMMENTED"}]}`
	g := git.NewWithFactory(dir, func(name string, args ...string) command.Command {
		if name != "gh" {
			return command.DefaultFactory(name, args...)
		}
		joined := strings.Join(args, " ")
		if strings.HasPrefix(joined, "pr view") {
			return command.StubCommand(prView, nil)
		}
		return command.StubCommand("", fmt.Errorf("HTTP 502: Bad gateway"))
	})

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reviews) != 2 {
		t.Fatalf("len(Reviews) = %d, want 2", len(result.Reviews))
	}
	if result.ReviewsTotal != 2 {
		t.Errorf("ReviewsTotal = %d, want 2 (fallback reports what it has)", result.ReviewsTotal)
	}
}

// TestPRAll_ReviewCommentsTruncationSurfaces covers the nested collection.
// Inline comments hang off a paginated parent, so they are capped at one page
// rather than paged per review (which would be an N+1 subprocess storm). The
// cap is only acceptable because the reported total travels with the review.
func TestPRAll_ReviewCommentsTruncationSurfaces(t *testing.T) {
	dir := setupTestRepo(t)
	stub := &pagedGH{
		prView: `{"number":1}`,
		pages:  []string{reviewsPageJSON(t, 1, 1, false, "", 100, 240)},
	}
	g := git.NewWithFactory(dir, stub.factory())

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reviews) != 1 {
		t.Fatalf("len(Reviews) = %d, want 1", len(result.Reviews))
	}
	r := result.Reviews[0]
	if len(r.Comments) != 100 {
		t.Errorf("len(Comments) = %d, want 100", len(r.Comments))
	}
	if r.CommentsTotal != 240 {
		t.Errorf("CommentsTotal = %d, want 240", r.CommentsTotal)
	}
	if len(stub.queries) != 1 {
		t.Errorf("reviews queries = %d, want 1 — inline comments must not fan out per review", len(stub.queries))
	}
}
