package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// Sessions is the tmux side of a node's life. The map view owns a node's
// identity and position; everything a session is happens behind this seam,
// which also lets the view be tested without a tmux server.
type Sessions interface {
	// Create starts a session; an empty cmd leaves it on a login shell.
	Create(session, dir, cmd string, env map[string]string) error
	Kill(session string) error
	Exists(session string) (bool, error)
	// List names every session on the server, ours and everyone else's, which
	// is what reconciliation classifies the map against (§9.2).
	List() ([]string, error)
	// Env is a session's own provenance, which is what lets a session describe
	// the node it belongs to when the workspace file no longer can.
	Env(session string) (map[string]string, error)
	// Handoff prepares an attach: the command that takes the terminal, and the
	// release to run once it has given the terminal back.
	Handoff(session, detachKey string) (*exec.Cmd, func() error, error)
	// Capture is a snapshot of a session's recent output, with its colour, for
	// the card's preview. Never a live terminal — see CONTEXT.md, "Preview".
	Capture(session string, lines int) (string, error)
	// Watch is the push side: what tmux says about sessions and activity, for
	// as long as ctx runs.
	Watch(ctx context.Context) <-chan tmux.Event
}

// maxTitleLen keeps a title inside a card and inside a sensible status line.
// Titles are typed by hand, so this is a trust boundary, not a style rule.
const maxTitleLen = 48

type nodeCreatedMsg struct {
	node state.Node
	err  error
}

type nodeKilledMsg struct {
	id  string
	err error
}

// selected is the node under the cursor. Nothing is selected on an empty cell,
// which is the normal state of a fresh map.
func (m Model) selected() (state.Node, bool) {
	for _, n := range m.ws.Nodes {
		if n.Pos == m.ws.Viewport.Cursor {
			return n, true
		}
	}
	return state.Node{}, false
}

// node finds a node by id. Ids are how an intent outlives the cursor: the kill
// prompt names one node from the moment x is pressed, whatever moves under the
// cursor afterwards.
func (m Model) node(id string) (state.Node, bool) {
	for _, n := range m.ws.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return state.Node{}, false
}

func (m Model) updateTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return m.createNode()
	case tea.KeyEsc:
		m.mode, m.input = modeNormal, ""
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.input = clampTitle(m.input + sanitise(string(msg.Runes)))
	}
	return m, nil
}

// createNode decides the node's identity and cell here, so that the session it
// is about to be given is named after a node that already exists in full. The
// node reaches the map only once tmux has confirmed the session (§5.2), so
// until then it is held in pending — where the next node still has to route
// around it, or two nodes started in quick succession would claim one cell.
func (m Model) createNode() (tea.Model, tea.Cmd) {
	draft := m.ws
	draft.Nodes = append(append([]state.Node(nil), m.ws.Nodes...), m.pending...)

	id := draft.NewNodeID()
	title := strings.TrimSpace(m.input)
	if title == "" {
		title = id
	}
	node := state.Node{
		ID:        id,
		Kind:      m.creating,
		Title:     title,
		Pos:       draft.NearestFreeCell(m.ws.Viewport.Cursor),
		CreatedAt: time.Now(),
	}
	m.mode, m.input, m.status = modeNormal, "", ""

	if !node.HasSession() {
		// With no session to confirm there is nothing to hold the node in
		// pending for: it reaches the map on the keystroke that made it.
		return m.place(node).save(), nil
	}
	m.pending = append(append([]state.Node(nil), m.pending...), node)

	sessions, workspace, dir := m.sessions, m.ws.Name, m.dirOf(node)
	return m, func() tea.Msg {
		err := sessions.Create(tmux.SessionName(workspace, node.ID), dir, node.Cmd, provenance(workspace, node))
		return nodeCreatedMsg{node: node, err: err}
	}
}

// provenance is what a session carries about the node behind it (§5.2). It is
// what reconciliation reads back when the workspace file has been lost, so a
// session started here and a session respawned later have to say the same thing.
func provenance(workspace string, n state.Node) map[string]string {
	return map[string]string{
		"TRIG_WORKSPACE": workspace,
		"TRIG_NODE_ID":   n.ID,
		"TRIG_NODE_KIND": string(n.Kind),
	}
}

// dirOf is where a node's session starts: its own working directory, or the
// workspace's when it has none of its own.
func (m Model) dirOf(n state.Node) string {
	if n.Dir != "" {
		return n.Dir
	}
	return m.ws.Dir
}

func (m Model) updateConfirmKill(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = modeNormal
		id := m.killing
		m.killing = ""
		node, ok := m.node(id)
		if !ok {
			return m, nil
		}
		if !node.HasSession() {
			// Removing the node is the whole operation — there is no session
			// behind it to ask tmux about, let alone kill.
			//
			// A node the map believes dead is deliberately not short-circuited
			// here. Kill already treats an absent session as the outcome that
			// was asked for, so routing every card through it costs a no-op
			// subprocess — and skipping it on a dead flag that turned out to be
			// wrong would abandon a live session with no card left to find it
			// by.
			m.ws.Nodes = without(m.ws.Nodes, id)
			return m.forget(id).save(), nil
		}
		sessions, workspace := m.sessions, m.ws.Name
		return m, func() tea.Msg {
			err := sessions.Kill(tmux.SessionName(workspace, id))
			return nodeKilledMsg{id: id, err: err}
		}
	case "n", "N", "esc", "q":
		m.mode, m.killing = modeNormal, ""
	}
	return m, nil
}

func (m Model) updateNodeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case nodeCreatedMsg:
		m.pending = without(m.pending, msg.node.ID)
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil, true
		}
		// The cell was chosen when the node was created, and the map has been
		// live ever since: a shove may have walked another node onto it. One
		// node per cell is the map's only layout rule, so the cell is settled
		// here, on arrival, rather than back when it was merely reserved.
		node := msg.node
		node.Pos = m.ws.NearestFreeCell(node.Pos)
		next, cmd := m.place(node).save().markDirty(node.ID)
		return next, cmd, true

	case attachedMsg:
		m.handingOff = false
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		// Whatever happened inside the session happened with the map's back
		// turned, so every card on screen is a snapshot of before the attach.
		// Bubble Tea repaints on taking the terminal back; this is what it has
		// to repaint with.
		next, cmd := m.refreshNow()
		return next, cmd, true

	case noteEditedMsg:
		m.handingOff = false
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		if !msg.edited {
			// Nothing came back to write — the body on the map is still the one
			// that went out, so the failure costs nothing but the trip.
			return m, nil, true
		}
		// The body is applied even when the editor exited badly: it is what the
		// file said, and an editor that wrote and then complained has still
		// done the writing.
		nodes := append([]state.Node(nil), m.ws.Nodes...)
		for i := range nodes {
			if nodes[i].ID == msg.id {
				nodes[i].Note = msg.body
			}
		}
		m.ws.Nodes = nodes
		return m.save(), nil, true

	case respawnedMsg:
		if msg.err != nil {
			// The node is exactly as dead as it was, and now the map says why.
			m.status = msg.err.Error()
			return m, nil, true
		}
		next, cmd := m.alive(msg.id).markDirty(msg.id)
		return next, cmd, true

	case nodeKilledMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil, true
		}
		m.ws.Nodes = without(m.ws.Nodes, msg.id)
		return m.forget(msg.id).save(), nil, true
	}
	return m, nil, false
}

// place puts a node on the map under the cursor, in a fresh slice: a Model is
// copied by value all over Bubble Tea, and appending in place would edit the
// map every older copy is still holding.
func (m Model) place(node state.Node) Model {
	m.ws.Nodes = append(append([]state.Node(nil), m.ws.Nodes...), node)
	m.ws.Viewport.Cursor = node.Pos
	return m.follow()
}

// without returns the nodes other than id, in a fresh slice: a Model is copied
// by value all over Bubble Tea, and filtering in place would edit the map every
// older copy is still holding.
func without(nodes []state.Node, id string) []state.Node {
	kept := make([]state.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.ID != id {
			kept = append(kept, n)
		}
	}
	return kept
}

// save persists the whole workspace after every mutation, in the update itself
// rather than in a command: two saves in flight at once could land in either
// order, and the loser would silently take the map back to what it used to be.
// There is no save action in the UI — state.Save is atomic, so a kill at any
// moment leaves the map either as it was or as it now is.
// ponytail: two fsyncs on the update loop; debounce if node movement, which
// saves per keystroke, ever makes that visible.
func (m Model) save() Model {
	if err := state.Save(m.stateDir, m.ws); err != nil {
		m.status = err.Error()
	}
	return m
}

func clampTitle(s string) string {
	if r := []rune(s); len(r) > maxTitleLen {
		return string(r[:maxTitleLen])
	}
	return s
}

// sanitise drops control characters, which would otherwise reach a card, a
// status line, and the workspace file on disk.
func sanitise(s string) string { return sanitiseKeeping(s) }

// sanitiseKeeping is sanitise with an exception list, for the one caller that
// has to keep a control character: a note body is many lines, and the newlines
// between them are what makes it so.
func sanitiseKeeping(s string, keep ...rune) string {
	return strings.Map(func(r rune) rune {
		if r >= 0x20 && r != 0x7f {
			return r
		}
		for _, k := range keep {
			if r == k {
				return r
			}
		}
		return -1
	}, s)
}

// Card geometry. A card is its two borders plus however many body lines the map
// is showing; a map with nothing to put in a body is exactly the two borders.
const (
	cardWidth = 22
	cellGap   = 1
	// cardRowsNoBody is a cell with no body lines in it: two borders and the
	// blank line under them.
	cardRowsNoBody = 3
	// cardBodyWidth is the room inside a card's walls, once "│ " and " │" have
	// taken theirs. It is the width the markdown renderer wraps to.
	cardBodyWidth = cardWidth - 4
	// maxNoteLines caps what one note can cost every other card on the map.
	// Card sizes (§7.3, `s`) will set this per node; until they do it is one
	// number, chosen as the largest a card was ever going to be.
	maxNoteLines = 10
)

// cardRows is the height of one cell of the grid. Cards line up in columns, so
// every card on the map is the same height — a taller note would otherwise
// knock the rest of its row out of line with the rows above and below it.
func (m Model) cardRows() int {
	return cardRowsNoBody + m.bodyHeight()
}

// bodyHeight is how many body lines every card shows, which is as many as the
// hungriest node on the map asks for.
func (m Model) bodyHeight() int {
	height := 0
	for _, n := range m.ws.Nodes {
		height = maxInt(height, m.nodeBodyHeight(n))
	}
	return height
}

// nodeBodyHeight is the lines one node would like. A session-backed node asks
// for its preview line count, whether or not a snapshot has been taken yet — a
// card that grew a body the moment its first output landed would move every
// other card on the map with it. A note asks for what it has to show, capped so
// that one long note cannot cost the whole map its screen.
func (m Model) nodeBodyHeight(n state.Node) int {
	if n.HasSession() {
		return m.previewHeight(n)
	}
	if got := len(noteLines(n.Note)); got < maxNoteLines {
		return got
	}
	return maxNoteLines
}

var (
	cardStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	// A dead card is dimmed, but dimming is invisible on a terminal with no
	// colour — so the badge, and not the colour, is what actually says dead.
	deadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Faint(true)
)

// Badges are a node's liveness on its card (§8). Dead is its own mark rather
// than a colour of the live one: a node whose session is gone is not in a state
// an agent could ever report.
const (
	liveBadge = "●"
	deadBadge = "✗"
)

// cards renders the occupied region of the map, one card per node, laid out on
// the cell grid so that a node's position on screen is its position on the map.
func (m Model) cards() string {
	minCell, maxCell := m.bounds()
	at := make(map[state.Cell]state.Node, len(m.ws.Nodes))
	for _, n := range m.ws.Nodes {
		at[n.Pos] = n
	}

	body := m.bodyHeight()
	blank := strings.Repeat(" ", cardWidth)

	var rows []string
	for row := minCell.Row; row <= maxCell.Row; row++ {
		lines := make([]strings.Builder, m.cardRows()-1)
		for col := minCell.Col; col <= maxCell.Col; col++ {
			cell := state.Cell{Col: col, Row: row}
			node, ok := at[cell]
			drawn := []string(nil)
			if ok {
				drawn = card(node, cell == m.ws.Viewport.Cursor, m.dead[node.ID], body, m.bodyOf(node))
			}
			for i := range lines {
				if col > minCell.Col {
					lines[i].WriteString(strings.Repeat(" ", cellGap))
				}
				if !ok {
					lines[i].WriteString(blank)
					continue
				}
				lines[i].WriteString(drawn[i])
			}
		}
		for i := range lines {
			rows = append(rows, lines[i].String())
		}
		rows = append(rows, "")
	}
	return strings.Join(rows[:len(rows)-1], "\n")
}

// bounds is the rectangle of cells worth drawing: the viewport offset, and as
// many cells beyond it as the terminal has room for. The rectangle is sized by
// the screen and not by the map, because a node's position is read off disk and
// can be arbitrarily far away — spanning to it would make a distant node an
// out-of-memory rather than something you scroll to.
func (m Model) bounds() (min, max state.Cell) {
	cols, rows := m.viewCells()
	min = m.ws.Viewport.Offset
	max = state.Cell{Col: min.Col + cols - 1, Row: min.Row + rows - 1}
	return min, max
}

// viewCells is how many cells of map the terminal has room for. Never zero:
// there is always a cell under the cursor, however small the window.
func (m Model) viewCells() (cols, rows int) {
	return maxInt((m.width+cellGap)/(cardWidth+cellGap), 1),
		maxInt((m.height-1+cellGap)/m.cardRows(), 1)
}

// bodyOf is what fills a node's card: the preview last captured for a session,
// the rendered markdown for a note.
func (m Model) bodyOf(n state.Node) []string {
	if n.HasSession() {
		return m.previews[n.ID]
	}
	return noteLines(n.Note)
}

// card is a node's rendering on the map: a border carrying its title, the body
// lines beneath it, and a border carrying its kind and age. Cards are never
// persisted; nodes are.
func card(n state.Node, selected, dead bool, body int, content []string) []string {
	style := cardStyle
	switch {
	case selected:
		// Selection outranks dimming: a cursor you cannot find is worse than a
		// card that looks livelier than it is, and the badge still says dead.
		style = selectedStyle
	case dead:
		style = deadStyle
	}
	// The status dot is the node's liveness (§8), and a note has none — so a
	// note's card carries no badge at all rather than a badge that means
	// nothing.
	badge := liveBadge
	if dead {
		badge = deadBadge
	}
	lead, trail := "╭─ "+badge+" ", kindLabel(n.Kind)+age(n.CreatedAt)
	if !n.HasSession() {
		lead, trail = "╭─ ", kindLabel(n.Kind)
	}

	lines := make([]string, 0, body+2)
	lines = append(lines, style.Render(border(lead, n.Title, "─╮")))
	for i := 0; i < body; i++ {
		text := ""
		if i < len(content) {
			text = content[i]
		}
		lines = append(lines, bodyLine(style, text))
	}
	return append(lines, style.Render(border("╰─ ", trail, "─╯")))
}

// bodyLine is one line inside a card, padded with spaces rather than the rule
// the borders are drawn with, so the card reads as a box with text in it.
//
// The text arrives already coloured by the markdown renderer, so only the walls
// are given the card's own style — styling the whole line would fight the
// rendering for it — and the cut is made by ansi.Truncate, which counts cells
// and not bytes, so a line is never cut through the middle of an escape
// sequence.
func bodyLine(style lipgloss.Style, text string) string {
	text = ansi.Truncate(text, cardBodyWidth, "…")
	pad := strings.Repeat(" ", maxInt(cardBodyWidth-lipgloss.Width(text), 0))
	if strings.Contains(text, "\x1b") {
		// Close any colour the renderer left open, so a heading's background
		// cannot run out over the card's right-hand wall. Only worth adding
		// where there is colour to close: on a terminal that cannot show any,
		// the renderer emits none and a bare reset would be the only escape
		// sequence in the whole frame.
		text += "\x1b[0m"
		if strings.Contains(text, "\x1b]8;;") {
			// And any hyperlink, which a colour reset does not close.
			// capture-pane emits OSC 8 for a pane that printed one, and
			// glamour emits it for a link in a note; either left open makes
			// the rest of the map clickable. Closing what is already closed
			// costs a no-op.
			text += "\x1b]8;;\x1b\\"
		}
	}
	return style.Render("│ ") + text + pad + style.Render(" │")
}

func border(lead, text, tail string) string {
	room := cardWidth - lipgloss.Width(lead) - lipgloss.Width(tail) - 1 // the space before the tail
	text = truncate(text, room)
	fill := room - lipgloss.Width(text)
	return lead + text + " " + strings.Repeat("─", maxInt(fill, 0)) + tail
}

// truncate cuts s to at most width terminal cells, marking the cut with an
// ellipsis. Width is measured in cells rather than runes: a card is a fixed
// number of columns, and a CJK or emoji title spends two of them per rune.
func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var kept strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > width-1 { // -1 leaves room for the ellipsis
			break
		}
		kept.WriteRune(r)
		used += w
	}
	return kept.String() + "…"
}

func kindLabel(k state.Kind) string {
	switch k {
	case state.KindShell:
		return "sh"
	case state.KindAgent:
		return "ag"
	case state.KindNote:
		return "note"
	default:
		return string(k)
	}
}

// age is how long the node has existed, in the coarsest unit that still says
// something. A node with no creation time — one written before this field
// existed — simply shows nothing.
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf(" · %ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf(" · %dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf(" · %dh", int(d.Hours()))
	default:
		return fmt.Sprintf(" · %dd", int(d.Hours()/24))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
