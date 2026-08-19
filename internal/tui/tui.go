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
	killing       string       // the node x was pressed on, held until y or n
	respawning    string       // the dead node Enter was pressed on, held until y or n
	creating      state.Kind   // the kind the title prompt is collecting a name for
	count         string       // the count prefix typed so far, applied to the next motion
	awaitZ        bool         // the first z of zz has been pressed
	handingOff    bool         // the terminal is out at a session or an editor, so Enter is spoken for

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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A shrunk terminal can leave the cursor outside the viewport, and a
		// cursor you cannot see is a node you are about to move blind.
		//
		// A resize also decides which cards exist to be captured at all — before
		// the first one there is no viewport, so this is where a map's previews
		// start.
		shown := m.follow()
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
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "x":
		// The target is fixed here rather than read again at y, because a
		// create landing while the prompt is up moves the cursor onto the new
		// node — and the user would then confirm one kill and get another.
		if node, ok := m.selected(); ok {
			m.mode, m.killing = modeConfirmKill, node.ID
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
	body := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height - 1).
		Render(lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, m.mapView()))
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())
}

func (m Model) mapView() string {
	if len(m.ws.Nodes) == 0 {
		return hintStyle.Render("The map is empty. Press n for a shell node, N for a note.")
	}
	return m.cards()
}

func (m Model) statusBar() string {
	// A prompt outranks a stale error: the error arrives from tmux whenever it
	// gets round to it, and taking the bar mid-prompt leaves the user typing
	// into something they can no longer see. The error is still there when the
	// prompt closes.
	switch {
	case m.mode == modeConfirmQuit:
		return m.bar(statusStyle, "Quit Trigpoint? Sessions keep running. (y/n)")
	case m.mode == modeTitle:
		return m.bar(statusStyle, titleLabel(m.creating)+": "+flatten(m.input)+"▏")
	case m.mode == modeConfirmRespawn:
		node, _ := m.node(m.respawning)
		return m.bar(statusStyle, fmt.Sprintf("Respawn %s? (y/n)", flatten(node.Title)))
	case m.mode == modeConfirmKill:
		node, _ := m.node(m.killing)
		if !node.HasSession() || m.dead[m.killing] {
			// There is no session behind a note, and a dead node's is already
			// gone, so offering to kill one would be asking the user to
			// confirm something that cannot happen.
			return m.bar(statusStyle, fmt.Sprintf("Remove %s? (y/n)", flatten(node.Title)))
		}
		return m.bar(statusStyle, fmt.Sprintf("Kill %s and its session? (y/n)", flatten(node.Title)))
	case m.status != "":
		return m.bar(errorStyle, flatten(m.status))
	}

	left := fmt.Sprintf("%s · %s", m.ws.Name, pluralise(len(m.ws.Nodes), "node"))
	right := "⏎ attach · n new · N note · x kill · q quit"
	if m.count != "" {
		right = m.count + " · " + right
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - barPadding
	if gap < 1 {
		return m.bar(statusStyle, left)
	}
	return m.bar(statusStyle, left+strings.Repeat(" ", gap)+right)
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
