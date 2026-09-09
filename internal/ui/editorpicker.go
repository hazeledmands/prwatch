package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/editor"
)

// editorPicker is the modal editor-selection list: PROMPT.md's "choosing an
// editor".
//
// It owns only the choice. The file and line are snapshotted at Open and
// carried through, so the launch cannot pick up a target that changed while
// the list was up; where that target comes from — the sidebar's selection or
// the pane's displayed file, depending on focus — is Model's business.
//
// State diagram:
//
//	closed --Open(entries, target, h)--> open
//	   ↑                               │  ╲
//	   │                        q/esc  │   enter, 1-9
//	   ╰───────────────────────────────┴──────╯
//	                                   (choose closes, then launches)
type editorPicker struct {
	visible  bool
	entries  []editor.Entry
	selected int
	offset   int
	target   editorTarget
	// last is the editor chosen most recently, pre-selected on the next
	// open. It deliberately survives Close: PROMPT.md says the choice sticks
	// "for the rest of the session".
	last string
}

// editorTarget is what a launch will open: the file, and the line to open it
// at. A line of zero means none is known, which is what a sidebar-focused
// launch carries — Resolve drops the line argument entirely rather than
// passing `+0`.
type editorTarget struct {
	file string
	line int
}

// editorPickerHooks is what the picker needs from its caller. Passed per call
// rather than stored, for the reason searchInputHooks gives: a stored hook is
// a reference to Model state that a later refresh can invalidate.
type editorPickerHooks struct {
	// Launch runs the chosen editor against the target.
	//
	// It is called after the picker has already closed itself. That ordering
	// is load-bearing: a terminal editor launches through tea.Exec, which
	// suspends the TUI and redraws on return, so a picker still marked open
	// would repaint the list over the file the user just came back from.
	Launch func(e editor.Entry, target editorTarget) tea.Cmd
}

func newEditorPicker() *editorPicker { return &editorPicker{} }

func (p *editorPicker) IsOpen() bool { return p.visible }

// Open shows the list. entries is the already-filtered set from
// editor.Available; an empty one is refused, since a list with nothing in it
// offers no choice.
//
// visibleHeight is needed here, not just at render time: the highlight starts
// on the remembered or default editor, which can be far enough down a long
// list to sit outside the first window. Seeding the offset from it is what
// keeps the highlight on screen before the first key press.
func (p *editorPicker) Open(entries []editor.Entry, target editorTarget, visibleHeight int) {
	if len(entries) == 0 {
		return
	}
	p.visible = true
	p.entries = entries
	p.target = target
	p.selected = initialEditorIndex(entries, p.last)
	p.offset = ensureVisible(0, p.selected, len(entries), editorPickerRows(visibleHeight))
}

// Close dismisses the list, keeping the session's last choice.
func (p *editorPicker) Close() {
	p.visible = false
	p.entries = nil
	p.selected = 0
	p.offset = 0
}

// initialEditorIndex is where the highlight starts: the last editor chosen
// this session, or the `$EDITOR` default, or the first row.
func initialEditorIndex(entries []editor.Entry, last string) int {
	if last != "" {
		for i, e := range entries {
			if e.Name == last {
				return i
			}
		}
	}
	for i, e := range entries {
		if e.Default {
			return i
		}
	}
	return 0
}

// editorPickerChrome is the number of rows Render spends on the header and
// footer, leaving the rest for entries. One function so the key handlers, the
// wheel and Render cannot disagree about how many rows are on screen.
const editorPickerChrome = 4

func editorPickerRows(visibleHeight int) int {
	return max(0, visibleHeight-editorPickerChrome)
}

// HandleKey processes a key press while the list is open.
//
// Unlike the help overlay, a key it does not recognize is ignored rather than
// dismissing the list.
func (p *editorPicker) HandleKey(msg tea.KeyPressMsg, visibleHeight int, h editorPickerHooks) tea.Cmd {
	rows := editorPickerRows(visibleHeight)

	switch {
	case key.Matches(msg, keys.QuitImmediate):
		return tea.Quit
	case key.Matches(msg, keys.QuitConfirm):
		p.Close()
		return nil
	case key.Matches(msg, keys.Enter):
		return p.choose(p.selected, h)
	case key.Matches(msg, keys.Down):
		p.moveTo(p.selected+1, rows)
		return nil
	case key.Matches(msg, keys.Up):
		p.moveTo(p.selected-1, rows)
		return nil
	case key.Matches(msg, keys.PageDown):
		p.moveTo(p.selected+max(1, rows), rows)
		return nil
	case key.Matches(msg, keys.PageUp):
		p.moveTo(p.selected-max(1, rows), rows)
		return nil
	case key.Matches(msg, keys.GoTop):
		p.moveTo(0, rows)
		return nil
	case key.Matches(msg, keys.GoBottom):
		p.moveTo(len(p.entries)-1, rows)
		return nil
	}

	// Digits address the first nine rows directly. Checked after the bindings
	// above so a binding that ever takes a digit still wins, and before the
	// ignore-everything default so the mode-switch keys `1`/`2`/`3` cannot
	// leak through a modal list.
	if idx, ok := editorPickerDigit(msg); ok {
		return p.choose(idx, h)
	}
	return nil
}

// editorPickerDigit maps `1`-`9` to a row index. `0` is not a row: nine rows
// are addressable, and treating `0` as the tenth would make the digit-to-row
// mapping off by one for every row after it.
func editorPickerDigit(msg tea.KeyPressMsg) (int, bool) {
	if len(msg.Text) != 1 {
		return 0, false
	}
	c := msg.Text[0]
	if c < '1' || c > '9' {
		return 0, false
	}
	return int(c - '1'), true
}

// moveTo clamps the highlight to a real row and scrolls the minimum distance
// that keeps it on screen.
func (p *editorPicker) moveTo(idx, rows int) {
	if len(p.entries) == 0 {
		return
	}
	p.selected = min(max(idx, 0), len(p.entries)-1)
	p.offset = ensureVisible(p.offset, p.selected, len(p.entries), rows)
}

// choose closes the list and launches the editor at idx. Out-of-range indices
// — a digit past the end of a short list — do nothing and leave the list up.
func (p *editorPicker) choose(idx int, h editorPickerHooks) tea.Cmd {
	if idx < 0 || idx >= len(p.entries) {
		return nil
	}
	entry := p.entries[idx]
	target := p.target

	p.last = entry.Name
	p.Close()

	if h.Launch == nil {
		return nil
	}
	return h.Launch(entry, target)
}

// HandleWheel scrolls the list one row per wheel event without moving the
// highlight, the same way the sidebar's wheel behaves.
func (p *editorPicker) HandleWheel(direction, visibleHeight int) {
	if direction == 0 {
		return
	}
	delta := 1
	if direction < 0 {
		delta = -1
	}
	p.offset = clampOffset(p.offset+delta, len(p.entries), editorPickerRows(visibleHeight))
}

// PageUp scrolls the list up by one visible page.
func (p *editorPicker) PageUp(visibleHeight int) {
	rows := editorPickerRows(visibleHeight)
	p.offset = clampOffset(p.offset-rows, len(p.entries), rows)
}

// Render builds the visible list (without the surrounding status bar).
func (p *editorPicker) Render(visibleHeight int) string {
	rows := editorPickerRows(visibleHeight)

	lines := []string{editorPickerTitle(p.target), ""}

	start := clampOffset(p.offset, len(p.entries), rows)
	end := min(start+rows, len(p.entries))
	for i := start; i < end; i++ {
		lines = append(lines, editorPickerRow(p.entries[i], i, i == p.selected))
	}

	lines = append(lines, "", "Press 1-9 or enter to launch, q/esc to cancel.")
	return strings.Join(lines, "\n")
}

func editorPickerTitle(t editorTarget) string {
	if t.line > 0 {
		return fmt.Sprintf("Open %s:%d with:", t.file, t.line)
	}
	return fmt.Sprintf("Open %s with:", t.file)
}

// editorPickerRow renders one entry. The number column is blank past the ninth
// row, because those rows have no digit that reaches them.
func editorPickerRow(e editor.Entry, idx int, selected bool) string {
	num := "  "
	if idx < 9 {
		num = fmt.Sprintf("%d.", idx+1)
	}

	kind := "GUI"
	if e.Preset.Terminal {
		kind = "terminal"
	}

	suffix := ""
	if e.Default {
		suffix = "  (default)"
	}

	row := fmt.Sprintf("%s %-12s %-8s%s", num, e.Name, kind, suffix)
	if selected {
		return editorPickerSelectedStyle.Render("> " + row)
	}
	return "  " + row
}
