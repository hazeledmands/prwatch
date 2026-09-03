package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// notificationTTL is how long a transient toast stays on screen. One
// definition, rather than the literal that was written out separately at each
// arming site.
const notificationTTL = 4 * time.Second

// notificationState owns the transient bottom-left toast described in
// PROMPT.md's `yank-path` row and its copy-on-release counterpart.
//
// Each shown notification gets a generation, and the expiry Cmd carries the
// generation it was armed for. Without that, every yank armed an unconditional
// timer against a single shared text: yank at t=0 (expiring at t=4) then yank
// again at t=3.9 and the first timer cleared the *second* toast at t=4, 0.1s
// after it appeared. Generations are this package's codified Cmd discipline —
// the msg carries the inputs it was computed from and the handler discards a
// result that no longer matches current state — applied to a timer instead of a
// fetch. It replaces a `notificationExpiry time.Time` field on Model that was
// declared for this job and never read by anything.
type notificationState struct {
	// text is the toast currently on screen; "" means nothing is shown.
	text string
	// gen identifies the current text. Monotonic, so a generation is never
	// reused and a late timer can always be recognized as late.
	gen uint64
}

// Text returns the toast to render, or "" when none is showing.
func (n *notificationState) Text() string { return n.text }

// Show puts text on screen and returns the Cmd that will expire it.
//
// The returned closure captures only the generation as a local — never n — so
// it cannot race Update from the goroutine bubbletea runs it on.
func (n *notificationState) Show(text string) tea.Cmd {
	n.text = text
	n.gen++
	gen := n.gen
	return tea.Tick(notificationTTL, func(time.Time) tea.Msg {
		return notificationExpiredMsg{gen: gen}
	})
}

// Expire clears the toast if msg was armed for the notification currently on
// screen, and does nothing otherwise — the timer belongs to a toast that has
// already been replaced.
func (n *notificationState) Expire(msg notificationExpiredMsg) {
	if msg.gen != n.gen {
		return
	}
	n.text = ""
}
