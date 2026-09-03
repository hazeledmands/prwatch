package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// ipcReadTimeout bounds how long a connected client may take to send its
// request, and ipcWriteTimeout how long the reply may take to drain. Both
// exist so a client that connects and then stalls — deliberately or by
// crashing mid-request — cannot pin a goroutine and a connection forever.
// Ten seconds is far longer than a local request needs and far shorter than
// "never". They are vars so tests can shorten them.
var (
	ipcReadTimeout  = 10 * time.Second
	ipcWriteTimeout = 10 * time.Second
)

// DefaultIPCSocketPath returns the per-user socket path used when no explicit
// one is given, creating its parent directory with mode 0700.
//
// It is deliberately not in /tmp. A fixed /tmp/prwatch.sock is a shared name
// in a world-writable directory: on a multi-user box any other user could
// connect to it and drive the TUI — the IPC protocol sends keystrokes and
// returns screen contents — or squat the name before prwatch got there. Under
// XDG_RUNTIME_DIR (already per-user and 0700 by spec) or the user cache dir,
// the containing directory's mode is what keeps other users out, since
// permissions on the socket file itself are not honoured on every platform.
func DefaultIPCSocketPath() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("ipc socket dir: %w", err)
		}
	}
	dir := filepath.Join(base, "prwatch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ipc socket dir: %w", err)
	}
	// MkdirAll leaves an existing directory's mode alone, so tighten it: a
	// directory left over from an earlier, looser version would otherwise
	// keep its permissions and the guarantee above would not hold.
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("ipc socket dir: %w", err)
	}
	return filepath.Join(dir, "prwatch.sock"), nil
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
	// Belt and braces over the 0700 directory: the caller may have supplied a
	// path in a shared directory via PRWATCH_IPC_SOCKET.
	if err := os.Chmod(socketPath, 0o600); err != nil {
		ln.Close()
		os.Remove(socketPath)
		return nil, fmt.Errorf("ipc socket perms: %w", err)
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

// writeIPCResponse encodes resp to conn under a write deadline, so a client
// that has stopped reading cannot block the writer forever.
func writeIPCResponse(conn net.Conn, resp IPCResponse) error {
	if err := conn.SetWriteDeadline(time.Now().Add(ipcWriteTimeout)); err != nil {
		return err
	}
	return json.NewEncoder(conn).Encode(resp)
}

func handleIPCConn(conn net.Conn, send func(tea.Msg)) {
	// The request should already be in flight; a client that connects and
	// then says nothing gets dropped rather than holding the connection.
	if err := conn.SetReadDeadline(time.Now().Add(ipcReadTimeout)); err != nil {
		conn.Close()
		return
	}
	dec := json.NewDecoder(conn)
	var req IPCRequest
	if err := dec.Decode(&req); err != nil {
		resp := IPCResponse{Error: fmt.Sprintf("invalid request: %v", err)}
		writeIPCResponse(conn, resp)
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
//
// The reply is encoded off the Update goroutine (see replyIPC): the model
// state it reports is a finished string by then, and writing it here meant a
// client that stopped reading wedged the entire TUI behind a socket write
// that never completed.
func (m *Model) handleIPC(msg ipcMsg) (tea.Model, tea.Cmd) {
	req := msg.req

	if req.Action == "quit" {
		// The one synchronous reply: tea.Quit may tear the process down
		// before a background writer got its turn. The write deadline is what
		// bounds this one.
		writeIPCResponse(msg.conn, IPCResponse{Screen: "quitting"})
		msg.conn.Close()
		close(msg.done)
		return m, tea.Quit
	}

	// Apply key sequence
	var followUpErrs []string
	if req.Keys != "" {
		for _, k := range strings.Split(req.Keys, ",") {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			keyMsg := parseKeyName(k)
			result, cmd := m.Update(keyMsg)
			m = result.(*Model)
			if err := m.execFollowUps(cmd); err != nil {
				followUpErrs = append(followUpErrs, fmt.Sprintf("after key %q: %s", k, err))
			}
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
	// A follow-up command that panicked leaves the screen half-updated. The
	// client asked for these keystrokes, so it is the right place to hear
	// that they did not fully land.
	if len(followUpErrs) > 0 {
		resp.Error = strings.Join(followUpErrs, "\n")
		if m.debugLog != nil {
			m.debugLog.Printf("[ipc] follow-up failures: %s", resp.Error)
		}
	}
	replyIPC(msg.conn, msg.done, resp, m.debugLog)

	return m, nil
}

// replyIPC writes one response and closes the connection, off the Update
// goroutine. It captures only finished values — the connection, the channel,
// the already-rendered response — and never touches the Model, per CLAUDE.md
// ("tea.Cmd closures must not read Model state"); the same reasoning applies
// to any goroutine Update spawns. The logger is safe to share.
func replyIPC(conn net.Conn, done chan struct{}, resp IPCResponse, debugLog *log.Logger) {
	go func() {
		defer close(done)
		defer conn.Close()
		if err := writeIPCResponse(conn, resp); err != nil && debugLog != nil {
			debugLog.Printf("[ipc] write error: %v", err)
		}
	}()
}

// IPCSocketPathFromEnv returns the socket path from PRWATCH_IPC_SOCKET, or empty string.
func IPCSocketPathFromEnv() string {
	return os.Getenv("PRWATCH_IPC_SOCKET")
}
