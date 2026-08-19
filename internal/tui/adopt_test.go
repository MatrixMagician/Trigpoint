package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// foreign is a session Trigpoint did not create — the tmux zoo adoption exists
// to make Trigpoint useful against on day one (§9.3).
const foreign = "someone-elses-work"

// mapWithSomethingToAdopt is an empty map on a server already running one
// session of Trigpoint's and one of somebody else's.
func mapWithSomethingToAdopt(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{tmux.SessionName("main", "aaa"), foreign}
	return m, sessions
}

// adoptedMap is a map with one adopted node on it, under the cursor.
func adoptedMap(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: foreign, Session: foreign, Tags: []string{adoptedTag}},
	}})
	sessions.live = []string{foreign}
	return m, sessions
}

// openAdopt presses A and lets the question reach tmux and come back.
func openAdopt(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := typeKeys(t, m, "A")
	return settle(t, m, cmd)
}

// adoptFirst takes the first session offered.
func adoptFirst(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := press(t, openAdopt(t, m), tea.KeyEnter)
	return settle(t, next, cmd)
}

func TestAOffersOnlyTheSessionsTrigpointDidNotCreate(t *testing.T) {
	m, _ := mapWithSomethingToAdopt(t)

	m = openAdopt(t, m)

	if m.mode != modeAdopt {
		t.Fatalf("A should open the adoption picker, mode = %v", m.mode)
	}
	if len(m.candidates) != 1 || m.candidates[0] != foreign {
		t.Errorf("only sessions outside the prefix are adoptable, got %v", m.candidates)
	}
	if view := m.View(); !strings.Contains(view, foreign) {
		t.Errorf("the picker should name the session it is offering, got:\n%s", view)
	}
}

func TestASessionAlreadyOnTheMapIsNotOfferedAgain(t *testing.T) {
	m, sessions := adoptedMap(t)
	sessions.live = []string{foreign, "other-work"}

	m = openAdopt(t, m)

	if len(m.candidates) != 1 || m.candidates[0] != "other-work" {
		t.Errorf("a session already adopted is not adoptable twice, got %v", m.candidates)
	}
}

func TestNothingToAdoptSaysSoRatherThanOpeningAnEmptyPicker(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{tmux.SessionName("main", "aaa")}

	m = openAdopt(t, m)

	if m.mode == modeAdopt {
		t.Error("every session on the server is Trigpoint's own, so there is nothing to open a picker over")
	}
	if m.status == "" {
		t.Error("a keystroke that does nothing has to say why")
	}
}

// errTestList stands for tmux refusing to say what is running.
var errTestList = errors.New("no server running on /tmp/tmux-1000/default")

func TestAReportsWhatTmuxSaidWhenItCannotBeAsked(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.listErr = errTestList

	m = openAdopt(t, m)

	if m.mode == modeAdopt {
		t.Error("a list that failed is not a list of nothing to adopt")
	}
	if !strings.Contains(m.status, errTestList.Error()) {
		t.Errorf("status = %q, want tmux's own complaint", m.status)
	}
}

func TestAdoptingPlacesATaggedShellNodeAndCreatesNothing(t *testing.T) {
	m, sessions := mapWithSomethingToAdopt(t)

	m = adoptFirst(t, m)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("expected the adopted node on the map, got %d", len(m.ws.Nodes))
	}
	node := m.ws.Nodes[0]
	if node.Kind != state.KindShell {
		t.Errorf("an adopted session maps as a shell node, got %q", node.Kind)
	}
	if !slices.Contains(node.Tags, adoptedTag) {
		t.Errorf("an adopted node is tagged %q, got %v", adoptedTag, node.Tags)
	}
	if node.ID == "" {
		t.Error("an adopted node is a node like any other, and has an id")
	}
	if len(sessions.created) != 0 {
		t.Errorf("adoption creates nothing — the session was already running — got %v", sessions.created)
	}
}

func TestAdoptionStoresTheForeignNameRatherThanImposingThePrefix(t *testing.T) {
	m, _ := mapWithSomethingToAdopt(t)

	m = adoptFirst(t, m)

	node := m.ws.Nodes[0]
	if node.Session != foreign {
		t.Errorf("the foreign session's own name is what is stored, got %q", node.Session)
	}
	if node.Title != foreign {
		t.Errorf("the card is titled after the session it adopted, got %q", node.Title)
	}
	if got := m.sessionOf(node); got != foreign {
		t.Errorf("an adopted node's session is the foreign one, got %q", got)
	}
}

func TestAnAdoptedNodeLandsOnAFreeCell(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Pos: state.Cell{}},
	}})
	sessions.live = []string{foreign}

	m = adoptFirst(t, m)

	if len(m.ws.Nodes) != 2 {
		t.Fatalf("expected two nodes on the map, got %d", len(m.ws.Nodes))
	}
	if m.ws.Nodes[1].Pos == (state.Cell{}) {
		t.Error("adoption should not stack a card on the cell the cursor is already on")
	}
}

func TestAnAdoptedNodeSurvivesTheRestartItIsThereFor(t *testing.T) {
	m, sessions, dir := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{foreign}

	_ = adoptFirst(t, m)

	ws, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ws.Nodes) != 1 || ws.Nodes[0].Session != foreign {
		t.Errorf("the stored session name has to reach the file, got %+v", ws.Nodes)
	}
}

func TestTheAdoptionPickerChoosesAmongItsCandidates(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{"bravo", "alpha"}

	m = openAdopt(t, m)
	if len(m.candidates) != 2 || m.candidates[0] != "alpha" {
		t.Fatalf("candidates are offered in a fixed order, got %v", m.candidates)
	}
	m, _ = typeKeys(t, m, "j")
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 || m.ws.Nodes[0].Session != "bravo" {
		t.Errorf("the chosen candidate is the one adopted, got %+v", m.ws.Nodes)
	}
}

func TestEscLeavesTheAdoptionPickerWithoutAdopting(t *testing.T) {
	m, _ := mapWithSomethingToAdopt(t)

	m = openAdopt(t, m)
	m, _ = press(t, m, tea.KeyEsc)

	if m.mode != modeNormal {
		t.Errorf("esc should close the picker, mode = %v", m.mode)
	}
	if len(m.ws.Nodes) != 0 {
		t.Errorf("nothing was adopted, got %+v", m.ws.Nodes)
	}
}

func TestAnAdoptedNodeAttachesToItsOwnSession(t *testing.T) {
	m, sessions := adoptedMap(t)

	m, _ = press(t, m, tea.KeyEnter)

	if len(sessions.handoffs) != 1 || sessions.handoffs[0].session != foreign {
		t.Errorf("attach should hand the terminal to the foreign session, got %v", sessions.handoffs)
	}
}

func TestAnAdoptedNodeIsPreviewedFromItsOwnSession(t *testing.T) {
	m, sessions := adoptedMap(t)

	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 1 || sessions.captured[0].session != foreign {
		t.Errorf("the preview is captured from the foreign session, got %v", sessions.captured)
	}
}

func TestReconciliationResolvesAnAdoptedNodeByItsStoredName(t *testing.T) {
	m, sessions := adoptedMap(t)

	m = reconciled(t, m)
	if m.dead["aaa"] {
		t.Error("an adopted node whose session is running is alive")
	}
	if len(m.ws.Nodes) != 1 {
		t.Errorf("an adopted node is neither an orphan nor a stranger, got %+v", m.ws.Nodes)
	}

	sessions.live = nil
	m = reconciled(t, m)
	if !m.dead["aaa"] {
		t.Error("an adopted node whose session has gone is dead like any other")
	}
	if view := m.View(); !strings.Contains(view, deadBadge) {
		t.Errorf("and its card says so, got:\n%s", view)
	}
}

func TestAForeignSessionIsNeverAnOrphanToReconstruct(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{foreign}

	m = reconciled(t, m)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("a session Trigpoint did not create is a stranger until it is adopted, got %+v", m.ws.Nodes)
	}
	if len(sessions.enved) != 0 {
		t.Errorf("and is never even asked about its provenance, got %v", sessions.enved)
	}
}

func TestKillingAnAdoptedNodeKillsTheForeignSession(t *testing.T) {
	m, sessions := adoptedMap(t)

	m, _ = typeKeys(t, m, "x")
	if m.mode != modeConfirmKill {
		t.Fatal("killing an adopted node is confirmed like any other")
	}
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if len(sessions.killed) != 1 || sessions.killed[0] != foreign {
		t.Errorf("the foreign session is what gets killed, got %v", sessions.killed)
	}
	if len(m.ws.Nodes) != 0 {
		t.Errorf("and the card goes with it, got %+v", m.ws.Nodes)
	}
}

func TestADeadAdoptedNodeIsNotOfferedARespawn(t *testing.T) {
	m, sessions := adoptedMap(t)
	sessions.dead = true // whatever Enter asks about, the session is gone

	m, _ = press(t, m, tea.KeyEnter)

	if m.mode == modeConfirmRespawn {
		t.Error("Trigpoint never started this session, so it has nothing to start again")
	}
	if !m.dead["aaa"] {
		t.Error("the card is corrected on the spot, like any other node's")
	}
	if m.status == "" {
		t.Error("a keystroke that does nothing has to say why")
	}
	if len(sessions.handoffs) != 0 {
		t.Errorf("nothing to attach to, so nothing was attached, got %v", sessions.handoffs)
	}
}

func TestASessionTmuxCannotBeToldApartIsNotOffered(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	// tmux accepts "." and ":" in a session name and then reads them back as
	// window and pane separators, so "=my.proj" targets a pane that is not there
	// — or, for capture-pane, quietly answers about a different session.
	sessions.live = []string{"my.proj", "my:proj", "plain"}

	m = openAdopt(t, m)

	if len(m.candidates) != 1 || m.candidates[0] != "plain" {
		t.Errorf("only sessions tmux can be asked about are adoptable, got %v", m.candidates)
	}
}

func TestNothingButUnaddressableSessionsSaysWhy(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{"my.proj"}

	m = openAdopt(t, m)

	if m.mode == modeAdopt {
		t.Fatal("a session tmux cannot be told apart is not on offer")
	}
	if !strings.Contains(m.status, "my.proj") {
		t.Errorf("the one session on the server was skipped, so the map has to say which: %q", m.status)
	}
}

func TestAnAnswerArrivingMidPromptDoesNotStealTheKeyboard(t *testing.T) {
	m, _ := mapWithSomethingToAdopt(t)

	// A is answered by a subprocess, so the answer can land at any moment —
	// including after the user has given up on it and started typing something
	// else. Taking the keyboard then would turn the next Enter into an adoption
	// of a session nobody was looking at.
	m, cmd := typeKeys(t, m, "A")
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	m = settle(t, m, cmd)

	if m.mode != modeTitle {
		t.Errorf("the title prompt should have survived the answer, mode = %v", m.mode)
	}
	if m.input != "api" {
		t.Errorf("and so should what was typed into it, got %q", m.input)
	}
}

func TestASessionIsNeverAdoptedTwice(t *testing.T) {
	m, _ := mapWithSomethingToAdopt(t)

	m = adoptFirst(t, m)
	// A second answer, from a press of A that was in flight while the first was
	// being acted on: it was listed before the node existed, so it still offers
	// the session that now has a card.
	m = update(t, m, adoptableMsg{names: []string{foreign}})
	next, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, next, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Errorf("one session, one card: %+v", m.ws.Nodes)
	}
	if m.status == "" {
		t.Error("a keystroke that does nothing has to say why")
	}
}
