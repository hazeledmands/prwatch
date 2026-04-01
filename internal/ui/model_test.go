package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/hazeledmands/prwatch/internal/git"
)

// testGit returns a dummy git instance so the model operates in git mode.
func testGit() *git.Git {
	return git.New("/tmp")
}

func TestModeSwitching(t *testing.T) {
	m := NewModel("/tmp", testGit())

	if m.mode != FileDiffMode {
		t.Error("initial mode should be FileDiffMode")
	}

	// Press space to cycle: FileDiff → FileView → Commit → FileDiff
	result, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after space, mode should be FileViewMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after second space, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("after third space, mode should be FileDiffMode")
	}

	// Direct mode keys
	result, _ = m.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	m = result.(*Model)
	if m.mode != CommitMode {
		t.Error("after c, mode should be CommitMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after f, mode should be FileViewMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	if m.mode != FileDiffMode {
		t.Error("after d, mode should be FileDiffMode")
	}

	result, _ = m.Update(tea.KeyPressMsg{Text: "v", Code: 'v'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("after v, mode should be FileViewMode")
	}
}

func TestFocusSwitching(t *testing.T) {
	m := NewModel("/tmp", testGit())

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
	m := NewModel("/tmp", testGit())

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
	m := NewModel("/tmp", testGit())

	result, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	m = result.(*Model)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd == nil {
		t.Error("second q should produce a quit command")
	}
}

func TestQuitConfirm_qThenOtherKeyCancels(t *testing.T) {
	m := NewModel("/tmp", testGit())

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
	m := NewModel("/tmp", testGit())

	_, cmd := m.Update(tea.KeyPressMsg{Text: "Q", Code: 'Q'})
	if cmd == nil {
		t.Error("Q should produce a quit command immediately")
	}
}

func TestNonGitMode_StartsInFileView(t *testing.T) {
	m := NewModel("/tmp", nil)
	if m.mode != FileViewMode {
		t.Error("non-git model should start in FileViewMode")
	}
}

func TestNonGitMode_BlocksModeSwitching(t *testing.T) {
	m := NewModel("/tmp", nil)

	// Space should not change mode
	result, _ := m.Update(tea.KeyPressMsg{Text: " ", Code: tea.KeySpace})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("space should not change mode in non-git")
	}

	// 'd' should not change mode
	result, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("d should not change mode in non-git")
	}

	// 'c' should not change mode
	result, _ = m.Update(tea.KeyPressMsg{Text: "c", Code: 'c'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("c should not change mode in non-git")
	}

	// 'f' should stay in file-view (already there)
	result, _ = m.Update(tea.KeyPressMsg{Text: "f", Code: 'f'})
	m = result.(*Model)
	if m.mode != FileViewMode {
		t.Error("f should keep FileViewMode in non-git")
	}
}
