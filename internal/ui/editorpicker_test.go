package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/editor"
	"pgregory.net/rapid"
)

// pickerEntries is a fixture list: three terminal editors and two GUI ones,
// with `code` marked the way editor.Available marks `$EDITOR`.
func pickerEntries() []editor.Entry {
	return []editor.Entry{
		{Name: "code", Preset: editor.Preset{Line: editor.LineGoto}, Default: true},
		{Name: "hx", Preset: editor.Preset{Line: editor.LineColon, Terminal: true}},
		{Name: "nvim", Preset: editor.Preset{Line: editor.LinePlus, Terminal: true}},
		{Name: "vim", Preset: editor.Preset{Line: editor.LinePlus, Terminal: true}},
		{Name: "zed", Preset: editor.Preset{Line: editor.LineColon}},
	}
}

func openPicker(t *testing.T) *editorPicker {
	t.Helper()
	p := newEditorPicker()
	p.Open(pickerEntries(), editorTarget{file: "main.go", line: 42}, pickerHeight)
	if !p.IsOpen() {
		t.Fatal("picker did not open")
	}
	return p
}

// recorder captures what a launch was asked to do, and whether the picker had
// already closed by the time it was asked.
type recorder struct {
	calls    int
	entry    editor.Entry
	target   editorTarget
	openAtGo bool
}

func (r *recorder) hooks(p *editorPicker) editorPickerHooks {
	return editorPickerHooks{
		Launch: func(e editor.Entry, target editorTarget) tea.Cmd {
			r.calls++
			r.entry = e
			r.target = target
			r.openAtGo = p.IsOpen()
			return nil
		},
	}
}

func keyText(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Text: s, Code: rune(s[0])}
}

const pickerHeight = 20 // comfortably taller than the fixture

// The load-bearing ordering: a terminal editor launches through tea.Exec,
// which suspends the TUI and redraws on return. A picker still marked open
// when the launch is dispatched would repaint the list over the file the user
// just came back from.
func TestEditorPicker_ClosesBeforeLaunching(t *testing.T) {
	p := openPicker(t)
	var r recorder

	p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter}, pickerHeight, r.hooks(p))

	if r.calls != 1 {
		t.Fatalf("Launch called %d times, want 1", r.calls)
	}
	if r.openAtGo {
		t.Error("picker was still open when Launch ran; tea.Exec would redraw the list over the editor's exit")
	}
	if p.IsOpen() {
		t.Error("picker still open after a launch")
	}
}

func TestEditorPicker_EnterLaunchesTheHighlightedEntry(t *testing.T) {
	p := openPicker(t)
	var r recorder
	hooks := r.hooks(p)

	p.HandleKey(keyText("j"), pickerHeight, hooks) // code -> hx
	p.HandleKey(keyText("j"), pickerHeight, hooks) // hx -> nvim
	p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter}, pickerHeight, hooks)

	if r.entry.Name != "nvim" {
		t.Errorf("launched %q, want nvim", r.entry.Name)
	}
	if r.target != (editorTarget{file: "main.go", line: 42}) {
		t.Errorf("target = %+v, want main.go:42", r.target)
	}
}

func TestEditorPicker_DigitsAddressRowsDirectly(t *testing.T) {
	for _, tc := range []struct{ digit, want string }{
		{"1", "code"},
		{"3", "nvim"},
		{"5", "zed"},
	} {
		p := openPicker(t)
		var r recorder
		p.HandleKey(keyText(tc.digit), pickerHeight, r.hooks(p))
		if r.entry.Name != tc.want {
			t.Errorf("%q launched %q, want %q", tc.digit, r.entry.Name, tc.want)
		}
	}
}

// A digit past the end is not a row. It must not launch anything, and must not
// dismiss the list either — the user mistyped, they did not cancel.
func TestEditorPicker_DigitPastTheEndIsInert(t *testing.T) {
	p := openPicker(t)
	var r recorder

	p.HandleKey(keyText("9"), pickerHeight, r.hooks(p))

	if r.calls != 0 {
		t.Errorf("Launch ran %d times for a digit past the end", r.calls)
	}
	if !p.IsOpen() {
		t.Error("an out-of-range digit dismissed the picker")
	}
}

// Unlike the help overlay, an unrecognized key is ignored rather than closing.
func TestEditorPicker_UnrecognizedKeysAreIgnored(t *testing.T) {
	p := openPicker(t)
	var r recorder
	hooks := r.hooks(p)

	for _, msg := range []tea.KeyPressMsg{
		keyText("x"),
		keyText("?"),
		keyText("/"),
		keyText("m"),
		{Code: tea.KeyTab},
	} {
		p.HandleKey(msg, pickerHeight, hooks)
		if !p.IsOpen() {
			t.Fatalf("key %v dismissed the picker", msg)
		}
	}
	if r.calls != 0 {
		t.Errorf("Launch ran %d times for keys that are not choices", r.calls)
	}
}

func TestEditorPicker_QuitClosesWithoutLaunching(t *testing.T) {
	for _, msg := range []tea.KeyPressMsg{keyText("q"), {Code: tea.KeyEscape}} {
		p := openPicker(t)
		var r recorder
		p.HandleKey(msg, pickerHeight, r.hooks(p))
		if p.IsOpen() {
			t.Errorf("%v did not close the picker", msg)
		}
		if r.calls != 0 {
			t.Errorf("%v launched an editor", msg)
		}
	}
}

func TestEditorPicker_HighlightStartsOnTheDefault(t *testing.T) {
	p := openPicker(t)
	if got := p.entries[p.selected].Name; got != "code" {
		t.Errorf("initial highlight on %q, want the default (code)", got)
	}
}

// PROMPT.md: the choice sticks "for the rest of the session".
func TestEditorPicker_RemembersTheLastChoiceAcrossOpens(t *testing.T) {
	p := openPicker(t)
	var r recorder
	p.HandleKey(keyText("4"), pickerHeight, r.hooks(p)) // vim

	p.Open(pickerEntries(), editorTarget{file: "other.go"}, pickerHeight)
	if got := p.entries[p.selected].Name; got != "vim" {
		t.Errorf("second open highlights %q, want the remembered vim", got)
	}
}

// A remembered editor that is no longer offered must not strand the highlight.
func TestEditorPicker_FallsBackWhenTheRememberedEditorIsGone(t *testing.T) {
	p := openPicker(t)
	var r recorder
	p.HandleKey(keyText("5"), pickerHeight, r.hooks(p)) // zed

	shortened := pickerEntries()[:2] // code, hx
	p.Open(shortened, editorTarget{file: "other.go"}, pickerHeight)
	if got := p.entries[p.selected].Name; got != "code" {
		t.Errorf("highlight on %q, want the default when the remembered editor is absent", got)
	}
}

func TestEditorPicker_OpenWithNoEntriesIsRefused(t *testing.T) {
	p := newEditorPicker()
	p.Open(nil, editorTarget{file: "main.go"}, pickerHeight)
	if p.IsOpen() {
		t.Error("picker opened on an empty list")
	}
}

func TestEditorPicker_RenderShowsTheTargetAndEveryEntry(t *testing.T) {
	p := openPicker(t)
	out := p.Render(pickerHeight)

	if !strings.Contains(out, "main.go:42") {
		t.Errorf("render does not name the target:\n%s", out)
	}
	for _, e := range pickerEntries() {
		if !strings.Contains(out, e.Name) {
			t.Errorf("render omits %q:\n%s", e.Name, out)
		}
	}
	if !strings.Contains(out, "(default)") {
		t.Errorf("render does not mark the default:\n%s", out)
	}
	if !strings.Contains(out, "terminal") || !strings.Contains(out, "GUI") {
		t.Errorf("render does not distinguish terminal from GUI:\n%s", out)
	}
}

// A sidebar-focused launch carries no line, and the title should not invent
// one.
func TestEditorPicker_RenderOmitsTheLineWhenThereIsNone(t *testing.T) {
	p := newEditorPicker()
	p.Open(pickerEntries(), editorTarget{file: "main.go"}, pickerHeight)

	out := p.Render(pickerHeight)
	if strings.Contains(out, "main.go:") {
		t.Errorf("render invented a line number:\n%s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("render does not name the file:\n%s", out)
	}
}

// The highlight starts on the remembered or default editor, which can sit
// past the first window on a long list. Open seeds the offset from it, so the
// highlight is on screen before any key is pressed.
func TestEditorPicker_HighlightIsVisibleImmediatelyAfterOpen(t *testing.T) {
	entries := make([]editor.Entry, 12)
	for i := range entries {
		entries[i] = editor.Entry{Name: string(rune('a' + i))}
	}
	entries[11].Default = true // the default is the last row

	p := newEditorPicker()
	const height = 9 // 5 entry rows once the chrome is taken out
	p.Open(entries, editorTarget{file: "f.go"}, height)

	rows := editorPickerRows(height)
	if p.selected < p.offset || p.selected >= p.offset+rows {
		t.Fatalf("selected=%d outside the initial window [%d,%d)", p.selected, p.offset, p.offset+rows)
	}
	if !strings.Contains(p.Render(height), entries[11].Name) {
		t.Errorf("the highlighted entry is not on screen:\n%s", p.Render(height))
	}
}

// pickerKeyGen draws the keys a user can press at the picker.
func pickerKeyGen() *rapid.Generator[tea.KeyPressMsg] {
	texts := []string{"j", "k", "g", "G", "1", "2", "3", "7", "9", "x", "?", " "}
	codes := []tea.KeyPressMsg{
		{Code: tea.KeyDown}, {Code: tea.KeyUp},
		{Code: tea.KeyPgDown}, {Code: tea.KeyPgUp},
		{Code: tea.KeyHome}, {Code: tea.KeyEnd},
	}
	return rapid.Custom(func(t *rapid.T) tea.KeyPressMsg {
		if rapid.Bool().Draw(t, "text") {
			return keyText(rapid.SampledFrom(texts).Draw(t, "key"))
		}
		return rapid.SampledFrom(codes).Draw(t, "code")
	})
}

// While the picker is open, the highlight always names a real entry and the
// scroll offset always keeps it on screen. Both are what Render indexes with,
// so a violation is an out-of-range panic or a highlight nobody can see.
func TestProperty_EditorPickerIndicesStayValid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 12).Draw(t, "entries")
		entries := make([]editor.Entry, n)
		for i := range entries {
			entries[i] = editor.Entry{Name: string(rune('a' + i)), Default: i == 0}
		}
		height := rapid.IntRange(0, 14).Draw(t, "height")

		p := newEditorPicker()
		// The highlight starts wherever the default lands, so seed the
		// remembered choice too — that is the case that can open far down a
		// long list.
		p.last = rapid.SampledFrom(append(entryNamesOf(entries), "")).Draw(t, "remembered")
		p.Open(entries, editorTarget{file: "f.go"}, height)

		assertPickerWindow(t, p, height)

		var noLaunch editorPickerHooks
		for range rapid.IntRange(1, 40).Draw(t, "presses") {
			if !p.IsOpen() {
				break
			}
			p.HandleKey(pickerKeyGen().Draw(t, "key"), height, noLaunch)
			if !p.IsOpen() {
				break
			}

			assertPickerWindow(t, p, height)
			// Render must not panic or drop the highlight off the end.
			_ = p.Render(height)
		}
	})
}

func entryNamesOf(entries []editor.Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

// assertPickerWindow checks the two indices Render dereferences: the highlight
// names a real entry, and the offset keeps it on screen.
func assertPickerWindow(t *rapid.T, p *editorPicker, height int) {
	t.Helper()
	if p.selected < 0 || p.selected >= len(p.entries) {
		t.Fatalf("selected=%d outside [0,%d)", p.selected, len(p.entries))
	}
	rows := editorPickerRows(height)
	if rows <= 0 {
		return
	}
	if ceiling := max(0, len(p.entries)-rows); p.offset < 0 || p.offset > ceiling {
		t.Fatalf("offset=%d outside [0,%d] (rows=%d)", p.offset, ceiling, rows)
	}
	if p.selected < p.offset || p.selected >= p.offset+rows {
		t.Fatalf("selected=%d outside the visible window [%d,%d)",
			p.selected, p.offset, p.offset+rows)
	}
}

// Every open picker has a dismiss path that does not launch anything.
func TestProperty_EditorPickerAlwaysDismissable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 12).Draw(t, "entries")
		entries := make([]editor.Entry, n)
		for i := range entries {
			entries[i] = editor.Entry{Name: string(rune('a' + i))}
		}
		height := rapid.IntRange(0, 14).Draw(t, "height")

		p := newEditorPicker()
		p.Open(entries, editorTarget{file: "f.go"}, height)

		var r recorder
		hooks := r.hooks(p)
		for range rapid.IntRange(0, 20).Draw(t, "presses") {
			if !p.IsOpen() {
				return // already left via a launch; nothing to dismiss
			}
			p.HandleKey(pickerKeyGen().Draw(t, "key"), height, hooks)
		}
		if !p.IsOpen() {
			return
		}

		before := r.calls
		p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape}, height, hooks)
		if p.IsOpen() {
			t.Fatal("escape did not dismiss the picker")
		}
		if r.calls != before {
			t.Fatal("escape launched an editor")
		}
	})
}
