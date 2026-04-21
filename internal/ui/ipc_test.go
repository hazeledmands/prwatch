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
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
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
		allCommits:  []git.Commit{{SHA: "abc", Subject: "test"}},
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
