package tui

// Peek (SPEC §7.3): a node's recent output, full-screen and scrollable, read
// without attaching. The read-only counterpart to attach — peek never gives the
// node your keyboard, which is why nothing in this file writes to a session and
// no key read here goes anywhere but the scroll position.
//
// A peek is a snapshot like a preview is, taken once when it opens. It does not
// follow the session: what is on screen is what the node had said by the time
// you looked, and scrolling through it is reading, not watching.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// peekLines is how far back a peek reaches (SPEC §7.3). Two orders of magnitude
// past a card's body, because peek exists for the question a preview cannot
// answer: not "is this node busy" but "what has it been doing".
const peekLines = 2000

// peekTakenMsg carries a snapshot back from tmux, named by the node that asked
// for it — the cursor is the user's and may have moved on while tmux was
// answering.
type peekTakenMsg struct {
	id    string
	lines []string
	err   error
}

// peek opens the snapshot on the selected node. Looking is what clears unread
// (§8), and the keystroke is the looking: the snapshot arriving is tmux's
// business, not the user's.
func (m Model) peek() (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok || !node.HasSession() {
		// A note has never had a session, so it has no output to read (§6).
		return m, nil
	}
	m = m.read(node.ID)
	if m.dead[node.ID] {
		// There is no session left to capture from, so the last snapshot taken
		// of it is all there is — and when there was never one, the peek says
		// so rather than opening blank (§9.2).
		return m.openPeek(node.ID, m.previews[node.ID]), nil
	}

	sessions, session := m.sessions, m.sessionOf(node)
	return m, func() tea.Msg {
		text, err := sessions.Capture(session, peekLines)
		if err != nil {
			// Unlike a preview's capture, which fails quietly on every tick,
			// this one was asked for by hand — so it is answered.
			return peekTakenMsg{id: node.ID, err: err}
		}
		return peekTakenMsg{id: node.ID, lines: trimBlank(strings.Split(strings.TrimRight(text, "\n"), "\n"))}
	}
}

func (m Model) updatePeekMsg(msg tea.Msg) (Model, tea.Cmd, bool) {
	taken, ok := msg.(peekTakenMsg)
	if !ok {
		return m, nil, false
	}
	if taken.err != nil {
		m.status = taken.err.Error()
		return m, nil, true
	}
	if _, known := m.node(taken.id); !known {
		// The node left the map while tmux was answering. Opening a peek on it
		// would be a full screen of a card that is no longer there.
		return m, nil, true
	}
	return m.openPeek(taken.id, taken.lines), nil, true
}

// openPeek puts the snapshot on screen, scrolled to its end: the newest output
// is what "what has this been doing" means, and the older lines are what you
// scroll back through to find out why.
func (m Model) openPeek(id string, lines []string) Model {
	m.mode, m.peeking, m.peeked = modePeek, id, lines
	m.peekTop = len(lines) - m.peekRows()
	return m.clampPeek()
}

// updatePeek is every key a peek reads. Esc closes it and the rest scroll;
// there is deliberately no key here that acts on the map or on the node, so a
// keystroke landing during a peek can do neither.
func (m Model) updatePeek(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := maxInt(m.peekRows()-1, 1)
	switch msg.String() {
	case "esc":
		m.mode, m.peeking, m.peeked, m.peekTop = modeNormal, "", nil, 0
		return m, nil
	case "j", "down":
		m.peekTop++
	case "k", "up":
		m.peekTop--
	case " ", "pgdown", "ctrl+f", "ctrl+d":
		m.peekTop += page
	case "b", "pgup", "ctrl+b", "ctrl+u":
		m.peekTop -= page
	case "g", "home":
		m.peekTop = 0
	case "G", "end":
		m.peekTop = len(m.peeked)
	}
	return m.clampPeek(), nil
}

// clampPeek keeps the window inside the snapshot: scrolling is a request, and
// the snapshot has ends.
func (m Model) clampPeek() Model {
	m.peekTop = clamp(m.peekTop, 0, maxInt(len(m.peeked)-m.peekRows(), 0))
	return m
}

// peekRows is how many lines of snapshot the terminal has room for: everything
// but the status bar.
func (m Model) peekRows() int { return maxInt(m.height-1, 1) }

// peekView is the snapshot itself, drawn from the top left like the terminal it
// came from rather than centred like a map.
func (m Model) peekView() string {
	if len(m.peeked) == 0 {
		return hintStyle.Render("Nothing has been captured from this node.")
	}
	end := m.peekTop + m.peekRows()
	if end > len(m.peeked) {
		end = len(m.peeked)
	}
	shown := make([]string, 0, end-m.peekTop)
	for _, line := range m.peeked[m.peekTop:end] {
		// The lines arrive with the colour tmux captured them in, and are cut
		// by cell rather than by byte so a cut never lands inside an escape
		// sequence.
		shown = append(shown, closeStyling(ansi.Truncate(line, m.width, "")))
	}
	return lipgloss.Place(m.width, m.peekRows(), lipgloss.Left, lipgloss.Top, strings.Join(shown, "\n"))
}

// peekBar says which node is being read and where in its output you are, which
// is the only orientation a full screen of somebody else's log gives you.
func (m Model) peekBar() string {
	title := m.peeking
	if node, ok := m.node(m.peeking); ok {
		title = node.Title
	}
	where := "no output"
	if len(m.peeked) > 0 {
		end := m.peekTop + m.peekRows()
		if end > len(m.peeked) {
			end = len(m.peeked)
		}
		where = fmt.Sprintf("%d–%d of %d", m.peekTop+1, end, len(m.peeked))
	}
	return fmt.Sprintf("peek %s · %s · j/k scroll · esc map", flatten(title), where)
}

// Unread (§8) is a property of your attention, not of the node's work: output
// arriving while you are elsewhere sets it, and looking — attaching or peeking —
// is the only thing that clears it. See CONTEXT.md, "Unread".

// unreadOn marks a node's card unread, in a fresh map: a Model is copied by
// value all over Bubble Tea, and writing through would edit the state every
// older copy is still holding.
func (m Model) unreadOn(id string) Model {
	if m.unread[id] {
		return m
	}
	m.unread = withID(m.unread, id, true)
	return m
}

// read clears the mark, which is what looking at a node does and the only thing
// that does it. An empty id is the no-node case — a handoff to an editor rather
// than to a session — and clears nothing.
func (m Model) read(id string) Model {
	if id == "" || !m.unread[id] {
		return m
	}
	m.unread = withID(m.unread, id, false)
	return m
}

// withID is a copy of a set with one id added or removed.
func withID(set map[string]bool, id string, member bool) map[string]bool {
	next := make(map[string]bool, len(set)+1)
	for other := range set {
		if other != id {
			next[other] = true
		}
	}
	if member {
		next[id] = true
	}
	return next
}
