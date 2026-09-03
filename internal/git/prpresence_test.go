package git_test

import (
	"context"
	"errors"
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
	return repoWithRemote(t, "https://github.com/testowner/testrepo.git")
}

func repoWithRemote(t *testing.T, url string) string {
	t.Helper()
	dir := setupTestRepo(t)
	if url != "" {
		runGit(t, dir, "remote", "add", "origin", url)
	}
	return dir
}

// TestPRAll_GitHubRemoteFormsAreRecognized covers the remote-URL shapes git
// accepts. The host check drove the absent-verdict shortcut, so a form it
// failed to parse meant "no GitHub remote" — and every real error on such a
// repo (auth, throttle, network) was silenced forever, on every poll. Prefix-
// matching a reconstructed HTTPS string recognized only two of these six.
//
// Each case gives the repo one remote and fails the probe with an auth
// error: a repo gh can speak to must reach the error state, and only a
// genuinely non-GitHub remote may take the silent shortcut.
func TestPRAll_GitHubRemoteFormsAreRecognized(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		wantErr bool
	}{
		{name: "https", remote: "https://github.com/o/r.git", wantErr: true},
		{name: "scp-like ssh", remote: "git@github.com:o/r.git", wantErr: true},
		{name: "ssh scheme", remote: "ssh://git@github.com/o/r.git", wantErr: true},
		{name: "git protocol", remote: "git://github.com/o/r.git", wantErr: true},
		{name: "non-git ssh user", remote: "ssh://someone@github.com/o/r.git", wantErr: true},
		{name: "credentials in url", remote: "https://user:token@github.com/o/r.git", wantErr: true},
		{name: "uppercase host", remote: "https://GitHub.com/o/r.git", wantErr: true},
		// Negative controls: nothing here is a GitHub remote, so the silent
		// shortcut is correct and no error should reach the user.
		{name: "non-github host", remote: "https://gitlab.com/o/r.git", wantErr: false},
		{name: "scp-like non-github host", remote: "git@gitlab.com:o/r.git", wantErr: false},
		{name: "local path", remote: "/srv/git/r.git", wantErr: false},
		{name: "no remote at all", remote: "", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := repoWithRemote(t, tt.remote)
			// No HTTP evidence in the view error, so the host check is what
			// decides whether we probe at all.
			stub := &probeGH{
				viewErr:  fmt.Errorf("exit status 1: gh: no pull requests found"),
				probeErr: fmt.Errorf("exit status 1: HTTP 401: Bad credentials"),
			}
			g := git.NewWithFactory(dir, stub.factory())

			_, err := g.PRAll()
			if tt.wantErr && err == nil {
				t.Errorf("PRAll() = nil error for remote %q; a GitHub remote must not take the silent no-PR shortcut", tt.remote)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("PRAll() error = %v for remote %q; a non-GitHub remote has no PR to find", err, tt.remote)
			}
		})
	}
}

// TestRepoInfo_RemoteURLForms pins the browse URL built from each remote
// form. The host check and this URL now share one parser, so the forms that
// used to come back empty here resolve — and credentials in a remote never
// reach a URL prwatch might display.
func TestRepoInfo_RemoteURLForms(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{"https://github.com/o/r.git", "https://github.com/o/r"},
		{"http://github.com/o/r.git", "http://github.com/o/r"},
		{"git@github.com:o/r.git", "https://github.com/o/r"},
		{"ssh://git@github.com/o/r.git", "https://github.com/o/r"},
		{"git://github.com/o/r.git", "https://github.com/o/r"},
		{"ssh://someone@github.com/o/r.git", "https://github.com/o/r"},
		{"https://user:token@github.com/o/r.git", "https://github.com/o/r"},
		{"git@gitlab.com:o/r.git", "https://gitlab.com/o/r"},
		// An SSH port is meaningless in a browse URL — and worse, wrong:
		// https://github.com:22/… points nowhere.
		{"ssh://git@github.com:22/o/r.git", "https://github.com/o/r"},
		// An http(s) port is part of the address and stays.
		{"https://ghe.example.com:8443/o/r.git", "https://ghe.example.com:8443/o/r"},
		// No host to speak of: no browse URL.
		{"/srv/git/r.git", ""},
	}

	for _, tt := range tests {
		t.Run(tt.remote, func(t *testing.T) {
			dir := repoWithRemote(t, tt.remote)
			info, err := noGH(dir).RepoInfo()
			if err != nil {
				t.Fatal(err)
			}
			if info.RepoURL != tt.want {
				t.Errorf("RepoURL for %q = %q, want %q", tt.remote, info.RepoURL, tt.want)
			}
		})
	}
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
		// wantProbeHead: the probe must scope itself to the current branch.
		// False for a detached HEAD, where there is no branch to scope to and
		// the probe is a reachability check only.
		wantProbeHead bool
	}{
		{
			name:       "reworded no-PR message, probe reports no PR",
			withRemote: true,
			// Would not have matched any of the old English substrings.
			viewErr:       fmt.Errorf("exit status 1: gh: keine Pull Requests für diesen Branch gefunden"),
			probeOut:      "[]",
			wantErr:       false,
			wantProbe:     true,
			wantProbeHead: true,
		},
		{
			name:          "old-substring wording, but a PR exists: real error",
			withRemote:    true,
			viewErr:       fmt.Errorf(`exit status 1: gh: no pull requests found for branch "hazel/test/feature"`),
			probeOut:      `[{"number":7}]`,
			wantErr:       true,
			wantProbe:     true,
			wantProbeHead: true,
		},
		{
			name:          "old-substring wording, but the probe fails: real error",
			withRemote:    true,
			viewErr:       fmt.Errorf("exit status 1: gh: no open pull requests found"),
			probeErr:      fmt.Errorf("exit status 1: HTTP 401: Bad credentials"),
			wantErr:       true,
			wantProbe:     true,
			wantProbeHead: true,
		},
		{
			name:          "probe output is not JSON: real error, not silence",
			withRemote:    true,
			viewErr:       fmt.Errorf("exit status 1: gh: no pull requests found"),
			probeOut:      "gh: something went sideways",
			wantErr:       true,
			wantProbe:     true,
			wantProbeHead: true,
		},
		{
			name:          "rate limit reaches the caller",
			withRemote:    true,
			viewErr:       fmt.Errorf("exit status 1: HTTP 403: API rate limit exceeded"),
			probeErr:      fmt.Errorf("exit status 1: HTTP 403: API rate limit exceeded"),
			wantErr:       true,
			wantProbe:     true,
			wantProbeHead: true,
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
			// gh missing used to read as "no PR" on every repo, so a
			// GitHub-backed checkout silently showed an empty PR pane with
			// nothing anywhere explaining why. There IS a PR question to
			// answer here and we cannot answer it, so the failure stands and
			// the UI reports it (as ghErrGhMissing, which does not back off).
			name:       "gh is not installed, but the repo is on GitHub: reported",
			withRemote: true,
			viewErr:    fmt.Errorf(`exec: "gh": %w`, command.ErrNotFound),
			wantErr:    true,
			// gh is missing; spending another doomed subprocess to learn that
			// again would tell us nothing.
			wantProbe: false,
		},
		{
			// …and with no GitHub remote there was never a PR to fetch, so a
			// missing gh changes nothing and must stay silent.
			name:       "gh is not installed and the repo is not on GitHub: silent",
			withRemote: false,
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
		// The structural shortcuts above are only safe when the view failure
		// shows no sign of having reached GitHub. These rows carry an HTTP
		// status, so the shortcut must stand aside and let the probe decide.
		{
			name:       "403 on a detached HEAD is surfaced, not swallowed",
			withRemote: true,
			detached:   true,
			viewErr:    fmt.Errorf("exit status 1: HTTP 403: Forbidden"),
			probeErr:   fmt.Errorf("exit status 1: HTTP 403: Forbidden"),
			wantErr:    true,
			wantProbe:  true,
		},
		{
			name:       "403 with no GitHub remote is surfaced, not swallowed",
			withRemote: false,
			viewErr:    fmt.Errorf("exit status 1: HTTP 403: Forbidden"),
			probeErr:   fmt.Errorf("exit status 1: HTTP 403: Forbidden"),
			wantErr:    true,
			wantProbe:  true,
			// The branch is still known here — it is the *remote* that looks
			// absent, and that shortcut is what the HTTP status overrules.
			wantProbeHead: true,
		},
		{
			name:       "transient view failure on a detached HEAD, GitHub reachable",
			withRemote: true,
			detached:   true,
			viewErr:    fmt.Errorf("exit status 1: HTTP 502: Bad gateway"),
			// Repo-scoped: the probe only establishes that GitHub is
			// answering. A detached HEAD has no branch PR either way.
			probeOut:  "[]",
			wantErr:   false,
			wantProbe: true,
		},
		{
			// Negative control for the graphql evidence pattern. A
			// connectivity error quotes the endpoint URL, so an unanchored
			// match on "graphql:" read the *URL path* as gh's error prefix
			// and turned a correctly-silent verdict on a non-GitHub repo into
			// a status-line error on every poll.
			name:       "a connectivity error quoting the graphql URL is not remote evidence",
			withRemote: false,
			viewErr:    fmt.Errorf("exit status 1: Post \"https://api.github.com/graphql: dial tcp 140.82.114.6:443: connect: connection refused"),
			wantErr:    false,
			wantProbe:  false,
		},
		{
			name:          "GraphQL-shaped failure is remote evidence too",
			withRemote:    true,
			viewErr:       fmt.Errorf("exit status 1: GraphQL: Something went wrong"),
			probeErr:      fmt.Errorf("exit status 1: GraphQL: Something went wrong"),
			wantErr:       true,
			wantProbe:     true,
			wantProbeHead: true,
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
			if tt.wantErr && err != nil && !errors.Is(err, tt.viewErr) {
				// The view error is what describes the actual failure; a
				// probe error standing in its place would misreport it.
				t.Errorf("PRAll() error = %v, want the original view error %v", err, tt.viewErr)
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
				// The probe must ask for JSON — an empty array is the whole
				// signal — and must scope to the branch whenever there is one.
				if got := strings.Contains(stub.probeArgs[0], "--head hazel/test/feature"); got != tt.wantProbeHead {
					t.Errorf("probe scoped to branch = %v, want %v: %q", got, tt.wantProbeHead, stub.probeArgs[0])
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
