//go:build linux

package tui

import (
	"os/exec"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// TestAdoptingARealSessionOnARealTerminal runs adoption against a real pty and a
// real tmux server, because it is the one part of it the fake cannot vouch for:
// the map's own tests stand in for tmux, and a session outside the prefix is
// exactly what the tmux package spends its time refusing. Here the session is
// really there, really adopted, and really killed by `x`.
func TestAdoptingARealSessionOnARealTerminal(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	cli := tmux.CLI{Socket: "trig-test-adopt-pty"}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", cli.Socket, "kill-server").Run() })

	// Short enough to survive a card border, so the map can be asked whether the
	// node arrived rather than only the status bar.
	const zoo = "zoo-shell"
	if err := exec.Command("tmux", "-L", cli.Socket, "new-session", "-d", "-s", zoo).Run(); err != nil {
		t.Fatalf("setting up the session to adopt: %v", err)
	}

	term := openTerminal(t)
	stateDir := t.TempDir()
	prog := tea.NewProgram(New(config.Default(), state.Workspace{Name: "main", Dir: t.TempDir()}, stateDir, cli),
		tea.WithAltScreen(), tea.WithInput(term.pts), tea.WithOutput(term.pts))
	done := make(chan error, 1)
	go func() { _, err := prog.Run(); done <- err }()

	term.waitFor(t, "map is empty", "the map never appeared")
	term.type_("A")
	term.waitFor(t, "Adopt "+zoo, "A never offered the session tmux is running")
	term.forget()
	term.type_("\r")
	term.waitFor(t, zoo, "the adopted node never reached the map")

	saved, err := state.Load(stateDir, "main")
	if err != nil || len(saved.Nodes) != 1 {
		t.Fatalf("reading back the workspace: %v, %d nodes", err, len(saved.Nodes))
	}
	if saved.Nodes[0].Session != zoo {
		t.Errorf("the foreign session's own name is what is stored, got %q", saved.Nodes[0].Session)
	}
	if alive, err := cli.Exists(zoo); err != nil || !alive {
		t.Fatalf("adoption must leave the session exactly as it found it (alive=%v err=%v)", alive, err)
	}

	// The card is over the real session, so killing the node kills it.
	term.forget()
	term.type_("x")
	term.waitFor(t, "Kill", "x did not ask before killing an adopted node")
	term.type_("y")
	waitForGone(t, cli, zoo)

	term.type_("q")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the programme ended badly: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("q did not quit")
	}
}

// waitForGone waits for a session to stop existing; tmux does its own work in
// its own time.
func waitForGone(t *testing.T, cli tmux.CLI, session string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if alive, err := cli.Exists(session); err == nil && !alive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the session %q outlived the node adopted from it", session)
}
