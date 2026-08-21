package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// reconciled drives one reconciliation pass by hand, the way the Bubble Tea
// runtime would drive the one Init asks for.
func reconciled(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.reconcile()
	if cmd == nil {
		t.Fatal("the map never asked tmux what is still running")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// drain runs a command and everything it batches, giving up on the ones that
// are waiting on a clock rather than on tmux.
func drain(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drain(t, c)
			}
		}
	case <-time.After(2 * time.Second):
	}
}

// mapWithADeadNode is the state a map comes back in after a reboot: one node
// whose session survived, one whose session did not.
func mapWithADeadNode(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Dir: "/tmp/work", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Colour: "red", Tags: []string{"work"}, Pos: state.Cell{Col: 0, Row: 0}},
		{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1, Row: 0}},
	}})
	sessions.live = []string{tmux.SessionName("main", "aaa")}
	sessions.dead = true // whatever the map is asked about, "bbb" is what it asks about
	return reconciled(t, m), sessions
}

func TestStartupClassifiesEveryNodeByItsSession(t *testing.T) {
	m, _ := mapWithADeadNode(t)

	if m.dead["aaa"] {
		t.Error("a node whose session is still running is alive")
	}
	if !m.dead["bbb"] {
		t.Error("a node whose session is gone is dead")
	}
}

func TestADeadNodeKeepsItsIdentityAndItsPlace(t *testing.T) {
	m, _ := mapWithADeadNode(t)

	// The map must not rearrange itself after a reboot: deadness is a badge,
	// not a reason to forget what the node was.
	node, ok := m.node("aaa")
	if !ok {
		t.Fatal("the node left the map")
	}
	if node.Title != "api" || node.Colour != "red" || len(node.Tags) != 1 || node.Pos != (state.Cell{Col: 0, Row: 0}) {
		t.Errorf("reconciliation changed a node's identity: %+v", node)
	}
	if len(m.ws.Nodes) != 2 {
		t.Errorf("expected both nodes still on the map, got %d", len(m.ws.Nodes))
	}
}

func TestADeadCardIsBadgedAsDead(t *testing.T) {
	m, _ := mapWithADeadNode(t)

	view := m.View()
	if !strings.Contains(view, deadBadge) {
		t.Errorf("a dead node's card should carry the dead badge, got:\n%s", view)
	}
	if strings.Count(view, deadBadge) != 1 {
		t.Errorf("only the dead node's card should carry it, got:\n%s", view)
	}
}

func TestANoteIsNeverAliveOrDead(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "nnn", Kind: state.KindNote, Title: "todo", Note: "buy milk"},
	}})
	sessions.live = nil // nothing at all is running

	m = reconciled(t, m)

	if m.dead["nnn"] {
		t.Error("a note has no session, so it is never dead")
	}
	if view := m.View(); strings.Contains(view, deadBadge) {
		t.Errorf("a note's card should carry no liveness badge at all, got:\n%s", view)
	}
}

func TestEnterOnADeadNodeOffersRespawnRatherThanAttaching(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = press(t, m, tea.KeyEnter)

	if len(sessions.handoffs) != 0 {
		t.Errorf("a dead node has nothing to attach to, got %v", sessions.handoffs)
	}
	if view := m.View(); !strings.Contains(view, "Respawn") {
		t.Errorf("Enter on a dead node should offer a respawn, got:\n%s", view)
	}
}

func TestRespawningKeepsTheNodesIdentity(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	// A node that knows its own directory and command: respawning is what
	// re-runs them (§9.2).
	nodes := append([]state.Node(nil), m.ws.Nodes...)
	nodes[1].Dir, nodes[1].Cmd = "/tmp/web", "npm run dev"
	m.ws.Nodes = nodes
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = press(t, m, tea.KeyEnter)
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if len(sessions.created) != 1 {
		t.Fatalf("expected one session created, got %v", sessions.created)
	}
	call := sessions.created[0]
	if want := tmux.SessionName("main", "bbb"); call.session != want {
		t.Errorf("respawned session = %q, want %q — respawning is not creating a new node", call.session, want)
	}
	if call.dir != "/tmp/web" {
		t.Errorf("respawned in %q, want the node's own directory", call.dir)
	}
	if call.cmd != "npm run dev" {
		t.Errorf("respawn ran %q, want the node's command", call.cmd)
	}
	if call.env["TRIG_NODE_ID"] != "bbb" {
		t.Errorf("respawned session provenance = %v, want the original node id", call.env)
	}

	node, ok := m.node("bbb")
	if !ok {
		t.Fatal("the node left the map")
	}
	if node.Title != "web" || node.Pos != (state.Cell{Col: 1, Row: 0}) {
		t.Errorf("respawn changed the node: %+v", node)
	}
	if m.dead["bbb"] {
		t.Error("a respawned node is alive again")
	}
	if len(m.ws.Nodes) != 2 {
		t.Errorf("respawn should not add a node, got %d", len(m.ws.Nodes))
	}
}

func TestRespawnFallsBackToTheWorkspaceDirectory(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = press(t, m, tea.KeyEnter)
	_, cmd := typeKeys(t, m, "y")
	settle(t, m, cmd)

	if len(sessions.created) != 1 || sessions.created[0].dir != "/tmp/work" {
		t.Errorf("a node with no directory of its own respawns in the workspace's, got %v", sessions.created)
	}
}

func TestDecliningARespawnLeavesTheNodeDead(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = press(t, m, tea.KeyEnter)
	m, _ = typeKeys(t, m, "n")

	if len(sessions.created) != 0 {
		t.Errorf("declining should start nothing, got %v", sessions.created)
	}
	if !m.dead["bbb"] {
		t.Error("declining a respawn leaves the node exactly as dead as it was")
	}
}

func TestARespawnThatFailsIsReportedAndTheNodeStaysDead(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	sessions.createErr = errors.New("tmux: no space left on device")
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = press(t, m, tea.KeyEnter)
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if !strings.Contains(m.status, "no space left") {
		t.Errorf("a respawn that failed should say so, got %q", m.status)
	}
	if !m.dead["bbb"] {
		t.Error("a respawn that failed leaves the node dead")
	}
}

func TestXOnADeadNodeOffersToKillItsSessionAndRemovesTheCard(t *testing.T) {
	m, _ := mapWithADeadNode(t)
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = typeKeys(t, m, "x")
	// The prompt matches what y does. A card the map believes dead is killed
	// like any other, because the belief is a derived cache that can be wrong
	// — the case the next test covers.
	if view := m.View(); !strings.Contains(view, "Kill") {
		t.Errorf("the prompt should say the session will be killed, got:\n%s", view)
	}
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if _, ok := m.node("bbb"); ok {
		t.Error("x should remove the node from the map")
	}
	if m.status != "" {
		t.Errorf("removing a dead node is not a failure, got %q", m.status)
	}
}

func TestXOnANodeWronglyBelievedDeadStillKillsItsSession(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	// The session came back — respawned by hand, or the map was told wrong. The
	// card is the only handle the user has on it, so removing the card without
	// killing would abandon the session with nothing left to find it by.
	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})

	m, _ = typeKeys(t, m, "x")
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if want := tmux.SessionName("main", "bbb"); len(sessions.killed) != 1 || sessions.killed[0] != want {
		t.Errorf("killed %v, want %q — tmux decides whether a session is there, not a cached flag", sessions.killed, want)
	}
	if _, ok := m.node("bbb"); ok {
		t.Error("x should remove the node from the map")
	}
}

func TestANodeStillBeingCreatedIsNeverDead(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	m, _ = press(t, m, tea.KeyEnter)
	if len(m.pending) != 1 {
		t.Fatalf("expected the node to be pending, got %+v", m.pending)
	}
	id := m.pending[0].ID
	sessions.live = nil // tmux has not finished making it

	m = reconciled(t, m)

	// "Not there yet" is not the same as dead: the node would otherwise land on
	// the map wearing a dead badge, and be skipped by capture until some later
	// pass corrected it.
	if m.dead[id] {
		t.Error("a node whose session is still being made is not dead")
	}
}

func TestAStalePassDoesNotUndoWhatTheMapHasSinceLearned(t *testing.T) {
	m, _ := mapWithADeadNode(t)
	// A pass sent before the respawn, answering after it.
	stale := m.reconcile()
	msg := stale().(reconciledMsg)

	m = moveTo(t, m, state.Cell{Col: 1, Row: 0})
	m, _ = press(t, m, tea.KeyEnter)
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)
	if m.dead["bbb"] {
		t.Fatal("the respawn should have brought the node back to life")
	}

	next, _ := m.Update(msg)
	m = next.(Model)

	if m.dead["bbb"] {
		t.Error("an answer that predates the respawn must not put the node back in the grave")
	}
}

func TestAForeignWorkspacesSessionIsRejectedWithoutAskingTmuxAboutIt(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	// Reconciliation runs on every session event the whole server produces, so
	// a subprocess per stranger's session per event is a cost the name check
	// exists to avoid.
	sessions.live = []string{tmux.SessionName("other", "zzz"), "someone-elses-work"}

	m = reconciled(t, m)

	if len(sessions.enved) != 0 {
		t.Errorf("no session outside this workspace's prefix should be asked about, got %v", sessions.enved)
	}
	if len(m.ws.Nodes) != 0 {
		t.Errorf("nothing here belongs on this map, got %+v", m.ws.Nodes)
	}
}

func TestASessionThatDiesMidPassIsNotReconstructed(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{tmux.SessionName("main", "zzz")}
	// Listed, then gone by the time it was asked about. A card for it would be
	// dead the moment it arrived and only the user could clear it.
	sessions.envErr = errors.New("tmux: no such session")

	m = reconciled(t, m)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("a session that could not be asked about is left alone, got %+v", m.ws.Nodes)
	}
}

func TestAnOrphanSessionIsReconstructedFromItsEnvironment(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	// The state file was lost; the session that outlived it still knows what
	// it was.
	orphan := tmux.SessionName("main", "zzz")
	sessions.live = []string{orphan}
	sessions.envs = map[string]map[string]string{orphan: {
		"TRIG_WORKSPACE": "main",
		"TRIG_NODE_ID":   "zzz",
		"TRIG_NODE_KIND": "agent",
	}}

	m = reconciled(t, m)

	node, ok := m.node("zzz")
	if !ok {
		t.Fatalf("a session with no node behind it should become a card, got %+v", m.ws.Nodes)
	}
	if node.Kind != state.KindAgent {
		t.Errorf("reconstructed kind = %q, want the one the session carries", node.Kind)
	}
	if m.dead["zzz"] {
		t.Error("a reconstructed node's session is running, so it is alive")
	}
}

func TestAnotherWorkspacesSessionIsNotReconstructed(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	other := tmux.SessionName("other", "zzz")
	sessions.live = []string{other, "someone-elses-work"}
	sessions.envs = map[string]map[string]string{other: {
		"TRIG_WORKSPACE": "other",
		"TRIG_NODE_ID":   "zzz",
		"TRIG_NODE_KIND": "shell",
	}}

	m = reconciled(t, m)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("a map should only reconstruct its own workspace's sessions, got %+v", m.ws.Nodes)
	}
}

func TestAnOrphanWithNoEnvironmentIsRecognisedByItsName(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	// A session whose environment was cleared, or one made by hand under the
	// prefix. The name is the other place the provenance lives (§5.2).
	sessions.live = []string{tmux.SessionName("main", "zzz")}

	m = reconciled(t, m)

	node, ok := m.node("zzz")
	if !ok {
		t.Fatalf("the session name alone should be enough, got %+v", m.ws.Nodes)
	}
	if node.Kind != state.KindShell {
		t.Errorf("reconstructed kind = %q, want a plain shell when nothing says otherwise", node.Kind)
	}
}

func TestAnOrphanIsReconstructedOnlyOnce(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{tmux.SessionName("main", "zzz")}

	m = reconciled(t, m)
	m = reconciled(t, m)
	m = reconciled(t, m)

	if len(m.ws.Nodes) != 1 {
		t.Errorf("reconciliation runs over and over; it must be idempotent, got %+v", m.ws.Nodes)
	}
}

func TestANodeStillBeingCreatedIsNotReconstructedBehindItsBack(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main"})
	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	m, _ = press(t, m, tea.KeyEnter) // the session is made, the node is still pending

	if len(m.pending) != 1 {
		t.Fatalf("expected the node to be pending, got %+v", m.pending)
	}
	sessions.live = []string{tmux.SessionName("main", m.pending[0].ID)}

	m = reconciled(t, m)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("a node tmux has not confirmed yet is not an orphan, got %+v", m.ws.Nodes)
	}
}

func TestAReconstructedNodeSurvivesARestart(t *testing.T) {
	m, sessions, stateDir := newNodeModel(t, state.Workspace{Name: "main"})
	sessions.live = []string{tmux.SessionName("main", "zzz")}

	m = reconciled(t, m)

	saved, err := state.Load(stateDir, "main")
	if err != nil {
		t.Fatalf("reloading the workspace: %v", err)
	}
	if len(saved.Nodes) != 1 || saved.Nodes[0].ID != "zzz" {
		t.Errorf("a reconstructed node should be written to disk, got %+v", saved.Nodes)
	}
}

func TestTmuxRefusingToListIsReportedAndChangesNothing(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	sessions.listErr = errors.New("tmux: server exited unexpectedly")

	m = reconciled(t, m)

	if !strings.Contains(m.status, "server exited") {
		t.Errorf("a failed reconciliation should say so, got %q", m.status)
	}
	if m.dead["aaa"] {
		t.Error("a failed reconciliation must not condemn a node it could not ask about")
	}
}

func TestStartupAsksTmuxWhatIsStillRunning(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api"},
	}})

	drain(t, m.Init())

	if sessions.listed == 0 {
		t.Error("startup should reconcile the map against the live sessions")
	}
}

func TestTheSlowTickAndSessionEventsKeepTheMapHonest(t *testing.T) {
	for name, msg := range map[string]tea.Msg{
		"the slow tick":   refreshTickMsg{},
		"a session event": tmuxEventMsg{ev: tmux.Event{Kind: tmux.Sessions}},
	} {
		t.Run(name, func(t *testing.T) {
			m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
				{ID: "aaa", Kind: state.KindShell, Title: "api"},
			}})
			_, cmd := m.Update(msg)
			drain(t, cmd)

			if sessions.listed == 0 {
				t.Errorf("%s should reconcile the map against the live sessions", name)
			}
		})
	}
}

func TestADeadNodeIsNotCaptured(t *testing.T) {
	m, sessions := mapWithADeadNode(t)
	sessions.captured = nil

	m = update(t, m, refreshTickMsg{})
	m = run(t, m, captureDueMsg{})

	for _, c := range sessions.captured {
		if c.session == tmux.SessionName("main", "bbb") {
			t.Errorf("a dead session has nothing to capture, got %v", sessions.captured)
		}
	}
}

// moveTo puts the cursor on a cell, which is how a test selects a node.
func moveTo(t *testing.T, m Model, cell state.Cell) Model {
	t.Helper()
	m.ws.Viewport.Cursor = cell
	return m
}

func TestAnOlderPassDoesNotUndoANewerOnesVerdict(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Dir: "/tmp/work", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Pos: state.Cell{Col: 0, Row: 0}},
		{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1, Row: 0}},
	}})
	both := []string{tmux.SessionName("main", "aaa"), tmux.SessionName("main", "bbb")}
	sessions.live = both
	// A session of ours with no node behind it, so the older pass carries an
	// orphan as well as a verdict.
	sessions.envs = map[string]map[string]string{
		tmux.SessionName("main", "ccc"): {"TRIG_WORKSPACE": "main", "TRIG_NODE_ID": "ccc"},
	}

	// The slow tick sends one pass; the session dies; the Sessions event that
	// follows sends another. Both are in flight at once.
	older := m.reconcile()
	newer := m.reconcile()

	// The newer pass answers first, and finds the session gone.
	sessions.live = []string{tmux.SessionName("main", "aaa")}
	next, _ := m.Update(newer())
	m = next.(Model)
	if !m.dead["bbb"] {
		t.Fatal("the newer pass should have found the session gone")
	}

	// The older one lands afterwards, still describing the world from before.
	sessions.live = append(both, tmux.SessionName("main", "ccc"))
	next, cmd := m.Update(older())
	m = settle(t, next.(Model), cmd)

	if !m.dead["bbb"] {
		t.Error("an answer from before the session died must not bring the card back to life")
	}
	// The orphan is not a verdict: a session that was there when the pass ran
	// is not un-found by anything that has happened since.
	if _, known := m.node("ccc"); !known {
		t.Error("a stale pass's orphan should still be reconstructed")
	}
}
