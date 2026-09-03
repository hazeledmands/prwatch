package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	git "github.com/hazeledmands/prwatch/internal/git"
)

// TestErr_ClearedBySuccessfulLoad covers CODE_REVIEW A3: m.err was set on a
// failed git load and never cleared, so one transient failure (index.lock
// during a rebase) wedged the UI on the error screen for the rest of the
// session while the 5s tick kept loading good data behind it.
//
// Every gitDataMsg that isn't itself an error carries proof the local git
// half succeeded, so all of them clear the error — including a local-only
// refresh, a load whose *PR* half failed (that failure surfaces separately
// as prError), and a load discarded by the stale-scope guard.
func TestErr_ClearedBySuccessfulLoad(t *testing.T) {
	okMsg := func() gitDataMsg {
		return gitDataMsg{
			repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
			changes:  git.NewChangedFiles(),
		}
	}
	cases := []struct {
		name   string
		follow func() gitDataMsg
	}{
		{"full load", okMsg},
		{"local-only refresh", func() gitDataMsg {
			m := okMsg()
			m.localOnly = true
			return m
		}},
		{"local half ok, PR half failed", func() gitDataMsg {
			m := okMsg()
			m.prFetchFailed = true
			return m
		}},
		{"discarded by stale-scope guard", func() gitDataMsg {
			m := okMsg()
			m.reqScrubbedBase = "some-other-base"
			return m
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
			m := NewModel("/tmp", mg)
			m.width, m.height = 80, 24
			m.updateLayout()

			res, _ := m.Update(gitDataMsg{err: fmt.Errorf("index.lock exists")})
			m = res.(*Model)
			if m.err == nil {
				t.Fatal("expected err to be set by the failed load")
			}

			res, _ = m.Update(c.follow())
			m = res.(*Model)
			if m.err != nil {
				t.Fatalf("err not cleared by a successful load: %v", m.err)
			}
			if v := m.View(); strings.Contains(v.Content, "index.lock exists") {
				t.Error("View still renders the stale error screen")
			}
		})
	}
}

// TestFetchPRStatus_ClassifiesErrors covers CODE_REVIEW A3: fetchPRStatus
// mapped *every* PRAll error to rateLimited, so expired gh auth reported
// "GitHub API rate limited" forever and every failure triggered rate-limit
// backoff. Classification goes through classifyGitHubError.
func TestFetchPRStatus_ClassifiesErrors(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantKind   githubErrorKind
		wantFailed bool
	}{
		{"no error", nil, ghErrNone, false},
		{"primary rate limit", fmt.Errorf("API rate limit exceeded for user"), ghErrRateLimited, true},
		{"secondary rate limit", fmt.Errorf("You have exceeded a secondary rate limit"), ghErrRateLimited, true},
		{"403", fmt.Errorf("HTTP 403: Forbidden"), ghErrAuth, true},
		{"expired auth", fmt.Errorf("gh: authentication token expired"), ghErrAuth, true},
		{"network", fmt.Errorf("dial tcp: lookup api.github.com: no such host"), ghErrOther, true},
		{"no PR", fmt.Errorf("no pull requests found for branch"), ghErrOther, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mg := &mockGit{prInfo: git.PRInfoResult{Number: 7}, prInfoErr: c.err}
			msg := fetchPRStatus(mg, 0).(prRefreshMsg)
			if msg.errKind != c.wantKind {
				t.Errorf("errKind = %v, want %v", msg.errKind, c.wantKind)
			}
			if msg.fetchFailed != c.wantFailed {
				t.Errorf("fetchFailed = %v, want %v", msg.fetchFailed, c.wantFailed)
			}
		})
	}
}

// A non-rate-limit failure must not trigger rate-limit backoff, and must not
// claim the app is rate limited. It takes the generic PR-error path, which
// renders the same "GitHub API error" the PR-inclusive git load already used.
func TestPRRefresh_NonRateLimitErrorTakesGenericPath(t *testing.T) {
	mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()
	before := m.activity.PRInterval()

	res, _ := m.Update(prRefreshMsg{fetchFailed: true})
	m = res.(*Model)

	if m.prError != "GitHub API error" {
		t.Errorf("prError = %q, want %q", m.prError, "GitHub API error")
	}
	if got := m.activity.PRInterval(); got != before {
		t.Errorf("PR interval = %v, want it unchanged at %v (no backoff for non-rate-limit errors)", got, before)
	}
}

// TestRateLimitBackoff_TickLoop covers CODE_REVIEW A3: the backoff was a
// no-op end-to-end. The bump landed on the tracker, but the tick that
// delivered the 403 had already scheduled the next fetch at the un-bumped
// interval, and that tick's handler then called ResetPRInterval and
// recomputed the bump away. This walks the real loop:
// tick → fetch → 403 → bump → next tick scheduled at the bumped interval →
// success → reset.
func TestRateLimitBackoff_TickLoop(t *testing.T) {
	var scheduled []time.Duration
	restore := prTickScheduler
	prTickScheduler = func(d time.Duration) tea.Cmd {
		scheduled = append(scheduled, d)
		return func() tea.Msg { return prTickMsg{} }
	}
	t.Cleanup(func() { prTickScheduler = restore })

	mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()

	// A tick fires, dispatches a fetch, and arms the next tick at the active
	// interval.
	res, cmd := m.Update(prTickMsg{})
	m = res.(*Model)
	if !containsFetch(drain(cmd)) {
		t.Fatal("a due tick must dispatch a PR fetch")
	}
	if got := scheduled[len(scheduled)-1]; got != prRefreshActive {
		t.Fatalf("first tick scheduled at %v, want %v", got, prRefreshActive)
	}

	// That fetch comes back rate limited: double the interval and latch it.
	res, _ = m.Update(prRefreshMsg{fetchFailed: true, errKind: ghErrRateLimited})
	m = res.(*Model)
	if got := m.activity.PRInterval(); got != prRefreshActive*2 {
		t.Fatalf("interval after 403 = %v, want %v", got, prRefreshActive*2)
	}

	// The tick armed *before* the 403 now fires, 30s in — sooner than the 60s
	// backoff allows. It must not fetch, must not recompute the backoff away,
	// and must re-arm for the remainder of the backoff.
	res, cmd = m.Update(prTickMsg{})
	m = res.(*Model)
	if containsFetch(drain(cmd)) {
		t.Error("a tick inside the backoff window dispatched a fetch anyway")
	}
	if got := m.activity.PRInterval(); got != prRefreshActive*2 {
		t.Errorf("interval after a tick during backoff = %v, want %v", got, prRefreshActive*2)
	}
	if got := scheduled[len(scheduled)-1]; got > prRefreshActive*2 || got < prRefreshActive {
		t.Errorf("held-back tick re-armed at %v, want the remainder of %v", got, prRefreshActive*2)
	}

	// Once the backoff has elapsed the fetch goes out, and the tick after it is
	// armed at the bumped interval.
	m.activity.lastPRFetch = time.Now().Add(-prRefreshActive * 3)
	res, cmd = m.Update(prTickMsg{})
	m = res.(*Model)
	if !containsFetch(drain(cmd)) {
		t.Fatal("no fetch dispatched after the backoff elapsed")
	}
	if got := scheduled[len(scheduled)-1]; got != prRefreshActive*2 {
		t.Errorf("tick after the backed-off fetch armed at %v, want %v", got, prRefreshActive*2)
	}

	// A second 403 doubles again.
	res, _ = m.Update(prRefreshMsg{fetchFailed: true, errKind: ghErrRateLimited})
	m = res.(*Model)
	if got := m.activity.PRInterval(); got != prRefreshActive*4 {
		t.Fatalf("interval after second 403 = %v, want %v", got, prRefreshActive*4)
	}

	// A success decays the backoff back to the activity-derived interval, and
	// the next tick both fetches and arms there.
	res, _ = m.Update(prRefreshMsg{})
	m = res.(*Model)
	if got := m.activity.PRInterval(); got != prRefreshActive {
		t.Fatalf("interval after success = %v, want %v", got, prRefreshActive)
	}
	res, cmd = m.Update(prTickMsg{})
	m = res.(*Model)
	if !containsFetch(drain(cmd)) {
		t.Error("no fetch dispatched after the rate limit cleared")
	}
	if got := scheduled[len(scheduled)-1]; got != prRefreshActive {
		t.Errorf("next tick after success scheduled at %v, want %v", got, prRefreshActive)
	}
}

// drain runs a command (unwrapping a batch) and returns the messages produced,
// recording any prTickScheduler calls made along the way. Tick commands are
// stubbed in these tests, so this never blocks on a real timer.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drain(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// containsFetch reports whether a drained command actually went to GitHub —
// a PR fetch is the only thing that answers with a prRefreshMsg.
func containsFetch(msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(prRefreshMsg); ok {
			return true
		}
	}
	return false
}

// TestChecksError_PreservesPreviousChecks covers CODE_REVIEW A3: PRChecksAll
// swallowed its errors and the callers applied the resulting zero value, so a
// transient failure on the checks call blanked the CI panel. The previous
// checks are kept instead.
func TestChecksError_PreservesPreviousChecks(t *testing.T) {
	setup := func() *Model {
		mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
		m := NewModel("/tmp", mg)
		m.width, m.height = 80, 24
		m.updateLayout()
		res, _ := m.Update(prRefreshMsg{
			prInfo:   git.PRInfoResult{Number: 7, Title: "t"},
			ciStatus: git.CIStatusResult{State: "SUCCESS"},
			ciChecks: []git.CICheck{{Name: "build", Bucket: "pass"}},
		})
		return res.(*Model)
	}

	t.Run("prRefreshMsg", func(t *testing.T) {
		m := setup()
		res, _ := m.Update(prRefreshMsg{
			prInfo:       git.PRInfoResult{Number: 7, Title: "t"},
			checksFailed: true,
		})
		m = res.(*Model)
		if len(m.ciChecks) != 1 || m.ciStatus.State != "SUCCESS" {
			t.Errorf("checks blanked by a checks-fetch failure: checks=%v status=%q", m.ciChecks, m.ciStatus.State)
		}
	})

	t.Run("gitDataMsg", func(t *testing.T) {
		m := setup()
		res, _ := m.Update(gitDataMsg{
			repoInfo:     git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
			changes:      git.NewChangedFiles(),
			prInfo:       git.PRInfoResult{Number: 7, Title: "t"},
			checksFailed: true,
		})
		m = res.(*Model)
		if len(m.ciChecks) != 1 || m.ciStatus.State != "SUCCESS" {
			t.Errorf("checks blanked by a checks-fetch failure: checks=%v status=%q", m.ciChecks, m.ciStatus.State)
		}
	})
}

// Preserving CI data across a checks failure is only right while it describes
// the *same* PR. When the PR changed underneath us (branch switch, PR
// recreated), the previous PR's checks must not render under the new PR's
// header — clear them and wait for a fetch that succeeds.
func TestChecksError_ClearedWhenPRChanged(t *testing.T) {
	setup := func() *Model {
		mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
		m := NewModel("/tmp", mg)
		m.width, m.height = 80, 24
		m.updateLayout()
		res, _ := m.Update(prRefreshMsg{
			prInfo:   git.PRInfoResult{Number: 7, Title: "old"},
			ciStatus: git.CIStatusResult{State: "SUCCESS"},
			ciChecks: []git.CICheck{{Name: "build", Bucket: "pass"}},
		})
		return res.(*Model)
	}

	t.Run("prRefreshMsg", func(t *testing.T) {
		m := setup()
		res, _ := m.Update(prRefreshMsg{
			prInfo:       git.PRInfoResult{Number: 8, Title: "new"},
			checksFailed: true,
		})
		m = res.(*Model)
		if len(m.ciChecks) != 0 || m.ciStatus.State != "" {
			t.Errorf("PR #7's checks still shown under PR #8: checks=%v status=%q", m.ciChecks, m.ciStatus.State)
		}
	})

	t.Run("gitDataMsg", func(t *testing.T) {
		m := setup()
		res, _ := m.Update(gitDataMsg{
			repoInfo:     git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
			changes:      git.NewChangedFiles(),
			prInfo:       git.PRInfoResult{Number: 8, Title: "new"},
			checksFailed: true,
		})
		m = res.(*Model)
		if len(m.ciChecks) != 0 || m.ciStatus.State != "" {
			t.Errorf("PR #7's checks still shown under PR #8: checks=%v status=%q", m.ciChecks, m.ciStatus.State)
		}
	})
}

// The PR-inclusive git load fetches the same GitHub data as the PR tick, so it
// must participate in the same classify/bump/clear state machine. Otherwise a
// rate limit on that path renders as a generic error with no backoff, and a
// success on that path — proof GitHub recovered — leaves the tick loop backed
// off for up to 15 minutes.
func TestGitLoadPRPath_ParticipatesInBackoff(t *testing.T) {
	newBackedOffModel := func() *Model {
		mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
		m := NewModel("/tmp", mg)
		m.width, m.height = 80, 24
		m.updateLayout()
		res, _ := m.Update(prRefreshMsg{fetchFailed: true, errKind: ghErrRateLimited})
		m = res.(*Model)
		if m.activity.PRInterval() != prRefreshActive*2 {
			t.Fatalf("setup: interval = %v, want %v", m.activity.PRInterval(), prRefreshActive*2)
		}
		return m
	}

	t.Run("success clears the backoff", func(t *testing.T) {
		m := newBackedOffModel()
		res, _ := m.Update(gitDataMsg{
			repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
			changes:  git.NewChangedFiles(),
			prInfo:   git.PRInfoResult{Number: 7},
		})
		m = res.(*Model)
		if got := m.activity.PRInterval(); got != prRefreshActive {
			t.Errorf("interval after a successful PR-inclusive load = %v, want %v", got, prRefreshActive)
		}
		if m.prError != "" {
			t.Errorf("prError = %q, want cleared", m.prError)
		}
	})

	t.Run("rate limit backs off", func(t *testing.T) {
		m := newBackedOffModel()
		res, _ := m.Update(gitDataMsg{
			repoInfo:      git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
			changes:       git.NewChangedFiles(),
			prFetchFailed: true,
			prErrKind:     ghErrRateLimited,
		})
		m = res.(*Model)
		if got := m.activity.PRInterval(); got != prRefreshActive*4 {
			t.Errorf("interval after a rate-limited PR-inclusive load = %v, want %v", got, prRefreshActive*4)
		}
		if m.prError != "GitHub API rate limited" {
			t.Errorf("prError = %q, want %q", m.prError, "GitHub API rate limited")
		}
	})

	t.Run("other errors do not back off", func(t *testing.T) {
		m := newBackedOffModel()
		before := m.activity.PRInterval()
		res, _ := m.Update(gitDataMsg{
			repoInfo:      git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
			changes:       git.NewChangedFiles(),
			prFetchFailed: true,
		})
		m = res.(*Model)
		if got := m.activity.PRInterval(); got != before {
			t.Errorf("interval after a non-rate-limit PR-inclusive load = %v, want it unchanged at %v", got, before)
		}
		if m.prError != "GitHub API error" {
			t.Errorf("prError = %q, want %q", m.prError, "GitHub API error")
		}
	})
}

// runGitLoad must classify its own PR-fetch error the same way fetchPRStatus
// does, so the handler can tell a rate limit from anything else.
func TestGitLoad_ClassifiesPRError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantKind githubErrorKind
	}{
		{"rate limit", fmt.Errorf("API rate limit exceeded for user"), ghErrRateLimited},
		{"expired auth", fmt.Errorf("gh: authentication token expired"), ghErrAuth},
		{"SAML 403", fmt.Errorf("HTTP 403: Resource protected by organization SAML enforcement"), ghErrAuth},
		{"network", fmt.Errorf("dial tcp: no such host"), ghErrOther},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mg := &mockGit{
				repoInfo:  git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
				base:      "abc",
				prInfoErr: c.err,
			}
			msg := runGitLoad(gitLoadRequest{git: mg, withPR: true}).(gitDataMsg)
			if !msg.prFetchFailed {
				t.Fatal("prFetchFailed = false, want true")
			}
			if msg.prErrKind != c.wantKind {
				t.Errorf("prErrKind = %v, want %v", msg.prErrKind, c.wantKind)
			}
		})
	}
}

// runGitLoad must report a checks failure rather than passing the zero value
// through as real data.
func TestGitLoad_ReportsChecksFailure(t *testing.T) {
	mg := &mockGit{
		repoInfo:    git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		prInfo:      git.PRInfoResult{Number: 7},
		base:        "abc",
		ciStatusErr: fmt.Errorf("gh: transient failure"),
	}
	msg := runGitLoad(gitLoadRequest{git: mg, withPR: true}).(gitDataMsg)
	if !msg.checksFailed {
		t.Error("checksFailed = false, want true when PRChecksAll errors")
	}
	if msg.prFetchFailed {
		t.Error("prFetchFailed = true, want false — the PR fetch itself succeeded")
	}
}

// TestBehindCount_UnknownIsHidden covers CODE_REVIEW A3: BehindCount returned
// 0 on any error, conflating "up to date" with "we couldn't tell". An unknown
// count is hidden rather than rendered as a wrong number.
func TestBehindCount_UnknownIsHidden(t *testing.T) {
	mg := &mockGit{
		repoInfo:    git.RepoInfoResult{Branch: "feature", RepoName: "repo", Upstream: "origin/main"},
		base:        "abc",
		behindCount: 3,
		behindErr:   fmt.Errorf("unknown revision origin/main"),
	}
	msg := runGitLoad(gitLoadRequest{git: mg}).(gitDataMsg)
	if msg.behindKnown {
		t.Error("behindKnown = true, want false when BehindCount errors")
	}

	m := NewModel("/tmp", mg)
	m.width, m.height = 100, 24
	m.updateLayout()
	// Seed a known count, then let the failing load land.
	res, _ := m.Update(gitDataMsg{
		repoInfo:    git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		changes:     git.NewChangedFiles(),
		behindCount: 3,
		behindKnown: true,
	})
	m = res.(*Model)
	if !strings.Contains(m.View().Content, "3 behind") {
		t.Fatal("known behind count is not rendered")
	}

	res, _ = m.Update(msg)
	m = res.(*Model)
	if strings.Contains(m.View().Content, "behind") {
		t.Error("status bar still shows a behind count after the count became unknown")
	}

	// The status bar itself must not render a count it was told is unknown,
	// whatever number happens to be sitting in the field.
	bar, _, _, _ := renderStatusBar(100, statusBarData{
		info:        git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		behindCount: 3,
		behindKnown: false,
	})
	if strings.Contains(bar, "behind") {
		t.Error("renderStatusBar rendered an unknown behind count")
	}
}

// TestSSO403_NotRateLimited is the regression test for the `isRateLimited`
// 403 misclassification (BUG_REPORTS.md, New Bugs): a SAML/SSO 403 is a
// permission condition no amount of waiting fixes, so it must neither be
// reported as a rate limit nor drive the exponential backoff.
// It walks the real loop — fetchPRStatus classifies, Update applies — for a
// stream of identical failures, which is what a misclassification actually
// costs: five SSO 403s used to take the poll from 30s to 16m.
func TestSSO403_NotRateLimited(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantGrows bool
		wantError string
	}{
		{
			name: "SAML/SSO 403",
			err: fmt.Errorf("exit status 1: gh: HTTP 403: Resource protected by organization SAML enforcement. " +
				"You must grant your OAuth token access to this organization. (https://api.github.com/graphql)"),
			wantGrows: false,
			wantError: ghErrAuth.statusMessage(),
		},
		{
			name:      "missing scopes",
			err:       fmt.Errorf("exit status 1: error: your authentication token is missing required scopes [read:org]"),
			wantGrows: false,
			wantError: ghErrAuth.statusMessage(),
		},
		{
			name:      "genuine rate limit",
			err:       fmt.Errorf("exit status 1: gh: HTTP 403: API rate limit exceeded for user ID 1234. (https://api.github.com/graphql)"),
			wantGrows: true,
			wantError: ghErrRateLimited.statusMessage(),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mg := &mockGit{
				repoInfo:  git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
				prInfoErr: c.err,
			}
			m := NewModel("/tmp", mg)
			m.width, m.height = 80, 24
			m.updateLayout()
			before := m.activity.PRInterval()

			const fetches = 5
			for i := range fetches {
				res, _ := m.Update(fetchPRStatus(mg, 0))
				m = res.(*Model)
				if !c.wantGrows {
					if got := m.activity.PRInterval(); got != before {
						t.Fatalf("after %d failures, PR interval = %v, want it unchanged at %v", i+1, got, before)
					}
				}
			}
			if c.wantGrows {
				// A3's contract: consecutive rate limits double the interval.
				want := min(before<<fetches, prRefreshMax)
				if got := m.activity.PRInterval(); got != want {
					t.Errorf("after %d rate limits, PR interval = %v, want %v", fetches, got, want)
				}
			}
			if m.prError != c.wantError {
				t.Errorf("prError = %q, want %q", m.prError, c.wantError)
			}
			// Whatever the cause, the message reaches line 3 — the PR pane
			// keeps whatever last-known-good data it had.
			bar, _, _, _ := renderStatusBar(m.width, m.statusBarData())
			if !strings.Contains(bar, c.wantError) {
				t.Errorf("status bar does not carry the error %q: %q", c.wantError, bar)
			}
		})
	}
}

// TestAuthErrorDuringRateLimitBackoff pins the interaction between the two
// error kinds, which the "auth keeps the normal cadence" rule alone does not
// describe: an auth failure arriving *while a rate-limit backoff is latched*
// holds the latched floor. ResetPRInterval recomputes through
// effectivePRInterval = max(activity-derived, rateLimitBackoff), and only
// MarkPRSuccess clears the latch — a 403 about a missing scope is not
// evidence the throttle lifted, so the poll must not speed back up on it.
// The message, however, does flip to the auth text: it names the most recent
// cause.
func TestAuthErrorDuringRateLimitBackoff(t *testing.T) {
	mg := &mockGit{repoInfo: git.RepoInfoResult{Branch: "feature", RepoName: "repo"}}
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()

	// One genuine rate limit latches a doubled interval.
	res, _ := m.Update(prRefreshMsg{fetchFailed: true, errKind: ghErrRateLimited})
	m = res.(*Model)
	latched := m.activity.PRInterval()
	if latched != prRefreshActive*2 {
		t.Fatalf("setup: interval after a rate limit = %v, want %v", latched, prRefreshActive*2)
	}

	// SSO failures now arrive. They must neither grow the interval (an auth
	// error never backs off) nor shrink it below the latched floor.
	for i := range 3 {
		res, _ = m.Update(prRefreshMsg{fetchFailed: true, errKind: ghErrAuth})
		m = res.(*Model)
		if got := m.activity.PRInterval(); got != latched {
			t.Fatalf("after %d auth failures during backoff, interval = %v, want the latched %v", i+1, got, latched)
		}
		if m.prError != ghErrAuth.statusMessage() {
			t.Errorf("after %d auth failures, prError = %q, want %q", i+1, m.prError, ghErrAuth.statusMessage())
		}
	}

	// The same holds on the PR-inclusive git-load path, which skips
	// ResetPRInterval entirely — both paths leave the same interval behind.
	res, _ = m.Update(gitDataMsg{
		repoInfo:      git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		changes:       git.NewChangedFiles(),
		prFetchFailed: true,
		prErrKind:     ghErrAuth,
	})
	m = res.(*Model)
	if got := m.activity.PRInterval(); got != latched {
		t.Errorf("after an auth failure on the git-load path, interval = %v, want the latched %v", got, latched)
	}

	// Only a success clears the latch.
	res, _ = m.Update(prRefreshMsg{})
	m = res.(*Model)
	if got := m.activity.PRInterval(); got != prRefreshActive {
		t.Errorf("interval after a successful fetch = %v, want the latch cleared to %v", got, prRefreshActive)
	}
	if m.prError != "" {
		t.Errorf("prError = %q, want cleared by the successful fetch", m.prError)
	}
}

// TestGenericError_CarriesRawText covers the hybrid-message adjudication: the
// classified kinds keep their fixed actionable text, but ghErrOther has no
// meaning to state, so it carries gh's own words — otherwise a DNS failure, a
// missing gh binary, a 502 and "no pull requests found" all render as the
// identical "GitHub API error" (PROMPT.md:83 wants the message).
func TestGenericError_CarriesRawText(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantLine string
	}{
		{
			"network failure",
			fmt.Errorf("dial tcp: lookup api.github.com: no such host"),
			"GitHub API error: dial tcp: lookup api.github.com: no such host",
		},
		{
			"gh missing",
			fmt.Errorf(`exec: "gh": executable file not found in $PATH`),
			`GitHub API error: exec: "gh": executable file not found in $PATH`,
		},
		{
			// Classified kinds ignore the detail: the summary is better than
			// gh's sentence-and-a-URL on a one-row status line.
			"rate limit keeps the fixed message",
			fmt.Errorf("gh: HTTP 403: API rate limit exceeded for user ID 1 (https://api.github.com/graphql)"),
			"GitHub API rate limited",
		},
		{
			"auth keeps the fixed message",
			fmt.Errorf("gh: HTTP 403: Resource protected by organization SAML enforcement (https://api.github.com/graphql)"),
			"GitHub API auth/permission error — check: gh auth status",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mg := &mockGit{
				repoInfo:  git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
				prInfoErr: c.err,
			}
			m := NewModel("/tmp", mg)
			m.width, m.height = 200, 24
			m.updateLayout()
			res, _ := m.Update(fetchPRStatus(mg, 0))
			m = res.(*Model)
			if m.prError != c.wantLine {
				t.Errorf("prError = %q, want %q", m.prError, c.wantLine)
			}
		})
	}
}

// A raw gh error can be multi-line and carry control characters. It must reach
// line 3 escaped, on exactly one row — the status bar's row-count promise does
// not bend for error text.
func TestGenericError_RawTextIsSanitizedOnLine3(t *testing.T) {
	nasty := fmt.Errorf("exit status 1: gh: request failed\n\tat api.github.com\rretrying\x07 now")
	mg := &mockGit{
		repoInfo:  git.RepoInfoResult{Branch: "feature", RepoName: "repo"},
		prInfoErr: nasty,
	}
	m := NewModel("/tmp", mg)
	m.width, m.height = 200, 24
	m.updateLayout()
	res, _ := m.Update(fetchPRStatus(mg, 0))
	m = res.(*Model)

	// The model keeps the raw text; sanitizing is the display boundary's job.
	if !strings.Contains(m.prError, "\n") {
		t.Errorf("prError = %q, want it to keep the raw error text", m.prError)
	}

	bar, _, _, _ := renderStatusBar(m.width, m.statusBarData())
	rows := strings.Split(bar, "\n")
	if got, want := len(rows), m.statusBarLines(); got != want {
		t.Fatalf("status bar rendered %d rows, layout reserved %d — control chars must not add rows", got, want)
	}
	line3 := rows[len(rows)-1]
	for _, raw := range []string{"\t", "\r", "\x07"} {
		if strings.Contains(line3, raw) {
			t.Errorf("line 3 contains a raw control character %q: %q", raw, line3)
		}
	}
	for _, esc := range []string{`\t`, `\r`, `\x07`} {
		if !strings.Contains(line3, esc) {
			t.Errorf("line 3 missing the escape %s: %q", esc, line3)
		}
	}
}
