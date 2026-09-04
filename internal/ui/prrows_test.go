package ui

import (
	"strings"
	"testing"

	git "github.com/hazeledmands/prwatch/internal/git"
)

// prModeModel wires a Model to a mock git carrying PR data, runs one load
// cycle, and leaves it in PR mode with the sidebar built.
func prModeModel(t *testing.T, mock *mockGit) *Model {
	t.Helper()
	m := NewModel("/tmp/test-repo", mock)
	m.width, m.height = 100, 40
	m.updateLayout()
	m.Update(m.loadGitData())
	if m.mode != PRMode {
		m.mode = PRMode
		m.updateSidebarItems()
		m.updateMainContent()
	}
	return m
}

// selectSidebarIndex moves the cursor onto a row by index and renders the
// main pane, returning its body and title-bar halves.
func selectSidebarIndex(t *testing.T, m *Model, idx int) (body, titleLeft, titleRight string) {
	t.Helper()
	m.sidebar.SelectIndex(idx)
	if got := m.sidebar.SelectedIndex(); got != idx {
		t.Fatalf("SelectIndex(%d) landed on %d", idx, got)
	}
	m.updateMainContent()
	return m.mainPane.content, m.mainPane.titleLeft, m.mainPane.titleRight
}

// Two CI checks can share a name (the same job name reported by two
// workflows), so their rendered rows are byte-identical. Resolving the
// selection by regenerating labels and comparing gave both rows the first
// check's body and URL. The row carries the check it was built from, so each
// resolves to its own.
func TestPRMode_DuplicateCheckRowsResolveIndependently(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		prInfo:   git.PRInfoResult{Number: 42, Title: "Test PR", URL: "https://gh/pr/42"},
		commits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		ciChecks: []git.CICheck{
			{Name: "build", Bucket: "pass", State: "COMPLETED", URL: "https://ci/one"},
			{Name: "build", Bucket: "pass", State: "COMPLETED", URL: "https://ci/two"},
		},
	}
	m := prModeModel(t, mock)

	var rows []int
	for i, it := range m.sidebar.items {
		if it.prefix+it.label == "[✓] build" {
			rows = append(rows, i)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 identically-labeled CI rows, got %d", len(rows))
	}

	for n, idx := range rows {
		want := mock.ciChecks[n].URL
		body, left, _ := selectSidebarIndex(t, m, idx)
		if !strings.Contains(body, want) {
			t.Errorf("row %d body = %q, want it to mention %q", n, body, want)
		}
		if left != "CI · build" {
			t.Errorf("row %d title left = %q, want %q", n, left, "CI · build")
		}
		if got := m.prItemURL(); got != want {
			t.Errorf("row %d prItemURL() = %q, want %q", n, got, want)
		}
	}
}

// Every PR-mode row resolves to the referent its builder recorded — including
// two comments by one author and two reviews by one author, whose labels
// differ only in the counted-down "#N" prefix.
func TestPRMode_EveryRowResolvesToItsOwnReferent(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		prInfo:   git.PRInfoResult{Number: 9, Title: "Test PR", URL: "https://gh/pr/9", Body: "the description"},
		commits:  []git.Commit{{SHA: "abc", Subject: "test"}},
		prComments: []git.PRComment{
			{Author: "alice", Body: "newest comment", URL: "https://gh/pr/9#c-newest"},
			{Author: "alice", Body: "oldest comment", URL: "https://gh/pr/9#c-oldest"},
		},
		reviews: []git.PRReview{
			{Author: "alice", State: "APPROVED", Body: "review one", URL: "https://gh/pr/9#r-1"},
			{Author: "alice", State: "APPROVED", Body: "review two", URL: "https://gh/pr/9#r-2"},
		},
		ciChecks: []git.CICheck{
			// "build" first so a substring matcher resolves the "build-arm"
			// row to it.
			{Name: "build", Bucket: "fail", URL: "https://ci/build"},
			{Name: "build-arm", Bucket: "pass", URL: "https://ci/build-arm"},
		},
	}
	m := prModeModel(t, mock)

	tests := []struct {
		row      string // prefix+label, as rendered
		wantBody string
		wantURL  string
	}{
		{"Description", "the description", "https://gh/pr/9"},
		{"#2 @alice", "newest comment", "https://gh/pr/9#c-newest"},
		{"#1 @alice", "oldest comment", "https://gh/pr/9#c-oldest"},
		{"#2 ✓ @alice", "review one", "https://gh/pr/9#r-1"},
		{"#1 ✓ @alice", "review two", "https://gh/pr/9#r-2"},
		{"[✗] build", "https://ci/build", "https://ci/build"},
		{"[✓] build-arm", "https://ci/build-arm", "https://ci/build-arm"},
	}
	for _, tc := range tests {
		t.Run(tc.row, func(t *testing.T) {
			idx := -1
			for i, it := range m.sidebar.items {
				if it.prefix+it.label == tc.row {
					idx = i
					break
				}
			}
			if idx < 0 {
				t.Fatalf("sidebar has no row %q", tc.row)
			}
			body, _, _ := selectSidebarIndex(t, m, idx)
			if !strings.Contains(body, tc.wantBody) {
				t.Errorf("body = %q, want it to mention %q", body, tc.wantBody)
			}
			if got := m.prItemURL(); got != tc.wantURL {
				t.Errorf("prItemURL() = %q, want %q", got, tc.wantURL)
			}
		})
	}
}

// A pseudo-entry row ("(no comments)") carries no referent: the main pane
// clears rather than showing the previously-selected item's body.
func TestPRMode_PseudoEntryRowClearsPane(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		base:     "abc",
		prInfo:   git.PRInfoResult{Number: 1, Title: "Test PR", URL: "https://gh/pr/1"},
		commits:  []git.Commit{{SHA: "abc", Subject: "test"}},
	}
	m := prModeModel(t, mock)

	for _, label := range []string{"(no comments)", "(no reviews)", "(no CI checks)"} {
		idx := -1
		for i, it := range m.sidebar.items {
			if it.label == label {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("sidebar has no row %q", label)
		}
		body, left, right := selectSidebarIndex(t, m, idx)
		if body != "" {
			t.Errorf("%s body = %q, want empty", label, body)
		}
		if left != label || right != "" {
			t.Errorf("%s title = (%q, %q), want (%q, \"\")", label, left, right, label)
		}
		if got := m.prItemURL(); got != "" {
			t.Errorf("%s prItemURL() = %q, want empty", label, got)
		}
	}
}
