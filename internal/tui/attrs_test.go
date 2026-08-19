package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

func TestRRenamesTheNodeUnderTheCursor(t *testing.T) {
	m, _, dir := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "r")
	if !strings.Contains(m.View(), "api") {
		t.Errorf("the rename prompt should open on the current title, got:\n%s", m.View())
	}
	m, _ = typeKeys(t, m, "-2")
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.ws.Nodes[0].Title; got != "api-2" {
		t.Errorf("title = %q, want %q", got, "api-2")
	}
	if !strings.Contains(m.View(), "api-2") {
		t.Errorf("the card should show the new title, got:\n%s", m.View())
	}
	saved, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the saved workspace: %v", err)
	}
	if got := saved.Nodes[0].Title; got != "api-2" {
		t.Errorf("saved title = %q, want %q", got, "api-2")
	}
}

func TestEscapeLeavesTheTitleAlone(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "r")
	m, _ = typeKeys(t, m, "junk")
	m, _ = press(t, m, tea.KeyEsc)

	if got := m.ws.Nodes[0].Title; got != "api" {
		t.Errorf("title = %q, want the rename to have been abandoned", got)
	}
}

// TestRenamingToNothingFallsBackToTheID keeps a card from losing its only
// handle: a blank border says nothing about which node it is.
func TestRenamingToNothingFallsBackToTheID(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "r")
	for i := 0; i < len("api"); i++ {
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.ws.Nodes[0].Title; got != "k4f2" {
		t.Errorf("title = %q, want the node id", got)
	}
}

// TestRenameTargetsTheNodeItOpenedOn pins the edit to the node the key was
// pressed on: a create landing while the prompt is up moves the cursor, and
// re-reading the selection at Enter would rename a node the user never chose.
func TestRenameTargetsTheNodeItOpenedOn(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())
	m.ws.Nodes = append(m.ws.Nodes, state.Node{
		ID: "m7qp", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1},
	})

	m, _ = typeKeys(t, m, "r")
	m.ws.Viewport.Cursor = state.Cell{Col: 1} // the cursor moves under the prompt
	m, _ = typeKeys(t, m, "-2")
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.ws.Nodes[0].Title; got != "api-2" {
		t.Errorf("first node title = %q, want %q", got, "api-2")
	}
	if got := m.ws.Nodes[1].Title; got != "web" {
		t.Errorf("second node title = %q, want it untouched", got)
	}
}

func TestRenameOnAnEmptyCellDoesNothing(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main"})

	m, _ = typeKeys(t, m, "r")
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want the map to have stayed in normal mode", m.mode)
	}
}

func TestTEditsTheTagsOnANode(t *testing.T) {
	m, _, dir := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "t")
	m, _ = typeKeys(t, m, "infra live")
	m, _ = press(t, m, tea.KeyEnter)

	want := []string{"infra", "live"}
	if got := m.ws.Nodes[0].Tags; !equalStrings(got, want) {
		t.Errorf("tags = %q, want %q", got, want)
	}
	saved, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the saved workspace: %v", err)
	}
	if got := saved.Nodes[0].Tags; !equalStrings(got, want) {
		t.Errorf("saved tags = %q, want %q", got, want)
	}
}

// TestTheTagPromptOpensOnTheTagsAlreadySet keeps editing from meaning retyping:
// t is edit, not replace.
func TestTheTagPromptOpensOnTheTagsAlreadySet(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Tags = []string{"infra", "live"}
	m, _, _ := newNodeModel(t, ws)

	m, _ = typeKeys(t, m, "t")
	if !strings.Contains(m.View(), "infra live") {
		t.Errorf("the tag prompt should open on the tags already set, got:\n%s", m.View())
	}
}

// TestATagIsStoredWithoutItsHash keeps the card's own punctuation out of the
// tag: #infra typed and infra typed are the same tag.
func TestATagIsStoredWithoutItsHash(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "t")
	m, _ = typeKeys(t, m, "#infra")
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.ws.Nodes[0].Tags; !equalStrings(got, []string{"infra"}) {
		t.Errorf("tags = %q, want the hash stripped", got)
	}
}

func TestAnEmptyTagPromptClearsTheTags(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Tags = []string{"infra"}
	m, _, _ := newNodeModel(t, ws)

	m, _ = typeKeys(t, m, "t")
	for i := 0; i < len("infra"); i++ {
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.ws.Nodes[0].Tags; len(got) != 0 {
		t.Errorf("tags = %q, want none", got)
	}
}

// TestTheCardShowsItsTags is the point of tagging a node: the label is on the
// card, not somewhere you have to go and look for it (SPEC §7.1).
func TestTheCardShowsItsTags(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Tags = []string{"infra"}
	m, _, _ := newNodeModel(t, ws)

	if !strings.Contains(m.View(), "#infra") {
		t.Errorf("the card should carry its tags on its border, got:\n%s", m.View())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCCyclesTheAccentColour(t *testing.T) {
	m, _, dir := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "c")
	if got := m.ws.Nodes[0].Colour; got != colours[0].Name {
		t.Errorf("colour = %q, want %q", got, colours[0].Name)
	}
	m, _ = typeKeys(t, m, "c")
	if got := m.ws.Nodes[0].Colour; got != colours[1].Name {
		t.Errorf("colour = %q, want %q", got, colours[1].Name)
	}
	saved, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the saved workspace: %v", err)
	}
	if got := saved.Nodes[0].Colour; got != colours[1].Name {
		t.Errorf("saved colour = %q, want %q", got, colours[1].Name)
	}
}

// TestCyclingPastTheLastColourClearsIt is how a card gets its plain border back
// without going through the picker.
func TestCyclingPastTheLastColourClearsIt(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Colour = colours[len(colours)-1].Name
	m, _, _ := newNodeModel(t, ws)

	m, _ = typeKeys(t, m, "c")
	if got := m.ws.Nodes[0].Colour; got != "" {
		t.Errorf("colour = %q, want none", got)
	}
}

// TestAColourNobodyNamedCyclesBackIntoThePalette keeps a hand-edited workspace
// file from leaving a card stuck on a colour c cannot move off.
func TestAColourNobodyNamedCyclesBackIntoThePalette(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Colour = "mauve"
	m, _, _ := newNodeModel(t, ws)

	m, _ = typeKeys(t, m, "c")
	if got := m.ws.Nodes[0].Colour; got != colours[0].Name {
		t.Errorf("colour = %q, want %q", got, colours[0].Name)
	}
}

func TestShiftCPicksAColourByName(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "C")
	if !strings.Contains(m.View(), colours[0].Name) {
		t.Errorf("the picker should name the colour under its cursor, got:\n%s", m.View())
	}
	m, _ = typeKeys(t, m, "jj")
	m, _ = press(t, m, tea.KeyEnter)

	if got := m.ws.Nodes[0].Colour; got != colours[2].Name {
		t.Errorf("colour = %q, want %q", got, colours[2].Name)
	}
}

// TestThePickerShowsTheColourOnTheCard is what makes it a picker rather than a
// list of words: the card wears the colour under the cursor while you choose.
func TestThePickerShowsTheColourOnTheCard(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())
	node := m.ws.Nodes[0]

	m, _ = typeKeys(t, m, "C")
	m, _ = typeKeys(t, m, "j")
	if got := m.shown(node).Colour; got != colours[1].Name {
		t.Errorf("the card is drawn in %q, want the colour under the picker's cursor", got)
	}

	m, _ = press(t, m, tea.KeyEsc)
	if got := m.ws.Nodes[0].Colour; got != "" {
		t.Errorf("colour = %q, want the picker to have changed nothing", got)
	}
	if got := m.shown(node).Colour; got != "" {
		t.Errorf("the card is still drawn in %q after the picker closed", got)
	}
}

// TestThePickerOpensOnTheColourTheNodeHas keeps C from being a way to lose the
// colour you already chose.
func TestThePickerOpensOnTheColourTheNodeHas(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Colour = colours[3].Name
	m, _, _ := newNodeModel(t, ws)

	m, _ = typeKeys(t, m, "C")
	if !strings.Contains(m.View(), colours[3].Name) {
		t.Errorf("the picker should open on the colour the node has, got:\n%s", m.View())
	}
}

// TestEveryNamedColourHasOne is the palette's own contract: the names are what
// persist, so every one of them has to resolve to something to draw with.
func TestEveryNamedColourHasOne(t *testing.T) {
	for _, c := range colours {
		if _, ok := accent(c.Name); !ok {
			t.Errorf("colour %q has no style", c.Name)
		}
	}
	if _, ok := accent("mauve"); ok {
		t.Error("a colour nobody named should have no style")
	}
	if _, ok := accent(""); ok {
		t.Error("a card with no colour should have no accent style")
	}
}

// TestSCyclesTheCardSize walks the three sizes in the order the spec gives them
// (§7.3, S→M→L). An unset size reads as medium, so the first press asks for a
// bigger card, which is what pressing s on a card usually means.
func TestSCyclesTheCardSize(t *testing.T) {
	m, _, dir := newNodeModel(t, oneNode())

	for _, want := range []state.CardSize{state.SizeL, state.SizeS, state.SizeM, state.SizeL} {
		m, _ = typeKeys(t, m, "s")
		if got := m.ws.Nodes[0].Size; got != want {
			t.Fatalf("size = %q, want %q", got, want)
		}
	}
	saved, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the saved workspace: %v", err)
	}
	if got := saved.Nodes[0].Size; got != state.SizeL {
		t.Errorf("saved size = %q, want %q", got, state.SizeL)
	}
}

// TestCardSizeDecidesHowMuchIsCaptured ties the size to the configured line
// counts: a card asks tmux for exactly what it has room to show.
func TestCardSizeDecidesHowMuchIsCaptured(t *testing.T) {
	lines := config.Default().General.PreviewLines
	for _, tc := range []struct {
		size state.CardSize
		want int
	}{
		{state.SizeM, lines.M},
		{state.SizeL, lines.L},
		{"", lines.M},
	} {
		ws := oneNode()
		ws.Nodes[0].Size = tc.size
		m, sessions, _ := newNodeModel(t, ws)
		m = run(t, m, captureDueMsg{})

		if len(sessions.captured) != 1 {
			t.Fatalf("size %q: expected one capture, got %v", tc.size, sessions.captured)
		}
		if got := sessions.captured[0].lines; got != tc.want {
			t.Errorf("size %q captured %d lines, want %d", tc.size, got, tc.want)
		}
	}
}

// TestASmallCardCapturesNothing is the whole point of the small size: a node
// you have finished watching stops costing a capture-pane per tick.
func TestASmallCardCapturesNothing(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Size = state.SizeS
	m, sessions, _ := newNodeModel(t, ws)

	m = run(t, m, captureDueMsg{})
	m = run(t, m, activity("k4f2"))
	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 0 {
		t.Errorf("a small card should never be captured, got %v", sessions.captured)
	}
}

// TestASmallCardShowsNoPreviewLines holds the size against the map's shared
// card height: every card is as tall as the hungriest node asks for, and a
// small card spends none of that on a preview it was told not to show.
func TestASmallCardShowsNoPreviewLines(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Size: state.SizeS, Pos: state.Cell{}},
		{ID: "bbb", Kind: state.KindShell, Title: "web", Size: state.SizeL, Pos: state.Cell{Col: 1}},
	}})
	m = m.withPreviews(map[string][]string{
		"aaa": {"secret from the small card"},
		"bbb": {"web output"},
	})

	if got := m.bodyOf(m.ws.Nodes[0]); len(got) != 0 {
		t.Errorf("a small card's body = %q, want nothing", got)
	}
	if strings.Contains(m.View(), "secret from the small card") {
		t.Errorf("a small card should show no preview lines, got:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "web output") {
		t.Errorf("the large card beside it should still show its preview, got:\n%s", m.View())
	}
}

// TestShrinkingACardDropsThePreviewItNoLongerHasRoomFor keeps a snapshot from
// outliving the size that asked for it.
func TestShrinkingACardDropsThePreviewItNoLongerHasRoomFor(t *testing.T) {
	ws := oneNode()
	ws.Nodes[0].Size = state.SizeL
	m, _, _ := newNodeModel(t, ws)
	m = m.withPreviews(map[string][]string{"k4f2": {"one", "two", "three", "four", "five"}})

	m, _ = typeKeys(t, m, "s") // large → small
	if got := m.bodyOf(m.ws.Nodes[0]); len(got) != 0 {
		t.Errorf("body = %q, want the small card to show nothing", got)
	}
}

// TestANoteIsSizedLikeAnyOtherCard settles what ADR 0003 left open: the cap on
// a note's body is the node's own size, not one constant for the whole map.
func TestANoteIsSizedLikeAnyOtherCard(t *testing.T) {
	lines := config.Default().General.PreviewLines
	m, _, _ := mapWithOneNote(t, strings.Repeat("line\n", 200))

	if got := m.bodyHeight(); got != lines.M {
		t.Errorf("an unsized note asked for %d body lines, want the medium %d", got, lines.M)
	}
	m, _ = typeKeys(t, m, "s") // medium → large
	if got := m.bodyHeight(); got != lines.L {
		t.Errorf("a large note asked for %d body lines, want %d", got, lines.L)
	}
	m, _ = typeKeys(t, m, "s") // large → small
	if got := m.bodyHeight(); got != lines.S {
		t.Errorf("a small note asked for %d body lines, want %d", got, lines.S)
	}
}

// TestTheTitleOutranksTheTagsOnTheBorder is the rule a 22-cell card forces: two
// labels want the same border, and the one that says which node this is wins.
func TestTheTitleOutranksTheTagsOnTheBorder(t *testing.T) {
	long := card(state.Node{
		ID: "k4f2", Kind: state.KindShell, Title: "a-very-long-node-name", Tags: []string{"infra"},
	}, false, false, false, 0, nil)
	if !strings.Contains(long[0], "a-very-lo") {
		t.Errorf("the title should keep the border it needs, got %q", long[0])
	}
	if strings.Contains(long[0], "#") {
		t.Errorf("the tags should give way rather than shrink to nothing, got %q", long[0])
	}

	short := card(state.Node{
		ID: "k4f2", Kind: state.KindShell, Title: "api", Tags: []string{"infra"},
	}, false, false, false, 0, nil)
	if !strings.Contains(short[0], "#infra") || !strings.Contains(short[0], "api") {
		t.Errorf("a short title leaves room for its tags, got %q", short[0])
	}
}

// TestATaggedCardIsStillACardWide holds the grid together: a border that grew
// with its tags would push the column beside it out of line.
func TestATaggedCardIsStillACardWide(t *testing.T) {
	for _, tags := range [][]string{
		nil,
		{"infra"},
		{"infra", "live", "eu-west", "on-call"},
		{"日本語のタグ"},
		{"🔥"},
	} {
		for _, title := range []string{"api", "a-very-long-node-name", "日本語のノードタイトル"} {
			n := state.Node{ID: "k4f2", Kind: state.KindShell, Title: title, Tags: tags}
			for _, line := range card(n, false, false, false, 1, []string{"body"}) {
				if w := lipgloss.Width(line); w != cardWidth {
					t.Errorf("a card line for %q/%q is %d cells wide, want %d: %q", title, tags, w, cardWidth, line)
				}
			}
		}
	}
}

// TestAttributeKeysAreNormalModeOnly keeps the new bindings out of the prompts:
// a node called "csrt" is a title, not four keystrokes.
func TestAttributeKeysAreNormalModeOnly(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())

	m, _ = typeKeys(t, m, "r")
	for i := 0; i < len("api"); i++ {
		m, _ = press(t, m, tea.KeyBackspace)
	}
	m, _ = typeKeys(t, m, "csrtq")
	m, _ = press(t, m, tea.KeyEnter)

	node := m.ws.Nodes[0]
	if node.Title != "csrtq" {
		t.Errorf("title = %q, want the keys to have been typed rather than obeyed", node.Title)
	}
	if node.Colour != "" || node.Size != "" || len(node.Tags) != 0 {
		t.Errorf("the prompt changed %+v as well as the title", node)
	}
}

// TestThePickedColourOutranksTheSelection is what makes the live preview
// visible at all: the card the picker is open on is also the card under the
// cursor, and the selection style would paint over the colour being chosen.
func TestThePickedColourOutranksTheSelection(t *testing.T) {
	m, _, _ := newNodeModel(t, oneNode())
	node := m.ws.Nodes[0]

	if !m.drawnSelected(node) {
		t.Error("the node under the cursor should be drawn as selected")
	}
	m, _ = typeKeys(t, m, "C")
	if m.drawnSelected(node) {
		t.Error("while the picker is open the card should wear the colour, not the selection")
	}
	m, _ = press(t, m, tea.KeyEsc)
	if !m.drawnSelected(node) {
		t.Error("closing the picker should give the card its selection back")
	}
}
