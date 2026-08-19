package tui

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/tmux"
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
	if !ok || m.handingOff {
		return m, nil
	}
	session := tmux.SessionName(m.ws.Name, node.ID)

	// tmux is asked here rather than trusted from the last render: a session
	// that died in between would otherwise be handed a terminal it cannot use,
	// and the complaint would flash past on a screen already being repainted.
	alive, err := m.sessions.Exists(session)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	if !alive {
		m.status = fmt.Sprintf("%s has no live session to attach to", flatten(node.Title))
		return m, nil
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
	m.handingOff = true
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
