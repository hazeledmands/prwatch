package ui

// modalOverlay is a full-region screen that covers both panes while it is up:
// the help screen, and the editor picker.
//
// It exists because "is a modal up?" was asked in six places in Update and
// View, each naming `m.help` directly. A second overlay would have turned
// every one of those into a two-term condition, and the one that got missed
// would not fail loudly — it would let a click land on the sidebar underneath
// a painted modal. Asking activeOverlay instead means adding a third overlay
// touches one function.
//
// Key handling is deliberately not part of this interface. Each overlay's keys
// need different things from Model — the help overlay needs nothing, the
// picker needs a launch hook — and flattening that into a common signature
// would hide the difference at exactly the call site that has to know about
// it.
type modalOverlay interface {
	IsOpen() bool
	Close()
	HandleWheel(direction, visibleHeight int)
	PageUp(visibleHeight int)
	Render(visibleHeight int) string
}

// activeOverlay returns the overlay currently covering the panes, or nil.
//
// The switch is what keeps a nil *helpOverlay from being returned as a
// non-nil interface: only an overlay that reports itself open is ever
// returned, so callers can compare the result against nil.
func (m *Model) activeOverlay() modalOverlay {
	switch {
	case m.help.IsOpen():
		return m.help
	case m.editorPicker.IsOpen():
		return m.editorPicker
	default:
		return nil
	}
}

// overlayIsOpen reports whether any modal overlay is covering the panes.
func (m *Model) overlayIsOpen() bool { return m.activeOverlay() != nil }
