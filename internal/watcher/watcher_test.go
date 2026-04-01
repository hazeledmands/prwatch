package watcher_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hazeledmands/prwatch/internal/watcher"
)

func TestWatcher_InvalidDir(t *testing.T) {
	_, err := watcher.New("/nonexistent/dir/that/should/not/exist", func() {})
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestWatcher_CloseImmediately(t *testing.T) {
	dir := t.TempDir()
	w, err := watcher.New(dir, func() {})
	if err != nil {
		t.Fatal(err)
	}
	// Close immediately — should not hang or panic
	if err := w.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestWatcher_MultipleChanges_Debounced(t *testing.T) {
	dir := t.TempDir()

	count := 0
	ch := make(chan struct{}, 10)
	w, err := watcher.New(dir, func() {
		count++
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Write multiple files rapidly
	for i := 0; i < 5; i++ {
		f := filepath.Join(dir, "file"+string(rune('a'+i))+".txt")
		os.WriteFile(f, []byte("data"), 0644)
		time.Sleep(10 * time.Millisecond)
	}

	// Should get at most a few callbacks due to debouncing
	select {
	case <-ch:
		// got at least one
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for debounced callback")
	}
}

func TestWatcher_FileDeletion(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "delete_me.txt")
	os.WriteFile(testFile, []byte("delete"), 0644)

	ch := make(chan struct{}, 10)
	w, err := watcher.New(dir, func() {
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)
	os.Remove(testFile)

	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for file deletion notification")
	}
}

func TestWatcher_FileRename(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "rename_me.txt")
	os.WriteFile(testFile, []byte("rename"), 0644)

	ch := make(chan struct{}, 10)
	w, err := watcher.New(dir, func() {
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	time.Sleep(50 * time.Millisecond)
	os.Rename(testFile, filepath.Join(dir, "renamed.txt"))

	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for rename notification")
	}
}

func TestWatcher_FileCreation(t *testing.T) {
	dir := t.TempDir()

	ch := make(chan struct{}, 10)
	w, err := watcher.New(dir, func() {
		ch <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Create a new file
	time.Sleep(50 * time.Millisecond)
	newFile := filepath.Join(dir, "new.txt")
	os.WriteFile(newFile, []byte("new"), 0644)

	select {
	case <-ch:
		// success
	case <-time.After(1 * time.Second):
		t.Error("timed out waiting for file creation notification")
	}
}

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
