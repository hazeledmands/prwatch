package ui

import (
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	git "github.com/hazeledmands/prwatch/internal/git"
)

// keyMsg builds a single-rune key press.
func keyMsg(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: s, Code: rune(s[0])}
}

// slowGit wraps mockGit and delays the first few calls of a git load, opening
// a window in which the Update goroutine can mutate Model state while the load
// is in flight. Used by the data-race regression tests: the delays are plain
// sleeps (no channels / mutexes), so they create no happens-before edge that
// would hide a race from -race.
type slowGit struct {
	*mockGit
	delay time.Duration
}

func (s *slowGit) RepoInfo() (git.RepoInfoResult, error) {
	time.Sleep(s.delay)
	return s.mockGit.RepoInfo()
}

func (s *slowGit) DetectBaseLocal() (string, error) {
	time.Sleep(s.delay)
	return s.mockGit.DetectBaseLocal()
}

func (s *slowGit) DetectBaseFromPR(baseRef string) (string, error) {
	time.Sleep(s.delay)
	return s.mockGit.DetectBaseFromPR(baseRef)
}

func raceFixtureGit() *mockGit {
	var commits []git.Commit
	for i := range 150 {
		commits = append(commits, git.Commit{SHA: shaN(i), Subject: "c"})
	}
	parents := map[string]string{}
	firstChildren := map[string]string{}
	for i := range 150 {
		if i+1 < 150 {
			parents[shaN(i)] = shaN(i + 1)
			firstChildren[shaN(i+1)] = shaN(i)
		}
	}
	parents["abc1234"] = shaN(0)
	firstChildren[shaN(0)] = "abc1234"
	return &mockGit{
		repoInfo: git.RepoInfoResult{
			Branch:   "feature",
			Upstream: "origin/main",
			RepoName: "repo",
		},
		base:          "abc1234",
		commits:       commits,
		parents:       parents,
		firstChildren: firstChildren,
		changedFiles:  git.ChangedFilesResult{},
	}
}

func shaN(i int) string {
	const hex = "0123456789abcdef"
	return string([]byte{
		'a', 'a', 'a',
		hex[(i>>12)&0xf], hex[(i>>8)&0xf], hex[(i>>4)&0xf], hex[i&0xf],
	})
}

// TestGitLoadCmd_NoModelStateRace is the regression test for CODE_REVIEW A2
// finding 1. Before the fix the git-load Cmd was the bound method value
// m.loadLocalGitData, whose body read m.scope.IsScrubbed()/OldBase(),
// m.commitsLoaded and m.prInfo.BaseRef from the Cmd goroutine while Update
// mutated exactly those fields on the main goroutine. Running under -race
// reports the write/read pair.
func TestGitLoadCmd_NoModelStateRace(t *testing.T) {
	for range 8 {
		mg := &slowGit{mockGit: raceFixtureGit(), delay: 2 * time.Millisecond}
		m := NewModel("/tmp", mg)
		m.width, m.height = 80, 24
		m.updateLayout()
		m.Update(m.loadGitData())

		// Snapshot happens here, on the Update goroutine.
		cmd := m.gitLoadCmd(false)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cmd()
		}()

		// Meanwhile Update keeps mutating the very state the load reads, for
		// long enough to overlap the (deliberately slow) load.
		deadline := time.Now().Add(30 * time.Millisecond)
		for time.Now().Before(deadline) {
			m.Update(keyMsg("]"))
			m.Update(keyMsg("["))
			m.Update(moreCommitsMsg{})
		}
		wg.Wait()
	}
}

// TestGitLoadCmd_PRVariantNoModelStateRace covers the with-PR load
// (previously m.loadGitData dispatched as a Cmd).
func TestGitLoadCmd_PRVariantNoModelStateRace(t *testing.T) {
	for range 8 {
		mg := &slowGit{mockGit: raceFixtureGit(), delay: 2 * time.Millisecond}
		m := NewModel("/tmp", mg)
		m.width, m.height = 80, 24
		m.updateLayout()
		m.Update(m.loadGitData())

		cmd := m.gitLoadCmd(true)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cmd()
		}()
		deadline := time.Now().Add(30 * time.Millisecond)
		for time.Now().Before(deadline) {
			m.Update(keyMsg("]"))
			m.Update(keyMsg("["))
		}
		wg.Wait()
	}
}

// TestLoadMoreCommitsCmd_NoModelStateRace covers loadMoreCommits, which read
// m.commitsLoaded and m.scope.OldBase() from the Cmd goroutine.
func TestLoadMoreCommitsCmd_NoModelStateRace(t *testing.T) {
	for range 8 {
		mg := &slowCommitsGit{mockGit: raceFixtureGit(), delay: 2 * time.Millisecond}
		m := NewModel("/tmp", mg)
		m.width, m.height = 80, 24
		m.updateLayout()
		m.Update(m.loadGitData())

		cmd := m.loadMoreCommitsCmd()
		if cmd == nil {
			t.Fatal("loadMoreCommitsCmd returned nil for a fresh model")
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cmd()
		}()
		deadline := time.Now().Add(30 * time.Millisecond)
		for time.Now().Before(deadline) {
			m.Update(keyMsg("]"))
			m.Update(keyMsg("["))
		}
		wg.Wait()
	}
}

type slowCommitsGit struct {
	*mockGit
	delay time.Duration
}

func (s *slowCommitsGit) Commits(base string, skip, limit int) ([]git.Commit, error) {
	time.Sleep(s.delay)
	return s.mockGit.Commits(base, skip, limit)
}

// ---------------------------------------------------------------------------
// Finding 2: moreCommitsMsg stale / duplicate guard
// ---------------------------------------------------------------------------

func loadedCommitsModel(t *testing.T) (*Model, *mockGit) {
	t.Helper()
	mg := raceFixtureGit()
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()
	m.Update(m.loadGitData())
	if len(m.commits) != commitPageSize {
		t.Fatalf("setup: expected %d commits, got %d", commitPageSize, len(m.commits))
	}
	return m, mg
}

func TestMoreCommits_DiscardedWhenScopeMovedMidFlight(t *testing.T) {
	m, _ := loadedCommitsModel(t)

	cmd := m.loadMoreCommitsCmd()
	if cmd == nil {
		t.Fatal("expected a load-more command")
	}
	msg := cmd()

	// The user scrubs the scope handle while the page is in flight.
	m.Update(keyMsg("]"))
	before := len(m.commits)

	m.Update(msg)

	if len(m.commits) != before {
		t.Fatalf("page computed against the pre-scrub base was appended: commits %d → %d", before, len(m.commits))
	}
}

func TestMoreCommits_DuplicateDispatchAppendsOnce(t *testing.T) {
	m, _ := loadedCommitsModel(t)

	// Both the click path and the enter path fire before either result lands.
	first := m.loadMoreCommitsCmd()
	if first == nil {
		t.Fatal("expected a load-more command")
	}
	second := m.loadMoreCommitsCmd()
	if second != nil {
		t.Fatal("a second load-more dispatched while one was already in flight")
	}

	m.Update(first())
	if len(m.commits) != 150 {
		t.Fatalf("after one page: expected 150 commits, got %d", len(m.commits))
	}

	// Even if a duplicate result somehow arrives, it must not double-append.
	m.Update(moreCommitsMsg{commits: m.commits[commitPageSize:], base: m.scope.OldBase(), skip: commitPageSize})
	if len(m.commits) != 150 {
		t.Fatalf("duplicate page appended: expected 150 commits, got %d", len(m.commits))
	}
}

func TestMoreCommits_ErrorClearsInFlightMarker(t *testing.T) {
	m, mg := loadedCommitsModel(t)
	mg.commitsErr = errors.New("boom")

	cmd := m.loadMoreCommitsCmd()
	if cmd == nil {
		t.Fatal("expected a load-more command")
	}
	m.Update(cmd())

	if next := m.loadMoreCommitsCmd(); next == nil {
		t.Fatal("in-flight marker stuck after a failed load-more; pagination is permanently wedged")
	}
}

// ---------------------------------------------------------------------------
// Finding 3: stale-load guard must not misfire on legitimate base movement
// ---------------------------------------------------------------------------

func TestGitData_AcceptedWhenNaturalBaseMoves(t *testing.T) {
	mg := raceFixtureGit()
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()
	m.Update(m.loadGitData())

	if m.scope.OldBase() != "abc1234" {
		t.Fatalf("setup: scope base = %q, want abc1234", m.scope.OldBase())
	}
	if m.scope.IsScrubbed() {
		t.Fatal("setup: scope should not be scrubbed")
	}

	// The base legitimately moves (rebase / base branch advanced). The load
	// that detects the movement queried against the NEW base, so its results
	// are the freshest available and must be applied — not discarded.
	mg.base = "def5678"
	mg.commits = mg.commits[:7]

	m.Update(m.gitLoadCmd(false)())

	if m.scope.OldBase() != "def5678" {
		t.Fatalf("scope base = %q, want def5678", m.scope.OldBase())
	}
	if len(m.commits) != 7 {
		t.Fatalf("commits = %d, want 7 — fresh data from the base-moving load was discarded", len(m.commits))
	}
}

func TestGitData_DiscardedWhenUserScrubsMidFlight(t *testing.T) {
	mg := raceFixtureGit()
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()
	m.Update(m.loadGitData())

	// A periodic tick's load is dispatched at the natural position.
	msg := m.gitLoadCmd(false)()

	// The user scrubs before it lands.
	m.Update(keyMsg("]"))
	if !m.scope.IsScrubbed() {
		t.Fatal("setup: expected scope to be scrubbed after ]")
	}
	scrubbed := m.scope.OldBase()
	before := len(m.commits)

	m.Update(msg)

	if m.scope.OldBase() != scrubbed {
		t.Fatalf("scrub was clobbered: scope base = %q, want %q", m.scope.OldBase(), scrubbed)
	}
	if len(m.commits) != before {
		t.Fatalf("pre-scrub load was applied over the user's scrub: commits %d → %d", before, len(m.commits))
	}
}

func TestGitData_DiscardedWhenUserUnscrubsMidFlight(t *testing.T) {
	mg := raceFixtureGit()
	m := NewModel("/tmp", mg)
	m.width, m.height = 80, 24
	m.updateLayout()
	m.Update(m.loadGitData())

	m.Update(keyMsg("]"))
	if !m.scope.IsScrubbed() {
		t.Fatal("setup: expected scope to be scrubbed")
	}

	// Load dispatched while scrubbed...
	msg := m.gitLoadCmd(false)()

	// ...user resets the scope before it lands.
	m.Update(keyMsg("\\"))
	if m.scope.IsScrubbed() {
		t.Fatal("setup: expected scope reset to clear the scrub")
	}
	before := len(m.commits)

	m.Update(msg)

	if len(m.commits) != before {
		t.Fatalf("scrubbed-range load was applied after the user reset the scope: commits %d → %d", before, len(m.commits))
	}
}
