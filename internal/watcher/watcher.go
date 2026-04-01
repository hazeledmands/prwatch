package watcher

import (
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceInterval = 200 * time.Millisecond

type Watcher struct {
	fsw  *fsnotify.Watcher
	done chan struct{}
}

// New creates a file watcher that calls onRefresh (debounced) when files change in dir.
func New(dir string, onRefresh func()) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		fsw:  fsw,
		done: make(chan struct{}),
	}

	go w.loop(onRefresh)
	return w, nil
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
