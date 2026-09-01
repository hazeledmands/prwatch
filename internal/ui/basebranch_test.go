package ui

import (
	"strings"
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// TestBaseRefChain_SingleSource covers CODE_REVIEW A1 sub-item 4: the ref
// the behind-count is measured against, and the base branch the status bar
// displays, must come from one chain. Previously loadGitData fell back to
// info.Upstream (the branch's *own* remote ref), loadLocalGitData hardcoded
// origin/main, and renderLine2 derived a third answer from Upstream.
func TestBaseRefChain_SingleSource(t *testing.T) {
	cases := []struct {
		name      string
		branch    string
		upstream  string
		prBaseRef string
		wantRef   string // ref BehindCount must be called with
		wantName  string // base branch name the status bar must show
	}{
		{
			name:     "no PR, branch tracks its own remote ref",
			branch:   "feature",
			upstream: "origin/feature",
			wantRef:  "origin/main",
			wantName: "main",
		},
		{
			name:     "no PR, branch tracks the base branch",
			branch:   "feature",
			upstream: "origin/develop",
			wantRef:  "origin/develop",
			wantName: "develop",
		},
		{
			name:      "PR base wins over upstream",
			branch:    "feature",
			upstream:  "origin/develop",
			prBaseRef: "release-2",
			wantRef:   "origin/release-2",
			wantName:  "release-2",
		},
		{
			name:     "no PR, no upstream",
			branch:   "feature",
			wantRef:  "origin/main",
			wantName: "main",
		},
		{
			// The `user/topic/desc` convention: only the remote segment
			// distinguishes the upstream from the branch, so a last-segment
			// comparison wrongly reads it as a *different* branch and
			// measures the branch against its own remote copy.
			name:     "no PR, slashed branch tracks its own remote ref",
			branch:   "hazel/ui/foo",
			upstream: "origin/hazel/ui/foo",
			wantRef:  "origin/main",
			wantName: "main",
		},
		{
			name:     "no PR, slashed branch tracks a slashed base",
			branch:   "hazel/ui/foo",
			upstream: "origin/release/1.2",
			wantRef:  "origin/release/1.2",
			wantName: "release/1.2",
		},
		{
			name:      "slashed PR base keeps its full name",
			branch:    "hazel/ui/foo",
			upstream:  "origin/hazel/ui/foo",
			prBaseRef: "release/1.2",
			wantRef:   "origin/release/1.2",
			wantName:  "release/1.2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newMock := func() *mockGit {
				return &mockGit{
					repoInfo: gitpkg.RepoInfoResult{
						RepoName: "repo", DirName: "repo",
						Branch: c.branch, Upstream: c.upstream,
					},
					prInfo:      gitpkg.PRInfoResult{Number: 4, BaseRef: c.prBaseRef, Title: "t"},
					base:        "abc1234",
					behindCount: 3,
				}
			}

			// loadGitData (full refresh, PR data in hand)
			mock := newMock()
			m := NewModel("/tmp/test-repo", mock)
			m.width, m.height = 120, 40
			m.Update(m.loadGitData())
			if got := mock.behindRefs; len(got) != 1 || got[0] != c.wantRef {
				t.Errorf("loadGitData: BehindCount called with %v, want [%s]", got, c.wantRef)
			}

			// loadLocalGitData (local-only refresh; PR data already on model)
			mock2 := newMock()
			m2 := NewModel("/tmp/test-repo", mock2)
			m2.width, m2.height = 120, 40
			m2.prInfo = gitpkg.PRInfoResult{Number: 4, BaseRef: c.prBaseRef}
			m2.Update(m2.loadLocalGitData())
			if got := mock2.behindRefs; len(got) != 1 || got[0] != c.wantRef {
				t.Errorf("loadLocalGitData: BehindCount called with %v, want [%s]", got, c.wantRef)
			}

			// renderLine2 display
			bar, _, _, _ := renderStatusBar(160, statusBarData{
				info: gitpkg.RepoInfoResult{
					RepoName: "repo", DirName: "repo",
					Branch: c.branch, Upstream: c.upstream,
				},
				pr:   gitpkg.PRInfoResult{Number: 4, BaseRef: c.prBaseRef},
				mode: FilesMode,
			})
			want := c.branch + " → " + c.wantName
			if !strings.Contains(stripANSI(bar), want) {
				t.Errorf("status bar does not show %q; got:\n%s", want, stripANSI(bar))
			}
		})
	}
}
