package tui

// Groups (SPEC §6, §7.1): created with `g`, membership by containment alone.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// groupedMap is two nodes already inside a group's rect and one outside it,
// with the cursor on the leftmost.
func groupedMap(t *testing.T, rect state.Rect) Model {
	t.Helper()
	return newModel(t, config.Default(), state.Workspace{Name: "main",
		Nodes: []state.Node{
			{ID: "aaa", Kind: state.KindShell, Title: "api"},
			{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1}},
			{ID: "ccc", Kind: state.KindShell, Title: "db", Pos: state.Cell{Col: 4}},
		},
		Groups: []state.Group{{ID: "g1", Title: "infra", Colour: "cyan", Rect: rect}},
	})
}

// wholeRect is the group both of the first two nodes sit in, with room to spare.
var wholeRect = state.Rect{Max: state.Cell{Col: 2, Row: 2}}

func TestGOnASelectionAsksForANameAndMakesAGroup(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "g")
	if m.mode != modeGroupTitle {
		t.Fatalf("g on a selection should ask for a name, mode is %v", m.mode)
	}
	m, _ = typeKeys(t, m, "infra")
	m, _ = press(t, m, tea.KeyEnter)

	if len(m.ws.Groups) != 1 {
		t.Fatalf("g should have made one group, got %+v", m.ws.Groups)
	}
	g := m.ws.Groups[0]
	if g.Title != "infra" {
		t.Errorf("the group is called %q, want the name that was typed", g.Title)
	}
	for _, id := range []string{"aaa", "bbb"} {
		if n, _ := m.node(id); !g.Rect.Contains(n.Pos) {
			t.Errorf("%s at %+v is outside the group it was made from %+v", id, n.Pos, g.Rect)
		}
	}
	if n, _ := m.node("ccc"); g.Rect.Contains(n.Pos) {
		t.Errorf("ccc was not selected but sits inside %+v", g.Rect)
	}
}

func TestTheGroupIsTightRatherThanSprawling(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api"},
		{ID: "zzz", Kind: state.KindShell, Title: "far", Pos: state.Cell{Col: 9, Row: 6}},
	}})
	m = pressKeys(t, m, "v", "l", "g")
	m, _ = typeKeys(t, m, "pair")
	m, _ = press(t, m, tea.KeyEnter)

	cols, rows := m.ws.Groups[0].Rect.Size()
	if cols*rows > 2 {
		t.Errorf("two nodes at opposite ends should gather tight, got a %dx%d rect", cols, rows)
	}
}

func TestEscLeavesTheMapExactlyAsItWas(t *testing.T) {
	m, _ := visualMap(t)
	before := append([]state.Node(nil), m.ws.Nodes...)
	m = pressKeys(t, m, "v", "l", "g")
	m, _ = press(t, m, tea.KeyEsc)

	if len(m.ws.Groups) != 0 {
		t.Errorf("esc should have made no group, got %+v", m.ws.Groups)
	}
	for i, n := range m.ws.Nodes {
		if n.Pos != before[i].Pos {
			t.Errorf("%s moved to %+v for a group that was never made", n.ID, n.Pos)
		}
	}
}

func TestGWithTheCursorInAGroupAddsTheSelectionToIt(t *testing.T) {
	m := groupedMap(t, wholeRect)
	// Out to the node outside, gather it, and back in — the walk in gathers
	// what it passes, which is what makes the cursor end up over the group.
	m = pressKeys(t, m, "l", "l", "v", "h", "g")

	if m.mode != modeNormal {
		t.Fatalf("adding to a group should not ask for a name, mode is %v", m.mode)
	}
	if len(m.ws.Groups) != 1 {
		t.Fatalf("g over a group should join it, not make another: %+v", m.ws.Groups)
	}
	if n, _ := m.node("ccc"); !m.ws.Groups[0].Rect.Contains(n.Pos) {
		t.Errorf("ccc at %+v did not join the group %+v", n.Pos, m.ws.Groups[0].Rect)
	}
}

func TestJoiningGrowsTheRectWhenNoCellInsideIsFree(t *testing.T) {
	full := state.Rect{Max: state.Cell{Col: 2, Row: 1}}
	m := groupedMap(t, full)
	m = pressKeys(t, m, "l", "l", "v", "h", "g")

	rect := m.ws.Groups[0].Rect
	if rect == full {
		t.Fatalf("every cell was taken, so the rect should have grown: %+v", rect)
	}
	for _, id := range []string{"aaa", "bbb", "ccc"} {
		if n, _ := m.node(id); !rect.Contains(n.Pos) {
			t.Errorf("%s at %+v is outside the grown rect %+v", id, n.Pos, rect)
		}
	}
}

func TestACardSaysWhichGroupItIsIn(t *testing.T) {
	m := groupedMap(t, wholeRect)
	if got := m.groupOf(state.Node{Pos: state.Cell{Col: 1}}); got != "infra" {
		t.Errorf("a node inside the rect is in %q, want infra", got)
	}
	if got := m.groupOf(state.Node{Pos: state.Cell{Col: 4}}); got != "" {
		t.Errorf("a node outside every rect is in %q, want no group", got)
	}

	node, _ := m.node("aaa")
	lines := card(node, false, false, false, false, m.groupOf(node), 0, nil)
	if bottom := lines[len(lines)-1]; !strings.Contains(bottom, "infra") {
		t.Errorf("a member's bottom border should name its group, got %q", bottom)
	}
}

func TestMovingANodeOutOfTheRectTakesItOutOfTheGroup(t *testing.T) {
	m := groupedMap(t, wholeRect)
	// L on the leftmost shoves its neighbour out of the far side of the rect.
	m = pressKeys(t, m, "L")

	pushed, _ := m.node("bbb")
	if m.ws.Groups[0].Rect.Contains(pushed.Pos) {
		t.Fatalf("bbb at %+v should have left the rect %+v", pushed.Pos, m.ws.Groups[0].Rect)
	}
	if got := m.groupOf(pushed); got != "" {
		t.Errorf("bbb is outside the rect but its card still says %q", got)
	}
	if moved, _ := m.node("aaa"); m.groupOf(moved) != "infra" {
		t.Errorf("aaa is still inside the rect and should still say so")
	}
}

func TestShrinkingTheRectPastANodeTakesItOutToo(t *testing.T) {
	m := groupedMap(t, wholeRect)
	m.ws.Groups[0].Rect = state.Rect{Max: state.Cell{Col: 1, Row: 1}}

	if n, _ := m.node("bbb"); m.groupOf(n) != "" {
		t.Errorf("bbb was left behind by the rect and should be in no group")
	}
	if n, _ := m.node("aaa"); m.groupOf(n) != "infra" {
		t.Errorf("aaa is still inside the shrunken rect")
	}
}

func TestTheGroupIsDrawnWithItsTitleInItsTopBorder(t *testing.T) {
	m := groupedMap(t, wholeRect)
	view := m.View()

	// Twice at least: once in the rectangle's own border, and once on each
	// member card's bottom border.
	if strings.Count(view, "infra") < 2 {
		t.Errorf("the group should be drawn and named on its members, got:\n%s", view)
	}
	if !strings.Contains(view, "╭─ infra") {
		t.Errorf("the group's title belongs in its top border, got:\n%s", view)
	}
}

func TestAGroupSurvivesAReloadAndIsStillDerivedFromGeometry(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "g")
	m, _ = typeKeys(t, m, "infra")
	m, _ = press(t, m, tea.KeyEnter)

	reloaded, err := state.Load(m.stateDir, "main")
	if err != nil {
		t.Fatalf("reloading the workspace: %v", err)
	}
	if len(reloaded.Groups) != 1 {
		t.Fatalf("the group did not survive the reload: %+v", reloaded.Groups)
	}
	for _, id := range []string{"aaa", "bbb"} {
		n, _ := m.node(id)
		if g, ok := reloaded.GroupAt(n.Pos); !ok || g.Title != "infra" {
			t.Errorf("%s at %+v is in no group after reload", id, n.Pos)
		}
	}
}

func TestGOnAnEmptyCellDoesNothing(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main"})
	m = pressKeys(t, m, "g")
	if m.mode != modeNormal || len(m.ws.Groups) != 0 {
		t.Errorf("g with nothing under the cursor should do nothing, mode %v groups %+v", m.mode, m.ws.Groups)
	}
}

// TestARectFarBiggerThanTheMapIsStillJustAFrame is what a hand-edited workspace
// file can hold. Nothing bounds the numbers in it, and drawing a rect cell by
// cell without clipping it first would spend the screen's worth of work per
// cell it claims.
func TestARectFarBiggerThanTheMapIsStillJustAFrame(t *testing.T) {
	m := groupedMap(t, state.Rect{
		Min: state.Cell{Col: -1_000_000, Row: -1_000_000},
		Max: state.Cell{Col: 1_000_000, Row: 1_000_000},
	})
	done := make(chan string, 1)
	go func() { done <- m.View() }()
	select {
	case view := <-done:
		if strings.Contains(view, "╭─ infra") {
			t.Errorf("the rect's corner is far off screen and should not be drawn, got:\n%s", view)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rendering a huge rect did not finish")
	}
}

func TestNamingAGroupOfNodesThatHaveGoneMakesNothing(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "g")
	m.ws.Nodes = nil // the map moved on while the prompt was up
	m, _ = typeKeys(t, m, "infra")
	m, _ = press(t, m, tea.KeyEnter)

	if len(m.ws.Groups) != 0 {
		t.Errorf("a group with no cells in it should not be made, got %+v", m.ws.Groups)
	}
}

// TestANewGroupIsNotDrawnInsideAnother is what containment costs: GroupAt hands
// a cell to the first group holding it, so a rect inside another rect would be
// a group its own members were never in.
func TestANewGroupIsNotDrawnInsideAnother(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main",
		Nodes: []state.Node{
			{ID: "aaa", Kind: state.KindShell, Title: "api", Pos: state.Cell{Col: 5, Row: 5}},
			{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 6, Row: 5}},
		},
		Groups:   []state.Group{{ID: "g1", Title: "big", Colour: "cyan", Rect: state.Rect{Max: state.Cell{Col: 10, Row: 10}}}},
		Viewport: state.Viewport{Cursor: state.Cell{Col: 5, Row: 5}},
	})
	// The cursor is moved off the group first, or g would join it rather than
	// make one; the selection is gathered before the walk out.
	m = pressKeys(t, m, "v", "l")
	m.ws.Viewport.Cursor = state.Cell{Col: 20, Row: 20}
	m = pressKeys(t, m, "g")
	m, _ = typeKeys(t, m, "small")
	m, _ = press(t, m, tea.KeyEnter)

	if len(m.ws.Groups) != 2 {
		t.Fatalf("expected the new group alongside the old, got %+v", m.ws.Groups)
	}
	small := m.ws.Groups[1]
	if small.Rect.Overlaps(m.ws.Groups[0].Rect) {
		t.Fatalf("the new rect %+v was drawn inside %+v", small.Rect, m.ws.Groups[0].Rect)
	}
	for _, id := range []string{"aaa", "bbb"} {
		if n, _ := m.node(id); m.groupOf(n) != "small" {
			t.Errorf("%s is in %q rather than the group it was just put in", id, m.groupOf(n))
		}
	}
}

func TestAGroupNameDoesNotCostACardItsTags(t *testing.T) {
	n := state.Node{ID: "k4f2", Kind: state.KindShell, Title: "api", Tags: []string{"prod"}}
	lines := card(n, false, false, false, false, "infrastructure", 0, nil)

	bottom := lines[len(lines)-1]
	if !strings.Contains(bottom, "#") {
		t.Errorf("the group took the whole border and left no tag at all: %q", bottom)
	}
	if !strings.Contains(bottom, "inf") {
		t.Errorf("the bottom border should still name the group, got %q", bottom)
	}
}

func TestEveryLineOfTheMapIsTheSameWidth(t *testing.T) {
	// A group title of double-width runes, on a rect running off the side of the
	// screen: a rune drawn half in and half out of the frame would leave one row
	// wider than the rest, and the whole map centred against it.
	m := groupedMap(t, state.Rect{Max: state.Cell{Col: 40, Row: 2}})
	m.ws.Groups[0].Title = "a" + strings.Repeat("漢", 30)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	m = sized.(Model)

	lines := strings.Split(m.View(), "\n")
	for i, line := range lines {
		if got := lipgloss.Width(line); got != lipgloss.Width(lines[0]) {
			t.Fatalf("line %d is %d cells wide, line 0 is %d:\n%s", i, got, lipgloss.Width(lines[0]), m.View())
		}
	}
}
