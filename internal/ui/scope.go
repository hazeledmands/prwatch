package ui

import "errors"

// scope describes the half-open commit range currently in view: (oldBase, newBase].
// It owns the question "what commits am I looking at?" for the Model — driving
// the in-scope commit list, the in-scope changed-files diff, the commits-mode
// cutline placement, and the status-bar handle indicator.
//
// Both endpoints are notionally scrubbable. Today only the outer (older) endpoint
// is wired to keys ([ / ] / \); the newer endpoint is always HEAD. The type
// encodes both up front so adding inner-endpoint scrubbing later is additive,
// not structural.
//
// oldOffset and newOffset (commits-from-HEAD counts) are a cache. They are
// kept in lockstep with oldBase / newBase by ExtendBack and ContractForward
// so the UI can render the new HEAD~N indicator in the same frame as the SHA
// change. Without the cache, the offset would only refresh after the next
// async git load — leaving the indicator out of step with the SHA for the
// duration of the load.
type scope struct {
	oldBase string // SHA of the older endpoint (exclusive)
	newBase string // SHA of the newer endpoint (inclusive); "" means HEAD

	oldOffset int // commits between oldBase and HEAD; HEAD~oldOffset == oldBase
	newOffset int // commits between newBase and HEAD; 0 when newBase is HEAD

	// pinned is the authority on scrubbed-ness. The user pins a *commit*,
	// not a distance: deriving scrubbed-ness from oldOffset != naturalOldOffset
	// meant that making a commit (which moves HEAD, and so moves the natural
	// offset up to meet the scrubbed one) silently un-pinned the scope and
	// snapped it back to the default range. It is maintained by the endpoint
	// movers (ExtendBack / ContractForward / Reset) and re-evaluated against
	// the freshly-detected natural endpoints on each load.
	pinned bool

	naturalOldBase   string
	naturalNewBase   string
	naturalOldOffset int
	naturalNewOffset int
}

// scopeHandleInfo describes the scrubbed handle position for the status bar.
// Today the indicator describes only the outer endpoint; when inner-endpoint
// scrubbing lands the rendering will change but the shape produced here stays
// suitable as a status-bar payload.
type scopeHandleInfo struct {
	sha7       string
	headOffset int
}

// errScopeUnloaded is returned by ExtendBack when scope.oldBase has not yet
// been populated by an initial load. Callers should treat it as a no-op.
var errScopeUnloaded = errors.New("scope: not yet loaded")

// OldBase returns the SHA of the older endpoint (exclusive in (oldBase, newBase]).
// Used by callers running git queries against the current scope (ChangedFiles,
// Commits, BaseCommits, ...).
func (s *scope) OldBase() string { return s.oldBase }

// NewBase returns the SHA of the newer endpoint. Empty string means HEAD,
// the default newer endpoint; only inner-endpoint scrubbing makes it non-empty.
func (s *scope) NewBase() string { return s.newBase }

// Len returns the number of commits inside the scope range.
func (s *scope) Len() int { return s.oldOffset - s.newOffset }

// IsScrubbed reports whether either endpoint has been moved from its natural
// position. The Handle indicator is shown iff IsScrubbed is true.
func (s *scope) IsScrubbed() bool { return s.pinned }

// repin re-evaluates the pin against the natural endpoints. Pinned means
// "the endpoint SHA differs from the natural one" — a pin that lands back on
// the natural endpoint (contracting all the way forward, or a rebase moving
// the natural base onto the pinned commit) is not a scrub any more.
func (s *scope) repin() {
	s.pinned = s.oldBase != s.naturalOldBase || s.newBase != s.naturalNewBase
}

// Handle returns the indicator to prefix on the status bar when scrubbed,
// or nil at the natural position (or before the first load completes).
// Today only describes the outer endpoint; the inner endpoint is always HEAD.
func (s *scope) Handle() *scopeHandleInfo {
	if !s.IsScrubbed() || s.oldBase == "" {
		return nil
	}
	return &scopeHandleInfo{sha7: shortSHA(s.oldBase), headOffset: s.oldOffset}
}

// ExtendBack walks the outer endpoint one commit further back (older).
// Updates oldOffset in lockstep so the indicator can render consistently.
// Returns an error at the root commit or before scope has been loaded;
// callers treat both as a silent no-op.
func (s *scope) ExtendBack(g GitDataSource) error {
	if s.oldBase == "" {
		return errScopeUnloaded
	}
	parent, err := g.Parent(s.oldBase)
	if err != nil {
		return err
	}
	s.oldBase = parent
	s.oldOffset++
	s.repin()
	return nil
}

// ContractForward walks the outer endpoint one commit toward HEAD. No-op
// when the range is already empty (Len() == 0) — the working tree is the
// floor and can't be crossed by the outer endpoint.
func (s *scope) ContractForward(g GitDataSource) error {
	if s.Len() == 0 {
		return nil
	}
	child, err := g.FirstChildToward(s.oldBase, "HEAD")
	if err != nil {
		return err
	}
	s.oldBase = child
	s.oldOffset--
	s.repin()
	return nil
}

// Reset snaps both endpoints back to their natural (detected) positions.
func (s *scope) Reset() {
	s.oldBase = s.naturalOldBase
	s.newBase = s.naturalNewBase
	s.oldOffset = s.naturalOldOffset
	s.newOffset = s.naturalNewOffset
	s.pinned = false
}

// SyncFromLoad reconciles after an async load reports freshly-detected
// natural endpoints (with their offsets from HEAD), plus the load's own
// measurement of how far the pinned outer endpoint sits from HEAD:
// pinnedOldOffset, and pinnedBase — the SHA that measurement was taken
// against ("" when the load carried no pin, offset -1 likewise).
//
// pinnedBase is what makes the measurement safe to apply. The offset is a
// fact about one specific commit, so it is adopted only when that commit is
// still the pinned one. Without the check, a load measured against an earlier
// pin (the user scrubbed twice in quick succession, or a load was already in
// flight when they scrubbed) would stamp its distance onto the *current* pin
// and the `HEAD~N` indicator would read wrong until the next load landed.
//
// When not pinned, scope adopts the natural values directly.
//
// When pinned, the user's commit is kept and only its cached distance is
// refreshed, so the indicator tracks the pinned commit as new commits land
// instead of going stale. A load that was already in flight at scrub time
// must carry the natural values it observed at load time — not the user's
// scrubbed values — or this preservation breaks.
//
// A pin that the natural endpoint has caught up with (rebase, base branch
// advanced onto the pinned commit) is dropped: it no longer describes
// anything different from the default.
func (s *scope) SyncFromLoad(naturalOldBase, naturalNewBase string, naturalOldOffset, naturalNewOffset int, pinnedBase string, pinnedOldOffset int) {
	wasPinned := s.pinned
	s.naturalOldBase = naturalOldBase
	s.naturalNewBase = naturalNewBase
	s.naturalOldOffset = naturalOldOffset
	s.naturalNewOffset = naturalNewOffset

	if !wasPinned {
		s.oldBase = naturalOldBase
		s.newBase = naturalNewBase
		s.oldOffset = naturalOldOffset
		s.newOffset = naturalNewOffset
		s.pinned = false
		return
	}

	// Only a measurement taken against the commit that is *still* pinned says
	// anything about where that commit now sits.
	if pinnedOldOffset >= 0 && pinnedBase != "" && pinnedBase == s.oldBase {
		s.oldOffset = pinnedOldOffset
	}
	if s.newBase == naturalNewBase {
		s.newOffset = naturalNewOffset
	}
	s.repin()
	if !s.pinned {
		s.oldOffset = naturalOldOffset
		s.newOffset = naturalNewOffset
	}
}
