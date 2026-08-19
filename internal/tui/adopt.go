package tui

// Adoption (SPEC §9.3): a tmux session Trigpoint did not create, mapped onto the
// map as a node. Nothing is created and nothing is renamed — the session was
// already running, and the node is a card over it that stores its name. That
// stored name is what every session-touching path then asks for, so an adopted
// node attaches, previews, and reconciles like any other shell node. See
// docs/adr/0007-an-adopted-node-stores-its-session-name.md.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// adoptedTag marks an adopted card for the reader. Nothing branches on it —
// tags are the user's to edit, and behaviour keys off the stored session name.
const adoptedTag = "adopted"

// adoptableMsg is the answer to what there is to adopt: every session on the
// server that is nobody's node yet, and the ones that could not be offered.
type adoptableMsg struct {
	names   []string
	skipped []string
	err     error
}

// addressable reports whether tmux can be asked about a session by name. tmux
// accepts "." and ":" in a session name and then reads them straight back as
// its window and pane separators, so `-t "=my.proj"` is a pane that is not
// there — and, for capture-pane, quietly a different session's output. A name
// like that cannot be attached to, previewed, or killed, so it is never
// offered: a card that cannot do any of those is worse than no card.
//
// This is the same judgement state.ValidName makes about workspace names, for
// the same reason, at the other end of the same string.
//
// ponytail: sessions are addressed by name throughout. Targeting by
// `#{session_id}` would reach these too, at the cost of a name-to-id lookup on
// every capture — worth it only if these names turn out to be common.
func addressable(session string) bool { return !strings.ContainsAny(session, ".:") }

// adoptable asks tmux what is running and keeps the strangers. Trigpoint's own
// sessions are not adoptable — they already have cards, on this map or on
// another workspace's — and neither is a session this map has adopted already.
func (m Model) adoptable() tea.Cmd {
	taken := make(map[string]bool, len(m.ws.Nodes))
	for _, n := range m.ws.Nodes {
		if n.Adopted() {
			taken[n.Session] = true
		}
	}
	sessions := m.sessions
	return func() tea.Msg {
		names, err := sessions.List()
		if err != nil {
			return adoptableMsg{err: err}
		}
		// Sorted, so that the same server offers the same first candidate every
		// time rather than whichever session tmux happened to name first.
		sort.Strings(names)
		var free, skipped []string
		for _, name := range names {
			if tmux.Ours(name) || taken[name] {
				continue
			}
			if !addressable(name) {
				skipped = append(skipped, name)
				continue
			}
			free = append(free, name)
		}
		return adoptableMsg{names: free, skipped: skipped}
	}
}

// openAdoption puts the picker up, or says why there is nothing to put up.
func (m Model) openAdoption(msg adoptableMsg) Model {
	if m.mode != modeNormal {
		// The question was a subprocess, and the user has moved on: a title half
		// typed, a kill waiting on y. Taking the keyboard now would turn the
		// next Enter into an adoption of a session nobody is looking at, so the
		// answer is dropped — A is one keystroke away whenever it is wanted.
		return m
	}
	switch {
	case msg.err != nil:
		// A list that could not be asked is not a list of nothing: saying "no
		// sessions to adopt" here would blame the tmux zoo for tmux being
		// unreachable.
		m.status = msg.err.Error()
	case len(msg.names) == 0 && len(msg.skipped) > 0:
		m.status = "Nothing to adopt: tmux reads \".\" and \":\" in a session name as a window or a pane, so " +
			flatten(strings.Join(msg.skipped, ", ")) + " cannot be addressed by name."
	case len(msg.names) == 0:
		m.status = "Nothing to adopt: every session on the server is Trigpoint's own."
	default:
		m.mode, m.candidates, m.choice = modeAdopt, msg.names, 0
	}
	return m
}

// ponytail: the picker lives in the status bar, one candidate at a time. The
// palette (§7.2) is where adoption candidates properly belong and it lands with
// M3; this is the M2 stand-in, and it goes when the palette arrives.
func (m Model) updateAdopt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.choice = (m.choice + 1) % len(m.candidates)
	case "k", "up":
		m.choice = (m.choice + len(m.candidates) - 1) % len(m.candidates)
	case "enter":
		session := m.candidates[m.choice]
		return m.closePicker().adopt(session)
	case "esc", "q":
		return m.closePicker(), nil
	}
	return m, nil
}

func (m Model) closeAdoption() Model {
	m.mode, m.candidates, m.choice = modeNormal, nil, 0
	return m
}

// adopt puts a foreign session on the map. There is no tmux call in here at
// all: the session is already running, adoption renames nothing, and the whole
// operation is a card appearing over something that was already there.
func (m Model) adopt(session string) (tea.Model, tea.Cmd) {
	// Re-checked here rather than trusted from when the list was drawn: a second
	// press of A while the first answer was being acted on offers a session that
	// now has a card. Two nodes over one session would fight over its liveness —
	// reconciliation classifies by session name — and killing either would leave
	// the other as a card over nothing.
	for _, n := range m.ws.Nodes {
		if n.Session == session {
			m.status = "The session " + flatten(session) + " is already on the map."
			return m, nil
		}
	}

	// The pending nodes are counted in, for the same reason createNode counts
	// them: their sessions are being made right now, and a cell or an id
	// reserved for one of them is not free.
	draft := m.ws
	draft.Nodes = append(append([]state.Node(nil), m.ws.Nodes...), m.pending...)

	node := state.Node{
		ID:      draft.NewNodeID(),
		Kind:    state.KindShell,
		Title:   clampRunes(sanitise(session), maxTitleLen),
		Tags:    []string{adoptedTag},
		Pos:     draft.NearestFreeCell(m.ws.Viewport.Cursor),
		Session: session,
		// CreatedAt is left zero, so the card shows no age rather than the age
		// of its own adoption: the session is as old as it is, and Trigpoint was
		// not there for the start of it.
	}
	next, cmd := m.place(node).save().markDirty(node.ID)
	return next, cmd
}

// sessionOf is the tmux session behind a node: the one Trigpoint names after
// the node, or — for an adopted node — the foreign session's own name, which
// adoption stores rather than imposes a prefix on (§9.3).
func (m Model) sessionOf(n state.Node) string {
	if n.Adopted() {
		return n.Session
	}
	return tmux.SessionName(m.ws.Name, n.ID)
}
