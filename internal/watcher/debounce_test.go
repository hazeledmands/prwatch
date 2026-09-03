package watcher_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/watcher"
)

// TestWatcher_SustainedChurnRefreshesWithinBound is the starvation regression.
//
// A trailing-edge debounce that resets on every event never fires while events
// keep arriving faster than the interval. That is precisely the wrong moment to
// go silent: the model marks filesystem activity only when a refresh actually
// arrives, so an unbroken stream of writes — a build, a branch switch, a
// formatter sweeping the tree — would let the poll decay to its idle interval
// exactly while the tree is busiest.
func TestWatcher_SustainedChurnRefreshesWithinBound(t *testing.T) {
	dir := t.TempDir()

	got := make(chan time.Time, 64)
	w, err := watcher.New(dir, func() { got <- time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)

	// A steady stream comfortably faster than the debounce interval.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			os.WriteFile(filepath.Join(dir, "churn"+strconv.Itoa(i)+".txt"), []byte("x"), 0o644)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	start := time.Now()
	select {
	case at := <-got:
		// The bound is a max-latency guarantee, not an exact deadline: the
		// generous slack absorbs scheduler noise on a loaded CI box.
		if d := at.Sub(start); d > watcher.MaxDebounceWait+2*time.Second {
			t.Errorf("first refresh took %v, want within about %v", d, watcher.MaxDebounceWait)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no refresh at all under sustained churn: the debounce is starving")
	}
}

// TestWatcher_SingleEventStillCoalesces pins the other half: the max-latency
// bound must not turn the debounce into a fire-on-every-event watcher.
func TestWatcher_SingleEventStillCoalesces(t *testing.T) {
	dir := t.TempDir()

	got := make(chan time.Time, 64)
	w, err := watcher.New(dir, func() { got <- time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case at := <-got:
		// One write can produce several filesystem events (create, then
		// write); all of them belong to one refresh, and that refresh waits
		// out the coalescing window rather than firing on the first event.
		if d := at.Sub(start); d < watcher.DebounceInterval {
			t.Errorf("refresh came after %v, want at least the %v debounce window", d, watcher.DebounceInterval)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no refresh for a single write")
	}

	select {
	case <-got:
		t.Error("a single write produced a second refresh")
	case <-time.After(3 * watcher.DebounceInterval):
	}
}

// TestWatcher_BurstBoundIsPerBurst guards against a stale burst clock: after a
// burst completes, a later lone event must get its full coalescing window
// again rather than firing immediately against the old burst's start time.
func TestWatcher_BurstBoundIsPerBurst(t *testing.T) {
	dir := t.TempDir()

	got := make(chan time.Time, 64)
	w, err := watcher.New(dir, func() { got <- time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no refresh for the first write")
	}

	// Well past the max-wait bound, so a burst clock that was never reset
	// would now be permanently expired.
	time.Sleep(watcher.MaxDebounceWait + 300*time.Millisecond)
	drain(got)

	start := time.Now()
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-got:
		if d := at.Sub(start); d < watcher.DebounceInterval {
			t.Errorf("second refresh came after %v, want at least the %v debounce window", d, watcher.DebounceInterval)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no refresh for the second write")
	}
}

func drain(ch chan time.Time) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
