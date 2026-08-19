package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// filterMap is three cards worth telling apart: a tagged shell, a plain shell,
// and a note — one for each of the three things the filter matches on (§7.1).
func filterMap(t *testing.T) Model {
	t.Helper()
	return newModel(t, config.Default(), state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api-server", Tags: []string{"infra"}},
		{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1}},
		{ID: "ccc", Kind: state.KindNote, Title: "scratch", Pos: state.Cell{Col: 2}},
	}})
}

func TestSlashNarrowsTheMapAsYouType(t *testing.T) {
	m, _ := typeKeys(t, filterMap(t), "/api")

	view := m.View()
	if !strings.Contains(view, "api-server") {
		t.Errorf("the matching card should still be on the map, got:\n%s", view)
	}
	if strings.Contains(view, "web") || strings.Contains(view, "scratch") {
		t.Errorf("the cards that do not match should be gone, got:\n%s", view)
	}
}

func TestFilterMatchesTagsAndKind(t *testing.T) {
	tagged, _ := typeKeys(t, filterMap(t), "/infra")
	if !strings.Contains(tagged.View(), "api-server") {
		t.Errorf("a filter should match a node's tags, got:\n%s", tagged.View())
	}
	if strings.Contains(tagged.View(), "web") {
		t.Errorf("an untagged card should be filtered out, got:\n%s", tagged.View())
	}

	notes, _ := typeKeys(t, filterMap(t), "/note")
	if !strings.Contains(notes.View(), "scratch") {
		t.Errorf("a filter should match a node's kind, got:\n%s", notes.View())
	}
	if strings.Contains(notes.View(), "api-server") {
		t.Errorf("a shell card should not answer to a note filter, got:\n%s", notes.View())
	}
}

// The filter outlives the prompt: Enter closes the prompt and leaves the map
// narrowed, which is what the status bar then has to say (§7.1).
func TestTheStatusBarSaysWhenAFilterIsActive(t *testing.T) {
	m, _ := typeKeys(t, filterMap(t), "/api")
	m, _ = press(t, m, tea.KeyEnter)

	if m.mode != modeNormal {
		t.Fatalf("Enter should close the filter prompt, mode = %v", m.mode)
	}
	if !strings.Contains(m.View(), "/api") {
		t.Errorf("the status bar should show the active filter, got:\n%s", m.View())
	}
	if strings.Contains(m.View(), "web") {
		t.Errorf("the filter should still be narrowing the map, got:\n%s", m.View())
	}
}

func TestEscClearsTheFilter(t *testing.T) {
	typing, _ := typeKeys(t, filterMap(t), "/api")
	cancelled, _ := press(t, typing, tea.KeyEsc)
	if cancelled.filter != "" {
		t.Errorf("Esc at the prompt should clear the filter, got %q", cancelled.filter)
	}
	if !strings.Contains(cancelled.View(), "web") {
		t.Errorf("clearing the filter should put every card back, got:\n%s", cancelled.View())
	}

	// And again on a filter that has outlived its prompt: Esc on the map is the
	// only way back from one of those.
	committed, _ := press(t, typing, tea.KeyEnter)
	cleared, _ := press(t, committed, tea.KeyEsc)
	if cleared.filter != "" {
		t.Errorf("Esc on the map should clear the filter, got %q", cleared.filter)
	}
	if !strings.Contains(cleared.View(), "web") {
		t.Errorf("clearing the filter should put every card back, got:\n%s", cleared.View())
	}
}

// A card the filter hides is not a card the cursor can act on: Enter would
// otherwise attach to a session with nothing on screen to say which. So the
// cursor moves to a card that is still there rather than being left pointing at
// one that is not.
func TestFilteringPutsTheCursorOnACardThatIsStillThere(t *testing.T) {
	m := filterMap(t)
	m.ws.Viewport.Cursor = state.Cell{Col: 1} // web, which /api hides
	narrowed, _ := typeKeys(t, m, "/api")

	node, ok := narrowed.selected()
	if !ok || node.Title != "api-server" {
		t.Errorf("the cursor should land on the nearest card the filter leaves, selected = %+v (%t)", node, ok)
	}
}

func TestAFilterThatMatchesNothingSelectsNothing(t *testing.T) {
	m, _ := typeKeys(t, filterMap(t), "/zqx")
	if node, ok := m.selected(); ok {
		t.Errorf("nothing matches, so nothing is under the cursor, got %q", node.Title)
	}
}

// Off screen is where activity events are dropped, so a card the filter was
// hiding has been unwatched for as long as it was hidden — and is owed a fresh
// snapshot the moment it comes back.
func TestClearingAFilterRecapturesTheCardsItHid(t *testing.T) {
	m := filterMap(t)
	// Both cards captured and nothing outstanding, which is where a map settles
	// once the first resize's captures have come back.
	m.previews, m.dirty = map[string][]string{"aaa": {"api"}, "bbb": {"web"}}, nil

	narrowed, _ := typeKeys(t, m, "/api")
	narrowed, _ = press(t, narrowed, tea.KeyEnter)
	if narrowed.dirty["bbb"] {
		t.Fatal("narrowing hides cards and brings none back, so it owes no captures")
	}

	cleared, _ := press(t, narrowed, tea.KeyEsc)
	if !cleared.dirty["bbb"] {
		t.Error("the card the filter was hiding should be owed a fresh capture")
	}
}

func TestAFilterThatMatchesNothingSaysSo(t *testing.T) {
	m, _ := typeKeys(t, filterMap(t), "/zqx")
	if !strings.Contains(m.View(), "matches") {
		t.Errorf("a filter with no matches should say so rather than draw a blank map, got:\n%s", m.View())
	}
}

func TestFuzzyMatchesSubsequencesAndScoresTightestFirst(t *testing.T) {
	if fuzzy("asrv", "api-server") < 0 {
		t.Error("a fuzzy match should take the pattern's characters in order, not together")
	}
	if fuzzy("API", "api-server") < 0 {
		t.Error("a fuzzy match should ignore case")
	}
	if fuzzy("zq", "api-server") >= 0 {
		t.Error("characters that are not there are not a match")
	}
	if fuzzy("", "api-server") != 0 {
		t.Error("an empty pattern matches everything, perfectly")
	}
	if fuzzy("api", "api-server") >= fuzzy("api", "an api server") {
		t.Error("a tighter match should score lower than a scattered one")
	}
}
