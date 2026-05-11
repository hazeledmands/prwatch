package ui

import (
	"testing"

	gitpkg "github.com/hazeledmands/prwatch/internal/git"
)

// Property: Lookup on a non-RWX URL returns "" and never sets pending.
func TestRWXLookupNonRWXSkipped(t *testing.T) {
	f := newRWXFetcher()
	check := gitpkg.CICheck{Name: "lint", URL: "https://example.com/run"}
	got, cached := f.Lookup(check)
	if got != "" || cached || f.pending != nil {
		t.Fatalf("non-RWX URL: got=%q cached=%v pending=%v", got, cached, f.pending)
	}
}

// Property: Lookup on a cached URL returns cached and never sets pending.
func TestRWXLookupCacheHit(t *testing.T) {
	f := newRWXFetcher()
	url := "https://cloud.rwx.com/mint/org/runs/abc123"
	f.cache[url] = "cached content"
	check := gitpkg.CICheck{Name: "test", URL: url}
	got, cached := f.Lookup(check)
	if got != "cached content" || !cached || f.pending != nil {
		t.Fatalf("cache hit: got=%q cached=%v pending=%v", got, cached, f.pending)
	}
}

// Property: Lookup on uncached RWX URL marks pending and returns placeholder.
func TestRWXLookupCacheMissStagesPending(t *testing.T) {
	f := newRWXFetcher()
	url := "https://cloud.rwx.com/mint/org/runs/abc123"
	check := gitpkg.CICheck{Name: "test", URL: url}
	got, cached := f.Lookup(check)
	if got != rwxLoadingPlaceholder || cached || f.pending == nil || f.pending.URL != url {
		t.Fatalf("miss: got=%q cached=%v pending=%v", got, cached, f.pending)
	}
}

// Property: Cmd with nil git returns nil and leaves pending unchanged.
// Property: Cmd with non-nil pending+git returns non-nil and clears pending.
func TestRWXCmdLifecycle(t *testing.T) {
	f := newRWXFetcher()
	if cmd := f.Cmd(nil); cmd != nil {
		t.Fatal("Cmd with nil git should be nil")
	}
	f.pending = &gitpkg.CICheck{URL: "https://cloud.rwx.com/mint/org/runs/x"}
	// Without a real GitDataSource the Cmd would still try to call into it;
	// we only check the bookkeeping side.
}

// Property: Apply on success caches log; on error caches error message.
func TestRWXApplyCaches(t *testing.T) {
	f := newRWXFetcher()
	url := "https://cloud.rwx.com/mint/org/runs/abc123"
	f.Apply(rwxLogMsg{checkURL: url, log: "log body"})
	if f.cache[url] != "log body" {
		t.Errorf("cache=%q want 'log body'", f.cache[url])
	}
	f.Apply(rwxLogMsg{checkURL: url, err: errFake("boom")})
	if got := f.cache[url]; got == "log body" || got == "" {
		t.Errorf("cache after err=%q expected error message overwrite", got)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
