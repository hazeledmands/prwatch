package watcher

import (
	"errors"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DebounceInterval is the quiet period a burst of filesystem events must reach
// before the refresh callback fires.
const DebounceInterval = 200 * time.Millisecond

// MaxDebounceWait bounds how long a burst may hold the callback off.
//
// Without a bound, a trailing-edge debounce that resets on every event never
// fires at all while events keep arriving closer together than
// DebounceInterval — a build writing output, a branch switch rewriting the
// tree, a formatter sweeping every file. The refresh is what marks filesystem
// activity for the poll scheduler, so starving it decays the poll to its idle
// interval at the exact moment the tree is most active.
//
// 1s is chosen against both directions. Below it, a burst of ordinary editor
// saves would stop coalescing and cost a git load each. Above it, the stall is
// long enough for a user to notice the sidebar lagging their own edit. It also
// sits an order of magnitude under the 5s active poll, so the bound is always
// the faster of the two paths to a refresh.
const MaxDebounceWait = 1 * time.Second

// errNoDirs reports that a watcher was asked to watch nothing.
var errNoDirs = errors.New("watcher: no directories to watch")

type Watcher struct {
	fsw      *fsnotify.Watcher
	done     chan struct{}
	watching []string
}

// New creates a file watcher that calls onRefresh (debounced) when files change in dir.
func New(dir string, onRefresh func()) (*Watcher, error) {
	return NewMulti([]string{dir}, onRefresh)
}

// NewMulti watches every directory in dirs and calls onRefresh (debounced)
// when anything in any of them changes.
//
// One fsnotify watcher and one debounce timer cover the whole set, which is
// the point of taking them together rather than as separate watchers. Separate
// watchers each ran their own 200ms timer with nothing coalescing across them,
// so a single commit could fire the callback once per watched location; here
// it fires once. The load gate downstream would absorb the duplicates anyway,
// but not producing them is cheaper and keeps growing the watch set from
// growing the refresh traffic.
//
// A directory that cannot be watched is skipped rather than fatal: the set is
// derived from a repo's layout, and losing one location should degrade
// coverage, not disable watching. An error comes back only when nothing at all
// could be watched.
func NewMulti(dirs []string, onRefresh func()) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	var watching []string
	var firstErr error
	for _, dir := range dirs {
		if err := fsw.Add(dir); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		watching = append(watching, dir)
	}
	if len(watching) == 0 {
		fsw.Close()
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, errNoDirs
	}

	w := &Watcher{
		fsw:      fsw,
		done:     make(chan struct{}),
		watching: watching,
	}

	go w.loop(onRefresh)
	return w, nil
}

// Watching returns the directories the watcher actually holds, in the order
// they were added.
func (w *Watcher) Watching() []string {
	return slices.Clone(w.watching)
}

// loop coalesces filesystem events into refresh calls.
//
// Two clocks run per burst. The debounce timer restarts on every event and
// fires once the tree goes quiet, which is what collapses a flurry of writes
// into one refresh. The max-wait timer starts with the burst and never
// restarts, so a stream of events that keeps resetting the debounce still gets
// a refresh within MaxDebounceWait of when it began.
//
// Both timers are selected on here rather than driven by time.AfterFunc, so
// every refresh and every piece of burst state stays on this one goroutine.
// The callback sends into a bubbletea program, and burst bookkeeping split
// across a timer goroutine would need a mutex to be race-free.
func (w *Watcher) loop(onRefresh func()) {
	debounce := stoppedTimer()
	maxWait := stoppedTimer()
	defer debounce.Stop()
	defer maxWait.Stop()

	// bursting is true from the first event of a burst until the refresh that
	// ends it, and is what keeps the max-wait clock per-burst: without it, a
	// lone event long after a previous burst would fire against an
	// already-expired deadline instead of getting its own coalescing window.
	bursting := false

	fire := func() {
		bursting = false
		debounce.Stop()
		maxWait.Stop()
		onRefresh()
	}

	for {
		select {
		case _, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if !bursting {
				bursting = true
				maxWait.Reset(MaxDebounceWait)
			}
			debounce.Reset(DebounceInterval)
		case <-debounce.C:
			if bursting {
				fire()
			}
		case <-maxWait.C:
			if bursting {
				fire()
			}
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		case <-w.done:
			return
		}
	}
}

// stoppedTimer returns a timer that is not running. Go 1.23 and later make
// timer channels unbuffered, so a Stop or Reset cannot leave a stale value
// behind to be received later — which is what lets the loop above reset these
// freely without draining them.
func stoppedTimer() *time.Timer {
	t := time.NewTimer(time.Hour)
	t.Stop()
	return t
}

func (w *Watcher) Close() error {
	close(w.done)
	return w.fsw.Close()
}
