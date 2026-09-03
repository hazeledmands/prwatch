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

var errLoadFailed = errStub("load failed")

type errStub string

func (e errStub) Error() string { return string(e) }
