package watcher

import (
	"errors"
	"slices"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 200 * time.Millisecond

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

func (w *Watcher) loop(onRefresh func()) {
	var timer *time.Timer
	for {
		select {
		case _, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceInterval, onRefresh)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		case <-w.done:
			return
		}
	}
}

func (w *Watcher) Close() error {
	close(w.done)
	return w.fsw.Close()
}
