package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// newWorkspaceModel opens on ws with others already on disk, which is what makes
// a workspace switch a thing that can happen at all.
func newWorkspaceModel(t *testing.T, ws state.Workspace, others ...state.Workspace) (Model, *fakeSessions, string) {
	t.Helper()
	dir := t.TempDir()
	// The workspace being opened is on disk too: a workspace anyone has used is
	// a file, and deleting the one you are on has to be a thing that can happen.
	for _, other := range append([]state.Workspace{ws}, others...) {
		if err := state.Save(dir, other); err != nil {
			t.Fatalf("saving workspace %q: %v", other.Name, err)
		}
	}
	sessions := &fakeSessions{}
	m := New(config.Default(), ws, dir, sessions)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model), sessions, dir
}

func TestTabCyclesWorkspacesInBothDirections(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{Name: "main"},
		state.Workspace{Name: "scratch"}, state.Workspace{Name: "work"})

	next, _ := press(t, m, tea.KeyTab)
	if next.ws.Name != "scratch" {
		t.Errorf("Tab from main went to %q, want scratch — the workspaces cycle in name order", next.ws.Name)
	}
	// The cycle wraps: the workspace before the first is the last.
	back, _ := press(t, m, tea.KeyShiftTab)
	if back.ws.Name != "work" {
		t.Errorf("Shift-Tab from main went to %q, want work", back.ws.Name)
	}
}

// Each workspace's cursor and viewport are its own (§7.1): switching away and
// back returns you to where you were looking, not to the origin.
func TestSwitchingKeepsEachWorkspacesOwnViewport(t *testing.T) {
	far := state.Workspace{
		Name:     "far",
		Nodes:    []state.Node{{ID: "away", Kind: state.KindShell, Title: "away", Pos: state.Cell{Col: 9, Row: 9}}},
		Viewport: state.Viewport{Cursor: state.Cell{Col: 9, Row: 9}, Offset: state.Cell{Col: 8, Row: 8}},
	}
	m, _, dir := newWorkspaceModel(t, state.Workspace{
		Name:     "main",
		Nodes:    []state.Node{{ID: "here", Kind: state.KindShell, Title: "here"}},
		Viewport: state.Viewport{Cursor: state.Cell{Col: 2, Row: 1}, Offset: state.Cell{Col: 1, Row: 1}},
	}, far)

	// far sorts before main, so Shift-Tab from main lands on it.
	there, _ := press(t, m, tea.KeyShiftTab)
	if there.ws.Name != "far" {
		t.Fatalf("switched to %q, want far", there.ws.Name)
	}
	if got := there.ws.Viewport; got != far.Viewport {
		t.Errorf("viewport = %+v, want %+v — a workspace is arrived at where it was left", got, far.Viewport)
	}

	home, _ := press(t, there, tea.KeyShiftTab)
	if home.ws.Name != "main" {
		t.Fatalf("switched back to %q, want main", home.ws.Name)
	}
	if got, want := home.ws.Viewport.Cursor, (state.Cell{Col: 2, Row: 1}); got != want {
		t.Errorf("cursor = %+v, want %+v", got, want)
	}
	// And it survives a restart, because leaving a workspace writes it.
	saved, err := state.Load(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Viewport.Cursor, (state.Cell{Col: 2, Row: 1}); got != want {
		t.Errorf("saved cursor = %+v, want %+v", got, want)
	}
}

// Previews, unread marks, and the dead set are about the sessions of one map.
// Carrying them across a switch would show one workspace's output on another's
// cards, since node ids are only unique against the map they are on.
func TestSwitchingDropsWhatWasDerivedFromTheOldMap(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	}, state.Workspace{Name: "scratch"})
	m.previews = map[string][]string{"k4f2": {"200 GET /health"}}
	m.unread = map[string]bool{"k4f2": true}
	m.dead = map[string]bool{"k4f2": true}
	m.pending = []state.Node{{ID: "n3wx", Kind: state.KindShell}}

	next, _ := press(t, m, tea.KeyTab)
	if len(next.previews) != 0 || len(next.unread) != 0 || len(next.dead) != 0 || len(next.pending) != 0 {
		t.Errorf("the old map's state came along: previews=%v unread=%v dead=%v pending=%v",
			next.previews, next.unread, next.dead, next.pending)
	}
}

// A node whose session was still being made when the map was left belongs to the
// map that made it, and must not land on the one now on screen.
func TestACreateFromAnotherWorkspaceNeverLandsOnThisMap(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{Name: "main"})

	next, _ := m.Update(nodeCreatedMsg{node: state.Node{ID: "els3", Kind: state.KindShell, Title: "elsewhere"}})
	if len(next.(Model).ws.Nodes) != 0 {
		t.Errorf("a create nobody was waiting for was placed anyway: %+v", next.(Model).ws.Nodes)
	}
}

func TestWorkspacePickerSwitchesOnEnter(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{Name: "main"}, state.Workspace{Name: "scratch"})

	m, _ = typeKeys(t, m, "w")
	if !strings.Contains(m.View(), "main") {
		t.Errorf("the picker should open on the workspace you are in, got:\n%s", m.View())
	}
	m, _ = typeKeys(t, m, "j")
	if !strings.Contains(m.View(), "scratch") {
		t.Errorf("j should offer the next workspace, got:\n%s", m.View())
	}

	chosen, _ := press(t, m, tea.KeyEnter)
	if chosen.ws.Name != "scratch" {
		t.Errorf("Enter opened %q, want scratch", chosen.ws.Name)
	}
	if chosen.mode != modeNormal {
		t.Error("the picker should close once a workspace is open")
	}
}

func TestPickerCreatesAWorkspaceAndOpensIt(t *testing.T) {
	m, _, dir := newWorkspaceModel(t, state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	})

	m, _ = typeKeys(t, m, "w")
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "scratch")
	created, _ := press(t, m, tea.KeyEnter)

	if created.ws.Name != "scratch" {
		t.Fatalf("opened %q, want the workspace just created", created.ws.Name)
	}
	if len(created.ws.Nodes) != 0 {
		t.Errorf("a new workspace shares no nodes, got %+v", created.ws.Nodes)
	}
	names, err := state.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "main,scratch" {
		t.Errorf("workspaces on disk = %v, want both main and scratch", names)
	}
}

// The name is carried into every session name this workspace makes, so the
// prompt holds the same boundary state.ValidName does rather than writing a file
// tmux could never be asked about.
func TestANewWorkspaceNameTmuxCannotCarryIsRefused(t *testing.T) {
	m, _, dir := newWorkspaceModel(t, state.Workspace{Name: "main"})

	m, _ = typeKeys(t, m, "w")
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "my ws")
	refused, _ := press(t, m, tea.KeyEnter)

	if refused.ws.Name != "main" {
		t.Errorf("opened %q; an invalid name must leave the map where it was", refused.ws.Name)
	}
	if !strings.Contains(refused.View(), "whitespace") {
		t.Errorf("the refusal should say what is wrong with the name, got:\n%s", refused.View())
	}
	if names, _ := state.List(dir); strings.Join(names, ",") != "main" {
		t.Errorf("a refused name still wrote a file: %v", names)
	}
}

// Deleting a workspace kills nothing (§5.2), and the confirmation says so: the
// sessions its nodes named go on running whether or not anything is mapping them.
func TestDeletingAWorkspaceKeepsItsSessionsRunning(t *testing.T) {
	m, sessions, dir := newWorkspaceModel(t, state.Workspace{Name: "main"}, state.Workspace{
		Name:  "scratch",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	})

	m, _ = typeKeys(t, m, "w")
	m, _ = typeKeys(t, m, "j") // scratch, the one that is not open
	asking, _ := typeKeys(t, m, "x")
	if !strings.Contains(asking.View(), "keep running") {
		t.Errorf("the confirmation must say the sessions survive, got:\n%s", asking.View())
	}
	declined, _ := typeKeys(t, asking, "n")
	if names, _ := state.List(dir); len(names) != 2 {
		t.Errorf("n deleted it anyway: %v", names)
	}
	if declined.mode == modeNormal {
		t.Error("declining should leave the picker up rather than dismiss it entirely")
	}

	gone, _ := typeKeys(t, asking, "y")
	names, err := state.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "main" {
		t.Errorf("workspaces on disk = %v, want main alone", names)
	}
	if len(sessions.killed) != 0 {
		t.Errorf("deleting a workspace killed sessions: %v", sessions.killed)
	}
	if gone.ws.Name != "main" {
		t.Errorf("deleting another workspace moved the map to %q", gone.ws.Name)
	}
}

func TestDeletingTheOpenWorkspaceOpensAnother(t *testing.T) {
	m, _, dir := newWorkspaceModel(t, state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	}, state.Workspace{Name: "scratch"})

	m, _ = typeKeys(t, m, "w") // opens on main, the one in front of us
	m, _ = typeKeys(t, m, "x")
	gone, _ := typeKeys(t, m, "y")

	if gone.ws.Name != "scratch" {
		t.Errorf("after deleting the open workspace the map shows %q, want scratch", gone.ws.Name)
	}
	names, err := state.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "scratch" {
		t.Errorf("workspaces on disk = %v — the deleted one must not be written back", names)
	}
}

// There is always a map to be on. Deleting the last workspace would leave the
// view with nothing to render and nothing to switch to.
func TestTheLastWorkspaceCannotBeDeleted(t *testing.T) {
	m, _, dir := newWorkspaceModel(t, state.Workspace{Name: "main"})

	m, _ = typeKeys(t, m, "w")
	refused, _ := typeKeys(t, m, "x")

	if refused.ws.Name != "main" {
		t.Errorf("the map left main for %q", refused.ws.Name)
	}
	if !strings.Contains(refused.View(), "last workspace") {
		t.Errorf("the refusal should say why, got:\n%s", refused.View())
	}
	if names, _ := state.List(dir); len(names) != 1 {
		t.Errorf("workspaces on disk = %v, want main kept", names)
	}
}

// A new node's session starts in the workspace's own default directory, and is
// named after the workspace it was made in — so both have to follow a switch.
func TestANodeMadeAfterASwitchBelongsToTheNewWorkspace(t *testing.T) {
	m, sessions, _ := newWorkspaceModel(t, state.Workspace{Name: "main", Dir: "/tmp/main-dir"},
		state.Workspace{Name: "work", Dir: "/tmp/work-dir"})

	m, _ = press(t, m, tea.KeyTab) // main → work
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	m, cmd := press(t, m, tea.KeyEnter)
	settle(t, m, cmd)

	if len(sessions.created) != 1 {
		t.Fatalf("created %d sessions, want 1", len(sessions.created))
	}
	if got := sessions.created[0].dir; got != "/tmp/work-dir" {
		t.Errorf("session started in %q, want the new workspace's directory", got)
	}
	if got := sessions.created[0].session; !strings.HasPrefix(got, "trig_work_") {
		t.Errorf("session named %q, want it named after the workspace it was made in", got)
	}
	if got := sessions.created[0].env["TRIG_WORKSPACE"]; got != "work" {
		t.Errorf("session provenance says workspace %q, want work", got)
	}
}

// Every question Trigpoint asks tmux is asked off the update loop, and a switch
// can land in between. An answer about the map that asked it must not be applied
// to the map that replaced it.
func TestAReconcilePassForAnotherWorkspaceIsDropped(t *testing.T) {
	m, sessions, dir := newWorkspaceModel(t, state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	}, state.Workspace{Name: "scratch"})
	// A session of main's with no card on main: exactly what a pending node
	// dropped by a switch leaves behind, and what a pass reconstructs.
	sessions.live = []string{"trig_main_orph"}
	sessions.envs = map[string]map[string]string{
		"trig_main_orph": {"TRIG_WORKSPACE": "main", "TRIG_NODE_ID": "orph", "TRIG_NODE_KIND": "shell"},
	}
	answer := m.reconcile()()

	elsewhere, _ := press(t, m, tea.KeyTab)
	if elsewhere.ws.Name != "scratch" {
		t.Fatalf("switched to %q, want scratch", elsewhere.ws.Name)
	}
	after, _ := elsewhere.Update(answer)

	if nodes := after.(Model).ws.Nodes; len(nodes) != 0 {
		t.Errorf("main's session became a card on scratch: %+v", nodes)
	}
	if len(after.(Model).dead) != 0 {
		t.Errorf("main's verdict on liveness was applied to scratch: %v", after.(Model).dead)
	}
	saved, err := state.Load(dir, "scratch")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Nodes) != 0 {
		t.Errorf("and it was written to scratch's file: %+v", saved.Nodes)
	}
}

// Node ids are unique against one map, not against the state directory, so a
// snapshot taken for one workspace must never be folded into another's cards.
func TestASnapshotFromAnotherWorkspaceIsDropped(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	}, state.Workspace{
		Name:  "scratch",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "notes"}},
	})
	taken := capturedMsg{mapStamp: m.stamp(), previews: map[string][]string{"k4f2": {"200 GET /health"}}}

	elsewhere, _ := press(t, m, tea.KeyTab)
	after, _ := elsewhere.Update(taken)

	if got := after.(Model).previews["k4f2"]; len(got) != 0 {
		t.Errorf("scratch's card shows main's output: %v", got)
	}
}

// A kill answered after the switch belongs to the map that asked for it. Removing
// the id from this map instead would take a card off the wrong workspace.
func TestAKillFromAnotherWorkspaceIsDropped(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{
		Name:  "main",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "api"}},
	}, state.Workspace{
		Name:  "scratch",
		Nodes: []state.Node{{ID: "k4f2", Kind: state.KindShell, Title: "notes"}},
	})
	killed := nodeKilledMsg{mapStamp: m.stamp(), id: "k4f2"}

	elsewhere, _ := press(t, m, tea.KeyTab)
	after, _ := elsewhere.Update(killed)

	if nodes := after.(Model).ws.Nodes; len(nodes) != 1 {
		t.Errorf("main's kill removed scratch's card: %+v", nodes)
	}
}

// The workspace being left is written on the way out, so a write that fails is
// the one moment the user has to hear about it: leaving anyway discards where
// they were.
func TestASwitchThatCannotWriteStaysWhereItIs(t *testing.T) {
	m, _, dir := newWorkspaceModel(t, state.Workspace{Name: "main"}, state.Workspace{Name: "scratch"})
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	stayed, _ := press(t, m, tea.KeyTab)

	if stayed.ws.Name != "main" {
		t.Errorf("switched to %q with the map unsaved", stayed.ws.Name)
	}
	if stayed.status == "" {
		t.Error("a failed write must be reported rather than swallowed by the switch")
	}
}

// Falling back after a delete goes where Tab would have gone, rather than to
// whichever workspace happens to sort first.
func TestDeletingTheOpenWorkspaceOpensTheNextInTheCycle(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{Name: "bee"},
		state.Workspace{Name: "ay"}, state.Workspace{Name: "cee"})

	m, _ = typeKeys(t, m, "w") // opens on bee
	m, _ = typeKeys(t, m, "x")
	gone, _ := typeKeys(t, m, "y")

	if gone.ws.Name != "cee" {
		t.Errorf("deleting bee opened %q, want cee — the one Tab would have reached", gone.ws.Name)
	}
}

// A workspace name becomes a filename, and a filename's limit counts bytes. The
// prompt bounds what it collects by the same measure, rather than collecting a
// name it will then refuse.
func TestALongNameIsBoundedRatherThanRefused(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{Name: "main"})

	m, _ = typeKeys(t, m, "w")
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, strings.Repeat("書", 40)) // 120 bytes of perfectly good name
	created, _ := press(t, m, tea.KeyEnter)

	if created.ws.Name == "main" {
		t.Fatalf("the name was refused rather than bounded, got:\n%s", created.View())
	}
	if len(created.ws.Name) > state.MaxNameLen {
		t.Errorf("the name is %d bytes, more than a workspace file may be", len(created.ws.Name))
	}
}
