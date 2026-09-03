package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// execFollowUps runs commands on behalf of the exploration harness
// (RenderWithKeys, the IPC socket). A panic escaping one of those commands
// used to be swallowed by a bare recover(), so an agent loop driving the
// harness got a screen that had silently half-updated and no indication
// anything went wrong. The panic must reach the caller as an error carrying
// both the panic value and a stack.

func TestExecFollowUps_NilCmdIsNoError(t *testing.T) {
	m := NewModel("/tmp", testGit())
	if err := m.execFollowUps(nil); err != nil {
		t.Errorf("nil cmd should not error, got %v", err)
	}
}

func TestExecFollowUps_QuietCmdIsNoError(t *testing.T) {
	m := NewModel("/tmp", testGit())
	if err := m.execFollowUps(func() tea.Msg { return nil }); err != nil {
		t.Errorf("cmd returning nil msg should not error, got %v", err)
	}
}

func TestExecFollowUps_PanicIsReportedWithStack(t *testing.T) {
	m := NewModel("/tmp", testGit())
	err := m.execFollowUps(func() tea.Msg { panic("boom in a follow-up") })
	if err == nil {
		t.Fatal("a panicking follow-up command must return an error, not be swallowed")
	}
	got := err.Error()
	if !strings.Contains(got, "boom in a follow-up") {
		t.Errorf("error should carry the panic value, got %q", got)
	}
	// The stack is the whole point: without it the harness caller cannot tell
	// which command blew up.
	if !strings.Contains(got, "execFollowUps") {
		t.Errorf("error should carry a stack trace naming the failing frame, got %q", got)
	}
}

func TestExecFollowUps_PanicInsideBatchPropagates(t *testing.T) {
	m := NewModel("/tmp", testGit())
	var ranSibling bool
	batch := func() tea.Msg {
		return tea.BatchMsg{
			func() tea.Msg { ranSibling = true; return nil },
			func() tea.Msg { panic("nested boom") },
		}
	}
	err := m.execFollowUps(batch)
	if err == nil {
		t.Fatal("a panic inside a batched sub-command must propagate to the caller")
	}
	if !strings.Contains(err.Error(), "nested boom") {
		t.Errorf("error should carry the nested panic value, got %q", err.Error())
	}
	if !ranSibling {
		t.Error("sibling commands ahead of the panicking one should still have run")
	}
}

// TestExecFollowUps_PanicSurvivesTheModel checks that reporting a panic does
// not also take down the process: the harness caller decides what to do.
func TestExecFollowUps_ModelStillUsableAfterPanic(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width, m.height = 120, 40
	m.updateLayout()
	if err := m.execFollowUps(func() tea.Msg { panic("boom") }); err == nil {
		t.Fatal("expected an error")
	}
	if v := m.View(); v.Content == "" {
		t.Error("model should still render after a reported follow-up panic")
	}
}
