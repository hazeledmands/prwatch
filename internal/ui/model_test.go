package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

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

func TestQuitConfirm_qEntersConfirming(t *testing.T) {
	m := NewModel("/tmp", nil)

	result, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)
	if !m.confirming {
		t.Error("after q, should be confirming")
	}
	if cmd != nil {
		t.Error("should not quit yet")
	}
}

func TestQuitConfirm_qThenqQuits(t *testing.T) {
	m := NewModel("/tmp", nil)

	result, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd == nil {
		t.Error("second q should produce a quit command")
	}
}

func TestQuitConfirm_qThenOtherKeyCancels(t *testing.T) {
	m := NewModel("/tmp", nil)

	result, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)

	result, cmd := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = result.(*Model)
	if m.confirming {
		t.Error("after j, should not be confirming")
	}
	if cmd != nil {
		t.Error("should not quit")
	}
}

func TestQuitImmediate_QQuitsDirectly(t *testing.T) {
	m := NewModel("/tmp", nil)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "Q", Code: 'Q'})
	if cmd == nil {
		t.Error("Q should produce a quit command immediately")
	}
}
