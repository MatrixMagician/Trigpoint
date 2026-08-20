package tui

// Reconciliation (SPEC §9.2): what the map believes, checked against what tmux
// is actually running. A node whose session is there is alive; a node whose
// session is gone is dead; a session of ours with no node behind it is a card
// this map lost and gets back.
//
// Liveness is derived here and never written to the workspace file — see
// docs/adr/0006-liveness-is-derived-not-stored.md.

import (
	"sort"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// reconciledMsg is one pass's answer: which nodes have no session left, and
// which sessions have no node yet.
type reconciledMsg struct {
	mapStamp
	dead    map[string]bool
	orphans []state.Node
	err     error
	// at is the number of corrections the map had made when the pass was sent.
	// An answer that predates a correction is stale by definition, and applying
	// it would take the map back to before something it has since been told.
	at int
	// seq is which pass this is. Passes are spawned freely — the slow tick,
	// every change to the session list, opening a map — so two are often in
	// flight at once, and they are applied in whatever order tmux answers.
	// Numbering them is what tells an answer from an older one behind it.
	seq uint64
}

// asked numbers the questions this process puts to tmux, so that the answers
// can be ordered. It is process-wide rather than a field on the Model because a
// Model is copied by value all over Bubble Tea: a counter incremented on the
// copy that asks would be missing from the copy that applies, which is the same
// hole again. Nothing reads the number but the map that sent it, against the
// last one it applied, so a second map sharing the sequence costs only gaps.
var asked atomic.Uint64

// reconcile asks tmux what is running and works out what that means for this
// map. It runs at startup, on every change to the session list, and on the slow
// tick — the last of which is also the only thing keeping the map honest while
// the control-mode client is down (§5.3).
//
// The whole pass happens off the update loop, so the answer describes the map
// as it was when the question was asked; applying it re-checks what has moved.
func (m Model) reconcile() tea.Cmd {
	// Two sets, because a pending node answers the two questions differently.
	// It counts as known — tmux may already have made its session, and
	// reconstructing a node that is moments from arriving would put it on the
	// map twice — but it is not classified: the session is being made right
	// now, and "not there yet" is not the same as dead.
	owned := make(map[string]bool, len(m.ws.Nodes)+len(m.pending))
	for _, n := range m.pending {
		owned[m.ws.SessionOf(n)] = true
	}
	for _, n := range m.ws.Nodes {
		if n.HasSession() {
			// The node's own session name, which for an adopted node is the
			// foreign one it stores rather than one derived from its id (§9.3).
			// That is the whole of what reconciliation has to know about
			// adoption: a foreign session with a node has been accounted for,
			// and one without is a stranger the orphan pass below leaves alone.
			owned[m.ws.SessionOf(n)] = true
		}
	}

	sessions, ws, at, stamp, seq := m.sessions, m.ws, m.corrections, m.stamp(), asked.Add(1)
	workspace := ws.Name
	return func() tea.Msg {
		names, err := sessions.List()
		if err != nil {
			// The map is left exactly as it was. A pass that could not ask must
			// not condemn every node on the strength of not knowing.
			return reconciledMsg{mapStamp: stamp, err: err, at: at, seq: seq}
		}
		// Liveness is the workspace's own rule, so that `trig ls` and the map
		// call the same node dead (ADR 0017).
		dead := ws.Dead(names)

		// Sorted, so that two orphans reconstructed in the same pass land in
		// the same two cells every time.
		sort.Strings(names)
		var orphans []state.Node
		for _, name := range names {
			if !tmux.Ours(name) || owned[name] {
				continue
			}
			if node, ok := reconstruct(sessions, workspace, name); ok {
				orphans = append(orphans, node)
			}
		}
		return reconciledMsg{mapStamp: stamp, dead: dead, orphans: orphans, at: at, seq: seq}
	}
}

// reconstruct builds a card from a session that has outlived the state file
// naming it. The name vetoes and the environment decides: a workspace name may
// contain "_", so trig_a_b_c could be node "c" of workspace "a_b" or node "b_c"
// of workspace "a", and only TRIG_WORKSPACE settles that. The name comes back
// as the fallback for the id when the environment was cleared, or when the
// session was made by hand under the prefix.
func reconstruct(sessions Sessions, workspace, session string) (state.Node, bool) {
	// The name is asked first because it is free and it can only ever say no:
	// every session of this map's is named trig_<workspace>_<id>, so one that
	// is not cannot be ours whatever its environment claims. That keeps every
	// other workspace's sessions off the subprocess path entirely, and
	// reconciliation runs on every session event the whole server produces.
	fromName, ok := strings.CutPrefix(session, tmux.SessionName(workspace, ""))
	if !ok || fromName == "" {
		return state.Node{}, false
	}

	env, err := sessions.Env(session)
	if err != nil {
		// Most often a session that died between being listed and being asked
		// about. Reconstructing it would put a card on the map that is dead the
		// moment it arrives, and that nothing but the user can clear.
		return state.Node{}, false
	}
	if from := env["TRIG_WORKSPACE"]; from != "" && from != workspace {
		// A workspace whose name starts with this one's, which is the only way
		// the name check above can say yes about someone else's session. It is
		// not this map's to show, and it is not an orphan either — the map that
		// owns it will reconstruct it.
		return state.Node{}, false
	}

	id := env["TRIG_NODE_ID"]
	if id == "" || tmux.SessionName(workspace, id) != session {
		// Nothing authoritative to go on, or an id that does not match the
		// session it claims to name. The name is what is left, and for this
		// workspace's own prefix it is unambiguous.
		id = fromName
	}

	kind := state.Kind(env["TRIG_NODE_KIND"])
	if kind != state.KindAgent {
		// A note never had a session, so a session claiming to be one is a
		// session lying; anything unrecognised is a shell, which is what a
		// session with a login shell in it actually is.
		kind = state.KindShell
	}
	// The title is the id: the human-readable name lived in the state file that
	// is gone, and inventing one would be a card claiming to know more than it
	// does. CreatedAt is left zero for the same reason — the card shows no age
	// rather than the age of its own rediscovery.
	return state.Node{ID: id, Kind: kind, Title: id}, true
}

// applyReconciled folds a pass's answer into the map: the dead set replaces the
// one before it, and any reconstructed node that is still missing is placed.
func (m Model) applyReconciled(msg reconciledMsg) (Model, tea.Cmd) {
	if !msg.about(m) {
		// A pass the map that asked for it has since been switched away from.
		// Its dead set and its orphans are both about that map's node ids, and
		// this one's file is not theirs to be written to.
		return m, nil
	}
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}
	// The orphans are still worth having — a session that was there when the
	// pass ran is not un-found by anything the map has since learned — but a
	// stale verdict on liveness would undo an attach that found a session gone,
	// or a respawn that has just brought one back, or a newer pass that has
	// already seen further than this one. Both guards have to hold: they answer
	// different questions, and an answer is only current if it is behind
	// neither.
	if msg.seq >= m.applied {
		m.applied = msg.seq
		if msg.at == m.corrections {
			m.dead = msg.dead
		}
	}

	var arrived []string
	for _, node := range msg.orphans {
		// Re-checked here rather than trusted from when the question was asked:
		// the node may have arrived under its own steam while tmux was
		// answering.
		if _, known := m.node(node.ID); known {
			continue
		}
		if pending(m.pending, node.ID) {
			continue
		}
		node.Pos = m.ws.NearestFreeCell(m.ws.Viewport.Cursor)
		// Appended rather than placed: the cursor belongs to the user, and a
		// card the map found on its own has no business moving it.
		m.ws.Nodes = append(append([]state.Node(nil), m.ws.Nodes...), node)
		arrived = append(arrived, node.ID)
	}
	if len(arrived) == 0 {
		return m, nil
	}
	return m.save().markDirty(arrived...)
}

func pending(nodes []state.Node, id string) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// respawnedMsg is what tmux made of a respawn.
type respawnedMsg struct {
	mapStamp
	id  string
	err error
}

// respawn starts a fresh session for a node whose old one is gone (§9.2). The
// node keeps its id — and so its session name, its place, and everything else
// it is — because respawning is not creating a new node.
func (m Model) respawn(node state.Node) (tea.Model, tea.Cmd) {
	sessions, workspace, dir, stamp := m.sessions, m.ws.Name, m.ws.DirOf(node), m.stamp()
	env := state.Provenance(workspace, node, m.statusDir)
	return m, func() tea.Msg {
		err := sessions.Create(tmux.SessionName(workspace, node.ID), dir, node.StartCmd(), env)
		return respawnedMsg{mapStamp: stamp, id: node.ID, err: err}
	}
}

// alive takes a node out of the dead set, in a fresh map: a Model is copied by
// value all over Bubble Tea, and writing through would edit the state every
// older copy is still holding.
func (m Model) alive(id string) Model {
	if !m.dead[id] {
		return m
	}
	m.corrections++
	dead := make(map[string]bool, len(m.dead))
	for other := range m.dead {
		if other != id {
			dead[other] = true
		}
	}
	m.dead = dead
	return m
}

// markDead is what an attach discovers: the map thought the node was alive and
// tmux says otherwise, so the card is corrected on the spot rather than at the
// next pass.
func (m Model) markDead(id string) Model {
	if m.dead[id] {
		return m
	}
	m.corrections++
	dead := make(map[string]bool, len(m.dead)+1)
	for other := range m.dead {
		dead[other] = true
	}
	dead[id] = true
	m.dead = dead
	return m
}
