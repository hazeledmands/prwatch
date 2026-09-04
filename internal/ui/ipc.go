package ui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// ipcTimeout is a deadline read from every connection goroutine and adjusted
// by tests. Those are different goroutines, so it is stored atomically rather
// than as a plain var — a connection accepted before an adjustment races the
// adjustment otherwise.
type ipcTimeout struct{ ns atomic.Int64 }

func newIPCTimeout(d time.Duration) *ipcTimeout {
	t := &ipcTimeout{}
	t.set(d)
	return t
}

func (t *ipcTimeout) get() time.Duration  { return time.Duration(t.ns.Load()) }
func (t *ipcTimeout) set(d time.Duration) { t.ns.Store(int64(d)) }

// ipcReadTimeout bounds how long a connected client may take to send its
// request, and ipcWriteTimeout how long the reply may take to drain. Both
// exist so a client that connects and then stalls — deliberately or by
// crashing mid-request — cannot pin a goroutine and a connection forever.
// Ten seconds is far longer than a local request needs and far shorter than
// "never".
var (
	ipcReadTimeout  = newIPCTimeout(10 * time.Second)
	ipcWriteTimeout = newIPCTimeout(10 * time.Second)
)

// ipcDialProbeTimeout bounds the "is anyone already listening here?" connect
// at startup. It only ever talks to a local socket, where an answer is
// immediate and a refusal is immediate too.
const ipcDialProbeTimeout = 2 * time.Second

// maxUnixSocketPath is the shortest sun_path any platform we run on offers:
// macOS/BSD give 104 bytes including the terminating NUL, Linux 108. Staying
// under the smaller one keeps behaviour identical everywhere.
const maxUnixSocketPath = 103

// repoSocketKey reduces a directory to the repo it belongs to, so every
// invocation from anywhere inside a checkout agrees on one socket name.
//
// It walks up looking for a `.git` entry rather than shelling out to
// `rev-parse --show-toplevel`: prwatch-ctl calls this on every run and should
// not pay for a subprocess, and the answer is wanted even where git is not
// installed. A linked worktree's `.git` is a file rather than a directory and
// stops the walk just the same — which is correct, since prwatch runs per
// worktree and each one wants its own socket. A directory in no repo at all
// keys on itself.
func repoSocketKey(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	// Resolve symlinks so two spellings of one repo agree.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	for cur := abs; ; {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs // no repo root above us; key on the directory itself
		}
		cur = parent
	}
}

// DefaultIPCSocketPath returns the per-user, per-repo socket path used when no
// explicit one is given, creating its parent directory with mode 0700.
//
// It is deliberately not in /tmp. A fixed /tmp/prwatch.sock is a shared name
// in a world-writable directory: on a multi-user box any other user could
// connect to it and drive the TUI — the IPC protocol sends keystrokes and
// returns screen contents — or squat the name before prwatch got there. Under
// XDG_RUNTIME_DIR (already per-user and 0700 by spec) or the user cache dir,
// the containing directory's mode is what keeps other users out, since
// permissions on the socket file itself are not honoured on every platform
// (see StartIPCListener).
//
// The name carries a hash of the repo root, so a second prwatch on another
// repo or worktree gets its own socket instead of unlinking the first one's
// live socket at startup and having its own removed by the first one's
// cleanup. Multi-repo and worktree workflows are the main use case here, so
// that collision would be the common path rather than an edge case. The hash
// rather than the path itself keeps the name short — see maxUnixSocketPath —
// and keeps the repo's location out of a directory others may be able to list.
func DefaultIPCSocketPath(dir string) (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("ipc socket dir: %w", err)
		}
	}
	socketDir := filepath.Join(base, "prwatch")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return "", fmt.Errorf("ipc socket dir: %w", err)
	}
	// MkdirAll leaves an existing directory's mode alone, so tighten it: a
	// directory left over from an earlier, looser version would otherwise
	// keep its permissions and the guarantee above would not hold.
	if err := os.Chmod(socketDir, 0o700); err != nil {
		return "", fmt.Errorf("ipc socket dir: %w", err)
	}

	sum := sha256.Sum256([]byte(repoSocketKey(dir)))
	path := filepath.Join(socketDir, fmt.Sprintf("prwatch-%x.sock", sum[:6]))
	if len(path) > maxUnixSocketPath {
		// bind() would fail with a bare "invalid argument", which says
		// nothing about the actual problem or the way out of it.
		return "", fmt.Errorf(
			"ipc socket path %q is %d bytes, over the %d-byte limit for a unix socket; "+
				"set PRWATCH_IPC_SOCKET to a shorter path",
			path, len(path), maxUnixSocketPath)
	}
	return path, nil
}

// StartIPCListener starts a Unix domain socket listener that accepts
// IPC commands and sends them as tea messages.
//
// An existing socket at the path is unlinked only after a connect proves
// nothing is listening. Unconditionally removing it — the previous behaviour
// — silently stole the socket of a live instance, and the thief's own cleanup
// then removed whatever had replaced it. A refusal is the right answer there,
// while a socket left by a crashed instance is reclaimed as before.
func StartIPCListener(socketPath string, send func(tea.Msg)) (cleanup func(), err error) {
	if _, statErr := os.Stat(socketPath); statErr == nil {
		if probe, dialErr := net.DialTimeout("unix", socketPath, ipcDialProbeTimeout); dialErr == nil {
			probe.Close()
			return nil, fmt.Errorf(
				"another prwatch is already listening on %s; quit it, or set PRWATCH_IPC_SOCKET to a different path",
				socketPath)
		}
		// Nothing answered: the file is a leftover, so it can go. (A dial to
		// an unlinked-but-present socket fails with ECONNREFUSED.)
		os.Remove(socketPath)
	}

	// Bind under a tight umask. Between Listen and the Chmod below the socket
	// would otherwise exist at 0777&^umask — connectable for that window,
	// which is exactly the shared-directory case the chmod is there for.
	ln, err := listenUnixPrivate(socketPath)
	if err != nil {
		return nil, fmt.Errorf("ipc listen: %w", err)
	}
	// Defence in depth only: on macOS and the BSDs the permissions on a
	// socket *file* are not consulted when connecting, so the real protection
	// is the 0700 parent directory DefaultIPCSocketPath creates. This still
	// matters on Linux, which does honour them, and costs nothing elsewhere.
	// A PRWATCH_IPC_SOCKET pointing into a shared directory is therefore
	// protected on Linux and unprotected on macOS — the override is the
	// caller's choice, and the default path is the safe one.
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
	if err := conn.SetWriteDeadline(time.Now().Add(ipcWriteTimeout.get())); err != nil {
		return err
	}
	return json.NewEncoder(conn).Encode(resp)
}

func handleIPCConn(conn net.Conn, send func(tea.Msg)) {
	// The request should already be in flight; a client that connects and
	// then says nothing gets dropped rather than holding the connection.
	if err := conn.SetReadDeadline(time.Now().Add(ipcReadTimeout.get())); err != nil {
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

	// Render and respond. The render is guarded because the follow-ups above
	// may have left the model corrupt, and a panic here would take the report
	// of that corruption down with it.
	content, renderErr := m.renderReport()
	if renderErr != nil {
		followUpErrs = append(followUpErrs, renderErr.Error())
	}
	resp := IPCResponse{Screen: ansiStripRE.ReplaceAllString(content, "")}
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
