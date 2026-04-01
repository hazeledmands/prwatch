package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pressKey(m tea.Model, k string) tea.Model {
	// Construct a KeyPressMsg that key.Matches will recognize
	msg := tea.KeyPressMsg{Text: k, Code: []rune(k)[0]}
	updated, _ := m.Update(msg)
	return updated
}

func TestModeSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	if m.mode != FileMode {
		t.Error("initial mode should be FileMode")
	}

	// Press space to toggle
	result, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after space, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != FileMode {
		t.Error("after space again, mode should be FileMode")
	}

	// Direct mode keys
	result, _ = m.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after c, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.mode != FileMode {
		t.Error("after f, mode should be FileMode")
	}
}

func TestFocusSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	if m.focus != SidebarFocus {
		t.Error("initial focus should be SidebarFocus")
	}

	result, _ := m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	m = result.(*Model)
	if m.focus != MainFocus {
		t.Error("after l, focus should be MainFocus")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = result.(*Model)
	if m.focus != SidebarFocus {
		t.Error("after h, focus should be SidebarFocus")
	}
}
