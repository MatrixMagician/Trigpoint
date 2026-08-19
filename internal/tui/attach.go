package tui

import (
	"cmp"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// attachedMsg says the terminal is Trigpoint's again, carrying whatever went
// wrong while it was not.
type attachedMsg struct{ err error }

// attach is the handoff (SPEC §5.1): the whole terminal goes to the selected
// node's session, so that TUI apps, colours, resize, and copy-mode behave
// exactly as they would under a plain tmux attach. Trigpoint emulates none of
// it — Bubble Tea releases the terminal, tmux takes it, and the detach binding
// installed for the duration hands it back.
func (m Model) attach() (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok || m.handoff != "" {
		return m, nil
	}
	session := m.sessionOf(node)

	// tmux is asked here rather than trusted from the last render: a session
	// that died in between would otherwise be handed a terminal it cannot use,
	// and the complaint would flash past on a screen already being repainted.
	alive, err := m.sessions.Exists(session)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	if !alive {
		// Nothing to hand the terminal to, so Enter means the other thing it
		// can mean on a node: offer to start it again (§9.2). The card is
		// corrected here too — the map believed this node alive until now.
		m = m.markDead(node.ID)
		if node.Adopted() {
			// Except on an adopted node, where there is no command to re-run and
			// no session name to re-run it under: Trigpoint did not start this
			// session and knows nothing about what was in it (§9.3). The card
			// stays, dead, until x removes it.
			m.status = "The session " + flatten(node.Session) + " has gone, and Trigpoint did not start it — there is nothing to respawn."
			return m, nil
		}
		return m.confirmRespawn(node), nil
	}

	// Both tmux calls happen on the keystroke rather than in a command: the
	// very next thing is handing the terminal over, so there is nothing the map
	// could usefully draw in between.
	handoff, release, err := m.sessions.Handoff(session, m.cfg.General.DetachKey)
	if err != nil {
		// Attaching with no way back would leave the user inside the session
		// with the map unreachable, so a handoff that cannot be prepared stays
		// on the map and says why.
		m.status = err.Error()
		return m, nil
	}
	// tmux writes the session to stdout and its complaints to stderr, so the
	// complaint is caught rather than left under the repainted map.
	var stderr strings.Builder
	handoff.Stderr = &stderr

	// Held until the terminal comes back. Handoff runs on the keystroke but the
	// terminal only changes hands once Bubble Tea reaches the exec, and a
	// second Enter landing in that gap would install a second binding for the
	// first attach to release — leaving the second one with no way back.
	m.handoff = node.ID
	// Attaching is looking, so whatever arrived on this node while you were
	// elsewhere has now been seen (§8). The mark is cleared again on the way
	// back, for the output that arrives while you are inside.
	m = m.read(node.ID)
	return m, tea.ExecProcess(handoff, onReturn(release, &stderr))
}

// onReturn is what happens on the way back from a handoff. The binding comes
// out however the attach went: left installed, the detach key would fire on a
// map that has nothing to detach.
func onReturn(release func() error, stderr *strings.Builder) tea.ExecCallback {
	return func(err error) tea.Msg {
		unbound := release()
		return attachedMsg{err: cmp.Or(exitErr(err, stderr.String()), unbound)}
	}
}

// exitErr is what to say about a program that ended badly — tmux on the way
// back from an attach, $EDITOR on the way back from a note. The program's own
// complaint is the useful half; an exit status on its own sends the user
// hunting for what it meant.
func exitErr(err error, stderr string) error {
	if err == nil {
		return nil
	}
	if complaint := strings.TrimSpace(stderr); complaint != "" {
		return errors.New(complaint)
	}
	return err
}

// confirmRespawn puts the respawn question up. The node is named by id, for the
// same reason the kill prompt is: whatever moves under the cursor between the
// question and the answer, the answer is about the node that was asked about.
func (m Model) confirmRespawn(node state.Node) Model {
	m.mode, m.respawning = modeConfirmRespawn, node.ID
	return m
}

func (m Model) updateConfirmRespawn(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = modeNormal
		id := m.respawning
		m.respawning = ""
		node, ok := m.node(id)
		if !ok {
			return m, nil
		}
		return m.respawn(node)
	case "n", "N", "esc", "q":
		m.mode, m.respawning = modeNormal, ""
	}
	return m, nil
}
