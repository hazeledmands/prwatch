package ui

import (
	"testing"

	"pgregory.net/rapid"
)

// TestNotification_EarlierExpiryDoesNotClearLaterToast is the regression test
// for the expiry race: yank at t=0 arms a timer for t=4s, yank again at t=3.9s
// replaces the text, and the first timer then fired and cleared the second
// toast 0.1s after it appeared.
func TestNotification_EarlierExpiryDoesNotClearLaterToast(t *testing.T) {
	t.Parallel()
	var n notificationState

	n.Show("copied first")
	stale := n.gen

	n.Show("copied second")
	if got := n.Text(); got != "copied second" {
		t.Fatalf("Text() = %q, want %q", got, "copied second")
	}

	// The first yank's timer lands here. It must not clear the second toast.
	n.Expire(notificationExpiredMsg{gen: stale})
	if got := n.Text(); got != "copied second" {
		t.Errorf("stale expiry cleared the live toast: Text() = %q, want %q", got, "copied second")
	}

	// The second yank's own timer does clear it.
	n.Expire(notificationExpiredMsg{gen: n.gen})
	if got := n.Text(); got != "" {
		t.Errorf("matching expiry did not clear: Text() = %q, want empty", got)
	}
}

// TestProperty_NotificationOnlyCurrentGenerationClears pins the guard over
// arbitrary interleavings of Show and Expire: a toast is cleared exactly by an
// expiry carrying its own generation, and never by any other.
func TestProperty_NotificationOnlyCurrentGenerationClears(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var n notificationState
		var seen []uint64 // every generation ever issued
		prevGen := n.gen

		for range rapid.IntRange(0, 30).Draw(t, "steps") {
			switch rapid.IntRange(0, 1).Draw(t, "op") {
			case 0:
				text := rapid.SampledFrom([]string{"copied a", "copied b", "copied selection (2 lines, 9 bytes)"}).Draw(t, "text")
				n.Show(text)
				if n.gen <= prevGen {
					t.Fatalf("generation did not advance: %d then %d", prevGen, n.gen)
				}
				prevGen = n.gen
				seen = append(seen, n.gen)
				if n.Text() != text {
					t.Fatalf("Text() = %q, want %q", n.Text(), text)
				}
			case 1:
				// Expire with an arbitrary previously-issued generation, or a
				// never-issued one.
				gen := rapid.Uint64Range(0, 40).Draw(t, "expireGen")
				if len(seen) > 0 && rapid.Bool().Draw(t, "useSeen") {
					gen = rapid.SampledFrom(seen).Draw(t, "seenGen")
				}
				before := n.Text()
				n.Expire(notificationExpiredMsg{gen: gen})
				if gen == n.gen {
					if n.Text() != "" {
						t.Fatalf("expiry for the current generation %d left %q", gen, n.Text())
					}
				} else if n.Text() != before {
					t.Fatalf("expiry for stale generation %d (current %d) changed %q to %q",
						gen, n.gen, before, n.Text())
				}
			}
		}
	})
}

// TestNotification_StaleExpiryThroughUpdate is the wiring check: the same race
// driven through Model.Update, so the guard is proven to be on the real path
// the tea.Tick msg travels rather than only on the type.
func TestNotification_StaleExpiryThroughUpdate(t *testing.T) {
	m := NewModel("/tmp", nil)
	m.loading = false
	m.width = 80
	m.height = 24
	m.updateLayout()

	m.notifications.Show("copied first")
	stale := m.notifications.gen
	m.notifications.Show("copied second")

	result, _ := m.Update(notificationExpiredMsg{gen: stale})
	m = result.(*Model)
	if got := m.notifications.Text(); got != "copied second" {
		t.Errorf("stale expiry msg cleared the live toast: %q, want %q", got, "copied second")
	}

	result, _ = m.Update(notificationExpiredMsg{gen: m.notifications.gen})
	m = result.(*Model)
	if got := m.notifications.Text(); got != "" {
		t.Errorf("current expiry msg did not clear: %q", got)
	}
}

// TestNotification_ExpireIsIdempotent: the dismiss path can fire more than once
// (a duplicate tick, a re-delivered msg) and must stay cleared without
// resurrecting anything.
func TestNotification_ExpireIsIdempotent(t *testing.T) {
	t.Parallel()
	var n notificationState
	n.Show("copied x")
	msg := notificationExpiredMsg{gen: n.gen}
	n.Expire(msg)
	n.Expire(msg)
	if got := n.Text(); got != "" {
		t.Errorf("Text() = %q, want empty", got)
	}
}
