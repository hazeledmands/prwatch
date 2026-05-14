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
func (s *scope) IsScrubbed() bool {
	return s.oldOffset != s.naturalOldOffset || s.newOffset != s.naturalNewOffset
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
	return nil
}

// Reset snaps both endpoints back to their natural (detected) positions.
func (s *scope) Reset() {
	s.oldBase = s.naturalOldBase
	s.newBase = s.naturalNewBase
	s.oldOffset = s.naturalOldOffset
	s.newOffset = s.naturalNewOffset
}

// SyncFromLoad reconciles after an async load reports freshly-detected
// natural endpoints (with their offsets from HEAD).
//
// When not scrubbed, scope adopts the natural values directly.
//
// When scrubbed, only the natural fields update; the user's scrub is preserved
// so a later Reset still snaps to the right place. A load that was already in
// flight at scrub time must therefore carry the natural values it observed at
// load time — not the user's scrubbed values — or this preservation breaks.
func (s *scope) SyncFromLoad(naturalOldBase, naturalNewBase string, naturalOldOffset, naturalNewOffset int) {
	wasScrubbed := s.IsScrubbed()
	s.naturalOldBase = naturalOldBase
	s.naturalNewBase = naturalNewBase
	s.naturalOldOffset = naturalOldOffset
	s.naturalNewOffset = naturalNewOffset
	if !wasScrubbed {
		s.oldBase = naturalOldBase
		s.newBase = naturalNewBase
		s.oldOffset = naturalOldOffset
		s.newOffset = naturalNewOffset
	}
}
