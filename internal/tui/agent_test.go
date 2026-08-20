package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// agentModel is a map whose config offers exactly the two default presets, so a
// test can count on what the picker is holding and on the order it is in.
func agentModel(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Dir: "/tmp/work"})
	return m, sessions
}

func TestAOpensThePickerOverTheConfiguredPresets(t *testing.T) {
	m, _ := agentModel(t)

	m, _ = typeKeys(t, m, "a")

	if m.mode != modeAgent {
		t.Fatalf("a should open the agent picker, mode = %v", m.mode)
	}
	if got, want := m.candidates, []string{"claude", "codex", customAgent}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("picker offers %v, want %v", got, want)
	}
	if view := m.View(); !strings.Contains(view, "claude") {
		t.Errorf("the picker should name the preset under its cursor, got:\n%s", view)
	}
}

func TestAPresetIsWhateverConfigSays(t *testing.T) {
	// Adding a preset is an edit to config, never a code change (§6), so the
	// picker is built from the map config hands over and from nothing else.
	cfg := config.Default()
	cfg.Agents = map[string]config.Agent{"aider": {Cmd: "aider --model sonnet"}}
	m := New(cfg, state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	next, _ := typeKeys(t, sized.(Model), "a")

	if got, want := next.candidates, []string{"aider", customAgent}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("picker offers %v, want %v", got, want)
	}
}

func TestChoosingAPresetCreatesANodeRunningItsCommand(t *testing.T) {
	m, sessions := agentModel(t)

	m, _ = typeKeys(t, m, "a")
	m, _ = press(t, m, tea.KeyEnter) // claude, the first preset
	m, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node on the map, got %d", len(m.ws.Nodes))
	}
	node := m.ws.Nodes[0]
	if node.Kind != state.KindAgent {
		t.Errorf("node kind = %q, want %q", node.Kind, state.KindAgent)
	}
	if node.Cmd != "claude" {
		t.Errorf("node cmd = %q, want %q", node.Cmd, "claude")
	}
	// The title prompt opens on the command's own name, so ⏎ names the card
	// after what it runs rather than after its id.
	if node.Title != "claude" {
		t.Errorf("node title = %q, want %q", node.Title, "claude")
	}
	if len(sessions.created) != 1 {
		t.Fatalf("expected one tmux session, got %d", len(sessions.created))
	}
	if got := sessions.created[0].cmd; !strings.Contains(got, "claude") {
		t.Errorf("session command = %q, want it to run claude", got)
	}
	if got := sessions.created[0].env["TRIG_NODE_KIND"]; got != string(state.KindAgent) {
		t.Errorf("session provenance kind = %q, want %q", got, state.KindAgent)
	}
}

func TestAnAgentNodeCanBeNamedLikeAnyOther(t *testing.T) {
	m, _ := agentModel(t)

	m, _ = typeKeys(t, m, "a")
	m, _ = press(t, m, tea.KeyEnter)
	for range "claude" { // clear the suggested name
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m, _ = typeKeys(t, m, "refactor")
	m, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 1 || m.ws.Nodes[0].Title != "refactor" {
		t.Fatalf("the title prompt should be editable, got %+v", m.ws.Nodes)
	}
}

func TestTheCustomOptionTakesACommandLine(t *testing.T) {
	m, sessions := agentModel(t)

	m, _ = typeKeys(t, m, "a")
	m, _ = typeKeys(t, m, "jj") // past claude and codex, onto custom
	m, _ = press(t, m, tea.KeyEnter)
	if m.mode != modeAgentCmd {
		t.Fatalf("custom should ask for a command line, mode = %v", m.mode)
	}
	m, _ = typeKeys(t, m, "claude --resume")
	m, _ = press(t, m, tea.KeyEnter)
	m, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node on the map, got %d", len(m.ws.Nodes))
	}
	if got := m.ws.Nodes[0].Cmd; got != "claude --resume" {
		t.Errorf("node cmd = %q, want %q", got, "claude --resume")
	}
	if m.ws.Nodes[0].Kind != state.KindAgent {
		t.Errorf("a custom agent is still an agent node, kind = %q", m.ws.Nodes[0].Kind)
	}
	if len(sessions.created) != 1 || !strings.Contains(sessions.created[0].cmd, "claude --resume") {
		t.Errorf("session should run the typed command, got %+v", sessions.created)
	}
}

func TestAnEmptyCustomCommandIsNotANode(t *testing.T) {
	m, sessions := agentModel(t)

	m, _ = typeKeys(t, m, "a")
	m, _ = typeKeys(t, m, "jj")
	m, _ = press(t, m, tea.KeyEnter)
	m, cmd := press(t, m, tea.KeyEnter)

	if cmd != nil {
		t.Error("an empty command should start nothing")
	}
	if m.mode != modeAgentCmd {
		t.Errorf("the prompt should still be collecting a command, mode = %v", m.mode)
	}
	if len(sessions.created) != 0 {
		t.Errorf("expected no tmux session, got %+v", sessions.created)
	}
}

func TestEscBacksOutOfTheAgentPicker(t *testing.T) {
	m, _ := agentModel(t)

	m, _ = typeKeys(t, m, "a")
	m, _ = typeKeys(t, m, "jj")
	m, _ = press(t, m, tea.KeyEnter)
	// Esc from the command prompt undoes the choice, not the whole keystroke:
	// the list is still what is being looked at.
	m, _ = press(t, m, tea.KeyEsc)
	if m.mode != modeAgent {
		t.Fatalf("Esc should go back to the picker, mode = %v", m.mode)
	}
	m, _ = press(t, m, tea.KeyEsc)
	if m.mode != modeNormal {
		t.Errorf("Esc should close the picker, mode = %v", m.mode)
	}
	if len(m.ws.Nodes) != 0 {
		t.Errorf("nothing should have been created, got %+v", m.ws.Nodes)
	}
}

func TestAPresetWithNoCommandIsRefused(t *testing.T) {
	cfg := config.Default()
	cfg.Agents = map[string]config.Agent{"typo": {}}
	m := New(cfg, state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	next, _ := typeKeys(t, sized.(Model), "a")
	next, cmd := press(t, next, tea.KeyEnter)

	if cmd != nil {
		t.Error("a preset with no command should start nothing")
	}
	if !strings.Contains(next.status, "typo") {
		t.Errorf("the status should name the empty preset, got %q", next.status)
	}
	if len(next.ws.Nodes) != 0 {
		t.Errorf("nothing should have been created, got %+v", next.ws.Nodes)
	}
}

func TestAnAgentCardSaysItIsAnAgent(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindAgent, Title: "claude", Cmd: "claude"},
	}}
	m, _, _ := newNodeModel(t, ws)

	if view := m.View(); !strings.Contains(view, state.KindAgent.Label()) {
		t.Errorf("the card should say the node is an agent, got:\n%s", view)
	}
}

func TestARespawnRerunsTheStoredCommand(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindAgent, Title: "claude", Cmd: "claude --resume"},
	}}
	m, sessions, _ := newNodeModel(t, ws)
	sessions.dead = true

	m, _ = press(t, m, tea.KeyEnter) // offers the respawn
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if len(sessions.created) != 1 {
		t.Fatalf("expected one tmux session, got %+v", sessions.created)
	}
	if got := sessions.created[0].cmd; !strings.Contains(got, "claude --resume") {
		t.Errorf("a respawn should re-run the agent, got %q", got)
	}
}

func TestACommandTooLongToHoldIsRefused(t *testing.T) {
	// A truncated title is a title; a truncated command is a different command,
	// and one that would reach a shell half-quoted.
	m, sessions := agentModel(t)

	m, _ = typeKeys(t, m, "a")
	m, _ = typeKeys(t, m, "jj")
	m, _ = press(t, m, tea.KeyEnter)
	m, _ = typeKeys(t, m, "claude -p "+strings.Repeat("x", state.MaxCmdLen))
	m, cmd := press(t, m, tea.KeyEnter)

	if cmd != nil {
		t.Error("a command that did not fit should start nothing")
	}
	if len(sessions.created) != 0 {
		t.Errorf("expected no tmux session, got %+v", sessions.created)
	}
	if !strings.Contains(m.status, "too long") {
		t.Errorf("the status should say the command did not fit, got %q", m.status)
	}
}
