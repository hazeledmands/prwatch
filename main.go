package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
	"github.com/hazeledmands/prwatch/internal/ui"
	"github.com/hazeledmands/prwatch/internal/watcher"
)

func main() {
	dir, err := os.Getwd()
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

	p := tea.NewProgram(m)

	// Start file watcher
	w, err := watcher.New(dir, func() {
		p.Send(ui.RefreshMsg{})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: file watcher failed: %v\n", err)
	} else {
		defer w.Close()
		// Also watch .git dir for ref changes
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			wGit, err := watcher.New(gitDir, func() {
				p.Send(ui.RefreshMsg{})
			})
			if err == nil {
				defer wGit.Close()
			}
		}
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
