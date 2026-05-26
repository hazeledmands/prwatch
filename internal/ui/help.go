package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// helpOverlay is the modal help screen state machine.
//
// State diagram (entrypoint Open → exits via dismiss/quit):
//
//	closed --Open()-->  open ────────────────────────────────────╮
//	   │              ↗     │  ╲                                 │
//	   │           "?"      │   '/'                              │
//	   │                    │    ↓                               │
//	   │              open+searching                             │
//	   │                    │                                    │
//	   │                  Enter (matches>0)                      │
//	   │                    ↓                                    │
//	   │              open+searchConfirmed (n/p navigation)      │
//	   ↑                                                         │
//	   ╰─────── q/esc/Help/any-unhandled-key ────────────────────╯
type helpOverlay struct {
	visible         bool
	scrollOffset    int
	searching       bool
	searchConfirmed bool
	searchQuery     string
	searchMatches   []int
	searchIdx       int
}

func newHelpOverlay() *helpOverlay { return &helpOverlay{} }

func (h *helpOverlay) IsOpen() bool { return h.visible }

// Open shows the help overlay. Resets scroll and clears search state.
func (h *helpOverlay) Open() {
	h.visible = true
	h.scrollOffset = 0
	h.searchQuery = ""
	h.searchMatches = nil
	h.searchIdx = 0
	h.searching = false
	h.searchConfirmed = false
}

// Close dismisses the overlay back to the no-help state.
func (h *helpOverlay) Close() {
	h.visible = false
	h.scrollOffset = 0
	h.searchQuery = ""
	h.searchMatches = nil
	h.searchIdx = 0
	h.searching = false
	h.searchConfirmed = false
}

// helpContentLines returns the help text as a flat slice of lines, one per
// row, including the leading "Keybindings:" header and the trailing
// "Press q/esc..." footer.
func helpContentLines() []string {
	sections := [][]helpEntry{
		{
			{bindings: []key.Binding{keys.ToggleMode}, desc: "Cycle mode (files → commits → pr)"},
			{bindings: []key.Binding{keys.FilesMode}, desc: "Files mode"},
			{bindings: []key.Binding{keys.CommitsMode}, desc: "Commits mode"},
			{bindings: []key.Binding{keys.PRMode}, desc: "PR mode (when PR exists)"},
		},
		{
			{bindings: []key.Binding{keys.FocusLeft}, desc: "Scroll left / switch to sidebar (when wrap off)"},
			{bindings: []key.Binding{keys.FocusRight}, desc: "Scroll right (when wrap off)"},
			{bindings: []key.Binding{keys.FocusSidebar}, desc: "Focus sidebar"},
			{bindings: []key.Binding{keys.FocusMain}, desc: "Focus main pane"},
			{bindings: []key.Binding{keys.FocusToggle}, desc: "Toggle focus (sidebar / main pane)"},
		},
		{
			{bindings: []key.Binding{keys.Down}, desc: "Move cursor down / select next (sidebar)"},
			{bindings: []key.Binding{keys.Up}, desc: "Move cursor up / select prev (sidebar)"},
			{bindings: []key.Binding{keys.CursorLeft}, desc: "Move cursor left (main pane)"},
			{bindings: []key.Binding{keys.CursorRight}, desc: "Move cursor right (main pane)"},
			{bindings: []key.Binding{keys.PageDown}, desc: "Page down"},
			{bindings: []key.Binding{keys.PageUp}, desc: "Page up"},
			{bindings: []key.Binding{keys.GoTop}, desc: "Go to top"},
			{bindings: []key.Binding{keys.GoBottom}, desc: "Go to bottom"},
		},
		{
			{bindings: []key.Binding{keys.SidebarGrow}, desc: "Grow sidebar"},
			{bindings: []key.Binding{keys.SidebarShrink}, desc: "Shrink sidebar"},
			{bindings: []key.Binding{keys.ToggleSidebar}, desc: "Toggle sidebar visibility"},
		},
		{
			{bindings: []key.Binding{keys.ToggleWrap}, desc: "Toggle word wrap"},
			{bindings: []key.Binding{keys.ToggleLineNums}, desc: "Toggle line numbers (files mode)"},
			{bindings: []key.Binding{keys.ToggleIgnored}, desc: "Toggle gitignored files (files mode)"},
			{bindings: []key.Binding{keys.ToggleRemoved}, desc: "Toggle removed lines in diff gutter (files mode)"},
		},
		{
			{bindings: []key.Binding{keys.NextDiff}, desc: "Jump to next diff hunk (files mode)"},
			{bindings: []key.Binding{keys.PrevDiff}, desc: "Jump to previous diff hunk (files mode)"},
			{bindings: []key.Binding{keys.NextLeaf}, desc: "Jump to next leaf"},
			{bindings: []key.Binding{keys.PrevLeaf}, desc: "Jump to previous leaf"},
		},
		{
			{bindings: []key.Binding{keys.VisualStream}, desc: "Visual mode (character selection, main pane)"},
			{bindings: []key.Binding{keys.VisualLine}, desc: "Visual mode (line selection, main pane)"},
			{bindings: []key.Binding{keys.VisualDismiss}, desc: "Dismiss visual selection"},
			{bindings: []key.Binding{keys.YankPath}, desc: "Yank selection (visual mode) / copy path (otherwise)"},
		},
		{
			{bindings: []key.Binding{keys.Enter}, desc: "Open file in $EDITOR / switch to main pane"},
			{bindings: []key.Binding{keys.Search}, desc: "Search (type to match, enter to confirm)"},
			{bindings: []key.Binding{keys.SearchNext}, desc: "Next search result (after search confirmed)"},
			{bindings: []key.Binding{keys.SearchPrev}, desc: "Previous search result (after search confirmed)"},
		},
		{
			{bindings: []key.Binding{keys.Refresh}, desc: "Refresh git state"},
			{bindings: []key.Binding{keys.PRBrowse}, desc: "Open the active PR in the browser"},
			{bindings: []key.Binding{keys.Help}, desc: "Show this help (scroll with j/k/mouse)"},
		},
		{
			{bindings: []key.Binding{keys.ScopeExtendBack}, desc: "Extend commit-range scope backward"},
			{bindings: []key.Binding{keys.ScopeContractForward}, desc: "Contract commit-range scope toward working tree"},
			{bindings: []key.Binding{keys.ScopeReset}, desc: "Reset commit-range scope to default"},
		},
		{
			{bindings: []key.Binding{keys.QuitConfirm}, desc: "Quit (confirm)"},
			{bindings: []key.Binding{keys.QuitImmediate}, desc: "Quit immediately"},
		},
	}

	width := 0
	for _, section := range sections {
		for _, e := range section {
			if w := len(keyList(e.bindings...)); w > width {
				width = w
			}
		}
	}

	lines := []string{"Keybindings:", ""}
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		for _, e := range section {
			lines = append(lines, fmt.Sprintf("  %-*s  %s", width, keyList(e.bindings...), e.desc))
		}
	}
	lines = append(lines, "", "Press q/esc to dismiss. Use j/k or mouse to scroll. / to search.")
	return lines
}

// updateSearchMatches refreshes the match index list against the current
// helpContentLines() and snaps scrollOffset to the first match (if any).
func (h *helpOverlay) updateSearchMatches() {
	h.searchMatches = nil
	if h.searchQuery == "" {
		return
	}
	q := strings.ToLower(h.searchQuery)
	for i, line := range helpContentLines() {
		if strings.Contains(strings.ToLower(line), q) {
			h.searchMatches = append(h.searchMatches, i)
		}
	}
	h.searchIdx = 0
	if len(h.searchMatches) > 0 {
		h.scrollOffset = h.searchMatches[0]
	}
}

// HandleKey processes a key press while help is open. Returns a tea.Cmd
// (for tea.Quit on Ctrl+C) plus a bool indicating whether the key was handled
// inside the overlay. visibleHeight is the number of help rows that fit
// on screen above the search bar.
func (h *helpOverlay) HandleKey(msg tea.KeyPressMsg, visibleHeight int) tea.Cmd {
	if h.searching {
		switch {
		case msg.Code == tea.KeyEscape:
			h.searching = false
			h.searchQuery = ""
			h.searchMatches = nil
			return nil
		case msg.Code == tea.KeyEnter:
			h.searching = false
			if len(h.searchMatches) > 0 {
				h.searchConfirmed = true
			}
			return nil
		case msg.Code == tea.KeyBackspace:
			if len(h.searchQuery) > 0 {
				h.searchQuery = h.searchQuery[:len(h.searchQuery)-1]
			}
			if h.searchQuery == "" {
				h.searching = false
				h.searchConfirmed = false
				h.searchMatches = nil
				return nil
			}
			h.updateSearchMatches()
			return nil
		default:
			if msg.Text != "" {
				h.searchQuery += msg.Text
			}
			h.updateSearchMatches()
			return nil
		}
	}

	if h.searchConfirmed {
		switch {
		case key.Matches(msg, keys.SearchNext):
			if len(h.searchMatches) > 0 {
				h.searchIdx = (h.searchIdx + 1) % len(h.searchMatches)
				h.scrollOffset = h.searchMatches[h.searchIdx]
			}
			return nil
		case key.Matches(msg, keys.SearchPrev):
			if len(h.searchMatches) > 0 {
				h.searchIdx = (h.searchIdx - 1 + len(h.searchMatches)) % len(h.searchMatches)
				h.scrollOffset = h.searchMatches[h.searchIdx]
			}
			return nil
		case msg.Code == tea.KeyEscape, key.Matches(msg, keys.QuitConfirm):
			h.searchConfirmed = false
			h.searchQuery = ""
			h.searchMatches = nil
			return nil
		default:
			h.searchConfirmed = false
			h.searchQuery = ""
			h.searchMatches = nil
			return h.HandleKey(msg, visibleHeight)
		}
	}

	helpLines := helpContentLines()

	switch {
	case key.Matches(msg, keys.QuitConfirm) || key.Matches(msg, keys.Help):
		h.Close()
		return nil
	case key.Matches(msg, keys.Search):
		h.searching = true
		h.searchQuery = ""
		h.searchMatches = nil
		return nil
	case key.Matches(msg, keys.Down):
		if h.scrollOffset < len(helpLines)-visibleHeight {
			h.scrollOffset++
		}
		return nil
	case key.Matches(msg, keys.Up):
		if h.scrollOffset > 0 {
			h.scrollOffset--
		}
		return nil
	case key.Matches(msg, keys.PageDown):
		maxOffset := max(0, len(helpLines)-visibleHeight)
		h.scrollOffset = min(h.scrollOffset+visibleHeight, maxOffset)
		return nil
	case key.Matches(msg, keys.PageUp):
		h.scrollOffset = max(0, h.scrollOffset-visibleHeight)
		return nil
	case key.Matches(msg, keys.GoBottom):
		h.scrollOffset = max(0, len(helpLines)-visibleHeight)
		return nil
	case key.Matches(msg, keys.QuitImmediate):
		return tea.Quit
	default:
		h.Close()
		return nil
	}
}

// HandleWheel scrolls the help overlay one row up or down per wheel event.
func (h *helpOverlay) HandleWheel(direction int, visibleHeight int) {
	helpLines := helpContentLines()
	if direction < 0 && h.scrollOffset > 0 {
		h.scrollOffset--
	} else if direction > 0 && h.scrollOffset < len(helpLines)-visibleHeight {
		h.scrollOffset++
	}
}

// PageUp scrolls the help overlay up by one visible page (clamped at zero).
func (h *helpOverlay) PageUp(visibleHeight int) {
	h.scrollOffset = max(0, h.scrollOffset-visibleHeight)
}

// Render builds the visible help screen (without the surrounding status bar).
// visibleHeight is the number of help rows that fit on screen.
func (h *helpOverlay) Render(visibleHeight int) string {
	lines := helpContentLines()

	if h.searchQuery != "" {
		for i, line := range lines {
			lines[i] = highlightMatchInLine(line, h.searchQuery)
		}
	}

	end := h.scrollOffset + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}
	start := h.scrollOffset
	if start > len(lines) {
		start = len(lines)
	}
	visible := lines[start:end]

	result := strings.Join(visible, "\n")

	if h.searching {
		searchBar := "/" + h.searchQuery + "_"
		if len(h.searchMatches) > 0 {
			searchBar += fmt.Sprintf("  %d/%d", h.searchIdx+1, len(h.searchMatches))
		} else if h.searchQuery != "" {
			searchBar += "  0/0"
		}
		result += "\n" + searchBar
	} else if h.searchConfirmed {
		searchBar := fmt.Sprintf("/%s  %d/%d", h.searchQuery, h.searchIdx+1, len(h.searchMatches))
		result += "\n" + searchBar
	}

	return result
}
