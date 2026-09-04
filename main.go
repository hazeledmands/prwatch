package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/command"
	"github.com/hazeledmands/prwatch/internal/git"
	"github.com/hazeledmands/prwatch/internal/ui"
	"github.com/hazeledmands/prwatch/internal/watcher"
)

func main() {
	// Parse --ipc flag
	var ipcMode bool
	args := os.Args[1:]
	for i, arg := range args {
		if arg == "--ipc" {
			ipcMode = true
			args = append(args[:i], args[i+1:]...)
			break
		}
	}

	var dir string
	var err error
	if len(args) > 0 {
		dir, err = filepath.Abs(args[0])
	} else {
		dir, err = os.Getwd()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	g := git.New(dir)
	var m *ui.Model
	if g.IsRepo() {
		m = ui.NewModel(dir, g)
	} else {
		m = ui.NewModel(dir, nil)
	}

	// Non-interactive render mode: print the TUI as text and exit.
	// Useful for automated review loops that can't drive an interactive terminal.
	// Set PRWATCH_RENDER_ONCE=1 to enable. Optionally set COLUMNS and LINES.
	if os.Getenv("PRWATCH_RENDER_ONCE") != "" {
		width, height := 120, 40
		if cols := os.Getenv("COLUMNS"); cols != "" {
			if n, err := strconv.Atoi(cols); err == nil {
				width = n
			}
		}
		if lines := os.Getenv("LINES"); lines != "" {
			if n, err := strconv.Atoi(lines); err == nil {
				height = n
			}
		}
		if keys := os.Getenv("PRWATCH_KEYS"); keys != "" {
			fmt.Print(m.RenderWithKeys(width, height, keys))
		} else {
			fmt.Print(m.RenderOnce(width, height))
		}
		return
	}

	// When IPC mode is requested, run headless (no TTY needed).
	var opts []tea.ProgramOption
	socketPath := ui.IPCSocketPathFromEnv()
	if socketPath == "" && ipcMode {
		socketPath, err = ui.DefaultIPCSocketPath(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	if socketPath != "" {
		// Headless mode: disable terminal I/O so the program runs without a TTY.
		opts = append(opts, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	}
	p := tea.NewProgram(m, opts...)

	// Start IPC listener if configured
	if socketPath != "" {
		cleanup, err := ui.StartIPCListener(socketPath, func(msg tea.Msg) {
			p.Send(msg)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: IPC listener failed: %v\n", err)
		} else {
			defer cleanup()
			fmt.Fprintf(os.Stderr, "IPC socket: %s\n", socketPath)
		}
	}

	// Start the file watcher. One watcher covers the working tree, the git
	// locations that hold HEAD and the branch refs, and — for a repo small
	// enough to afford it — the subdirectories holding tracked files.
	// watcher.RepoDirs works out which paths those are, including for a linked
	// worktree, where `.git` is a pointer file and the state lives elsewhere.
	w, err := watcher.WatchRepo(dir, command.DefaultFactory, func() {
		p.Send(ui.RefreshMsg{})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: file watcher failed: %v\n", err)
	} else {
		defer w.Close()
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
