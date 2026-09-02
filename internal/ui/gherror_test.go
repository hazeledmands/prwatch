package ui

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestClassifyGitHubError is the table of real `gh` error shapes, one row per
// shape. gh folds the API's own `message` into its output as `HTTP %d: %s
// (%s)` (REST) or `GraphQL: %s`, and internal/git's runExternal folds gh's
// stderr into the error, so these strings are what actually reaches us.
//
// The bug this replaces: any error containing "403" was called a rate limit,
// which is wrong for every 403 GitHub emits *except* a throttle — SAML
// enforcement, a missing scope, and a token that may not see the resource are
// all permanent conditions that backing off cannot fix.
func TestClassifyGitHubError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want githubErrorKind
	}{
		{"nil", nil, ghErrNone},

		// --- rate limits: waiting is the remedy ---
		{
			"REST primary rate limit",
			fmt.Errorf("exit status 1: gh: HTTP 403: API rate limit exceeded for user ID 1234. (https://api.github.com/repos/o/r)"),
			ghErrRateLimited,
		},
		{
			"GraphQL primary rate limit",
			fmt.Errorf("exit status 1: gh: GraphQL: API rate limit exceeded for user ID 1234. (repository)"),
			ghErrRateLimited,
		},
		{
			"GraphQL RATE_LIMITED error type",
			fmt.Errorf(`exit status 1: gh: {"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`),
			ghErrRateLimited,
		},
		{
			"secondary (abuse-detection) rate limit",
			fmt.Errorf("exit status 1: gh: HTTP 403: You have exceeded a secondary rate limit and have been " +
				"temporarily blocked from content creation. Please retry your request again later. (https://api.github.com/graphql)"),
			ghErrRateLimited,
		},
		{
			"x-ratelimit-remaining header evidence",
			fmt.Errorf("exit status 1: gh: HTTP 403 (https://api.github.com/graphql)\nx-ratelimit-remaining: 0\nx-ratelimit-reset: 1712345678"),
			ghErrRateLimited,
		},

		// --- auth / permission: waiting fixes nothing ---
		{
			"SAML enforcement",
			fmt.Errorf("exit status 1: gh: HTTP 403: Resource protected by organization SAML enforcement. " +
				"You must grant your OAuth token access to this organization. (https://api.github.com/graphql)"),
			ghErrAuth,
		},
		{
			"SSO authorization required",
			fmt.Errorf("exit status 1: gh: your token has not been granted the required scopes; " +
				"single sign-on authorization is required"),
			ghErrAuth,
		},
		{
			"missing OAuth scopes",
			fmt.Errorf("exit status 1: error: your authentication token is missing required scopes [read:org]"),
			ghErrAuth,
		},
		{
			"bad credentials",
			fmt.Errorf("exit status 1: gh: HTTP 401: Bad credentials (https://api.github.com/graphql)"),
			ghErrAuth,
		},
		{
			"resource not accessible by token",
			fmt.Errorf("exit status 1: gh: HTTP 403: Resource not accessible by personal access token (https://api.github.com/repos/o/r)"),
			ghErrAuth,
		},
		{
			"bare 403 with no recognisable message",
			fmt.Errorf("exit status 1: gh: HTTP 403 (https://api.github.com/graphql)"),
			ghErrAuth,
		},
		{
			"expired auth",
			fmt.Errorf("exit status 4: gh: authentication token expired"),
			ghErrAuth,
		},

		// --- everything else ---
		{"network", fmt.Errorf("dial tcp: lookup api.github.com: no such host"), ghErrOther},
		{"server error", fmt.Errorf("exit status 1: gh: HTTP 502: Bad gateway (https://api.github.com/graphql)"), ghErrOther},
		{
			// gh appends the request URL, which carries the PR number. A 502
			// on PR #403 is not an auth failure: only the status code position
			// counts, never a digit run somewhere in the URL.
			"502 on PR #403",
			fmt.Errorf("exit status 1: gh: HTTP 502: Bad gateway (https://api.github.com/repos/o/r/pulls/403)"),
			ghErrOther,
		},
		{
			"503 on PR #401",
			fmt.Errorf("exit status 1: gh: HTTP 503: Service unavailable (https://api.github.com/repos/o/r/pulls/401)"),
			ghErrOther,
		},
		{"gh missing", fmt.Errorf(`exec: "gh": executable file not found in $PATH`), ghErrOther},
		{"no PR", fmt.Errorf("exit status 1: gh: no pull requests found for branch \"feature\""), ghErrOther},
		{
			// "forbidden" is a content word: a branch can be named after it.
			// The auth signal is the anchored status code, not the word.
			"branch named forbidden-fix",
			fmt.Errorf("exit status 1: gh: no pull requests found for branch \"forbidden-fix\""),
			ghErrOther,
		},
		{
			// A digit run containing 403 is not a status code. The old
			// substring match called this a rate limit.
			"sha containing 403",
			fmt.Errorf("fatal: bad object 40316d2b9c1e"),
			ghErrOther,
		},
		{
			"byte count containing 403",
			fmt.Errorf("unexpected EOF after 14031 bytes"),
			ghErrOther,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyGitHubError(c.err); got != c.want {
				t.Errorf("classifyGitHubError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestClassifyGitHubError_RateLimitEvidenceWinsOver403: a throttle response
// carries a 403 status too. The evidence, not the status code, decides.
func TestClassifyGitHubError_RateLimitEvidenceWinsOver403(t *testing.T) {
	err := fmt.Errorf("gh: HTTP 403: API rate limit exceeded for user ID 1 (https://api.github.com/graphql)")
	if got := classifyGitHubError(err); got != ghErrRateLimited {
		t.Errorf("classifyGitHubError = %v, want %v", got, ghErrRateLimited)
	}
}

// errFragment is a piece of a real gh error message, labelled with what it
// means. The label is the generator's ground truth — declared data, not a
// second copy of the classifier's logic — so the property can assert the
// exact kind rather than a self-satisfying relation. ("backs off implies rate
// limit" is unsatisfiable by construction, since backsOff() is *defined* as
// `k == ghErrRateLimited`; it would hold even if SAML text were bucketed as a
// rate limit.)
type errFragment struct {
	text string
	// kind is what this fragment alone implies: ghErrNone for a fragment
	// that carries no signal at all.
	kind githubErrorKind
}

var errFragments = []errFragment{
	// Rate limit — the only bucket that may back the poll off.
	{"API rate limit exceeded for user ID 1234.", ghErrRateLimited},
	{"You have exceeded a secondary rate limit", ghErrRateLimited},
	{`{"type":"RATE_LIMITED"}`, ghErrRateLimited},
	{"x-ratelimit-remaining: 0", ghErrRateLimited},

	// Auth or permission — reported, never backed off.
	{"HTTP 403", ghErrAuth},
	{"HTTP 401", ghErrAuth},
	{"Resource protected by organization SAML enforcement", ghErrAuth},
	{"single sign-on authorization is required", ghErrAuth},
	{"your authentication token is missing required scopes [read:org]", ghErrAuth},
	{"Bad credentials", ghErrAuth},
	{"Resource not accessible by integration", ghErrAuth},

	// Carries no signal: framing, URLs, and the digit runs that the old
	// substring classifier mistook for status codes.
	{"exit status 1", ghErrNone},
	{"gh:", ghErrNone},
	{"HTTP 502", ghErrNone},
	{"dial tcp: lookup api.github.com: no such host", ghErrNone},
	{"(https://api.github.com/graphql)", ghErrNone},
	{"(https://api.github.com/repos/o/r/pulls/403)", ghErrNone},
	{"40316d2b", ghErrNone},
	{"14031 bytes", ghErrNone},
	{"", ghErrNone},
}

// Property: a message's classification is decided by the signals it actually
// carries. Rate-limit evidence wins (a throttle response carries a 403 too);
// auth signals win over no signal; a message with neither is generic. The
// bug this replaces put SAML text and stray "403" digits in the rate-limit
// bucket, which drove the backoff — so the assertion is the exact kind, and
// backing off follows from it.
func TestProperty_ClassificationNeverBacksOffNonRateLimit(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		parts := rapid.SliceOfN(rapid.SampledFrom(errFragments), 0, 6).Draw(t, "parts")

		// Ground truth from the draw itself: what did we actually put in?
		want := ghErrOther
		sawAuth := false
		sawRateLimit := false
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			texts = append(texts, p.text)
			switch p.kind {
			case ghErrRateLimited:
				sawRateLimit = true
			case ghErrAuth:
				sawAuth = true
			}
		}
		switch {
		case sawRateLimit:
			want = ghErrRateLimited
		case sawAuth:
			want = ghErrAuth
		}

		msg := strings.Join(texts, " ")
		kind := classifyGitHubError(fmt.Errorf("%s", msg))
		if kind != want {
			t.Fatalf("classifyGitHubError(%q) = %v, want %v (rateLimitFragment=%v authFragment=%v)",
				msg, kind, want, sawRateLimit, sawAuth)
		}
		// The consequence that matters: only a rate limit may slow the poll.
		if kind.backsOff() != sawRateLimit {
			t.Fatalf("backsOff() = %v for %q, want %v", kind.backsOff(), msg, sawRateLimit)
		}
		// Every non-nil error must say something on line 3.
		if kind.statusMessage() == "" {
			t.Fatalf("kind %v has no status message (msg %q)", kind, msg)
		}
		// Deterministic: same input, same outcome.
		if again := classifyGitHubError(fmt.Errorf("%s", msg)); again != kind {
			t.Fatalf("classification not deterministic: %v then %v (msg %q)", kind, again, msg)
		}
	})
}

// A nil error is the only input that yields no message at all.
func TestClassifyGitHubError_NilHasNoMessage(t *testing.T) {
	if got := classifyGitHubError(nil).statusMessage(); got != "" {
		t.Errorf("nil error status message = %q, want empty", got)
	}
	if ghErrNone.backsOff() {
		t.Error("ghErrNone backs off")
	}
	// A failure that arrived without a classification still has to say
	// something — it reports generically rather than blanking line 3.
	if got := ghErrNone.reported(); got != ghErrOther {
		t.Errorf("ghErrNone.reported() = %v, want %v", got, ghErrOther)
	}
	for _, k := range []githubErrorKind{ghErrRateLimited, ghErrAuth, ghErrOther} {
		if got := k.reported(); got != k {
			t.Errorf("%v.reported() = %v, want it unchanged", k, got)
		}
	}
}
