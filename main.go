package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"github.com/hazeledmands/prwatch/internal/ui"
	"github.com/hazeledmands/prwatch/internal/watcher"
)

func main() {
	var dir string
	var err error
	if len(os.Args) > 1 {
		dir, err = filepath.Abs(os.Args[1])
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
	if socketPath != "" {
		opts = append(opts, tea.WithInput(nil), tea.WithOutput(io.Discard))
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

	// Start file watcher
	w, err := watcher.New(dir, func() {
		p.Send(ui.RefreshMsg{})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: file watcher failed: %v\n", err)
	} else {
		defer w.Close()
		// Also watch .git dir and key subdirs for ref changes (new commits)
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			refreshFn := func() { p.Send(ui.RefreshMsg{}) }
			// Watch .git itself for HEAD changes
			if wGit, err := watcher.New(gitDir, refreshFn); err == nil {
				defer wGit.Close()
			}
			// Watch .git/refs/heads for new branch commits
			refsDir := filepath.Join(gitDir, "refs", "heads")
			if info, err := os.Stat(refsDir); err == nil && info.IsDir() {
				if wRefs, err := watcher.New(refsDir, refreshFn); err == nil {
					defer wRefs.Close()
				}
			}
		}
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
