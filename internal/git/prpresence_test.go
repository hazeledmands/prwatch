package git_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
)

// probeGH stubs the gh calls PRAll makes when `gh pr view` fails: the view
// itself, and the `gh pr list` probe that answers "does this branch have a PR
// at all?". It records the probe's arguments so a test can assert the probe
// was (or was not) issued.
type probeGH struct {
	viewOut   string
	viewErr   error
	probeOut  string
	probeErr  error
	probeArgs []string
}

func (p *probeGH) factory() command.Factory {
	return func(name string, args ...string) command.Command {
		if name != "gh" {
			return command.DefaultFactory(name, args...)
		}
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "pr list"):
			p.probeArgs = append(p.probeArgs, joined)
			return command.StubCommand(p.probeOut, p.probeErr)
		case strings.HasPrefix(joined, "pr view"):
			return command.StubCommand(p.viewOut, p.viewErr)
		}
		// Reviews/deployments/checks calls are irrelevant here.
		return command.StubCommand("", fmt.Errorf("unstubbed gh call: %s", joined))
	}
}

// repoWithGitHubRemote is setupTestRepo plus an origin that points at
// GitHub, which is what makes gh worth asking about the repo at all.
func repoWithGitHubRemote(t *testing.T) string {
	t.Helper()
	dir := setupTestRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/testowner/testrepo.git")
	return dir
}

// TestPRAll_NoPRIsAStructuredSignal pins the three states a failed
// `gh pr view` can mean apart: a branch with no PR, a branch with a PR whose
// fetch failed, and a query that failed on its own terms. The old code told
// them apart by matching gh's English error text, so a reworded message
// flipped "no PR" into a hard error — and, worse, an auth or network failure
// whose text happened to match read as "no PR" and vanished from the UI.
//
// Nothing here matches prose. The cases deliberately mix the wording:
// messages that would NOT have matched the old substrings must still reach
// the no-PR state, and messages that WOULD have matched must still reach the
// error state when the structured signal says a PR exists or the query broke.
func TestPRAll_NoPRIsAStructuredSignal(t *testing.T) {
	tests := []struct {
		name string
		// withRemote: give the repo a GitHub origin. Without one there is
		// nothing for gh to answer about, and no error worth showing.
		withRemote bool
		detached   bool
		viewErr    error
		probeOut   string
		probeErr   error
		wantErr    bool
		wantProbe  bool
	}{
		{
			name:       "reworded no-PR message, probe reports no PR",
			withRemote: true,
			// Would not have matched any of the old English substrings.
			viewErr:   fmt.Errorf("exit status 1: gh: keine Pull Requests für diesen Branch gefunden"),
			probeOut:  "[]",
			wantErr:   false,
			wantProbe: true,
		},
		{
			name:       "old-substring wording, but a PR exists: real error",
			withRemote: true,
			viewErr:    fmt.Errorf(`exit status 1: gh: no pull requests found for branch "hazel/test/feature"`),
			probeOut:   `[{"number":7}]`,
			wantErr:    true,
			wantProbe:  true,
		},
		{
			name:       "old-substring wording, but the probe fails: real error",
			withRemote: true,
			viewErr:    fmt.Errorf("exit status 1: gh: no open pull requests found"),
			probeErr:   fmt.Errorf("exit status 1: HTTP 401: Bad credentials"),
			wantErr:    true,
			wantProbe:  true,
		},
		{
			name:       "probe output is not JSON: real error, not silence",
			withRemote: true,
			viewErr:    fmt.Errorf("exit status 1: gh: no pull requests found"),
			probeOut:   "gh: something went sideways",
			wantErr:    true,
			wantProbe:  true,
		},
		{
			name:       "rate limit reaches the caller",
			withRemote: true,
			viewErr:    fmt.Errorf("exit status 1: HTTP 403: API rate limit exceeded"),
			probeErr:   fmt.Errorf("exit status 1: HTTP 403: API rate limit exceeded"),
			wantErr:    true,
			wantProbe:  true,
		},
		{
			name:       "timed-out subprocess is never no-PR",
			withRemote: true,
			viewErr:    fmt.Errorf("gh timed out after 45s: %w", context.DeadlineExceeded),
			wantErr:    true,
			// A deadline is decisive on its own; don't spend another
			// subprocess asking GitHub about it.
			wantProbe: false,
		},
		{
			name:       "no remote at all: no PR, no probe, no error",
			withRemote: false,
			viewErr:    fmt.Errorf("exit status 1: gh: no git remotes found"),
			wantErr:    false,
			wantProbe:  false,
		},
		{
			name:       "gh is not installed: no PR, no error",
			withRemote: true,
			viewErr:    fmt.Errorf(`exec: "gh": %w`, command.ErrNotFound),
			wantErr:    false,
			wantProbe:  false,
		},
		{
			name:       "detached HEAD has no branch to match",
			withRemote: true,
			detached:   true,
			viewErr:    fmt.Errorf("exit status 1: gh: could not determine current branch"),
			wantErr:    false,
			wantProbe:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dir string
			if tt.withRemote {
				dir = repoWithGitHubRemote(t)
			} else {
				dir = setupTestRepo(t)
			}
			if tt.detached {
				runGit(t, dir, "checkout", "--detach", "HEAD")
			}

			stub := &probeGH{viewErr: tt.viewErr, probeOut: tt.probeOut, probeErr: tt.probeErr}
			g := git.NewWithFactory(dir, stub.factory())

			result, err := g.PRAll()
			if tt.wantErr && err == nil {
				t.Errorf("PRAll() = nil error, want the failure surfaced")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("PRAll() error = %v, want no PR reported quietly", err)
			}
			if result.Info.Number != 0 {
				t.Errorf("PRAll() returned PR #%d on a failed view", result.Info.Number)
			}
			if got := len(stub.probeArgs) > 0; got != tt.wantProbe {
				t.Errorf("probe issued = %v, want %v (args: %v)", got, tt.wantProbe, stub.probeArgs)
			}
			if tt.wantProbe && len(stub.probeArgs) > 0 {
				// The probe must ask about this branch specifically, and must
				// ask for JSON — an empty array is the whole signal.
				if !strings.Contains(stub.probeArgs[0], "--head hazel/test/feature") {
					t.Errorf("probe did not scope to the current branch: %q", stub.probeArgs[0])
				}
				if !strings.Contains(stub.probeArgs[0], "--json number") {
					t.Errorf("probe did not request JSON: %q", stub.probeArgs[0])
				}
			}
		})
	}
}

// TestPRAll_NoProbeOnSuccess checks the probe stays on the failure path: a
// successful `gh pr view` must not cost a second subprocess on every poll.
func TestPRAll_NoProbeOnSuccess(t *testing.T) {
	dir := repoWithGitHubRemote(t)
	stub := &probeGH{viewOut: `{"number":3,"title":"t"}`}
	g := git.NewWithFactory(dir, func(name string, args ...string) command.Command {
		if name == "gh" && strings.HasPrefix(strings.Join(args, " "), "pr list") {
			stub.probeArgs = append(stub.probeArgs, strings.Join(args, " "))
			return command.StubCommand("[]", nil)
		}
		if name == "gh" && strings.HasPrefix(strings.Join(args, " "), "pr view") {
			return command.StubCommand(stub.viewOut, nil)
		}
		if name == "gh" {
			// Reviews / deployments best-effort calls; let them fail.
			return command.StubCommand("", fmt.Errorf("HTTP 502: Bad gateway"))
		}
		return command.DefaultFactory(name, args...)
	})

	result, err := g.PRAll()
	if err != nil {
		t.Fatal(err)
	}
	if result.Info.Number != 3 {
		t.Errorf("PR number = %d, want 3", result.Info.Number)
	}
	if len(stub.probeArgs) != 0 {
		t.Errorf("probe issued on the success path: %v", stub.probeArgs)
	}
}
