package tui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// mapWith builds a workspace whose nodes sit on the given cells, named n0, n1,
// … in the order they are listed, with the cursor on the first of them.
func mapWith(t *testing.T, cells ...state.Cell) Model {
	t.Helper()
	ws := state.Workspace{Name: "main"}
	for i, c := range cells {
		ws.Nodes = append(ws.Nodes, state.Node{
			ID:    string(rune('a' + i)),
			Kind:  state.KindShell,
			Title: string(rune('a' + i)),
			Pos:   c,
		})
	}
	if len(cells) > 0 {
		ws.Viewport.Cursor = cells[0]
	}
	m, err := New(config.Default(), ws, t.TempDir(), &fakeSessions{})
	if err != nil {
		t.Fatal(err)
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model)
}

func pressKeys(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up", "down", "left", "right":
			msg = tea.KeyMsg{Type: map[string]tea.KeyType{
				"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
			}[k]}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func posOf(t *testing.T, m Model, id string) state.Cell {
	t.Helper()
	n, ok := m.node(id)
	if !ok {
		t.Fatalf("node %q is gone", id)
	}
	return n.Pos
}

func visible(m Model, c state.Cell) bool {
	min, max := m.bounds()
	return c.Col >= min.Col && c.Col <= max.Col && c.Row >= min.Row && c.Row <= max.Row
}

func TestCursorSkipsEmptyCellsToTheNextNode(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want state.Cell
	}{
		{"l", state.Cell{Col: 4}},
		{"h", state.Cell{Col: -3}},
		{"j", state.Cell{Row: 2}},
		{"k", state.Cell{Row: -5}},
	} {
		m := mapWith(t, state.Cell{}, state.Cell{Col: 4}, state.Cell{Col: -3}, state.Cell{Row: 2}, state.Cell{Row: -5})
		if got := pressKeys(t, m, tc.key).ws.Viewport.Cursor; got != tc.want {
			t.Errorf("%s moved the cursor to %+v, want %+v", tc.key, got, tc.want)
		}
	}
}

func TestArrowKeysMoveTheCursorToo(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 4})
	if got := pressKeys(t, m, "right").ws.Viewport.Cursor; got != (state.Cell{Col: 4}) {
		t.Errorf("right arrow moved the cursor to %+v, want the node at col 4", got)
	}
}

func TestCursorStaysPutWithNothingInThatDirection(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 4})
	if got := pressKeys(t, m, "h").ws.Viewport.Cursor; got != (state.Cell{}) {
		t.Errorf("cursor wandered to %+v with no node to the left", got)
	}
}

// Nearest means nearest in the direction pressed, not nearest overall: a node
// on the cursor's own row wins over a closer one several rows off it.
func TestCursorPrefersTheNodeOnItsOwnRow(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 1, Row: 3}, state.Cell{Col: 3, Row: 0})
	if got := pressKeys(t, m, "l").ws.Viewport.Cursor; got != (state.Cell{Col: 3}) {
		t.Errorf("cursor moved to %+v, want the node on its own row", got)
	}
}

func TestCountPrefixRepeatsCursorMotion(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 1}, state.Cell{Col: 2}, state.Cell{Col: 3})
	if got := pressKeys(t, m, "3", "l").ws.Viewport.Cursor; got != (state.Cell{Col: 3}) {
		t.Errorf("3l moved the cursor to %+v, want three nodes along", got)
	}
}

func TestCountIsForgottenAfterTheMotion(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 1}, state.Cell{Col: 2}, state.Cell{Col: 3})
	m = pressKeys(t, m, "3", "l", "h")
	if got := m.ws.Viewport.Cursor; got != (state.Cell{Col: 2}) {
		t.Errorf("cursor at %+v, want one node back — the count should not have carried over", got)
	}
}

func TestShiftKeysMoveTheSelectedNode(t *testing.T) {
	m := mapWith(t, state.Cell{})
	m = pressKeys(t, m, "L")
	if got := posOf(t, m, "a"); got != (state.Cell{Col: 1}) {
		t.Errorf("node moved to %+v, want one cell right", got)
	}
	if m.ws.Viewport.Cursor != (state.Cell{Col: 1}) {
		t.Errorf("cursor at %+v, want it to stay on the node it is moving", m.ws.Viewport.Cursor)
	}
}

func TestShiftKeysShoveTheOccupantAside(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 1})
	m = pressKeys(t, m, "L")
	if got := posOf(t, m, "a"); got != (state.Cell{Col: 1}) {
		t.Errorf("mover at %+v, want the cell it pushed into", got)
	}
	if got := posOf(t, m, "b"); got != (state.Cell{Col: 2}) {
		t.Errorf("occupant at %+v, want to have been shoved on", got)
	}
}

func TestCountPrefixRepeatsNodeMotion(t *testing.T) {
	m := mapWith(t, state.Cell{})
	m = pressKeys(t, m, "2", "J")
	if got := posOf(t, m, "a"); got != (state.Cell{Row: 2}) {
		t.Errorf("node moved to %+v, want two cells down", got)
	}
}

func TestShiftKeysDoNothingOnAnEmptyCell(t *testing.T) {
	m := mapWith(t, state.Cell{Col: 4})
	m.ws.Viewport.Cursor = state.Cell{}
	m = pressKeys(t, m, "L")
	if got := posOf(t, m, "a"); got != (state.Cell{Col: 4}) {
		t.Errorf("node at %+v moved with the cursor on an empty cell", got)
	}
}

func TestViewportFollowsTheCursorOffScreen(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 40, Row: 40})
	far := state.Cell{Col: 40, Row: 40}
	if visible(m, far) {
		t.Fatal("the far node should start off screen for this test to mean anything")
	}
	m = pressKeys(t, m, "l", "j")
	if !visible(m, m.ws.Viewport.Cursor) {
		t.Errorf("cursor at %+v is outside the viewport; it should have scrolled to follow", m.ws.Viewport.Cursor)
	}
}

func TestViewportDoesNotScrollWhileTheCursorIsVisible(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 1, Row: 1})
	before := m.ws.Viewport.Offset
	m = pressKeys(t, m, "l")
	if m.ws.Viewport.Offset != before {
		t.Errorf("viewport scrolled to %+v for a cursor it could already see", m.ws.Viewport.Offset)
	}
}

func TestZZCentresTheViewportOnTheCursor(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 40, Row: 40})
	m = pressKeys(t, m, "l", "z", "z")

	// An even number of visible cells cannot be split evenly, so centred means
	// within one cell of the middle.
	min, max := m.bounds()
	cursor := m.ws.Viewport.Cursor
	if lean(cursor.Col-min.Col, max.Col-cursor.Col) || lean(cursor.Row-min.Row, max.Row-cursor.Row) {
		t.Errorf("cursor %+v sits off centre in viewport %+v..%+v", cursor, min, max)
	}
}

func lean(before, after int) bool {
	d := before - after
	return d < -1 || d > 1
}

func TestZAloneDoesNotCentre(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 40, Row: 40})
	m = pressKeys(t, m, "l")
	after := pressKeys(t, m, "z")
	if after.ws.Viewport.Offset != m.ws.Viewport.Offset {
		t.Errorf("a lone z moved the viewport to %+v", after.ws.Viewport.Offset)
	}
}

func TestZeroJumpsToTheOrigin(t *testing.T) {
	m := mapWith(t, state.Cell{Col: 40, Row: 40})
	m = pressKeys(t, m, "0")
	if m.ws.Viewport.Cursor != (state.Cell{}) {
		t.Errorf("cursor at %+v, want the origin", m.ws.Viewport.Cursor)
	}
	if !visible(m, state.Cell{}) {
		t.Errorf("the origin is not in view %+v after jumping to it", m.ws.Viewport.Offset)
	}
}

func TestZeroAfterADigitIsPartOfTheCount(t *testing.T) {
	var cells []state.Cell
	for col := 0; col <= 10; col++ {
		cells = append(cells, state.Cell{Col: col})
	}
	m := pressKeys(t, mapWith(t, cells...), "1", "0", "l")
	if got := m.ws.Viewport.Cursor; got != (state.Cell{Col: 10}) {
		t.Errorf("10l moved the cursor to %+v, want ten nodes along", got)
	}
}

func TestMovementPersists(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "a", Kind: state.KindShell, Pos: state.Cell{}},
		{ID: "b", Kind: state.KindShell, Pos: state.Cell{Col: 40, Row: 40}},
	}}
	dir := t.TempDir()
	m, err := New(config.Default(), ws, dir, &fakeSessions{})
	if err != nil {
		t.Fatal(err)
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = pressKeys(t, sized.(Model), "l", "L")

	saved, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the workspace back: %v", err)
	}
	if saved.Viewport != m.ws.Viewport {
		t.Errorf("saved viewport %+v, want %+v", saved.Viewport, m.ws.Viewport)
	}
	if saved.Nodes[1].Pos != m.ws.Nodes[1].Pos {
		t.Errorf("saved node at %+v, want %+v", saved.Nodes[1].Pos, m.ws.Nodes[1].Pos)
	}
}

func TestAnUnrelatedKeyForgetsTheCount(t *testing.T) {
	m := mapWith(t, state.Cell{}, state.Cell{Col: 1}, state.Cell{Col: 2}, state.Cell{Col: 3})
	m = pressKeys(t, m, "3", "p", "l")
	if got := m.ws.Viewport.Cursor; got != (state.Cell{Col: 1}) {
		t.Errorf("cursor at %+v, want one node along — the count should have been dropped", got)
	}
}

func TestAMotionThatGoesNowhereWritesNothing(t *testing.T) {
	dir := t.TempDir()
	ws := state.Workspace{Name: "main", Nodes: []state.Node{{ID: "a", Kind: state.KindShell}}}
	m, err := New(config.Default(), ws, dir, &fakeSessions{})
	if err != nil {
		t.Fatal(err)
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	pressKeys(t, sized.(Model), "h", "k")

	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("motions that moved nothing wrote %v (err %v)", entries, err)
	}
}
