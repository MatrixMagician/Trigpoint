package tui

// Workspaces and switching (SPEC §6, §7.3). A workspace is an independent map,
// and switching to another changes everything on screen: they share no nodes, no
// groups, and no default working directory.
//
// The view holds one workspace at a time. Switching writes the one being left
// and reads the one arrived at — see
// docs/adr/0010-a-workspace-switch-is-a-reload.md.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// mapStamp marks a message with the map it is an answer about. Every question
// Trigpoint puts to tmux is asked off the update loop, and a switch reloads
// everything in between — so an answer that arrives after one is about a map that
// is no longer on screen, and node ids are unique against one map rather than
// against the state directory. Applying it would put one workspace's nodes,
// output, or verdicts on another's cards.
type mapStamp struct{ workspace string }

// stamp is the map as it is now, for a message to carry back with it.
func (m Model) stamp() mapStamp { return mapStamp{workspace: m.ws.Name} }

// about reports whether an answer is about the map now on screen.
func (s mapStamp) about(m Model) bool { return s.workspace == m.ws.Name }

// workspaces is every workspace there is, in name order, with the one on screen
// counted in whether or not it has ever been written: a workspace opened by name
// for the first time has no file yet, and Tab still has to be able to come back
// to it.
//
// The directory is read on the keystroke rather than cached: another trig may
// have made a workspace since this one launched, and one readdir costs less than
// the two fsyncs every mutation already spends.
func (m Model) workspaces() ([]string, error) {
	names, err := state.List(m.stateDir)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(names, m.ws.Name) {
		names = append(names, m.ws.Name)
		slices.Sort(names)
	}
	return names, nil
}

// cycleWorkspace is Tab and Shift-Tab: one step through the workspaces in name
// order, wrapping at both ends.
func (m Model) cycleWorkspace(step int) (tea.Model, tea.Cmd) {
	names, err := m.workspaces()
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	if len(names) < 2 {
		// Silence here would read as a broken key rather than as a map with
		// nowhere to switch to.
		m.status = "Only one workspace. Press w then n to make another."
		return m, nil
	}
	at := slices.Index(names, m.ws.Name)
	return m.switchTo(names[((at+step)%len(names)+len(names))%len(names)])
}

// switchTo leaves one map for another. The one being left is written first: its
// viewport is where you were looking, and coming back to somewhere else would
// make Tab a way of losing your place.
func (m Model) switchTo(name string) (Model, tea.Cmd) {
	if name == m.ws.Name {
		return m, nil
	}
	m.status = ""
	left := m.save()
	if left.status != "" {
		// The map could not be written, and open() would replace it — and clear
		// the very message saying so. Leaving now would discard where the user
		// was looking, and everything the file has not caught up with, silently.
		return left, nil
	}
	return left.open(name)
}

// open reads a workspace and puts it on screen, without writing the one it
// replaces — which is what deleting the open workspace needs, and what switching
// does after saving.
//
// Everything the view had derived about the old map goes with it. Previews,
// unread marks, and the dead set are keyed by node id, and an id is only unique
// against the map it belongs to, so carrying them across would show one
// workspace's output on another's card.
func (m Model) open(name string) (Model, tea.Cmd) {
	ws, err := state.Load(m.stateDir, name)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.ws, m.status = ws, ""
	m.previews, m.dirty, m.unread, m.dead = nil, nil, nil, nil
	// The filter goes with them. It is a way of looking at the map being left,
	// and carrying it over would narrow a map nobody filtered — and could hide
	// the very card the cursor arrives from the file on (§7.1).
	m.filter = ""
	// So does the selection, for the same reason the previews do: an id is only
	// unique against the map it belongs to.
	m.selection = nil
	// And the held group: a group id belongs to the map it was drawn on, and a
	// switch leaves the hand holding a rectangle that is no longer there.
	m = m.release()
	// The pending nodes belong to the map that asked for them, so they are
	// dropped too — and a create that lands afterwards is dropped with them,
	// because nothing on this map is waiting for it.
	//
	// ponytail: the session is on its way regardless, so the map that ordered it
	// gets a card back at its next reconciliation pass (§9.2) — named after the
	// node's id rather than the title that was typed. The window is one tmux
	// call wide; hold pending nodes per workspace if it ever bites.
	m.pending = nil

	// The cursor arrives from the file, so the viewport has to be re-followed:
	// this terminal is not necessarily the shape the one that saved it was.
	m = m.follow()
	shown, cmd := m.markDirty(m.visible()...)
	// Reconciliation, because the map opens on what a previous session left
	// behind — the same reason Init runs it (§9.2).
	return shown, tea.Batch(cmd, shown.reconcile())
}

// openWorkspaces puts the picker up, on the workspace already on screen — so w
// is a way of moving between maps rather than a way of losing the one you are on.
func (m Model) openWorkspaces() (tea.Model, tea.Cmd) {
	names, err := m.workspaces()
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.mode, m.candidates, m.choice = modeWorkspace, names, maxInt(slices.Index(names, m.ws.Name), 0)
	return m, nil
}

// updateWorkspace is the picker's own keys. It stays now the palette offers
// workspace switching too (§7.2): the palette is the backstop for finding
// things, and `w` is the fast path for the one list a user steps through
// without knowing what they are looking for — it is also the only place a
// workspace is made or deleted.
func (m Model) updateWorkspace(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.choice = (m.choice + 1) % len(m.candidates)
	case "k", "up":
		m.choice = (m.choice + len(m.candidates) - 1) % len(m.candidates)
	case "enter":
		name := m.candidates[m.choice]
		return m.closePicker().switchTo(name)
	case "n":
		m.mode, m.input = modeNewWorkspace, ""
	case "x":
		if len(m.candidates) < 2 {
			// Refused with the picker closed: the picker owns the status bar
			// while it is up, and the reason would never be read.
			m = m.closePicker()
			m.status = "The last workspace cannot be deleted — there would be no map to be on."
			return m, nil
		}
		m.mode = modeConfirmDeleteWorkspace
	case "esc", "q":
		return m.closePicker(), nil
	}
	return m, nil
}

func (m Model) updateNewWorkspace(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m, act := m.typed(msg, state.MaxNameLen)
	// typed bounds runes, and a name is bounded in bytes: what it has to fit
	// inside is a filename. Bounding it here rather than refusing it at Enter,
	// because a name too long is a name to stop collecting, not one to reject.
	m.input = clampBytes(m.input, state.MaxNameLen)
	switch act {
	case actCancel:
		// Back to the picker rather than out to the map: Esc undoes the n, and
		// the list is still the thing being looked at.
		m.mode, m.input = modeWorkspace, ""
	case actCommit:
		name := strings.TrimSpace(m.input)
		m = m.closePicker()
		if name == "" {
			return m, nil
		}
		// The name is carried into every session this workspace makes
		// (trig_<workspace>_<id>, §5.2), so the prompt holds the same boundary
		// the command line does rather than writing a file tmux could never be
		// asked about.
		if err := state.ValidName(name); err != nil {
			m.status = err.Error()
			return m, nil
		}
		names, err := m.workspaces()
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		if slices.Contains(names, name) {
			// A name that is taken opens that workspace rather than emptying
			// it: n is how a workspace is reached, and the map behind an
			// existing name is not this keystroke's to throw away.
			return m.switchTo(name)
		}
		// Written empty on the spot rather than at its first node, so that it is
		// in the cycle from the moment it exists.
		if err := state.Save(m.stateDir, state.Workspace{Name: name}); err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m.switchTo(name)
	}
	return m, nil
}

func (m Model) updateConfirmDeleteWorkspace(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name := m.candidates[m.choice]
		replacement := m.nextOf(name)
		m = m.closePicker()
		if err := state.Remove(m.stateDir, name); err != nil {
			m.status = err.Error()
			return m, nil
		}
		if name != m.ws.Name {
			return m, nil
		}
		// The workspace deleted is the one on screen, so another is opened —
		// and opened rather than switched to, because writing the map on the
		// way out would put the file straight back.
		return m.open(replacement)
	case "n", "N", "esc", "q":
		// Back to the picker: declining a delete is not a reason to close the
		// list it was chosen from.
		m.mode = modeWorkspace
	}
	return m, nil
}

// nextOf is the workspace to fall back to when name goes: the one after it in
// the cycle, which is where Tab would have gone. There is always one, because
// deleting the last workspace is refused.
func (m Model) nextOf(name string) string {
	at := maxInt(slices.Index(m.candidates, name), 0)
	for i := 1; i <= len(m.candidates); i++ {
		if other := m.candidates[(at+i)%len(m.candidates)]; other != name {
			return other
		}
	}
	return name
}

// closePicker puts the status bar back. The two pickers — adoption and
// workspaces — share the candidate list and the cursor into it, because a
// keyboard can only be in one of them at a time.
func (m Model) closePicker() Model {
	m.mode, m.candidates, m.choice, m.input = modeNormal, nil, 0, ""
	return m
}

// clampBytes cuts text to at most n bytes, on a rune boundary — a name cut
// through the middle of a rune would be a filename ending in a broken character.
func clampBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s { // ranges over rune starts
		if i > n {
			break
		}
		cut = i
	}
	return s[:cut]
}

func (m Model) workspaceBar() string {
	return fmt.Sprintf("Workspace %s (%d of %d) · j/k choose · ⏎ open · n new · x delete · esc cancel",
		flatten(m.candidates[m.choice]), m.choice+1, len(m.candidates))
}
