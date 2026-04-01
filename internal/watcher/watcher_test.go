package watcher_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/watcher"
)

func TestWatcher_DetectsFileChange(t *testing.T) {
	dir := t.TempDir()

	// Create a file to watch
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}

	ch := make(chan struct{}, 10)
	w, err := watcher.New(dir, func() {
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Modify the file
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(testFile, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should receive a notification within 500ms (debounce is 200ms)
	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for file change notification")
	}
}
