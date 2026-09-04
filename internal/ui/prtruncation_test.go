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

// TestBuildReviewContent_InlineCommentsRenderMarkdown is the regression test
// for inline review comment bodies being appended verbatim: `**bold**`,
// backticks, and links showed as literal markdown in the review view while the
// review's own top-level body was rendered (BUG_REPORTS.md, 2026-09-04).
func TestBuildReviewContent_InlineCommentsRenderMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantVisible string   // rendered text, after markdown processing
		wantANSI    string   // styling the markdown must have produced
		notVisible  []string // markdown syntax that must not survive verbatim
	}{
		{
			name:        "bold",
			body:        "**Low** — invariant bookkeeping.",
			wantVisible: "Low — invariant bookkeeping.",
			wantANSI:    ansiBold + "Low" + ansiReset,
			notVisible:  []string{"**"},
		},
		{
			name:        "inline code",
			body:        "set `exp-histogram-max-buckets` to `1`",
			wantVisible: "set exp-histogram-max-buckets to 1",
			wantANSI:    ansiCodeBg + ansiCodeFg + "exp-histogram-max-buckets" + ansiReset,
			notVisible:  []string{"`"},
		},
		{
			name:        "link",
			body:        "see [the docs](https://example.com/docs)",
			wantVisible: "see the docs (https://example.com/docs)",
			wantANSI:    ansiLinkFg + "the docs" + ansiReset,
			notVisible:  []string{"[the docs]", "](", "[image"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gitpkg.PRReview{
				Author:        "alice",
				State:         "COMMENTED",
				Comments:      []gitpkg.PRReviewComment{{Path: "a.go", Line: 12, Body: tt.body}},
				CommentsTotal: 1,
			}
			got := buildReviewContent(r, 80)
			visible := stripANSIForWidth(got)
			if !strings.Contains(visible, tt.wantVisible) {
				t.Errorf("rendered text %q missing; visible content:\n%s", tt.wantVisible, visible)
			}
			if !strings.Contains(got, tt.wantANSI) {
				t.Errorf("markdown styling %q missing; content:\n%q", tt.wantANSI, got)
			}
			for _, raw := range tt.notVisible {
				if strings.Contains(visible, raw) {
					t.Errorf("markdown syntax %q survived verbatim; visible content:\n%s", raw, visible)
				}
			}
			// The separator naming the file and line stays plain text.
			if !strings.Contains(visible, "--- a.go:12 ---") {
				t.Errorf("path:line separator missing; visible content:\n%s", visible)
			}
		})
	}
}

// TestReviewView_CodeBlockDoesNotBleedIntoNextSeparator is the symptom-level
// regression for the carried-styling bug pinned by
// TestWrapLines_LineOwnResetBeatsCarriedStyling: an inline comment ending in a
// fenced code block left the code background painted onto the next comment's
// `--- path:line ---` separator when the review rendered in the main pane.
func TestReviewView_CodeBlockDoesNotBleedIntoNextSeparator(t *testing.T) {
	r := gitpkg.PRReview{
		Author: "alice",
		State:  "COMMENTED",
		Comments: []gitpkg.PRReviewComment{
			{Path: "a.go", Line: 1, Body: "Record the cap:\n\n```go\nconv.Capped = true\n```"},
			{Path: "b.go", Line: 2, Body: "Another note."},
		},
		CommentsTotal: 2,
	}
	mp := newMainPane()
	mp.SetSize(80, 30)
	mp.ShowItem(paneContent{body: buildReviewContent(r, 80)})

	rows := mp.viewportLines()
	sep := -1
	for i, row := range rows {
		if strings.Contains(stripANSIForWidth(row), "--- b.go:2 ---") {
			sep = i
			break
		}
	}
	if sep < 0 {
		t.Fatalf("second separator not found in rows: %q", rows)
	}

	// The code block itself must carry its background — otherwise this test is
	// not exercising a styled block at all.
	sawCodeBg := false
	for _, row := range rows[:sep] {
		if strings.Contains(row, ansiCodeBg) {
			sawCodeBg = true
			break
		}
	}
	if !sawCodeBg {
		t.Fatalf("no row before the separator carries the code background; rows: %q", rows)
	}

	// Each row is style-self-contained, so background bleeding onto the
	// separator can only appear as a re-opened sequence in the row itself.
	var carried sgrModel
	for _, row := range rows[:sep] {
		_, carried = modelRow(carried, row)
	}
	cells, _ := modelRow(carried, rows[sep])
	for j, cell := range cells {
		if cell.bg != "" {
			t.Errorf("separator row %q cell %d renders with background %q carried over from the code block",
				rows[sep], j, cell.bg)
			break
		}
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
