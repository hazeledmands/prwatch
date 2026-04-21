package ui

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// IPCRequest is a command sent to the IPC socket.
type IPCRequest struct {
	// Keys is a comma-separated list of key names to send (same format as PRWATCH_KEYS).
	Keys string `json:"keys,omitempty"`
	// Action is a special action: "render" returns the current screen, "quit" stops the app.
	Action string `json:"action,omitempty"`
}

// IPCResponse is the reply from the IPC socket.
type IPCResponse struct {
	Screen string `json:"screen"`
	Error  string `json:"error,omitempty"`
}

// ipcMsg is sent to the bubbletea model when an IPC request arrives.
type ipcMsg struct {
	req  IPCRequest
	conn net.Conn
	done chan struct{} // closed after the model writes the response
}

// StartIPCListener starts a Unix domain socket listener that accepts
// IPC commands and sends them as tea messages.
func StartIPCListener(socketPath string, send func(tea.Msg)) (cleanup func(), err error) {
	// Remove stale socket file
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("ipc listen: %w", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go handleIPCConn(conn, send)
		}
	}()

	return func() {
		ln.Close()
		os.Remove(socketPath)
	}, nil
}

func handleIPCConn(conn net.Conn, send func(tea.Msg)) {
	dec := json.NewDecoder(conn)
	var req IPCRequest
	if err := dec.Decode(&req); err != nil {
		resp := IPCResponse{Error: fmt.Sprintf("invalid request: %v", err)}
		json.NewEncoder(conn).Encode(resp)
		conn.Close()
		return
	}

	// Send the request to the model and wait for it to write the response.
	// The done channel is closed by handleIPC after it writes to conn.
	done := make(chan struct{})
	send(ipcMsg{req: req, conn: conn, done: done})
	<-done
}

// handleIPC processes an IPC request within the model's Update loop.
func (m *Model) handleIPC(msg ipcMsg) (tea.Model, tea.Cmd) {
	defer close(msg.done)
	req := msg.req

	if req.Action == "quit" {
		resp := IPCResponse{Screen: "quitting"}
		json.NewEncoder(msg.conn).Encode(resp)
		msg.conn.Close()
		return m, tea.Quit
	}

	// Apply key sequence
	if req.Keys != "" {
		for _, k := range strings.Split(req.Keys, ",") {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			keyMsg := parseKeyName(k)
			result, cmd := m.Update(keyMsg)
			m = result.(*Model)
			m.execFollowUps(cmd)
		}
	}

	// Ensure dimensions are set for headless mode
	if m.width == 0 || m.height == 0 {
		m.width = 120
		m.height = 40
		m.updateLayout()
	}

	// Render and respond
	v := m.View()
	resp := IPCResponse{Screen: ansiStripRE.ReplaceAllString(v.Content, "")}
	if err := json.NewEncoder(msg.conn).Encode(resp); err != nil {
		if m.debugLog != nil {
			m.debugLog.Printf("[ipc] write error: %v", err)
		}
	}
	msg.conn.Close()

	return m, nil
}

// DefaultIPCSocketPath returns the default IPC socket path for a prwatch instance.
func DefaultIPCSocketPath() string {
	return fmt.Sprintf("/tmp/prwatch-%d.sock", os.Getpid())
}

// IPCSocketPathFromEnv returns the socket path from PRWATCH_IPC_SOCKET, or empty string.
func IPCSocketPathFromEnv() string {
	return os.Getenv("PRWATCH_IPC_SOCKET")
}
