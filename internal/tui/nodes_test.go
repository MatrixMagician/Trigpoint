package tui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// fakeSessions stands in for tmux so the map view can be exercised without a
// tmux server, and so a test can assert exactly what Trigpoint asked tmux to do.
type fakeSessions struct {
	created    []createCall
	killed     []string
	handoffs   []handoffCall
	captured   []captureCall
	events     chan tmux.Event
	output     map[string]string // what Capture hands back, by session
	live       []string          // the sessions List reports as running
	envs       map[string]map[string]string
	enved      []string // the sessions the map asked for provenance
	listed     int      // how many times the map has asked what is running
	released   int
	listErr    error
	envErr     error
	createErr  error
	killErr    error
	existsErr  error
	handoffErr error
	captureErr error
	dead       bool // Exists reports the node's session as gone
}

type captureCall struct {
	session string
	lines   int
}

func (f *fakeSessions) Capture(session string, lines int) (string, error) {
	if f.captureErr != nil {
		return "", f.captureErr
	}
	f.captured = append(f.captured, captureCall{session, lines})
	return f.output[session], nil
}

// Watch hands back a stream the test drives by hand, or none at all — a nil
// stream is a map nobody is pushing events at, which is most of these tests.
func (f *fakeSessions) Watch(context.Context) <-chan tmux.Event { return f.events }

type createCall struct {
	session string
	dir     string
	cmd     string
	env     map[string]string
}

func (f *fakeSessions) Create(session, dir, cmd string, env map[string]string) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, createCall{session, dir, cmd, env})
	return nil
}

func (f *fakeSessions) List() ([]string, error) {
	f.listed++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.live, nil
}

func (f *fakeSessions) Env(session string) (map[string]string, error) {
	f.enved = append(f.enved, session)
	if f.envErr != nil {
		return nil, f.envErr
	}
	return f.envs[session], nil
}

type handoffCall struct {
	session   string
	detachKey string
}

func (f *fakeSessions) Exists(session string) (bool, error) {
	return !f.dead, f.existsErr
}

func (f *fakeSessions) Handoff(session, detachKey string) (*exec.Cmd, func() error, error) {
	if f.handoffErr != nil {
		return nil, nil, f.handoffErr
	}
	f.handoffs = append(f.handoffs, handoffCall{session, detachKey})
	// Never run by these tests: the map is asked what it did, not driven
	// through a real Bubble Tea programme.
	return exec.Command("true"), func() error { f.released++; return nil }, nil
}

func (f *fakeSessions) Kill(session string) error {
	if f.killErr != nil {
		return f.killErr
	}
	f.killed = append(f.killed, session)
	return nil
}

func newNodeModel(t *testing.T, ws state.Workspace) (Model, *fakeSessions, string) {
	t.Helper()
	dir := t.TempDir()
	sessions := &fakeSessions{}
	m := New(config.Default(), ws, dir, sessions)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model), sessions, dir
}

// settle runs a command and feeds its message back, the way the Bubble Tea
// runtime would, until the model stops asking for more work.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 10 {
			t.Fatal("commands did not settle")
		}
		msg := cmd()
		if msg == nil {
			break
		}
		next, next2 := m.Update(msg)
		m, cmd = next.(Model), next2
	}
	return m
}

func typeKeys(t *testing.T, m Model, keys string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, r := range keys {
		next, c := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m, cmd = next.(Model), c
	}
	return m, cmd
}

func press(t *testing.T, m Model, key tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: key})
	return next.(Model), cmd
}

func TestNPromptsForATitle(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")

	if !strings.Contains(m.View(), "Title") {
		t.Errorf("n should prompt for a title, got:\n%s", m.View())
	}
}

func TestNCreatesAShellNodeWithALiveSession(t *testing.T) {
	m, sessions, stateDir := newNodeModel(t, state.Workspace{Name: "main", Dir: "/tmp/work"})

	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	m, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node on the map, got %d", len(m.ws.Nodes))
	}
	node := m.ws.Nodes[0]
	if node.Title != "api" {
		t.Errorf("node title = %q, want %q", node.Title, "api")
	}
	if node.Kind != state.KindShell {
		t.Errorf("node kind = %q, want %q", node.Kind, state.KindShell)
	}
	if node.ID == "" {
		t.Error("node has no id")
	}
	if node.CreatedAt.IsZero() {
		t.Error("node has no creation time")
	}

	if len(sessions.created) != 1 {
		t.Fatalf("expected one tmux session, got %d", len(sessions.created))
	}
	call := sessions.created[0]
	if want := tmux.SessionName("main", node.ID); call.session != want {
		t.Errorf("session name = %q, want %q", call.session, want)
	}
	if call.dir != "/tmp/work" {
		t.Errorf("session working directory = %q, want the workspace default", call.dir)
	}
	for k, want := range map[string]string{
		"TRIG_WORKSPACE": "main",
		"TRIG_NODE_ID":   node.ID,
		"TRIG_NODE_KIND": string(state.KindShell),
	} {
		if got := call.env[k]; got != want {
			t.Errorf("session environment %s = %q, want %q", k, got, want)
		}
	}

	// Identity and position have to outlive this process.
	saved, err := state.Load(stateDir, "main")
	if err != nil {
		t.Fatalf("reloading the workspace: %v", err)
	}
	if len(saved.Nodes) != 1 || saved.Nodes[0].ID != node.ID || saved.Nodes[0].Pos != node.Pos || saved.Nodes[0].Title != node.Title {
		t.Errorf("the node did not survive a restart: %+v", saved.Nodes)
	}
}

func TestNewNodesLandOnFreeCells(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})

	for _, title := range []string{"one", "two", "three"} {
		m, _ = typeKeys(t, m, "n")
		m, _ = typeKeys(t, m, title)
		next, cmd := press(t, m, tea.KeyEnter)
		m = settle(t, next, cmd)
	}

	seen := map[state.Cell]string{}
	for _, n := range m.ws.Nodes {
		if other, clash := seen[n.Pos]; clash {
			t.Errorf("%q and %q are both at %+v", other, n.Title, n.Pos)
		}
		seen[n.Pos] = n.Title
	}
	if len(m.ws.Nodes) != 3 {
		t.Fatalf("expected three nodes, got %d", len(m.ws.Nodes))
	}
}

func TestAnUntitledNodeFallsBackToItsID(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("an empty title should still make a node, got %d", len(m.ws.Nodes))
	}
	if m.ws.Nodes[0].Title != m.ws.Nodes[0].ID {
		t.Errorf("untitled node is titled %q, want its id %q", m.ws.Nodes[0].Title, m.ws.Nodes[0].ID)
	}
}

func TestEscapeAbandonsTheTitlePrompt(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	m, cmd := press(t, m, tea.KeyEsc)
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 0 {
		t.Error("escaping the prompt should not create a node")
	}
	if len(sessions.created) != 0 {
		t.Error("escaping the prompt should not start a session")
	}
	if strings.Contains(m.View(), "Title") {
		t.Error("escaping the prompt should dismiss it")
	}
}

func TestBackspaceEditsTheTitle(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "apix")
	m, _ = press(t, m, tea.KeyBackspace)
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if got := m.ws.Nodes[0].Title; got != "api" {
		t.Errorf("title = %q, want %q", got, "api")
	}
}

func TestASessionThatWillNotStartLeavesNoNode(t *testing.T) {
	m, sessions, stateDir := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.createErr = errors.New("tmux: no space left on device")

	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 0 {
		t.Error("a node whose session never started should not appear on the map")
	}
	if !strings.Contains(m.View(), "no space left") {
		t.Errorf("the failure should be visible, got:\n%s", m.View())
	}
	saved, err := state.Load(stateDir, "main")
	if err != nil {
		t.Fatalf("reloading the workspace: %v", err)
	}
	if len(saved.Nodes) != 0 {
		t.Error("a node whose session never started should not be persisted")
	}
}

func TestKillConfirmsFirst(t *testing.T) {
	m, sessions, _ := newNodeModel(t, oneNode())
	m, _ = typeKeys(t, m, "x")

	if !strings.Contains(m.View(), "api") || !strings.ContainsAny(m.View(), "?") {
		t.Errorf("x should ask before killing, got:\n%s", m.View())
	}
	m, _ = typeKeys(t, m, "n")
	if len(sessions.killed) != 0 {
		t.Error("declining the confirmation should kill nothing")
	}
	if len(m.ws.Nodes) != 1 {
		t.Error("declining the confirmation should keep the node")
	}
}

func TestKillRemovesTheNodeAndItsSession(t *testing.T) {
	ws := oneNode()
	m, sessions, stateDir := newNodeModel(t, ws)

	m, _ = typeKeys(t, m, "x")
	next, cmd := typeKeys(t, m, "y")
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("the node should be gone from the map, got %+v", m.ws.Nodes)
	}
	want := tmux.SessionName("main", ws.Nodes[0].ID)
	if len(sessions.killed) != 1 || sessions.killed[0] != want {
		t.Errorf("killed sessions = %v, want [%s]", sessions.killed, want)
	}
	saved, err := state.Load(stateDir, "main")
	if err != nil {
		t.Fatalf("reloading the workspace: %v", err)
	}
	if len(saved.Nodes) != 0 {
		t.Error("the node should be gone from state too")
	}
}

func TestASessionThatWillNotDieKeepsItsNode(t *testing.T) {
	m, sessions, _ := newNodeModel(t, oneNode())
	sessions.killErr = errors.New("tmux: session not found")

	m, _ = typeKeys(t, m, "x")
	next, cmd := typeKeys(t, m, "y")
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Error("a node whose session would not die should stay on the map")
	}
	if !strings.Contains(m.View(), "session not found") {
		t.Errorf("the failure should be visible, got:\n%s", m.View())
	}
}

func TestKillWithNothingSelectedDoesNothing(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "x")

	if len(sessions.killed) != 0 {
		t.Error("x on an empty map should kill nothing")
	}
	if strings.Contains(m.View(), "Kill") {
		t.Error("x on an empty map should not open a confirmation")
	}
}

func TestQuittingKillsNothing(t *testing.T) {
	m, sessions, _ := newNodeModel(t, oneNode())
	_, cmd := typeKeys(t, m, "q")

	if !isQuit(cmd) {
		t.Fatal("q should quit")
	}
	if len(sessions.killed) != 0 {
		t.Error("quitting Trigpoint must leave every session running")
	}
}

func TestTheNewNodeIsSelected(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if m.ws.Viewport.Cursor != m.ws.Nodes[0].Pos {
		t.Errorf("cursor at %+v, want the new node's cell %+v", m.ws.Viewport.Cursor, m.ws.Nodes[0].Pos)
	}
}

func TestTheMapShowsItsNodes(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())
	if !strings.Contains(m.View(), "api") {
		t.Errorf("a placed node should be visible on the map, got:\n%s", m.View())
	}
}

func oneNode() state.Workspace {
	return state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api", Pos: state.Cell{}}},
	}
}

func TestNodesStartedBeforeEitherSessionConfirmsDoNotShareACell(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})

	m, _ = typeKeys(t, m, "n")
	m, first := press(t, m, tea.KeyEnter)
	m, _ = typeKeys(t, m, "n")
	m, second := press(t, m, tea.KeyEnter)

	a, ok := first().(nodeCreatedMsg)
	if !ok {
		t.Fatal("the first Enter did not start a node")
	}
	b, ok := second().(nodeCreatedMsg)
	if !ok {
		t.Fatal("the second Enter did not start a node")
	}
	if a.node.ID == b.node.ID {
		t.Errorf("both nodes were given the id %q", a.node.ID)
	}
	if a.node.Pos == b.node.Pos {
		t.Errorf("both nodes were placed at %+v", a.node.Pos)
	}

	next, _ := m.Update(a)
	next, _ = next.(Model).Update(b)
	if got := len(next.(Model).ws.Nodes); got != 2 {
		t.Errorf("expected two nodes on the map, got %d", got)
	}
}

func TestAFailedSessionFreesTheCellItWasHolding(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.createErr = errors.New("tmux: nope")

	m, _ = typeKeys(t, m, "n")
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	sessions.createErr = nil
	m, _ = typeKeys(t, m, "n")
	next, cmd = press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(m.ws.Nodes))
	}
	if m.ws.Nodes[0].Pos != (state.Cell{}) {
		t.Errorf("the node landed at %+v; the cell the failed node held should have been freed", m.ws.Nodes[0].Pos)
	}
}

func TestCardGeometrySurvivesWideRunes(t *testing.T) {
	for _, title := range []string{
		"api-server",
		"日本語のノードタイトルです日本語のノード",
		"🔥🔥🔥 deploy 🔥🔥🔥 everything 🔥🔥🔥",
		"日本",
	} {
		// The title is used as the body too, so the body lines are held to the
		// same width as the borders.
		for _, line := range card(state.Node{Kind: state.KindNote, Title: title, Note: title}, false, false, false, badgeMark{}, "", 2, noteLines(title)) {
			if w := lipgloss.Width(line); w != cardWidth {
				t.Errorf("a card line of %q is %d cells wide, want %d: %q", title, w, cardWidth, line)
			}
		}
	}
}

// TestTheMapRendersOnlyWhatFitsOnScreen keeps rendering cost tied to the
// terminal rather than to a coordinate read off disk, where a far-away position
// would otherwise be an out-of-memory rather than a scroll.
func TestTheMapRendersOnlyWhatFitsOnScreen(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindShell, Title: "here", Pos: state.Cell{}},
		{ID: "m7qp", Kind: state.KindShell, Title: "far", Pos: state.Cell{Col: 100_000_000, Row: 100_000_000}},
	}}
	m, _, _ := newNodeModel(t, ws)

	view := m.View()
	if lines := strings.Split(view, "\n"); len(lines) != 24 {
		t.Errorf("view is %d lines in a 24-line terminal", len(lines))
	}
	if !strings.Contains(view, "here") {
		t.Error("the node at the cursor should be on screen")
	}
	if strings.Contains(view, "far") {
		t.Error("a node a hundred million cells away should not be drawn")
	}
}

// TestKillTargetsTheNodeConfirmedAgainst pins the kill to the node that was
// under the cursor when x was pressed. A create landing while the prompt is up
// moves the cursor to the new node, and re-reading the selection at y time would
// destroy a session the user never looked at, let alone confirmed.
func TestKillTargetsTheNodeConfirmedAgainst(t *testing.T) {
	m, sessions, _ := newNodeModel(t, oneNode())
	doomed := m.ws.Nodes[0].ID

	// A create is in flight: its message has not been fed back yet.
	m, _ = typeKeys(t, m, "n")
	m, create := press(t, m, tea.KeyEnter)

	m, _ = typeKeys(t, m, "x")
	prompt := m.View()

	// The create lands while the confirmation is on screen.
	created, ok := create().(nodeCreatedMsg)
	if !ok {
		t.Fatal("Enter did not start a node")
	}
	next, _ := m.Update(created)
	m = next.(Model)

	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	want := tmux.SessionName("main", doomed)
	if len(sessions.killed) != 1 || sessions.killed[0] != want {
		t.Errorf("killed %v, want [%s] — the node the user confirmed against", sessions.killed, want)
	}
	for _, n := range m.ws.Nodes {
		if n.ID == doomed {
			t.Error("the confirmed node survived")
		}
		if n.ID == created.node.ID && len(sessions.killed) > 0 && sessions.killed[0] != want {
			t.Error("the newly created node was killed instead")
		}
	}
	if !strings.Contains(prompt, "api") {
		t.Errorf("the prompt should name the node being killed, got:\n%s", prompt)
	}
}

// TestThePromptOutranksAStaleError keeps the title prompt on screen while it is
// being typed into. An earlier node's failure arrives whenever tmux gets round
// to it, and letting it take the bar leaves the user typing blind.
func TestThePromptOutranksAStaleError(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})

	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")

	next, _ := m.Update(nodeCreatedMsg{node: state.Node{ID: "gone"}, err: errors.New("tmux exploded")})
	m = next.(Model)

	if !strings.Contains(m.View(), "api") {
		t.Errorf("the title being typed should still be visible, got:\n%s", m.View())
	}
}

// A node created while tmux is still thinking reserves a cell, but the map does
// not stand still: shoving a node onto that cell must not end with two nodes
// sharing it, because selected() and the card renderer would then disagree
// about which of them x kills.
func TestACreateLandingOnAShovedCellFindsAnotherCell(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "a", Kind: state.KindShell, Title: "a", Pos: state.Cell{}},
	}}
	m, _, _ := newNodeModel(t, ws)

	m, cmd := typeKeys(t, m, "n")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	// The new node has reserved a cell; walk the existing node onto it before
	// tmux confirms.
	reserved := m.pending[0].Pos
	for m.ws.Nodes[0].Pos != reserved {
		step := state.Cell{Col: sign(reserved.Col - m.ws.Nodes[0].Pos.Col)}
		if step.Col == 0 {
			step = state.Cell{Row: sign(reserved.Row - m.ws.Nodes[0].Pos.Row)}
		}
		m = m.moveNode(step, 1)
	}
	m = settle(t, m, cmd)

	seen := map[state.Cell]string{}
	for _, n := range m.ws.Nodes {
		if other, taken := seen[n.Pos]; taken {
			t.Fatalf("nodes %q and %q both sit on %+v", other, n.ID, n.Pos)
		}
		seen[n.Pos] = n.ID
	}
	if _, ok := m.selected(); !ok {
		t.Errorf("cursor at %+v is on no node after the create landed", m.ws.Viewport.Cursor)
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
