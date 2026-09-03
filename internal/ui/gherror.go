package ui

import (
	"errors"
	"regexp"
	"strings"

	"github.com/hazeledmands/prwatch/internal/command"
)

// githubErrorKind classifies a failed GitHub call into the four outcomes the
// UI treats differently: a rate limit (back the poll off and say so), an
// auth-or-permission failure (say so, but keep polling at the normal cadence
// — no amount of waiting grants a scope or clears SAML enforcement), gh
// missing from PATH (say so; installing it is the remedy), and everything
// else (network, JSON, gh itself).
//
// The distinction is load-bearing: the rate-limit branch drives the
// exponential backoff in activityTracker (PROMPT.md:21), which doubles out to
// a 15-minute poll. Sending a permanent condition down that path both names
// the wrong cause on the status bar and freezes the UI's refresh for a quarter
// of an hour for no benefit.
type githubErrorKind int

const (
	ghErrNone githubErrorKind = iota
	ghErrRateLimited
	ghErrAuth
	// ghErrGhMissing: the gh binary is not on PATH, so no GitHub call ran at
	// all. internal/git only lets this reach us when the repo has a GitHub
	// remote — a repo with no such remote has no PR to fetch, and stays
	// silent as before. Installing gh is the remedy, so it does not back off.
	ghErrGhMissing
	ghErrOther
)

// backsOff reports whether this outcome should grow the PR poll interval.
// Only a genuine rate limit does — waiting is the remedy for exactly one of
// these conditions. This is the single place that decision is made, so no
// caller can back off on an auth error.
func (k githubErrorKind) backsOff() bool { return k == ghErrRateLimited }

// reported is the kind to report for a failure that carries no classification
// — a hand-built msg that set only its "failed" flag. Such a failure is real
// and must say something, so it reports generically rather than blanking the
// status line.
func (k githubErrorKind) reported() githubErrorKind {
	if k == ghErrNone {
		return ghErrOther
	}
	return k
}

// String names the outcome for debug logs and test failures.
func (k githubErrorKind) String() string {
	switch k {
	case ghErrNone:
		return "none"
	case ghErrRateLimited:
		return "rate-limited"
	case ghErrAuth:
		return "auth"
	case ghErrGhMissing:
		return "gh-missing"
	case ghErrOther:
		return "other"
	default:
		return "unknown"
	}
}

// statusMessage is the line-3 text for this outcome, with no detail. Callers
// that have the underlying error text should use statusMessageWith.
func (k githubErrorKind) statusMessage() string { return k.statusMessageWith("") }

// statusMessageWith is the line-3 text for this outcome, given the raw error
// text the fetch failed with.
//
// The messages are hybrid by adjudication. A rate limit and an auth failure
// keep fixed, actionable text: the classifier has already extracted the
// meaning, and its one-line summary beats gh's raw sentence-and-a-URL on a
// single status row. ghErrOther has no such meaning to state — DNS failure,
// gh missing from PATH, a 502 and "no pull requests found" would otherwise
// all render as the identical "GitHub API error" — so it carries the raw
// text, which is the closest this bucket gets to PROMPT.md:83's "put the
// error message here!".
//
// The detail is NOT sanitized here: this returns display text, and
// sanitizing is the display boundary's job (renderStatusBar runs it through
// sanitizeDisplayText, then ellipsize). Doing it in both places would
// double-escape.
func (k githubErrorKind) statusMessageWith(detail string) string {
	switch k {
	case ghErrRateLimited:
		return "GitHub API rate limited"
	case ghErrAuth:
		return "GitHub API auth/permission error — check: gh auth status"
	case ghErrGhMissing:
		// Fixed text, like the other two classified outcomes: exec's raw
		// `exec: "gh": executable file not found in $PATH` names the cause
		// but not the consequence, and the consequence is what a status row
		// has space to say.
		return "gh not installed — PR data unavailable"
	case ghErrOther:
		if detail = strings.TrimSpace(detail); detail != "" {
			return "GitHub API error: " + detail
		}
		return "GitHub API error"
	default:
		return ""
	}
}

// What we have to classify on: internal/git's runExternal folds gh's stderr
// into the returned error ("<exit status>: <stderr>"), so the text below is
// literally what gh printed. gh formats API failures as `HTTP %d: %s (%s)`
// (REST) and `GraphQL: %s` (GraphQL), where %s is GitHub's own `message`
// field. gh does not print response headers, so `x-ratelimit-remaining: 0` —
// the canonical signal, and the one gh itself checks internally — only ever
// reaches us if a caller captured headers (`gh api -i`). It is matched anyway
// because it is the authoritative form; the message texts below are the forms
// actually available from `gh pr view` / `gh api graphql`.
var (
	// x-ratelimit-remaining: 0 — the canonical primary-rate-limit signal.
	rateLimitHeaderRe = regexp.MustCompile(`x-ratelimit-remaining\s*:\s*0(\D|$)`)
	// A 401/403 status with no other evidence. The digits must appear where a
	// status code appears — gh writes `HTTP %d:`, and a bare `403 Forbidden`
	// is the other shape seen in the wild — because gh appends the request
	// URL to its message and that URL carries the PR number:
	// `HTTP 502: Bad gateway (https://api.github.com/repos/o/r/pulls/403)`
	// is a gateway failure on PR #403, not an auth failure. Merely
	// digit-bounding the number is not enough; only the position is evidence.
	// (No `exit status 40[13]` form: gh exits 1/4, never with a status code.)
	bareAuthStatusRe = regexp.MustCompile(`\bhttp[ /]40[13]\b|\b40[13][: ]+(forbidden|unauthorized)\b`)
)

// rateLimitSignals are the texts that mean "GitHub is throttling us, and
// waiting is the remedy":
//   - "api rate limit exceeded" / "rate limit exceeded": GitHub's message for
//     a primary rate limit, returned with x-ratelimit-remaining: 0 and echoed
//     by gh on both the REST and GraphQL paths.
//   - "secondary rate limit": the abuse-detection throttle, e.g. "You have
//     exceeded a secondary rate limit and have been temporarily blocked".
//   - "rate_limited": the GraphQL error *type* (`errors[].type ==
//     "RATE_LIMITED"`), for callers that surface the raw error object.
var rateLimitSignals = []string{
	"rate limit exceeded",
	"secondary rate limit",
	"rate_limited",
}

// authSignals are texts that mean the token cannot do this, now or later:
// SAML/SSO enforcement, a missing OAuth scope, bad or expired credentials,
// or a resource the token simply may not touch.
var authSignals = []string{
	"saml",
	"single sign-on",
	"missing required scopes",
	"bad credentials",
	"requires authentication",
	"resource not accessible",
	"must have admin rights",
	"authentication token expired",
	"not logged into",
	// Deliberately no bare "forbidden": it is a content word, and gh's own
	// shape (`HTTP 403: Forbidden`) is already matched by bareAuthStatusRe
	// below, anchored to the status position. Unanchored, a branch named
	// "forbidden-fix" would turn `no pull requests found for branch
	// "forbidden-fix"` into an auth error.
}

// classifyGitHubError decides which outcome an error from the gh CLI is.
// Rate-limit evidence is checked first: a throttle response can also carry a
// 403 status, and it is the throttle, not the status code, that decides.
func classifyGitHubError(err error) githubErrorKind {
	if err == nil {
		return ghErrNone
	}
	// Structural, and checked before any prose: gh never ran, so nothing in
	// the message is a GitHub verdict to classify. The wrapped sentinel is
	// exact where exec's "executable file not found in $PATH" wording is not.
	if errors.Is(err, command.ErrNotFound) {
		return ghErrGhMissing
	}

	msg := strings.ToLower(err.Error())

	if rateLimitHeaderRe.MatchString(msg) {
		return ghErrRateLimited
	}
	for _, s := range rateLimitSignals {
		if strings.Contains(msg, s) {
			return ghErrRateLimited
		}
	}

	for _, s := range authSignals {
		if strings.Contains(msg, s) {
			return ghErrAuth
		}
	}
	// A bare 401/403 with no message we recognise: auth or permission, per
	// the adjudication in BUG_REPORTS.md. Reported, not backed off.
	if bareAuthStatusRe.MatchString(msg) {
		return ghErrAuth
	}
	return ghErrOther
}
