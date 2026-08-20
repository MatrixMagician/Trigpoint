// Package tui renders the map view: the home screen a workspace's map is seen
// through. The map persists; the view is only what you are looking at it with.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

var (
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238")).Padding(0, 1)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("124")).Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// mode is what the next keystroke means. Trigpoint is modal, so every key is
// read by exactly one of these.
type mode int

const (
	modeNormal mode = iota
	modeTitle
	modeConfirmKill
	modeConfirmRespawn
	modeConfirmQuit
	modeAdopt
	modePeek
	modeRename
	modeTags
	modeBulkTags
	modeGroupTitle
	modeColour
	modeWorkspace
	modeNewWorkspace
	modeConfirmDeleteWorkspace
	modeFilter
	modePalette
	modeAgent
	modeAgentCmd
)

// Model is the map view's state. The workspace it renders is owned here; the
// terminal size arrives from Bubble Tea and is zero until the first resize.
type Model struct {
	cfg      config.Config
	ws       state.Workspace
	stateDir string
	sessions Sessions

	width, height int
	mode          mode
	input         string
	status        string       // the last failure, shown until the next action
	pending       []state.Node // nodes whose sessions tmux has not confirmed yet
	killing       []string     // the nodes x was pressed on, held until y or n
	grouping      []string     // the nodes g was pressed on, held until the name is in
	respawning    string       // the dead node Enter was pressed on, held until y or n
	creating      state.Kind   // the kind the title prompt is collecting a name for
	creatingCmd   string       // the command an agent node is being created to run
	editing       string       // the node an attribute prompt was opened on
	count         string       // the count prefix typed so far, applied to the next motion
	// selection is the nodes v has gathered (§7.3), by id, in the order they
	// were gathered. Empty is the map's ordinary state: the node under the
	// cursor is then the selection, and every bulk action falls back to it.
	selection []string
	// holding is the group `V` has picked up (§7.3), by id, and held is the
	// nodes inside it at the moment it was picked up — the snapshot a rigid
	// move carries, so that a gesture moves what it began on. Empty is the
	// map's ordinary state, in which the motion keys move cards.
	holding string
	held    []string
	// filter narrows the map to the cards matching it (§7.1). It outlives the
	// prompt that collects it — a filter is how the map is being looked at, and
	// Esc is the only thing that clears it.
	filter     string
	palette    []entry  // what the palette is offering, built when it opened
	candidates []string // the sessions the adoption picker is offering
	choice     int      // which of them is under the picker's own cursor
	awaitZ     bool     // the first z of zz has been pressed
	handoff    string   // the node the terminal is out at, so Enter is spoken for

	// The peek: a snapshot of one node's output, read full-screen (§7.3). It
	// is held here rather than fetched per frame because a peek is a snapshot —
	// scrolling reads what was taken, it does not ask the session again.
	peeking string   // the node whose output is on screen
	peeked  []string // the snapshot taken of it
	peekTop int      // the line of it the screen starts at

	// dead is the nodes tmux has no session for (§9.2). It is derived on every
	// reconciliation pass and never written to disk — see
	// docs/adr/0006-liveness-is-derived-not-stored.md.
	dead map[string]bool
	// corrections counts what the map has learned about liveness on its own,
	// between reconciliation passes. A pass carries the count it was sent with,
	// so an answer that predates a correction is dropped rather than applied.
	corrections int

	previews map[string][]string // the last snapshot taken of each node, by id
	dirty    map[string]bool     // the cards whose snapshot has been overtaken
	settling bool                // a debounce is running, so dirty cards have a tick coming
	// unread is the cards output has arrived on since you last looked at them
	// (§8). Attention, not work: it is set by activity and cleared by looking,
	// and it is never persisted — a mark that survived a restart would be one
	// nothing had learned anything about.
	unread map[string]bool
}

func New(cfg config.Config, ws state.Workspace, stateDir string, sessions Sessions) Model {
	// Built here, and not on the first note drawn, because choosing the
	// markdown style asks the terminal what colour it is — a question that has
	// to be put before Bubble Tea owns the terminal, or the answer comes back
	// as keystrokes on a screen already being painted.
	noteMarkdown()
	return Model{cfg: cfg, ws: ws, stateDir: stateDir, sessions: sessions}
}

// Init starts the two things that keep previews current: the control-mode
// client's event stream, and the slow tick that refreshes whatever the stream
// missed — including the whole time there was no stream.
//
// The stream lives as long as the process does. There is nothing to cancel it
// with, and nothing to cancel it for: quitting Trigpoint closes stdin on the
// control client, which is how tmux is told the client is gone.
func (m Model) Init() tea.Cmd {
	// Reconciliation is third because the map opens on what the last session
	// left behind, and a card claiming a session that did not survive the
	// reboot is the one lie a map like this must not tell (§9.2).
	return tea.Batch(listen(m.sessions.Watch(context.Background())), m.slowTick(), m.reconcile())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updatePreview(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateNodeMsg(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updatePeekMsg(msg); handled {
		return next, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A shrunk terminal can leave the cursor outside the viewport, and a
		// cursor you cannot see is a node you are about to move blind.
		//
		// A resize also decides which cards exist to be captured at all — before
		// the first one there is no viewport, so this is where a map's previews
		// start.
		// A resize re-windows an open peek as well as the map: a snapshot is
		// not taken again when the terminal changes shape, only shown through a
		// window of a different size.
		shown := m.follow().clampPeek()
		next, cmd := shown.markDirty(shown.visible()...)
		return next, cmd

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.mode {
		case modeTitle:
			return m.updateTitle(msg)
		case modeConfirmKill:
			return m.updateConfirmKill(msg)
		case modeConfirmRespawn:
			return m.updateConfirmRespawn(msg)
		case modeConfirmQuit:
			return m.updateConfirmQuit(msg)
		case modeAdopt:
			return m.updateAdopt(msg)
		case modeRename:
			return m.updateRename(msg)
		case modeTags:
			return m.updateTags(msg)
		case modeBulkTags:
			return m.updateBulkTags(msg)
		case modeGroupTitle:
			return m.updateGroupTitle(msg)
		case modeColour:
			return m.updateColour(msg)
		case modeWorkspace:
			return m.updateWorkspace(msg)
		case modeNewWorkspace:
			return m.updateNewWorkspace(msg)
		case modeConfirmDeleteWorkspace:
			return m.updateConfirmDeleteWorkspace(msg)
		case modeFilter:
			return m.updateFilter(msg)
		case modePalette:
			return m.updatePalette(msg)
		case modePeek:
			return m.updatePeek(msg)
		case modeAgent:
			return m.updateAgent(msg)
		case modeAgentCmd:
			return m.updateAgentCmd(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A held group answers the motion keys first: while one is in hand they
	// move the rectangle, and letting them reach the map as well would move a
	// card and a group with one press.
	if m.holding != "" {
		return m.updateHeld(msg)
	}
	m.status = ""
	// motion is asked first and its model kept either way: a key it does not
	// take is still a key that ends a half-typed count.
	looking := m.ws.Viewport.Offset
	m, handled := m.motion(msg.String())
	if handled {
		// Cards that have never been captured, and — if the viewport itself
		// moved — every card now on screen: off screen is where activity events
		// are dropped, so a card scrolled back to has been unwatched, whether or
		// not it has a snapshot already. Moving the cursor without scrolling is
		// not news about any session, so a map merely being navigated does not
		// keep asking tmux the same question.
		stale := m.unpreviewed()
		if m.ws.Viewport.Offset != looking {
			stale = m.visible()
		}
		next, cmd := m.markDirty(stale...)
		return next, cmd
	}
	switch msg.String() {
	case "enter":
		// A note has no session; Enter opens its body in $EDITOR instead, by the
		// same release-the-terminal mechanism (§6).
		if node, ok := m.selected(); ok && node.Kind == state.KindNote {
			return m.editNote(node)
		}
		return m.attach()
	case "q":
		if m.cfg.General.ConfirmQuit {
			m.mode = modeConfirmQuit
			return m, nil
		}
		// Quitting Trigpoint kills nothing: every session outlives it (§5.2).
		return m, tea.Quit
	case "n":
		m.mode, m.input, m.creating = modeTitle, "", state.KindShell
	case "N":
		m.mode, m.input, m.creating = modeTitle, "", state.KindNote
	case "a":
		return m.openAgents()
	case "A":
		// The picker opens on tmux's answer rather than on the keystroke: the
		// question is a subprocess, and a map that froze on it would freeze on
		// the one thing adoption is for — a server with a great many sessions.
		return m, m.adoptable()
	case " ":
		// Peek: the node's output, read without being given the keyboard
		// (§7.3).
		return m.peek()
	case "r":
		return m.editAttr(modeRename, func(n state.Node) string { return n.Title })
	case "t":
		if len(m.selection) > 0 {
			return m.openBulkTags()
		}
		return m.editAttr(modeTags, func(n state.Node) string { return strings.Join(n.Tags, " ") })
	case "v":
		return m.toggleSelect()
	case "V":
		return m.hold()
	case "g":
		return m.group()
	case "c":
		return m.cycleColour()
	case "C":
		return m.openColours()
	case "s":
		return m.cycleSize()
	case "tab":
		return m.cycleWorkspace(1)
	case "shift+tab":
		return m.cycleWorkspace(-1)
	case "w":
		return m.openWorkspaces()
	case "/":
		return m.openFilter()
	case "ctrl+k", ":":
		return m.openPalette()
	case "esc":
		// Esc has two things to clear and one key. The selection goes first: it
		// is the newer of the two, and the filter is still there to Esc again.
		if next, cleared := m.clearSelection(); cleared {
			return next, nil
		}
		return m.clearFilter()
	case "x":
		// The targets are fixed here rather than read again at y, because a
		// create landing while the prompt is up moves the cursor onto the new
		// node — and the user would then confirm one kill and get another.
		if ids := m.targets(); len(ids) > 0 {
			m.mode, m.killing = modeConfirmKill, ids
		}
	}
	return m, nil
}

func (m Model) updateConfirmQuit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, tea.Quit
	case "n", "N", "esc":
		m.mode = modeNormal
	}
	return m, nil
}

// View renders the map above the status bar, filling the terminal exactly. It is
// called before the first resize, when the size is still unknown.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 1 {
		return ""
	}
	// A peek fills the screen the map would be on: it is the whole view, not an
	// overlay, because the output it shows is the thing being read.
	content := lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, m.mapView())
	switch m.mode {
	case modePeek:
		content = m.peekView()
	case modePalette:
		content = m.paletteView()
	}
	body := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height - 1).Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())
}

func (m Model) mapView() string {
	switch {
	case len(m.ws.Nodes) == 0:
		return hintStyle.Render("The map is empty. Press n for a shell node, a for an agent, N for a note.")
	case len(m.filtered()) == 0:
		return hintStyle.Render("No card matches /" + flatten(m.filter) + ". Press esc to clear the filter.")
	}
	return m.cards()
}

func (m Model) statusBar() string {
	// A prompt outranks a stale error: the error arrives from tmux whenever it
	// gets round to it, and taking the bar mid-prompt leaves the user typing
	// into something they can no longer see. The error is still there when the
	// prompt closes.
	switch {
	case m.mode == modePeek:
		return m.bar(statusStyle, m.peekBar())
	case m.mode == modePalette:
		return m.bar(statusStyle, m.paletteBar())
	case m.mode == modeFilter:
		return m.bar(statusStyle, "Filter: "+flatten(m.input)+"▏")
	case m.mode == modeConfirmQuit:
		return m.bar(statusStyle, "Quit Trigpoint? Sessions keep running. (y/n)")
	case m.mode == modeTitle:
		return m.bar(statusStyle, titleLabel(m.creating)+": "+flatten(m.input)+"▏")
	case m.mode == modeRename:
		return m.bar(statusStyle, "Rename: "+flatten(m.input)+"▏")
	case m.mode == modeTags:
		return m.bar(statusStyle, "Tags: "+flatten(m.input)+"▏")
	case m.mode == modeGroupTitle:
		return m.bar(statusStyle, fmt.Sprintf("Group %s: %s▏",
			pluralise(len(m.grouping), "node"), flatten(m.input)))
	case m.mode == modeBulkTags:
		// The prompt says which way round the two verbs are, because a bulk
		// edit that silently set the tags instead would be unrecoverable.
		return m.bar(statusStyle, fmt.Sprintf("Tags on %s (tag adds, -tag removes): %s▏",
			pluralise(len(m.targets()), "node"), flatten(m.input)))
	case m.mode == modeColour:
		return m.bar(statusStyle, m.colourBar())
	case m.mode == modeWorkspace:
		return m.bar(statusStyle, m.workspaceBar())
	case m.mode == modeNewWorkspace:
		return m.bar(statusStyle, "New workspace: "+flatten(m.input)+"▏")
	case m.mode == modeConfirmDeleteWorkspace:
		// What deleting a workspace does to the sessions its nodes named is
		// nothing, and the confirmation says so rather than leaving the user to
		// find out (§5.2).
		return m.bar(statusStyle, fmt.Sprintf("Delete workspace %s? Its sessions keep running. (y/n)",
			flatten(m.candidates[m.choice])))
	case m.mode == modeAgent:
		return m.bar(statusStyle, fmt.Sprintf("Agent %s (%d of %d) · j/k choose · ⏎ start · esc cancel",
			flatten(m.candidates[m.choice]), m.choice+1, len(m.candidates)))
	case m.mode == modeAgentCmd:
		return m.bar(statusStyle, "Agent command: "+flatten(m.input)+"▏")
	case m.mode == modeAdopt:
		return m.bar(statusStyle, fmt.Sprintf("Adopt %s (%d of %d) · j/k choose · ⏎ adopt · esc cancel",
			flatten(m.candidates[m.choice]), m.choice+1, len(m.candidates)))
	case m.mode == modeConfirmRespawn:
		node, _ := m.node(m.respawning)
		return m.bar(statusStyle, fmt.Sprintf("Respawn %s? (y/n)", flatten(node.Title)))
	case m.mode == modeConfirmKill && len(m.killing) != 1:
		// One confirmation for the whole selection, naming how many nodes it
		// will take (§7.3). What is behind each of them is not spelled out:
		// a selection can hold notes, dead nodes, and live sessions at once,
		// and the count is the thing the user is being asked about.
		return m.bar(statusStyle, fmt.Sprintf("Kill %s? (y/n)", pluralise(len(m.killing), "node")))
	case m.mode == modeConfirmKill:
		node, _ := m.node(m.killing[0])
		if !node.HasSession() || m.dead[m.killing[0]] {
			// There is no session behind a note, and a dead node's is already
			// gone, so offering to kill one would be asking the user to
			// confirm something that cannot happen.
			return m.bar(statusStyle, fmt.Sprintf("Remove %s? (y/n)", flatten(node.Title)))
		}
		return m.bar(statusStyle, fmt.Sprintf("Kill %s and its session? (y/n)", flatten(node.Title)))
	case m.status != "":
		return m.bar(errorStyle, flatten(m.status))
	case m.holding != "":
		// The keys a held group answers are not the keys the map answers, so
		// they are on the bar for as long as it is held.
		return m.bar(statusStyle, fmt.Sprintf("Group %s · %s", flatten(m.heldTitle()), heldHints))
	}

	left := fmt.Sprintf("%s · %s", m.ws.Name, pluralise(len(m.ws.Nodes), "node"))
	if m.filter != "" {
		// The count says what the filter is doing, and the query says what to
		// press Esc to be rid of.
		left = fmt.Sprintf("%s · %d of %s · /%s",
			m.ws.Name, len(m.filtered()), pluralise(len(m.ws.Nodes), "node"), flatten(m.filter))
	}
	if len(m.selection) > 0 {
		left += fmt.Sprintf(" · %d selected", len(m.selection))
	}
	// The count prefix is what the next keystroke will do, so it is offered
	// before any hint about which keystroke that might be.
	prefix := ""
	if m.count != "" {
		prefix = m.count + " · "
	}
	// TrimSuffix for the terminal with room for the count and not one hint: a
	// separator with nothing after it reads as something that failed to render.
	hints := selectionHints
	if len(m.selection) == 0 {
		hints = fitHints(m.width - lipgloss.Width(left) - lipgloss.Width(prefix) - barPadding - 1)
	}
	right := strings.TrimSuffix(prefix+hints, " · ")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - barPadding
	if gap < 1 {
		return m.bar(statusStyle, left)
	}
	return m.bar(statusStyle, left+strings.Repeat(" ", gap)+right)
}

// fitHints is as many of the status bar's hints as width has room for. They are
// the commands' own (§7.2), in the order the command table gives them up: a
// terminal too narrow for all of them loses them from the end, rather than
// losing the lot the moment one does not fit.
func fitHints(width int) string {
	var kept strings.Builder
	for _, c := range commands {
		if c.hint == "" {
			continue
		}
		next := c.hint
		if kept.Len() > 0 {
			next = " · " + c.hint
		}
		if lipgloss.Width(kept.String())+lipgloss.Width(next) > width {
			break
		}
		kept.WriteString(next)
	}
	return kept.String()
}

// barPadding is the columns statusStyle spends on its own padding.
const barPadding = 2

// bar renders one line of status bar, and exactly one line: a title in a narrow
// terminal would otherwise wrap and push the map off the bottom of the screen.
//
// It cuts but does not flatten. Untrusted text — a tmux error, a typed title —
// is flattened by its caller before being composed in, because the composed
// line carries a run of spaces holding its two halves apart, and flattening
// here would collapse that alignment along with the newlines.
func (m Model) bar(style lipgloss.Style, text string) string {
	return style.Width(m.width).MaxWidth(m.width).
		Render(truncate(text, m.width-barPadding))
}

// flatten turns any run of whitespace, newlines included, into a single space.
func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// titleLabel names what the prompt is collecting a title for, so that N and n
// are told apart once the prompt has replaced the hint that said which was
// pressed.
func titleLabel(k state.Kind) string {
	if k == state.KindNote {
		return "Note title"
	}
	return "Title"
}

func pluralise(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
