package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// mapWithOneNode is a map holding a single node with the cursor on it, which is
// the state every attach test starts from.
func mapWithOneNode(t *testing.T, cfg config.Config) (Model, *fakeSessions) {
	t.Helper()
	sessions := &fakeSessions{}
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindShell, Title: "api"},
	}}
	m := New(cfg, ws, t.TempDir(), sessions)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model), sessions
}

func TestEnterHandsTheTerminalToTheSelectedNodesSession(t *testing.T) {
	m, sessions := mapWithOneNode(t, config.Default())

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd == nil {
		t.Fatalf("Enter should hand the terminal over, status: %s", next.status)
	}
	if len(sessions.handoffs) != 1 {
		t.Fatalf("expected one handoff, got %d", len(sessions.handoffs))
	}
	if want := tmux.SessionName("main", "k4f2"); sessions.handoffs[0].session != want {
		t.Errorf("attached to %q, want the selected node's session %q", sessions.handoffs[0].session, want)
	}
}

func TestEnterInstallsTheConfiguredDetachKey(t *testing.T) {
	cfg := config.Default()
	cfg.General.DetachKey = "M-q"
	m, sessions := mapWithOneNode(t, cfg)

	press(t, m, tea.KeyEnter)
	if len(sessions.handoffs) != 1 || sessions.handoffs[0].detachKey != "M-q" {
		t.Errorf("the handoff should carry the configured detach key, got %+v", sessions.handoffs)
	}
}

func TestEnterOnAnEmptyCellDoesNothing(t *testing.T) {
	m := mapWith(t) // no nodes, so the cursor sits on an empty cell
	next, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Error("Enter on an empty cell should not hand the terminal to anything")
	}
	if next.status != "" {
		t.Errorf("an empty cell is not a failure, got status %q", next.status)
	}
}

func TestEnterReportsANodeWhoseSessionHasGone(t *testing.T) {
	m, sessions := mapWithOneNode(t, config.Default())
	sessions.dead = true

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Error("a dead node should not hand the terminal to a session that is not there")
	}
	if !strings.Contains(next.status, "api") {
		t.Errorf("the status should name the node with no session, got %q", next.status)
	}
	if len(sessions.handoffs) != 0 {
		t.Error("nothing should have been handed off")
	}
}

func TestEnterReportsTmuxRefusingToSayWhetherTheSessionIsThere(t *testing.T) {
	m, sessions := mapWithOneNode(t, config.Default())
	sessions.existsErr = errors.New("tmux: something went wrong")

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Error("Enter should not attach when tmux cannot be asked about the session")
	}
	if !strings.Contains(next.status, "something went wrong") {
		t.Errorf("the status should carry tmux's complaint, got %q", next.status)
	}
}

func TestEnterRefusesToAttachWithNoWayBack(t *testing.T) {
	// Handing the terminal over without a detach binding would trap the user
	// inside the session, so a failed handoff stays on the map.
	m, sessions := mapWithOneNode(t, config.Default())
	sessions.handoffErr = errors.New("unknown key: M-Nonsense")

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Error("Enter should stay on the map when the detach binding cannot be installed")
	}
	if !strings.Contains(next.status, "M-Nonsense") {
		t.Errorf("the status should say why there is no way back, got %q", next.status)
	}
}

func TestReturningFromAttachReportsWhatWentWrong(t *testing.T) {
	m, _ := mapWithOneNode(t, config.Default())
	next, _ := m.Update(attachedMsg{err: errors.New("no sessions")})
	if !strings.Contains(next.(Model).status, "no sessions") {
		t.Errorf("a failed attach should say so, got %q", next.(Model).status)
	}
}

func TestReturningFromAttachLeavesTheMapAsItWas(t *testing.T) {
	m, _ := mapWithOneNode(t, config.Default())
	next, cmd := m.Update(attachedMsg{})
	if cmd != nil {
		t.Error("returning from an attach should not set more work going")
	}
	if got := next.(Model); got.status != "" || len(got.ws.Nodes) != 1 {
		t.Errorf("the map should be as it was, got status %q and %d nodes", got.status, len(got.ws.Nodes))
	}
}

func TestAttachErrorPrefersTmuxsOwnComplaint(t *testing.T) {
	// An exit status on its own sends the user hunting.
	if got := attachErr(errors.New("exit status 1"), "can't find session: trig_main_k4f2\n"); got == nil ||
		!strings.Contains(got.Error(), "can't find session") {
		t.Errorf("attachErr = %v, want tmux's own complaint", got)
	}
	if got := attachErr(errors.New("exit status 1"), "  "); got == nil || got.Error() != "exit status 1" {
		t.Errorf("attachErr with nothing on stderr = %v, want the exit status", got)
	}
	if got := attachErr(nil, "some noise"); got != nil {
		t.Errorf("attachErr on a clean exit = %v, want nil", got)
	}
}

func TestEnterStillEndsTheTitlePrompt(t *testing.T) {
	// Attach must not steal Enter from the modes that already use it.
	m, sessions := mapWithOneNode(t, config.Default())
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "db")
	next, cmd := press(t, m, tea.KeyEnter)
	settle(t, next, cmd)
	if len(sessions.created) != 1 {
		t.Errorf("Enter in the title prompt should still create a node, got %d creates", len(sessions.created))
	}
	if len(sessions.handoffs) != 0 {
		t.Error("Enter in the title prompt should not attach")
	}
}

func TestASecondEnterDoesNotAttachTwice(t *testing.T) {
	// Handoff runs on the keystroke but the terminal changes hands later, in
	// the Bubble Tea loop, so a double-tap or an auto-repeat can land two
	// Enters in the gap. The second binding would then be released by the
	// first attach, leaving the second one with no way back.
	m, sessions := mapWithOneNode(t, config.Default())

	m, _ = press(t, m, tea.KeyEnter)
	next, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Error("a second Enter should not start a second handoff")
	}
	if len(sessions.handoffs) != 1 {
		t.Errorf("expected one handoff, got %d", len(sessions.handoffs))
	}
	// Returning from the attach puts Enter back in service.
	back, _ := next.Update(attachedMsg{})
	if _, cmd := press(t, back.(Model), tea.KeyEnter); cmd == nil {
		t.Error("Enter should attach again once the terminal is back")
	}
}

func TestReturningFromAttachTakesTheDetachBindingBack(t *testing.T) {
	// The binding never outliving the attach is the property ADR 0002 rests on.
	released := 0
	var stderr strings.Builder
	msg := onReturn(func() error { released++; return nil }, &stderr)(nil)

	if released != 1 {
		t.Errorf("the detach binding was released %d times, want once", released)
	}
	if got := msg.(attachedMsg); got.err != nil {
		t.Errorf("a clean attach reported %v", got.err)
	}
}

func TestReturningFromAttachReportsAFailedRelease(t *testing.T) {
	var stderr strings.Builder
	msg := onReturn(func() error { return errors.New("unbind-key failed") }, &stderr)(nil)
	if got := msg.(attachedMsg); got.err == nil || !strings.Contains(got.err.Error(), "unbind-key") {
		t.Errorf("a binding left installed should be reported, got %v", got.err)
	}
}
