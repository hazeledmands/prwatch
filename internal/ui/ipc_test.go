package ui

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

func TestHandleIPC_Render(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", Upstream: "origin/main", RepoName: "repo", DirName: "repo"},
		base:     "origin/main",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"file.go"},
		},
		fileContent: "package main\n",
		allFiles:    []string{"file.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
	}

	m := NewModel("/tmp/test", mock)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Create a pipe to simulate a connection
	server, client := net.Pipe()

	done := make(chan struct{})
	ipc := ipcMsg{
		req:  IPCRequest{Action: "render"},
		conn: server,
		done: done,
	}

	go func() {
		m.handleIPC(ipc)
	}()

	// Read response from client side
	var resp IPCResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	client.Close()
	<-done

	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
	if !strings.Contains(resp.Screen, "file.go") {
		t.Error("render response should contain file.go")
	}
	if !strings.Contains(resp.Screen, "feature") {
		t.Error("render response should contain branch name")
	}
}

func TestHandleIPC_Keys(t *testing.T) {
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feature", Upstream: "origin/main", RepoName: "repo", DirName: "repo"},
		base:     "origin/main",
		changedFiles: git.ChangedFilesResult{
			Committed: []string{"a.go", "b.go"},
		},
		fileContent: "package main\n",
		fileDiff:    "+new",
		allFiles:    []string{"a.go", "b.go"},
		commits:     []git.Commit{{SHA: "abc", Subject: "test"}},
		commitPatch: "diff\n+added",
	}

	m := NewModel("/tmp/test", mock)
	m.width = 80
	m.height = 24
	m.updateLayout()
	msg := m.loadGitData()
	m.Update(msg)

	// Send "c" key to switch to commit mode
	server, client := net.Pipe()
	done := make(chan struct{})
	ipc := ipcMsg{
		req:  IPCRequest{Keys: "c"},
		conn: server,
		done: done,
	}

	go func() {
		m.handleIPC(ipc)
	}()

	var resp IPCResponse
	json.NewDecoder(client).Decode(&resp)
	client.Close()
	<-done

	if !strings.Contains(resp.Screen, "commits") {
		t.Error("after 'c' key, screen should show commit mode")
	}
}

func TestHandleIPC_Quit(t *testing.T) {
	m := NewModel("/tmp/test", nil)
	m.width = 80
	m.height = 24
	m.updateLayout()

	server, client := net.Pipe()
	done := make(chan struct{})
	ipc := ipcMsg{
		req:  IPCRequest{Action: "quit"},
		conn: server,
		done: done,
	}

	go func() {
		m.handleIPC(ipc)
	}()

	var resp IPCResponse
	json.NewDecoder(client).Decode(&resp)
	client.Close()
	<-done

	if resp.Screen != "quitting" {
		t.Errorf("quit response should say 'quitting', got %q", resp.Screen)
	}
}

func TestHandleIPC_HeadlessDimensions(t *testing.T) {
	// When width/height are 0 (headless), handleIPC should set defaults
	mock := &mockGit{
		repoInfo: git.RepoInfoResult{Branch: "feat", Upstream: "origin/main", RepoName: "r", DirName: "r"},
		base:     "origin/main",
		allFiles: []string{"x.go"},
		commits:  []git.Commit{{SHA: "abc", Subject: "test"}},
	}

	m := NewModel("/tmp/test", mock)
	msg := m.loadGitData()
	m.Update(msg)
	// Don't set width/height — simulate headless

	server, client := net.Pipe()
	done := make(chan struct{})
	ipc := ipcMsg{
		req:  IPCRequest{Action: "render"},
		conn: server,
		done: done,
	}

	go func() {
		m.handleIPC(ipc)
	}()

	var resp IPCResponse
	json.NewDecoder(client).Decode(&resp)
	client.Close()
	<-done

	if resp.Screen == "" {
		t.Error("headless render should produce non-empty screen")
	}
	lines := strings.Split(resp.Screen, "\n")
	if len(lines) != 40 {
		t.Errorf("headless render should default to 40 lines, got %d", len(lines))
	}
}

func TestStartIPCListener_RoundTrip(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	var received ipcMsg
	receivedCh := make(chan struct{})
	cleanup, err := StartIPCListener(socketPath, func(msg tea.Msg) {
		received = msg.(ipcMsg)
		// Simulate handleIPC: write response and close done
		resp := IPCResponse{Screen: "test-screen"}
		json.NewEncoder(received.conn).Encode(resp)
		received.conn.Close()
		close(received.done)
		close(receivedCh)
	})
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer cleanup()

	// Connect and send request
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	req := IPCRequest{Keys: "j,k"}
	json.NewEncoder(conn).Encode(req)

	var resp IPCResponse
	json.NewDecoder(conn).Decode(&resp)

	select {
	case <-receivedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	if resp.Screen != "test-screen" {
		t.Errorf("expected 'test-screen', got %q", resp.Screen)
	}
	if received.req.Keys != "j,k" {
		t.Errorf("expected keys 'j,k', got %q", received.req.Keys)
	}
}

// The default socket used to be a fixed, world-accessible /tmp/prwatch.sock:
// any user on the box could connect to it and drive another user's TUI, and
// any user could squat the name first. The default must be per-user and live
// in a directory only its owner can traverse.
func TestDefaultIPCSocketPath_IsPerUserAndPrivate(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	got, err := DefaultIPCSocketPath()
	if err != nil {
		t.Fatalf("DefaultIPCSocketPath: %v", err)
	}
	if got == "/tmp/prwatch.sock" {
		t.Fatal("default socket path is still the shared world-accessible /tmp path")
	}
	if !strings.HasPrefix(got, runtimeDir) {
		t.Errorf("path %q should live under XDG_RUNTIME_DIR %q when it is set", got, runtimeDir)
	}

	info, err := os.Stat(filepath.Dir(got))
	if err != nil {
		t.Fatalf("socket directory should have been created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("socket directory mode = %o, want 700", perm)
	}
}

// With no XDG_RUNTIME_DIR the path falls back to the user cache dir — still
// per-user, still not /tmp.
func TestDefaultIPCSocketPath_FallsBackToUserCacheDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	got, err := DefaultIPCSocketPath()
	if err != nil {
		t.Fatalf("DefaultIPCSocketPath: %v", err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache dir on this system: %v", err)
	}
	if !strings.HasPrefix(got, cache) {
		t.Errorf("path %q should live under the user cache dir %q", got, cache)
	}
	if strings.HasPrefix(got, "/tmp/") {
		t.Errorf("path %q must not fall back to /tmp", got)
	}
}

// The listener must not leave a world-accessible socket behind even when the
// caller supplied the path (PRWATCH_IPC_SOCKET can point anywhere).
func TestStartIPCListener_SocketIsNotWorldAccessible(t *testing.T) {
	socketPath := filepath.Join(shortTempDir(t), "s.sock")
	cleanup, err := StartIPCListener(socketPath, func(tea.Msg) {})
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer cleanup()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("socket mode = %o, want no group/other access", perm)
	}
}

// shortTempDir is t.TempDir() for things that must hold a unix socket. macOS
// caps a socket path at ~104 bytes and t.TempDir() spends most of that on the
// test's own name, so a socket under it fails to bind with EINVAL.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "pw")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// shortIPCTimeouts shrinks both IPC deadlines for the duration of a test and
// restores them only once the goroutines that read them have finished — the
// caller signals that by closing/receiving on the returned wait function.
func shortIPCTimeouts(t *testing.T, d time.Duration) (restore func()) {
	t.Helper()
	oldRead, oldWrite := ipcReadTimeout, ipcWriteTimeout
	ipcReadTimeout, ipcWriteTimeout = d, d
	return func() { ipcReadTimeout, ipcWriteTimeout = oldRead, oldWrite }
}

// A client that connects and then says nothing must not tie up a goroutine
// and a connection forever.
func TestHandleIPCConn_ReadDeadlineReleasesASilentClient(t *testing.T) {
	restore := shortIPCTimeouts(t, 50*time.Millisecond)

	server, client := net.Pipe()
	defer client.Close()

	returned := make(chan struct{})
	go func() {
		handleIPCConn(server, func(tea.Msg) {
			t.Error("a request that never arrived must not be dispatched to the model")
		})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("handleIPCConn hung on a client that sent nothing; read deadline missing")
	}
	// Restore only after the goroutine reading these vars is done.
	restore()
}

// The response encode used to run on the Update goroutine. A client that
// stops reading fills the socket buffer, and the whole TUI wedges behind a
// write that never completes. handleIPC must return promptly regardless of
// what the client does.
func TestHandleIPC_StuckClientDoesNotBlockUpdate(t *testing.T) {
	restore := shortIPCTimeouts(t, 50*time.Millisecond)

	m := NewModel("/tmp/test", nil)
	m.width = 80
	m.height = 24
	m.updateLayout()

	// net.Pipe is unbuffered and synchronous: a client that never reads
	// blocks the writer indefinitely, which is exactly the stuck client.
	server, client := net.Pipe()
	defer client.Close()

	ipc := ipcMsg{req: IPCRequest{Action: "render"}, conn: server, done: make(chan struct{})}

	returned := make(chan struct{})
	go func() {
		m.handleIPC(ipc)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("handleIPC blocked on a client that never read the response")
	}

	// And the connection is not leaked: the write eventually gives up.
	select {
	case <-ipc.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the response writer never finished; connection leaked")
	}
	restore()
}

// A model left corrupt by a follow-up can panic on render. The client must
// still get a response carrying the error — an unguarded render would kill
// the report that explains what went wrong, which is the exact failure the
// panic-reporting exists to eliminate.
func TestHandleIPC_RenderPanicStillReportsToTheClient(t *testing.T) {
	m := NewModel("/tmp/test", nil)
	m.width = 80
	m.height = 24
	m.updateLayout()
	// loading must be false or View early-returns before it reaches the panes.
	m.loading = false
	m.sidebar = nil // corruption of the kind a half-applied Update leaves

	server, client := net.Pipe()
	defer client.Close()
	ipc := ipcMsg{req: IPCRequest{Action: "render"}, conn: server, done: make(chan struct{})}

	go func() { m.handleIPC(ipc) }()

	var resp IPCResponse
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("no response reached the client: %v", err)
	}
	<-ipc.done

	if resp.Error == "" {
		t.Fatal("a panicking render must be reported in IPCResponse.Error")
	}
	if !strings.Contains(resp.Error, "report render") {
		t.Errorf("error should name the failing activity, got %q", resp.Error)
	}
}

func TestIPCSocketPathFromEnv(t *testing.T) {
	os.Setenv("PRWATCH_IPC_SOCKET", "/tmp/test.sock")
	defer os.Unsetenv("PRWATCH_IPC_SOCKET")

	if got := IPCSocketPathFromEnv(); got != "/tmp/test.sock" {
		t.Errorf("expected /tmp/test.sock, got %q", got)
	}

	os.Unsetenv("PRWATCH_IPC_SOCKET")
	if got := IPCSocketPathFromEnv(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
