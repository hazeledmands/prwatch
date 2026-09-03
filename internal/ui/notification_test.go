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

// TestNotification_ShowArmsMatchingExpiry is the test the other four cannot be:
// it takes the expiry msg from the arming path itself rather than building one
// from n.gen, so it can see a disagreement between the generation Show arms its
// timer with and the generation Expire compares against.
//
// Every other test here hand-constructs notificationExpiredMsg{gen: n.gen},
// which silently assumes those two agree. They didn't have to: an off-by-one in
// show (returning the pre-increment generation) arms every timer against a
// generation that is never current, so no toast ever expires and the toast
// becomes immortal.
//
// Verified by injecting exactly that mutation: this test fails with "the expiry
// msg show() armed did not clear the toast it armed it for", and every other
// test in this file passes — TestNotification_StaleExpiryThroughUpdate
// included, since it too builds its msg from gen and so is equally blind. This
// test is the only thing standing between that mutation and a green suite.
//
// It runs the real Show as well, to pin that Show delegates to show rather than
// doing its own bookkeeping. The one link neither can reach without sleeping
// notificationTTL is the literal `return msg` inside the tea.Tick closure.
func TestNotification_ShowArmsMatchingExpiry(t *testing.T) {
	t.Parallel()

	// The arming path's own payload must be the one that clears the toast.
	var n notificationState
	msg := n.show("copied x")
	if got := n.Text(); got != "copied x" {
		t.Fatalf("Text() = %q, want %q", got, "copied x")
	}
	n.Expire(msg)
	if got := n.Text(); got != "" {
		t.Errorf("the expiry msg show() armed did not clear the toast it armed it for: Text() = %q", got)
	}

	// Show must route through show, so its generation bookkeeping is the same.
	var viaCmd notificationState
	if cmd := viaCmd.Show("copied y"); cmd == nil {
		t.Fatal("Show returned a nil Cmd, so nothing would ever expire the toast")
	}
	if viaCmd.gen != 1 {
		t.Errorf("Show left gen = %d, want 1 — it is not delegating to show", viaCmd.gen)
	}
	if got := viaCmd.Text(); got != "copied y" {
		t.Errorf("Show left Text() = %q, want %q", got, "copied y")
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
