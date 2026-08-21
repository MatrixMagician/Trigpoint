package tui

// Visual select and bulk operations (SPEC §7.3).

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// visualMap is a row of three shell nodes with the cursor on the leftmost, so
// that l walks along them and a selection is a run the eye can follow.
func visualMap(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api"},
		{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1}},
		{ID: "ccc", Kind: state.KindShell, Title: "db", Pos: state.Cell{Col: 2}},
	}}
	sessions := &fakeSessions{}
	m, err := New(config.Default(), ws, t.TempDir(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model), sessions
}

// settleBatch runs a command and everything it batches, feeding each message
// back the way the Bubble Tea runtime would. settle takes one command at a
// time; a bulk kill is several at once.
func settleBatch(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0; i++ {
		if i > 50 {
			t.Fatal("commands did not settle")
		}
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		msg := next()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		after, cmd := m.Update(msg)
		m, queue = after.(Model), append(queue, cmd)
	}
	return m
}

func TestVSelectsTheNodeUnderTheCursor(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v")

	if got := m.selection; !slices.Equal(got, []string{"aaa"}) {
		t.Errorf("v should select the node under the cursor, got %v", got)
	}
}

func TestVOnAnEmptyCellSelectsNothing(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main"})
	if m = pressKeys(t, m, "v"); len(m.selection) != 0 {
		t.Errorf("v on an empty cell should select nothing, got %v", m.selection)
	}
}

func TestMotionExtendsTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "l")

	if got := m.selection; !slices.Equal(got, []string{"aaa", "bbb", "ccc"}) {
		t.Errorf("the motion keys should extend the selection, got %v", got)
	}
}

// A count prefix is a motion repeated, so it gathers every node it passes
// rather than only the one it lands on.
func TestACountPrefixExtendsAcrossEveryNodeItPasses(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "2", "l")

	if got := m.selection; !slices.Equal(got, []string{"aaa", "bbb", "ccc"}) {
		t.Errorf("2l should gather both nodes it walked over, got %v", got)
	}
}

// Without a selection to extend, the motion keys are how the map is navigated.
func TestMotionAloneSelectsNothing(t *testing.T) {
	m, _ := visualMap(t)
	if m = pressKeys(t, m, "l"); len(m.selection) != 0 {
		t.Errorf("moving the cursor should not start a selection, got %v", m.selection)
	}
}

func TestVAgainDropsTheNodeFromTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "v")

	if got := m.selection; !slices.Equal(got, []string{"aaa"}) {
		t.Errorf("v on a selected node should drop it, got %v", got)
	}
}

func TestEscClearsTheSelectionWithoutActingOnIt(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l")
	before := len(m.ws.Nodes)

	m, _ = press(t, m, tea.KeyEsc)
	if len(m.selection) != 0 {
		t.Errorf("esc should clear the selection, got %v", m.selection)
	}
	if len(m.ws.Nodes) != before {
		t.Errorf("esc should act on nothing, node count went %d → %d", before, len(m.ws.Nodes))
	}
}

// Esc has two things to clear and one key. The selection is the newer of them,
// so it goes first — and the filter is still there to Esc again.
func TestEscClearsTheSelectionBeforeTheFilter(t *testing.T) {
	m, _ := visualMap(t)
	m, _ = typeKeys(t, m, "/a")
	m, _ = press(t, m, tea.KeyEnter)
	m = pressKeys(t, m, "v")

	m, _ = press(t, m, tea.KeyEsc)
	if len(m.selection) != 0 {
		t.Errorf("the first esc should clear the selection, got %v", m.selection)
	}
	if m.filter == "" {
		t.Error("the first esc should leave the filter alone")
	}
	m, _ = press(t, m, tea.KeyEsc)
	if m.filter != "" {
		t.Errorf("the second esc should clear the filter, got %q", m.filter)
	}
}

func TestTheStatusBarCountsTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l")

	if bar := lastLine(m.View()); !strings.Contains(stripANSI(bar), "2 selected") {
		t.Errorf("the status bar should count the selection, got %q", bar)
	}
}

// The same rule as the colour picker's: the seam test alone would pass with a
// style chosen and then painted over.
func TestSelectedCardsAreDrawnDifferently(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(1)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	m, _ := visualMap(t)
	plain := m.View()

	// One node — the cursor's own card, and the case a cursor highlight that
	// outranked the selection would leave looking exactly as it did.
	one := pressKeys(t, m, "v")
	if one.View() == plain {
		t.Error("v on one node should change how its card is drawn")
	}
	if !strings.Contains(one.View(), selectionCode) {
		t.Errorf("a selected card should wear the selection's colour:\n%q", one.View())
	}

	// And a gathered run, where the cursor has to stay findable inside it.
	several := pressKeys(t, one, "l")
	if several.View() == one.View() {
		t.Error("extending the selection should change how the map is drawn")
	}
}

func TestTheSelectionMovesAsAUnit(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "J")

	for _, id := range []string{"aaa", "bbb"} {
		if got := posOf(t, m, id); got.Row != 1 {
			t.Errorf("node %q should have moved down with the selection, got %v", id, got)
		}
	}
	if got := posOf(t, m, "ccc"); got.Row != 0 {
		t.Errorf("an unselected node out of the way should not move, got %v", got)
	}
	// Relative positions are what makes it a unit rather than a pile.
	if a, b := posOf(t, m, "aaa"), posOf(t, m, "bbb"); b.Col-a.Col != 1 {
		t.Errorf("the selection should keep its shape, got %v and %v", a, b)
	}
}

// Two nodes gathered and a third standing in front of them: the bystander is
// shoved by the existing collision rule rather than landed on. A selection of
// one would only re-test the single-node path.
func TestBulkMovementShovesBystandersAside(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "L")

	if got := posOf(t, m, "aaa"); got.Col != 1 {
		t.Errorf("the selection should have moved right, got %v", got)
	}
	if got := posOf(t, m, "bbb"); got.Col != 2 {
		t.Errorf("the rest of the selection should have moved with it, got %v", got)
	}
	if got := posOf(t, m, "ccc"); got.Col != 3 {
		t.Errorf("the bystander in the way should have been shoved on, got %v", got)
	}
}

// With nothing gathered, H J K L still move the one node under the cursor.
func TestNodeMovementWithoutASelectionIsUnchanged(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "J")

	if got := posOf(t, m, "aaa"); got.Row != 1 {
		t.Errorf("the node under the cursor should have moved, got %v", got)
	}
	if got := posOf(t, m, "bbb"); got.Row != 0 {
		t.Errorf("no other node should have moved, got %v", got)
	}
}

func TestTagsAreAddedAcrossTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l")
	m, _ = typeKeys(t, m, "t")
	if bar := stripANSI(lastLine(m.View())); !strings.Contains(bar, "2 nodes") {
		t.Errorf("the prompt should say how many nodes it is about, got %q", bar)
	}
	m, _ = typeKeys(t, m, "infra")
	m, _ = press(t, m, tea.KeyEnter)

	for _, id := range []string{"aaa", "bbb"} {
		n, _ := m.node(id)
		if !slices.Contains(n.Tags, "infra") {
			t.Errorf("node %q should have been tagged, got %v", id, n.Tags)
		}
	}
	if n, _ := m.node("ccc"); len(n.Tags) != 0 {
		t.Errorf("an unselected node should be left alone, got %v", n.Tags)
	}
}

// A bulk edit adds and removes; it does not set. Setting would throw away the
// tags the selected cards do not have in common.
func TestBulkTaggingKeepsTagsItWasNotAskedAbout(t *testing.T) {
	m, _ := visualMap(t)
	m = m.withNode("aaa", func(n *state.Node) { n.Tags = []string{"infra"} })
	m = pressKeys(t, m, "v", "l")
	m, _ = typeKeys(t, m, "t")
	m, _ = typeKeys(t, m, "live")
	m, _ = press(t, m, tea.KeyEnter)

	if n, _ := m.node("aaa"); !slices.Equal(n.Tags, []string{"infra", "live"}) {
		t.Errorf("a bulk add should keep the tags already there, got %v", n.Tags)
	}
	if n, _ := m.node("bbb"); !slices.Equal(n.Tags, []string{"live"}) {
		t.Errorf("node bbb should have the added tag, got %v", n.Tags)
	}
}

func TestTagsAreRemovedAcrossTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	for _, id := range []string{"aaa", "bbb"} {
		m = m.withNode(id, func(n *state.Node) { n.Tags = []string{"infra", "live"} })
	}
	m = pressKeys(t, m, "v", "l")
	m, _ = typeKeys(t, m, "t")
	m, _ = typeKeys(t, m, "-infra")
	m, _ = press(t, m, tea.KeyEnter)

	for _, id := range []string{"aaa", "bbb"} {
		n, _ := m.node(id)
		if !slices.Equal(n.Tags, []string{"live"}) {
			t.Errorf("node %q should have lost only the tag asked for, got %v", id, n.Tags)
		}
	}
}

// t on one node is unchanged: the prompt opens on the tags it already has and
// commits the list typed, because there is nothing there to disagree with.
func TestTaggingOneNodeStillSetsItsTags(t *testing.T) {
	m, _ := visualMap(t)
	m = m.withNode("aaa", func(n *state.Node) { n.Tags = []string{"infra"} })
	m, _ = typeKeys(t, m, "t")

	if m.input != "infra" {
		t.Errorf("the prompt should open on the tags the node has, got %q", m.input)
	}
	m, _ = typeKeys(t, m, " live")
	m, _ = press(t, m, tea.KeyEnter)
	if n, _ := m.node("aaa"); !slices.Equal(n.Tags, []string{"infra", "live"}) {
		t.Errorf("tags = %v", n.Tags)
	}
}

func TestBulkKillConfirmsOnceNamingTheCount(t *testing.T) {
	m, sessions := visualMap(t)
	m = pressKeys(t, m, "v", "l")
	m = pressKeys(t, m, "x")

	if m.mode != modeConfirmKill {
		t.Fatalf("x should ask before killing, mode = %v", m.mode)
	}
	if bar := stripANSI(lastLine(m.View())); !strings.Contains(bar, "2 nodes") {
		t.Errorf("the confirmation should name the count, got %q", bar)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = settleBatch(t, next.(Model), cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("both selected nodes should be gone, %d left", len(m.ws.Nodes))
	}
	if m.ws.Nodes[0].ID != "ccc" {
		t.Errorf("the unselected node should have survived, got %q", m.ws.Nodes[0].ID)
	}
	if len(sessions.killed) != 2 {
		t.Errorf("both sessions should have been killed, got %v", sessions.killed)
	}
	if len(m.selection) != 0 {
		t.Errorf("killed nodes should leave the selection, got %v", m.selection)
	}
}

func TestBulkKillCanBeDeclined(t *testing.T) {
	m, sessions := visualMap(t)
	m = pressKeys(t, m, "v", "l", "x", "n")

	if len(m.ws.Nodes) != 3 {
		t.Errorf("declining should kill nothing, %d nodes left", len(m.ws.Nodes))
	}
	if len(sessions.killed) != 0 {
		t.Errorf("declining should touch no session, got %v", sessions.killed)
	}
}

// A selected node a filter has hidden is not on screen, and acting on it would
// be acting blind — the same rule that keeps the cursor off a hidden card.
func TestAFilterPrunesTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l")
	m, _ = typeKeys(t, m, "/web")
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.selection; !slices.Equal(got, []string{"bbb"}) {
		t.Errorf("the selection should keep only the cards still on the map, got %v", got)
	}
}

// Node ids are unique against one map, so a selection cannot cross to another.
func TestAWorkspaceSwitchClearsTheSelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v")
	m = m.save()

	next, _ := m.open("other")
	if len(next.selection) != 0 {
		t.Errorf("switching workspace should clear the selection, got %v", next.selection)
	}
}

// A selection is a set of nodes and not a set of sessions: a note has none, and
// the one confirmation covers both kinds at once.
func TestBulkKillTakesNotesAndSessionsTogether(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api"},
		{ID: "bbb", Kind: state.KindNote, Title: "scratch", Pos: state.Cell{Col: 1}},
	}}
	sessions := &fakeSessions{}
	base, err := New(config.Default(), ws, t.TempDir(), sessions)
	if err != nil {
		t.Fatal(err)
	}
	sized, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := pressKeys(t, sized.(Model), "v", "l", "x")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = settleBatch(t, next.(Model), cmd)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("both nodes should be gone, %d left", len(m.ws.Nodes))
	}
	if len(sessions.killed) != 1 {
		t.Errorf("only the node with a session should have been killed, got %v", sessions.killed)
	}
}

// c, C and s take the selection through the same targets() seam H J K L, t and
// x already use (#42). c and s cycle from the first gathered card and set all
// of them to that one result: cycling each independently would leave a spread
// of colours behind a gesture asked to unify them.
func TestSCyclesEveryGatheredCardToTheSameSize(t *testing.T) {
	m, _ := visualMap(t)
	m = m.withNodes([]string{"bbb"}, func(n *state.Node) { n.Size = state.SizeL })
	m = pressKeys(t, m, "v", "l", "s")

	for _, id := range []string{"aaa", "bbb"} {
		n, _ := m.node(id)
		if n.Size != state.SizeL {
			t.Errorf("%s is %q, want every gathered card at the first one's next size", id, n.Size)
		}
	}
	if n, _ := m.node("ccc"); n.Size != "" {
		t.Errorf("ccc was not gathered but is %q", n.Size)
	}
}

func TestCCyclesEveryGatheredCardToTheSameColour(t *testing.T) {
	m, _ := visualMap(t)
	m = m.withNodes([]string{"bbb"}, func(n *state.Node) { n.Colour = "green" })
	m = pressKeys(t, m, "v", "l", "c")

	for _, id := range []string{"aaa", "bbb"} {
		n, _ := m.node(id)
		if n.Colour != "red" {
			t.Errorf("%s is %q, want the colour the first gathered card steps to", id, n.Colour)
		}
	}
	if n, _ := m.node("ccc"); n.Colour != "" {
		t.Errorf("ccc was not gathered but is %q", n.Colour)
	}
}

func TestTheColourPickerSetsEveryGatheredCard(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "C")

	if m.mode != modeColour {
		t.Fatalf("C should open the picker, mode is %v", m.mode)
	}
	if !strings.Contains(m.View(), "2 nodes") {
		t.Errorf("the picker should say how many cards it is about, got:\n%s", m.View())
	}
	m = pressKeys(t, m, "j")
	m, _ = press(t, m, tea.KeyEnter)

	want := colours[1].Name
	for _, id := range []string{"aaa", "bbb"} {
		if n, _ := m.node(id); n.Colour != want {
			t.Errorf("%s is %q, want the colour chosen in the picker %q", id, n.Colour, want)
		}
	}
	if n, _ := m.node("ccc"); n.Colour != "" {
		t.Errorf("ccc was not gathered but is %q", n.Colour)
	}
}

// r is deliberately left on the node under the cursor. A title is a node's
// handle, and renaming a gathered selection would give several cards the same
// one at once.
func TestRRenamesOnlyTheNodeUnderTheCursorEvenWithASelection(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l", "r")
	for range "web" {
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m, _ = typeKeys(t, m, "gateway")
	m, _ = press(t, m, tea.KeyEnter)

	if n, _ := m.node("bbb"); n.Title != "gateway" {
		t.Errorf("the node under the cursor is %q, want the name that was typed", n.Title)
	}
	if n, _ := m.node("aaa"); n.Title != "api" {
		t.Errorf("aaa is gathered but not under the cursor, and is now %q", n.Title)
	}
}

func TestWithNoSelectionTheAttributeKeysActOnTheCursorAlone(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "c", "s")

	n, _ := m.node("aaa")
	if n.Colour != "red" || n.Size != state.SizeL {
		t.Errorf("the cursor's card is %q/%q, want the first colour and the next size", n.Colour, n.Size)
	}
	for _, id := range []string{"bbb", "ccc"} {
		if o, _ := m.node(id); o.Colour != "" || o.Size != "" {
			t.Errorf("%s is not under the cursor but is %q/%q", id, o.Colour, o.Size)
		}
	}
}

// The selection's hints are not fitted to the width the way the map's own are,
// so a table long enough to overflow an eighty-column bar would take every hint
// off it at once rather than the last of them.
func TestTheSelectionHintsAreCutToFitRatherThanDroppedWholesale(t *testing.T) {
	m, _ := visualMap(t)
	m = pressKeys(t, m, "v", "l")

	if !strings.Contains(m.View(), "HJKL move") {
		t.Errorf("the bar should keep the hints it has room for, got:\n%s", m.View())
	}
	for _, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) > 80 {
			t.Errorf("a line is %d columns wide in an eighty-column terminal: %q", lipgloss.Width(line), line)
		}
	}
}
