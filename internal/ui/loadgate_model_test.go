package ui

import (
	"testing"

	git "github.com/hazeledmands/prwatch/internal/git"
)

// The three fs watchers (worktree, .git, .git/refs/heads) debounce
// independently, so one commit delivers up to three RefreshMsg values, and the
// 30s git tick can land on top of them. Every one of those used to dispatch its
// own git load. Only the first may dispatch; the rest coalesce into a single
// trailing load released when the first result lands.
func TestRefreshMsg_SingleFlight(t *testing.T) {
	m := NewModel("/tmp", testGit())

	res, cmd := m.Update(RefreshMsg{})
	m = res.(*Model)
	if cmd == nil {
		t.Fatal("first RefreshMsg should dispatch a load")
	}

	for i := 0; i < 2; i++ {
		res, cmd = m.Update(RefreshMsg{})
		m = res.(*Model)
		if cmd != nil {
			t.Fatalf("RefreshMsg %d dispatched a load while one was in flight", i)
		}
	}
	// The git tick's cmd is never nil — it always re-arms its own timer — so
	// the gate state is what proves the load itself was suppressed.
	res, _ = m.Update(gitTickMsg{})
	m = res.(*Model)
	if !m.gitLoads.Pending() {
		t.Fatal("suppressed triggers should have set the pending rerun bit")
	}
	if got := m.gitDispatchSeq; got != 1 {
		t.Fatalf("gitDispatchSeq = %d, want 1: only one load should have been snapshotted", got)
	}

	// The result of the in-flight load releases exactly one trailing load.
	res, cmd = m.Update(gitDataMsg{repoInfo: git.RepoInfoResult{Branch: "feature"}})
	m = res.(*Model)
	if cmd == nil {
		t.Fatal("adopting the result should release the trailing load")
	}
	if m.gitLoads.Pending() {
		t.Fatal("pending bit should be cleared once the trailing load goes out")
	}

	// ...and only one: the next result drains the gate.
	res, cmd = m.Update(gitDataMsg{repoInfo: git.RepoInfoResult{Branch: "feature"}})
	m = res.(*Model)
	if cmd != nil {
		t.Fatal("a drained gate should not dispatch a second trailing load")
	}
	if m.gitLoads.InFlight() {
		t.Fatal("gate should be idle after the trailing load's result")
	}
}

// An errored load still has to release the gate, or one index.lock collision
// wedges every later refresh for the session.
func TestRefreshMsg_ErroredLoadReleasesGate(t *testing.T) {
	m := NewModel("/tmp", testGit())

	res, _ := m.Update(RefreshMsg{})
	m = res.(*Model)
	res, _ = m.Update(gitDataMsg{err: errLoadFailed})
	m = res.(*Model)
	if m.gitLoads.InFlight() {
		t.Fatal("an errored gitDataMsg must clear the in-flight bit")
	}

	res, cmd := m.Update(RefreshMsg{})
	m = res.(*Model)
	if cmd == nil {
		t.Fatal("a refresh after a failed load must still dispatch")
	}
}

// The gitDataMsg arm can itself dispatch a load — the branch-switch path
// resets a stale scrub and reloads the default range. If the gate is released
// *after* the arm runs, that dispatch's claim on the in-flight slot is undone
// the moment it is made: the load is genuinely outstanding but the gate reads
// idle, so the next trigger dispatches a second one concurrently. The release
// therefore has to happen before the arm can claim the slot.
//
// Not reachable from the production message flow today (every gitDataMsg there
// arrives with the gate already in flight, which masks it), but RenderWithKeys
// and any new caller that feeds a load result with the gate idle hit it.
func TestGitData_ArmDispatchKeepsGateInFlight(t *testing.T) {
	mg := raceFixtureGit()
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()
	// Synchronous load: establishes repoInfo.Branch without touching the gate.
	m.Update(m.loadGitData())

	// Scrub the scope the same way the ScopeExtendBack key does, but without
	// its dispatch, so the gate stays idle.
	if err := m.scope.ExtendBack(m.git); err != nil {
		t.Fatalf("setup: ExtendBack: %v", err)
	}
	if !m.scope.IsScrubbed() {
		t.Fatal("setup: expected the scope to be scrubbed")
	}
	if m.gitLoads.InFlight() {
		t.Fatal("setup: expected an idle gate")
	}

	// A load result reporting a different branch: the arm resets the scrub and
	// dispatches a fresh load for the default range.
	res, cmd := m.Update(gitDataMsg{repoInfo: git.RepoInfoResult{Branch: "other"}})
	m = res.(*Model)

	if cmd == nil {
		t.Fatal("setup: a branch switch over a scrubbed scope should dispatch a load")
	}
	if !m.gitLoads.InFlight() {
		t.Fatal("the arm dispatched a load, so the gate must read in-flight; " +
			"releasing after the arm un-claims a slot that is genuinely taken")
	}

	// And the consequence that makes it a bug: the next trigger must coalesce
	// rather than start a second concurrent load.
	res, cmd = m.Update(RefreshMsg{})
	m = res.(*Model)
	if cmd != nil {
		t.Fatal("a trigger arriving while the arm's load is outstanding must not dispatch")
	}
}

var errLoadFailed = errStub("load failed")

type errStub string

func (e errStub) Error() string { return string(e) }
