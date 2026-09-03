package ui

import (
	"fmt"
	"testing"
	"time"

	git "github.com/hazeledmands/prwatch/internal/git"
)

// commitsModeModel wires a Model to a mock git, runs one load cycle, and
// leaves it in commits mode with the sidebar built.
func commitsModeModel(t *testing.T, mock *mockGit) *Model {
	t.Helper()
	m := NewModel("/tmp/test-repo", mock)
	m.mode = CommitsMode
	m.width, m.height = 100, 40
	m.updateLayout()
	m.Update(m.loadGitData())
	return m
}

// selectSidebarLabel moves the cursor onto the row with the given label and
// renders the main pane, returning its body and title-bar halves.
func selectSidebarLabel(t *testing.T, m *Model, label string) (body, titleLeft, titleRight string) {
	t.Helper()
	for i := range m.sidebar.items {
		if m.sidebar.items[i].label != label {
			continue
		}
		m.sidebar.SelectIndex(i)
		if got := m.sidebar.SelectedItem(); got != label {
			t.Fatalf("selected %q, want %q", got, label)
		}
		m.updateMainContent()
		return m.mainPane.content, m.mainPane.titleLeft, m.mainPane.titleRight
	}
	t.Fatalf("sidebar row %q not found; rows: %v", label, sidebarLabels(m))
	return "", "", ""
}

func sidebarLabels(m *Model) []string {
	out := make([]string, 0, len(m.sidebar.items))
	for _, it := range m.sidebar.items {
		out = append(out, it.label)
	}
	return out
}

// The bug: the commits-mode Base section's rows come from m.baseCommits, but
// the main-content path resolved the selected row by searching m.commits
// only — so selecting a Base row rendered an empty pane. PROMPT.md's commits
// mode says the right pane shows "the patch associated with the commit", with
// no exemption for the Base section.
func TestCommitsMode_RowBodyMatchesItsCommit(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:     "feature",
			Upstream:   "origin/feature",
			AheadCount: 1,
		},
		base: "origin/main",
		commits: []git.Commit{
			{SHA: "aaaaaaa1111", Subject: "unpushed one", Author: "ann", AuthorDate: when},
			{SHA: "bbbbbbb2222", Subject: "pushed one", Author: "bob", AuthorDate: when},
		},
		baseCommits: []git.Commit{
			{SHA: "ccccccc3333", Subject: "base one", Author: "cyd", AuthorDate: when},
			{SHA: "ddddddd4444", Subject: "base two", Author: "dee", AuthorDate: when},
		},
		commitPatches: map[string]string{
			"aaaaaaa1111": "diff --git a/a b/a\n@@ -1 +1 @@\n-a\n+A\n",
			"bbbbbbb2222": "diff --git a/b b/b\n@@ -1 +1 @@\n-b\n+B\n",
			"ccccccc3333": "diff --git a/c b/c\n@@ -1 +1 @@\n-c\n+C\n",
			"ddddddd4444": "diff --git a/d b/d\n@@ -1 +1 @@\n-d\n+D\n",
		},
	}
	m := commitsModeModel(t, mock)

	all := append(append([]git.Commit{}, mock.commits...), mock.baseCommits...)
	for _, c := range all {
		t.Run(c.Subject, func(t *testing.T) {
			label := fmt.Sprintf("%.7s %s", c.SHA, c.Subject)
			body, left, right := selectSidebarLabel(t, m, label)
			if want := mock.commitPatches[c.SHA]; body != want {
				t.Errorf("body = %q, want %q", body, want)
			}
			if want := commitTitleLeft(c); left != want {
				t.Errorf("title left = %q, want %q", left, want)
			}
			if want := formatAuthorAndTime(c.Author, c.AuthorDate); right != want {
				t.Errorf("title right = %q, want %q", right, want)
			}
		})
	}
}

// A base commit and a branch commit can share a subject; the row's identity
// must still resolve to the commit it was built from, not the first sha-7
// match in some other list.
func TestCommitsMode_BaseRowWithDuplicateSubject(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", Upstream: "origin/feature"},
		base:     "origin/main",
		commits: []git.Commit{
			{SHA: "1111111aaaa", Subject: "same subject"},
		},
		baseCommits: []git.Commit{
			{SHA: "2222222bbbb", Subject: "same subject"},
		},
		commitPatches: map[string]string{
			"1111111aaaa": "branch patch\n",
			"2222222bbbb": "base patch\n",
		},
	}
	m := commitsModeModel(t, mock)

	body, _, _ := selectSidebarLabel(t, m, "2222222 same subject")
	if body != "base patch\n" {
		t.Errorf("base row body = %q, want %q", body, "base patch\n")
	}
}

// SelectedCommit is the seam: the builder records which commit a row stands
// for, so the content path never re-derives it from the label.
func TestSelectedCommit(t *testing.T) {
	commits := []git.Commit{{SHA: "aaaaaaa1111", Subject: "branch"}}
	baseCommits := []git.Commit{{SHA: "bbbbbbb2222", Subject: "base"}}
	items := buildCommitsSidebar(commits, baseCommits, nil, nil, 0, 0, 0)

	sb := newSidebar()
	sb.SetItems(items)

	tests := []struct {
		label   string
		wantSHA string // "" means "row carries no commit"
	}{
		{"aaaaaaa branch", "aaaaaaa1111"},
		{"bbbbbbb base", "bbbbbbb2222"},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			found := false
			for i := range items {
				if items[i].label == tc.label {
					sb.SelectIndex(i)
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("row %q not built", tc.label)
			}
			got := sb.SelectedCommit()
			if got == nil {
				t.Fatalf("SelectedCommit() = nil, want sha %s", tc.wantSHA)
			}
			if got.SHA != tc.wantSHA {
				t.Errorf("SelectedCommit().SHA = %q, want %q", got.SHA, tc.wantSHA)
			}
		})
	}

	// Non-commit rows (headers, pseudo-entries, "load more") carry no commit.
	items = buildCommitsSidebar(nil, nil, []string{"f.go"}, nil, 0, 0, 5)
	sb.SetItems(items)
	for i := range items {
		if !items[i].kind.selectable() {
			continue
		}
		sb.SelectIndex(i)
		if c := sb.SelectedCommit(); c != nil {
			t.Errorf("row %q carries commit %q, want none", items[i].label, c.SHA)
		}
	}
}
