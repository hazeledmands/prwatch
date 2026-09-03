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
	var ranBefore, ranAfter bool
	batch := func() tea.Msg {
		return tea.BatchMsg{
			func() tea.Msg { ranBefore = true; return nil },
			func() tea.Msg { panic("nested boom") },
			func() tea.Msg { ranAfter = true; return nil },
		}
	}
	err := m.execFollowUps(batch)
	if err == nil {
		t.Fatal("a panic inside a batched sub-command must propagate to the caller")
	}
	if !strings.Contains(err.Error(), "nested boom") {
		t.Errorf("error should carry the nested panic value, got %q", err.Error())
	}
	if !ranBefore {
		t.Error("sibling commands ahead of the panicking one should still have run")
	}
	// Bailing at the first panic leaves the model MORE half-updated than
	// running the batch out does: the remaining sub-commands are exactly the
	// updates that would have finished the job. Collect the failures and keep
	// going.
	if !ranAfter {
		t.Error("sibling commands behind the panicking one must still run")
	}
}

// Every panic in a batch is reported, not just the first: a caller told about
// one of two failures would wrongly believe the rest of the batch landed.
func TestExecFollowUps_AllBatchPanicsAreReported(t *testing.T) {
	m := NewModel("/tmp", testGit())
	batch := func() tea.Msg {
		return tea.BatchMsg{
			func() tea.Msg { panic("first boom") },
			func() tea.Msg { panic("second boom") },
		}
	}
	err := m.execFollowUps(batch)
	if err == nil {
		t.Fatal("expected an error")
	}
	got := err.Error()
	for _, want := range []string{"first boom", "second boom"} {
		if !strings.Contains(got, want) {
			t.Errorf("error should carry %q, got %q", want, got)
		}
	}
}

// The report render is the last step of the harness, and it runs on a model
// the follow-ups may have left corrupt. If the corruption is what panics
// View, an unguarded render destroys the very report describing it.
func TestRenderReport_PanicBecomesAnError(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width, m.height = 120, 40
	m.updateLayout()
	// loading must be false or View early-returns before it touches the
	// panes, and the corruption below would never be reached.
	m.loading = false
	m.sidebar = nil // the shape a half-applied Update can leave behind

	content, err := m.renderReport()
	if err == nil {
		t.Fatal("a View that panics must be reported, not allowed to escape")
	}
	if content != "" {
		t.Errorf("content = %q, want empty when the render failed", content)
	}
	if !strings.Contains(err.Error(), "report render") {
		t.Errorf("error should name the failing activity, got %q", err.Error())
	}
}

func TestRenderReport_HealthyModelHasNoError(t *testing.T) {
	m := NewModel("/tmp", testGit())
	m.width, m.height = 120, 40
	m.updateLayout()

	content, err := m.renderReport()
	if err != nil {
		t.Fatalf("healthy render should not error: %v", err)
	}
	if content == "" {
		t.Error("healthy render should produce content")
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
