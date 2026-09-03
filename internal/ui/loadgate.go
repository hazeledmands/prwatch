package ui

// loadGate is a single-flight gate with a trailing rerun, guarding async git
// load dispatch.
//
// The problem it solves: three independent fs watchers (the worktree, `.git`,
// and `.git/refs/heads`) each debounce on their own 200ms timer, and nothing
// coalesced across them, so one commit could produce three RefreshMsg values —
// plus the 30s git tick — and every one of them dispatched a full load. Four
// concurrent `git` process trees over the same repo is both wasteful and a
// source of index.lock collisions. The `seq` staleness protocol
// (gitLoadRequest.seq) only discards stale *results*; it never stopped the
// loads from running.
//
// The gate composes with seq rather than replacing it: seq still decides which
// answer wins when loads do overlap (a trailing load and a hand-dispatched one
// can still be in flight together across a gitDataMsg boundary), while the
// gate keeps the count of concurrent loads at one.
//
// Semantics: Begin reports whether the caller should actually dispatch. A
// trigger arriving while a load is in flight is remembered as a single pending
// rerun instead. Done, called once per load result — adopted or discarded as
// stale, it makes no difference — reports whether that pending rerun should now
// go out. So N near-simultaneous triggers cost one load plus at most one
// trailing load.
//
// The gate is deliberately flavour-agnostic, and the trailing load its caller
// releases is always the local-only one. Every async dispatch site passes
// withPR=false — gitLoadCmd(true) has no production caller — because PR data
// has its own independent path (loadPRStatusCmd, driven by the PR tick and the
// refresh key). So a suppressed trigger can never cost a PR fetch that nothing
// else would make, and tracking a per-trigger flavour here would be state that
// only ever holds one value.
//
// The zero value is an idle gate. It is Update-goroutine state only: nothing
// here is safe to touch from a tea.Cmd closure.
type loadGate struct {
	// inFlight is true between a dispatching Begin and its matching Done.
	inFlight bool
	// pending is set by a Begin that was suppressed, and cleared by the Done
	// that releases the trailing load. It is a bit, not a counter: the trailing
	// load reads the world as it is when it runs, so ten suppressed triggers
	// and one suppressed trigger want exactly the same follow-up.
	pending bool
}

// Begin registers a load trigger. It returns true when the caller should
// dispatch the load, and false when a load is already in flight — in which
// case the trigger has been recorded as a pending rerun that Done will release.
func (g *loadGate) Begin() bool {
	if g.inFlight {
		g.pending = true
		return false
	}
	g.inFlight = true
	return true
}

// Done registers the arrival of a load result. It returns true when a trigger
// was suppressed while that load was running, in which case the caller must
// dispatch one trailing load — the gate has already counted it as in flight.
//
// Done on an idle gate is a no-op, so a hand-built gitDataMsg from a test (or
// a msg from a load that predates the gate) cannot conjure a load.
func (g *loadGate) Done() bool {
	if !g.inFlight {
		return false
	}
	if !g.pending {
		g.inFlight = false
		return false
	}
	g.pending = false
	// inFlight stays true: the trailing load the caller is about to dispatch is
	// the load now in flight.
	return true
}

// InFlight reports whether a load is outstanding.
func (g *loadGate) InFlight() bool { return g.inFlight }

// Pending reports whether a suppressed trigger is waiting for a trailing load.
func (g *loadGate) Pending() bool { return g.pending }
