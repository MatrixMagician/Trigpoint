package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// TestAnAgentSessionOutlivesItsAgent is the one thing a fake tmux cannot say:
// tmux ends a session whose command exits, and an agent node whose agent has
// finished is still an agent node with a shell in it (§6).
func TestAnAgentSessionOutlivesItsAgent(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	cli := tmux.CLI{Socket: "trig-test-agent"}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", cli.Socket, "kill-server").Run() })

	cfg := config.Default()
	// A preset that is over before it starts, which is the case this is about.
	cfg.Agents = map[string]config.Agent{"quick": {Cmd: "echo agent-ran"}}
	m := New(cfg, state.Workspace{Name: "main", Dir: t.TempDir()}, t.TempDir(), cli)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = sized.(Model)

	m, _ = typeKeys(t, m, "a")
	m, _ = press(t, m, tea.KeyEnter)
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node, got %d (status: %s)", len(m.ws.Nodes), m.status)
	}
	session := tmux.SessionName("main", m.ws.Nodes[0].ID)
	// Long enough for a command this short to have come and gone.
	time.Sleep(500 * time.Millisecond)

	alive, err := cli.Exists(session)
	if err != nil || !alive {
		t.Fatalf("the session should outlive the agent: alive=%v err=%v", alive, err)
	}
	out, err := cli.Capture(session, 10)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.Contains(out, "agent-ran") {
		t.Errorf("the session should have run the agent, got:\n%s", out)
	}
	env, err := exec.Command("tmux", "-L", cli.Socket, "show-environment", "-t", "="+session).Output()
	if err != nil {
		t.Fatalf("show-environment: %v", err)
	}
	if !strings.Contains(string(env), "TRIG_NODE_KIND=agent") {
		t.Errorf("the session should say it is an agent's, got:\n%s", env)
	}
}

// TestAnAgentReportsFromInsideItsOwnSession is the other thing a fake tmux
// cannot say: that TRIG_STATUS_FILE actually reaches the process running in the
// pane, and that a file written there — by hand, by a hook, by anything at all —
// is a report Trigpoint reads back. This is the whole contract, end to end,
// without `trig emit-status` in the middle of it.
func TestAnAgentReportsFromInsideItsOwnSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	cli := tmux.CLI{Socket: "trig-test-status"}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", cli.Socket, "kill-server").Run() })

	cfg := config.Default()
	// An "agent" whose whole job is to announce itself, the way a real one's
	// hook would.
	cfg.Agents = map[string]config.Agent{"reporter": {
		Cmd: `printf '{"state":"needs_you","detail":"from the pane"}' > "$TRIG_STATUS_FILE"`,
	}}
	stateDir := t.TempDir()
	m := New(cfg, state.Workspace{Name: "main", Dir: t.TempDir()}, stateDir, cli)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = sized.(Model)

	m, _ = typeKeys(t, m, "a")
	m, _ = press(t, m, tea.KeyEnter)
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node, got %d (status: %s)", len(m.ws.Nodes), m.status)
	}
	id := m.ws.Nodes[0].ID
	// Long enough for one printf to have run.
	time.Sleep(500 * time.Millisecond)

	reports, err := status.Read(status.DirBeside(stateDir), "main")
	if err != nil {
		t.Fatalf("reading the status directory: %v", err)
	}
	report, ok := reports[id]
	if !ok {
		t.Fatalf("read %v, want a report for the node that just reported one", reports)
	}
	if report.State != status.NeedsYou || report.Detail != "from the pane" {
		t.Errorf("report = %+v, want needs_you from the pane", report)
	}
	// And what the map makes of it, which is the point of the file existing.
	m.reports = reports
	if got := m.badgeOf(m.ws.Nodes[0]); got.colour != statusColours[status.NeedsYou] {
		t.Errorf("badge = %+v, want it drawn in the needs-you colour", got)
	}
}
