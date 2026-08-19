package tui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// TestNAndXAgainstRealTmux exercises the whole of a node's life against a real
// tmux server on a private socket, so nothing here can touch the sessions the
// user is actually working in.
func TestNAndXAgainstRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	cli := tmux.CLI{Socket: "trig-test-tui"}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", cli.Socket, "kill-server").Run() })

	workDir := t.TempDir()
	m := New(config.Default(), state.Workspace{Name: "main", Dir: workDir}, t.TempDir(), cli)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = sized.(Model)

	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node, got %d (status: %s)", len(m.ws.Nodes), m.status)
	}
	session := tmux.SessionName("main", m.ws.Nodes[0].ID)

	alive, err := cli.Exists(session)
	if err != nil || !alive {
		t.Fatalf("session %q should be live: alive=%v err=%v", session, alive, err)
	}
	env, err := exec.Command("tmux", "-L", cli.Socket, "show-environment", "-t", "="+session).Output()
	if err != nil {
		t.Fatalf("show-environment: %v", err)
	}
	for _, want := range []string{"TRIG_WORKSPACE=main", "TRIG_NODE_ID=" + m.ws.Nodes[0].ID, "TRIG_NODE_KIND=shell"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("session environment is missing %q, got:\n%s", want, env)
		}
	}

	m, _ = typeKeys(t, m, "x")
	next, cmd = typeKeys(t, m, "y")
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("the node should be gone, status: %s", m.status)
	}
	alive, err = cli.Exists(session)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if alive {
		t.Errorf("session %q should have been killed", session)
	}
}
