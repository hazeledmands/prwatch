package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// TestPRFetch_SingleFlight covers the missing in-flight guard on the PR
// fetch path. prTickMsg dispatched loadPRStatusCmd unconditionally — the
// activity tracker's PRFetchDue only holds fetches back while a rate-limit
// backoff is latched — so a fetch slower than the tick interval stacked. With
// the reviews page cap that is up to 5 gh subprocesses per fetch and a fetch
// worst case of ~225s against a ~30s tick: about 8 overlapping fetches, the
// very storm the cap exists to prevent.
func TestPRFetch_SingleFlight(t *testing.T) {
	m := NewModel("/tmp", testGit())

	// A tick with an idle gate dispatches: prFetchSeq advances. This also
	// establishes that PRFetchDue holds here, so the suppression assertion
	// below cannot pass vacuously.
	before := m.prFetchSeq
	result, _ := m.Update(prTickMsg{})
	m = result.(*Model)
	if m.prFetchSeq != before+1 {
		t.Fatalf("prFetchSeq = %d after first tick, want %d — the tick must dispatch through the gate", m.prFetchSeq, before+1)
	}
	if !m.prFetchInFlight {
		t.Fatal("prFetchInFlight = false after dispatching a fetch")
	}

	// A second tick while that fetch is outstanding must not dispatch.
	dispatched := m.prFetchSeq
	result, cmd := m.Update(prTickMsg{})
	m = result.(*Model)
	if m.prFetchSeq != dispatched {
		t.Errorf("prFetchSeq = %d, want %d — a tick must not stack a second PR fetch", m.prFetchSeq, dispatched)
	}
	// The tick must still re-arm, or the poll dies on the suppressed tick.
	if cmd == nil {
		t.Error("suppressed tick returned no cmd; the PR tick must still re-arm")
	}

	// The result landing releases the gate.
	result, _ = m.Update(prRefreshMsg{seq: m.prFetchSeq, prInfo: gitpkg.PRInfoResult{Number: 1}})
	m = result.(*Model)
	if m.prFetchInFlight {
		t.Fatal("prFetchInFlight still set after the result landed")
	}
	result, _ = m.Update(prTickMsg{})
	m = result.(*Model)
	if m.prFetchSeq != dispatched+1 {
		t.Errorf("prFetchSeq = %d, want %d — the gate must reopen once the result lands", m.prFetchSeq, dispatched+1)
	}
}

// TestPRRefresh_StaleResultDiscarded covers the missing staleness protocol.
// prRefreshMsg carried no seq, so a slow fetch that landed after a newer one
// overwrote the fresher data wholesale — the same hazard gitDataMsg.seq
// exists to prevent.
func TestPRRefresh_StaleResultDiscarded(t *testing.T) {
	m := NewModel("/tmp", testGit())

	// The newer fetch (seq 2) lands first.
	result, _ := m.Update(prRefreshMsg{
		seq:          2,
		prInfo:       gitpkg.PRInfoResult{Number: 20, Title: "current"},
		reviews:      []gitpkg.PRReview{{Author: "alice", State: "APPROVED"}},
		reviewsTotal: 1,
		commentCount: 5,
	})
	m = result.(*Model)

	// The older fetch (seq 1) arrives late and must be dropped entirely.
	result, _ = m.Update(prRefreshMsg{
		seq:          1,
		prInfo:       gitpkg.PRInfoResult{Number: 10, Title: "superseded"},
		reviews:      nil,
		reviewsTotal: 0,
		commentCount: 0,
	})
	m = result.(*Model)

	if m.prInfo.Number != 20 || m.prInfo.Title != "current" {
		t.Errorf("prInfo = #%d %q, want #20 \"current\" — a stale fetch overwrote newer data", m.prInfo.Number, m.prInfo.Title)
	}
	if len(m.prReviews) != 1 {
		t.Errorf("len(prReviews) = %d, want 1", len(m.prReviews))
	}
	if m.prCommentCount != 5 {
		t.Errorf("prCommentCount = %d, want 5", m.prCommentCount)
	}
}

// TestPRRefresh_StaleErrorDoesNotClobberNewerSuccess covers the other half
// of the staleness guard. A superseded fetch's *error* is as out of date as
// its payload: applying it would paint a rate limit or an auth failure over
// data that just arrived fine, and would back the poll off on evidence a
// later fetch has already contradicted.
func TestPRRefresh_StaleErrorDoesNotClobberNewerSuccess(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(prRefreshMsg{
		seq:    2,
		prInfo: gitpkg.PRInfoResult{Number: 20, Title: "current"},
	})
	m = result.(*Model)
	interval := m.activity.PRInterval()

	// The older fetch failed, and says so far too late.
	result, _ = m.Update(prRefreshMsg{
		seq:         1,
		fetchFailed: true,
		errKind:     ghErrRateLimited,
		errText:     "API rate limit exceeded",
	})
	m = result.(*Model)

	if m.prError != "" {
		t.Errorf("prError = %q, want empty — a stale failure must not report over a newer success", m.prError)
	}
	if m.activity.PRInterval() != interval {
		t.Errorf("PR interval = %v, want %v — a stale rate limit must not back the poll off", m.activity.PRInterval(), interval)
	}
	if m.prInfo.Number != 20 {
		t.Errorf("prInfo.Number = %d, want 20", m.prInfo.Number)
	}
}

// TestPRRefresh_EqualSeqApplies pins the boundary: the guard discards only
// what is strictly older. A result carrying the seq already adopted is that
// same dispatch's answer, so it applies — the comparison must not be <=.
func TestPRRefresh_EqualSeqApplies(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(prRefreshMsg{seq: 3, prInfo: gitpkg.PRInfoResult{Number: 30, Title: "first"}})
	m = result.(*Model)
	result, _ = m.Update(prRefreshMsg{seq: 3, prInfo: gitpkg.PRInfoResult{Number: 31, Title: "same dispatch"}})
	m = result.(*Model)

	if m.prInfo.Number != 31 {
		t.Errorf("prInfo.Number = %d, want 31 — an equal seq is not stale", m.prInfo.Number)
	}
}

// TestPRRefresh_UnsequencedMsgAlwaysApplies pins the escape hatch gitDataMsg
// uses: seq 0 means "not from a tracked dispatch" — a hand-built msg from a
// test or RenderOnce — and must never be judged stale.
func TestPRRefresh_UnsequencedMsgAlwaysApplies(t *testing.T) {
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(prRefreshMsg{seq: 7, prInfo: gitpkg.PRInfoResult{Number: 70}})
	m = result.(*Model)
	result, _ = m.Update(prRefreshMsg{prInfo: gitpkg.PRInfoResult{Number: 99, Title: "unsequenced"}})
	m = result.(*Model)

	if m.prInfo.Number != 99 {
		t.Errorf("prInfo.Number = %d, want 99 — an unsequenced msg must apply", m.prInfo.Number)
	}
}

// TestPRRefresh_ReviewsTotalCountsAsServerChange covers the activity
// detector at the page cap. It compared len(msg.reviews) against
// len(m.prReviews), which is pinned at 250 once a PR passes the cap: reviews
// could keep arriving forever without the poll ever noticing the PR was
// active.
func TestPRRefresh_ReviewsTotalCountsAsServerChange(t *testing.T) {
	atCap := make([]gitpkg.PRReview, 250)
	for i := range atCap {
		atCap[i] = gitpkg.PRReview{Author: fmt.Sprintf("r%d", i), State: "COMMENTED"}
	}

	m := NewModel("/tmp", testGit())
	result, _ := m.Update(prRefreshMsg{
		prInfo:       gitpkg.PRInfoResult{Number: 1, Title: "big"},
		reviews:      atCap,
		reviewsTotal: 300,
	})
	m = result.(*Model)

	// Backdate the marker so a fresh MarkServerChange is observable.
	stale := time.Now().Add(-time.Hour)
	m.activity.lastServerChange = stale

	// Same 250 reviews on the wire, but GitHub now reports 400 in total:
	// the PR demonstrably changed.
	result, _ = m.Update(prRefreshMsg{
		prInfo:       gitpkg.PRInfoResult{Number: 1, Title: "big"},
		reviews:      atCap,
		reviewsTotal: 400,
	})
	m = result.(*Model)

	if !m.activity.lastServerChange.After(stale) {
		t.Error("a change in reviewsTotal at the page cap did not register as server activity")
	}
}

// TestPRRefresh_PartialReviewsSurfaceTheError covers the UI half of the
// partial-pagination fix: the data lands (so the sidebar can say "100 of
// 500") and the failure is still reported on the status line, through the
// same classifier every other gh failure goes through.
func TestPRRefresh_PartialReviewsSurfaceTheError(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width, m.height = 120, 40

	result, _ := m.Update(prRefreshMsg{
		prInfo:         gitpkg.PRInfoResult{Number: 4, Title: "big"},
		reviews:        []gitpkg.PRReview{{Author: "alice", State: "APPROVED"}},
		reviewsTotal:   500,
		reviewsErrKind: ghErrOther,
		reviewsErrText: "HTTP 502: Bad gateway",
	})
	m = result.(*Model)

	// The partial data applied.
	if len(m.prReviews) != 1 || m.prReviewsTotal != 500 {
		t.Errorf("reviews = %d of %d, want 1 of 500 — partial data must still apply", len(m.prReviews), m.prReviewsTotal)
	}
	// And the failure is visible rather than swallowed.
	if m.prError == "" {
		t.Fatal("prError empty; a partial reviews fetch must still report its failure")
	}
	if !strings.Contains(m.prError, "502") {
		t.Errorf("prError = %q, want it to carry the underlying error", m.prError)
	}
}

// TestPRRefresh_PartialReviewsRateLimitBacksOff checks the partial-failure
// path feeds the backoff the same way a whole-fetch failure does: a throttle
// discovered mid-pagination is still a throttle.
func TestPRRefresh_PartialReviewsRateLimitBacksOff(t *testing.T) {
	m := NewModel("/tmp", testGit())
	before := m.activity.PRInterval()

	result, _ := m.Update(prRefreshMsg{
		prInfo:         gitpkg.PRInfoResult{Number: 4},
		reviews:        []gitpkg.PRReview{{Author: "alice", State: "APPROVED"}},
		reviewsTotal:   500,
		reviewsErrKind: ghErrRateLimited,
		reviewsErrText: "API rate limit exceeded",
	})
	m = result.(*Model)

	if m.activity.PRInterval() <= before {
		t.Errorf("PR interval = %v, want a backoff above %v", m.activity.PRInterval(), before)
	}
}

// TestPRFetch_ReviewsErrReachesTheBackoff closes the chain end to end: a
// reviews failure recorded by internal/git must arrive classified, so a
// throttle backs the poll off no matter which page hit it. Asserted on the
// msg the fetch actually produces, not a hand-built one, because the
// classification step is the link that was missing.
func TestPRFetch_ReviewsErrReachesTheBackoff(t *testing.T) {
	mg := &mockGit{
		prInfo:       gitpkg.PRInfoResult{Number: 4, Title: "big"},
		reviews:      []gitpkg.PRReview{{Author: "fallback", State: "APPROVED"}},
		reviewsTotal: 1,
		reviewsErr:   fmt.Errorf("exit status 1: HTTP 403: API rate limit exceeded"),
	}

	msg, ok := fetchPRStatus(mg, 0).(prRefreshMsg)
	if !ok {
		t.Fatal("fetchPRStatus did not return a prRefreshMsg")
	}
	// Not a whole-fetch failure: the data is good.
	if msg.fetchFailed {
		t.Error("fetchFailed = true; a reviews-only failure must not discard the PR data")
	}
	if msg.reviewsErrKind != ghErrRateLimited {
		t.Errorf("reviewsErrKind = %v, want %v", msg.reviewsErrKind, ghErrRateLimited)
	}

	m := NewModel("/tmp", testGit())
	interval := m.activity.PRInterval()
	result, _ := m.Update(msg)
	m = result.(*Model)

	if m.activity.PRInterval() <= interval {
		t.Errorf("PR interval = %v, want a backoff above %v", m.activity.PRInterval(), interval)
	}
	if len(m.prReviews) != 1 {
		t.Errorf("len(prReviews) = %d, want 1 — the fallback data must still apply", len(m.prReviews))
	}
}

// TestSectionCount covers the header-count helper directly: the truncated
// form appears only when a total genuinely exceeds what we hold, and an
// unknown total (0) never renders as "N of 0".
func TestSectionCount(t *testing.T) {
	tests := []struct {
		shown, total int
		want         string
	}{
		{0, 0, "Reviews (0)"},
		{3, 3, "Reviews (3)"},
		{3, 0, "Reviews (3)"},
		{3, 2, "Reviews (3)"},
		{250, 500, "Reviews (250 of 500)"},
	}
	for _, tt := range tests {
		if got := sectionCount("Reviews", tt.shown, tt.total); got != tt.want {
			t.Errorf("sectionCount(Reviews, %d, %d) = %q, want %q", tt.shown, tt.total, got, tt.want)
		}
	}
}
