package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// bindings is every key the map answers to (§7.3). The palette is the
// discoverability backstop, so an action reachable only by its binding is a
// bug — this is the list that says so.
var bindings = []string{
	"enter", " ", "n", "N", "A", "x", "q", "r", "c", "C", "t", "s",
	"tab", "shift+tab", "w", "/", "esc", "v", "g",
	"h", "j", "k", "l", "H", "J", "K", "L", "0", "z",
}

func TestEveryBindingHasAPaletteEntry(t *testing.T) {
	offered, labels := map[string]bool{}, map[string]bool{}
	for _, c := range commands {
		if c.label == "" {
			t.Errorf("the command bound to %v has no name, so the palette cannot offer it", c.keys)
		}
		if labels[c.label] {
			t.Errorf("two commands are called %q", c.label)
		}
		labels[c.label] = true
		for _, k := range c.keys {
			offered[k] = true
		}
	}
	for _, key := range bindings {
		if !offered[key] {
			t.Errorf("%q does something on the map but has no palette entry", key)
		}
	}
}

func TestCtrlKAndColonOpenTheSamePalette(t *testing.T) {
	byChord, _ := press(t, filterMap(t), tea.KeyCtrlK)
	byColon, _ := typeKeys(t, filterMap(t), ":")

	if byChord.mode != modePalette || byColon.mode != modePalette {
		t.Fatalf("Ctrl-K and : should both open the palette, got %v and %v", byChord.mode, byColon.mode)
	}
	if len(byChord.palette) != len(byColon.palette) || len(byChord.palette) == 0 {
		t.Errorf("both keys should open the same palette, got %d and %d entries",
			len(byChord.palette), len(byColon.palette))
	}
}

// twoMaps is a workspace with a node on it and another workspace, elsewhere,
// with a node of its own — which is what "any node in any workspace" needs.
func twoMaps(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions, _ := newWorkspaceModel(t,
		state.Workspace{Name: "main", Nodes: []state.Node{{ID: "aaa", Kind: state.KindShell, Title: "here"}}},
		state.Workspace{Name: "far", Nodes: []state.Node{
			{ID: "bbb", Kind: state.KindShell, Title: "away", Pos: state.Cell{Col: 3, Row: 2}},
		}})
	return m, sessions
}

func TestThePaletteMatchesNodesInEveryWorkspace(t *testing.T) {
	m, _ := twoMaps(t)
	open, _ := press(t, m, tea.KeyCtrlK)
	open, _ = typeKeys(t, open, "away")

	matches := open.paletteMatches()
	if len(matches) == 0 || matches[0].label != "away" {
		t.Fatalf("the palette should offer a node from another workspace, got %d matches", len(matches))
	}
	if !strings.Contains(matches[0].detail, "far") {
		t.Errorf("the entry should say which workspace the node is on, got %q", matches[0].detail)
	}
}

func TestChoosingANodeElsewhereSwitchesWorkspaceAndPlacesTheCursor(t *testing.T) {
	m, _ := twoMaps(t)
	open, _ := press(t, m, tea.KeyCtrlK)
	open, _ = typeKeys(t, open, "away")

	chosen, _ := press(t, open, tea.KeyEnter)
	if chosen.ws.Name != "far" {
		t.Fatalf("choosing a node elsewhere should switch workspace, on %q", chosen.ws.Name)
	}
	if got, want := chosen.ws.Viewport.Cursor, (state.Cell{Col: 3, Row: 2}); got != want {
		t.Errorf("cursor = %+v, want %+v — the node chosen is the one selected", got, want)
	}
	if chosen.mode != modeNormal {
		t.Errorf("the palette should close behind the entry it ran, mode = %v", chosen.mode)
	}
}

// The palette matches every node there is; a filter matches only some. Jumping
// to one the filter hides has to show it, or the cursor lands on a card that is
// not on screen.
func TestJumpingToANodeClearsAFilterHidingIt(t *testing.T) {
	m := filterMap(t)
	narrowed, _ := typeKeys(t, m, "/api")
	narrowed, _ = press(t, narrowed, tea.KeyEnter)

	open, _ := press(t, narrowed, tea.KeyCtrlK)
	open, _ = typeKeys(t, open, "scratch")
	jumped, _ := press(t, open, tea.KeyEnter)

	if jumped.filter != "" {
		t.Errorf("the jump should clear the filter that was hiding its node, got %q", jumped.filter)
	}
	if node, ok := jumped.selected(); !ok || node.Title != "scratch" {
		t.Errorf("the cursor should be on the node chosen, selected = %+v (%t)", node, ok)
	}
}

func TestThePaletteSwitchesWorkspace(t *testing.T) {
	m, _ := twoMaps(t)
	open, _ := press(t, m, tea.KeyCtrlK)
	open, _ = typeKeys(t, open, "Workspace far")

	chosen, _ := press(t, open, tea.KeyEnter)
	if chosen.ws.Name != "far" {
		t.Errorf("the palette should switch workspace, on %q", chosen.ws.Name)
	}
}

func TestThePaletteOffersAdoptionCandidates(t *testing.T) {
	m, sessions, _ := newWorkspaceModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{foreign}

	open, cmd := press(t, m, tea.KeyCtrlK)
	open = settle(t, open, cmd)

	found := false
	for _, e := range open.palette {
		found = found || strings.Contains(e.label, foreign)
	}
	if !found {
		t.Fatalf("the palette should offer the sessions there are to adopt, got %d entries", len(open.palette))
	}

	typed, _ := typeKeys(t, open, foreign)
	adopted, _ := press(t, typed, tea.KeyEnter)
	if len(adopted.ws.Nodes) != 1 || adopted.ws.Nodes[0].Session != foreign {
		t.Errorf("choosing a candidate should put a card over the session, nodes = %+v", adopted.ws.Nodes)
	}
}

// The palette asks tmux what there is to adopt, and tmux answers in its own
// time. An answer that outlives the palette must open nothing: the picker
// taking the keyboard would turn the next Enter into an adoption of a session
// nobody was looking at.
func TestAClosedPaletteDoesNotOpenTheAdoptionPickerBehindIt(t *testing.T) {
	m, sessions, _ := newWorkspaceModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{foreign}

	open, ask := press(t, m, tea.KeyCtrlK)
	closed, _ := press(t, open, tea.KeyEsc)

	late, _ := closed.Update(ask())
	after := late.(Model)
	if after.mode != modeNormal {
		t.Errorf("a late answer to the palette should open nothing, mode = %v", after.mode)
	}
	if after.status != "" {
		t.Errorf("nor say anything about it, status = %q", after.status)
	}
}

// A filter is a way of looking at one map. The next map is not the one it was
// typed at, and the cursor it arrives with may be on a card the filter hides.
func TestAWorkspaceSwitchClearsTheFilter(t *testing.T) {
	m, _ := twoMaps(t)
	narrowed, _ := typeKeys(t, m, "/here")
	narrowed, _ = press(t, narrowed, tea.KeyEnter)

	switched, _ := press(t, narrowed, tea.KeyTab)
	if switched.ws.Name == "main" {
		t.Fatal("Tab should have switched workspace")
	}
	if switched.filter != "" {
		t.Errorf("the filter should not follow you to another map, got %q", switched.filter)
	}
}

// The palette is what a new user is told to press, so it has to work on the map
// a new user has: nothing on it, and no workspace but the one they started in.
func TestThePaletteOpensOnAnEmptyMap(t *testing.T) {
	m, _, _ := newWorkspaceModel(t, state.Workspace{Name: "main"})

	open, cmd := press(t, m, tea.KeyCtrlK)
	open = settle(t, open, cmd)

	if open.mode != modePalette {
		t.Fatalf("the palette should open, mode = %v", open.mode)
	}
	if len(open.paletteMatches()) == 0 {
		t.Error("the commands are always there, whatever is on the map")
	}
	if open.View() == "" {
		t.Error("the palette should draw something")
	}
}

func TestAPaletteCommandRunsItsBinding(t *testing.T) {
	open, _ := press(t, filterMap(t), tea.KeyCtrlK)
	open, _ = typeKeys(t, open, "New shell")

	next, _ := press(t, open, tea.KeyEnter)
	if next.mode != modeTitle {
		t.Errorf("running the new-shell command should open the title prompt, mode = %v", next.mode)
	}
}

func TestTypingNarrowsThePaletteAndEscCloses(t *testing.T) {
	open, _ := press(t, filterMap(t), tea.KeyCtrlK)
	all := len(open.paletteMatches())

	narrowed, _ := typeKeys(t, open, "colour")
	if got := len(narrowed.paletteMatches()); got == 0 || got >= all {
		t.Errorf("typing should narrow the palette, %d of %d matched", got, all)
	}

	closed, _ := press(t, narrowed, tea.KeyEsc)
	if closed.mode != modeNormal {
		t.Errorf("Esc should close the palette, mode = %v", closed.mode)
	}
}
