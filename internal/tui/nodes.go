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

type nodeCreatedMsg struct {
	node state.Node
	err  error
}

type nodeKilledMsg struct {
	mapStamp
	id  string
	err error
}

// selected is the node under the cursor. Nothing is selected on an empty cell,
// which is the normal state of a fresh map — nor on a cell whose card a filter
// is hiding (§7.1), because acting on a card that is not on screen is acting
// blind.
func (m Model) selected() (state.Node, bool) {
	for _, n := range m.filtered() {
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
		m.mode, m.input, m.creatingCmd = modeNormal, "", ""
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.input = state.ClampRunes(m.input+sanitise(string(msg.Runes)), state.MaxTitleLen)
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
		Cmd:       m.creatingCmd,
		Pos:       draft.NearestFreeCell(m.ws.Viewport.Cursor),
		CreatedAt: time.Now(),
	}
	m.mode, m.input, m.status, m.creatingCmd = modeNormal, "", "", ""

	if !node.HasSession() {
		// With no session to confirm there is nothing to hold the node in
		// pending for: it reaches the map on the keystroke that made it.
		return m.place(node).save(), nil
	}
	m.pending = append(append([]state.Node(nil), m.pending...), node)

	sessions, workspace, dir := m.sessions, m.ws.Name, m.ws.DirOf(node)
	env := state.Provenance(workspace, node, m.statusDir)
	return m, func() tea.Msg {
		err := sessions.Create(tmux.SessionName(workspace, node.ID), dir, node.StartCmd(), env)
		return nodeCreatedMsg{node: node, err: err}
	}
}

func (m Model) updateConfirmKill(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = modeNormal
		ids := m.killing
		m.killing = nil
		// One confirmation, one kill per node: a bulk kill is the same operation
		// several times over, and each node's own answer from tmux comes back on
		// its own message.
		removed := false
		var kills []tea.Cmd
		for _, id := range ids {
			node, ok := m.node(id)
			if !ok {
				continue
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
				m, removed = m.forget(id), true
				continue
			}
			// An adopted node kills the foreign session it was adopted from: the
			// card is over a real session, and the confirmation it took to get here
			// is the same one every other node's kill takes (§9.3).
			sessions, session, stamp := m.sessions, m.ws.SessionOf(node), m.stamp()
			kills = append(kills, func() tea.Msg {
				err := sessions.Kill(session)
				return nodeKilledMsg{mapStamp: stamp, id: id, err: err}
			})
		}
		if removed {
			m = m.save()
		}
		return m, tea.Batch(kills...)
	case "n", "N", "esc", "q":
		m.mode, m.killing = modeNormal, nil
	}
	return m, nil
}

func (m Model) updateNodeMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case nodeCreatedMsg:
		if !pending(m.pending, msg.node.ID) {
			// Nothing on this map is waiting for this node: the workspace it was
			// created in has been switched away from, and its nodes went with
			// it. Placing it here would put another map's card on this one.
			return m, nil, true
		}
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
		// Cleared on the way back as well as on the way out: the events tmux
		// pushed while the terminal was at the session arrive on the map now,
		// and the output they announced is output you were sitting in front of.
		m = m.read(m.handoff)
		m.handoff = ""
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
		m.handoff = ""
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
		return m.withNode(msg.id, func(n *state.Node) { n.Note = msg.body }).save(), nil, true

	case respawnedMsg:
		if !msg.about(m) {
			return m, nil, true
		}
		if msg.err != nil {
			// The node is exactly as dead as it was, and now the map says why.
			m.status = msg.err.Error()
			return m, nil, true
		}
		// The old session's agent reported about a session that no longer
		// exists, so its report goes with it — on disk as well as in hand, or
		// the next poll would put it straight back and badge a fresh agent with
		// what the last one said.
		next, cmd := m.alive(msg.id).forgetReport(msg.id).markDirty(msg.id)
		return next, cmd, true

	case adoptableMsg:
		if msg.forPalette {
			// The palette asked, so the answer folds into the list rather than
			// taking the keyboard — and opens nothing at all if the palette has
			// since closed, which is what the picker's own guard cannot see.
			return m.withCandidates(msg), nil, true
		}
		return m.openAdoption(msg), nil, true

	case nodeKilledMsg:
		if !msg.about(m) {
			// The map that killed it has been switched away from. The session is
			// gone either way, so its card goes dead on that map at its next
			// reconciliation pass and x removes it — better than taking a card
			// off this map because it happens to share the id.
			return m, nil, true
		}
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

// withNode returns the map with one node edited, in a fresh slice: a Model is
// copied by value all over Bubble Tea, and editing in place would change the
// map every older copy is still holding.
func (m Model) withNode(id string, edit func(*state.Node)) Model {
	return m.withNodes([]string{id}, edit)
}

// withNodes is the same for several at once, in one pass and one fresh slice —
// which is what a bulk edit over a selection needs (§7.3).
func (m Model) withNodes(ids []string, edit func(*state.Node)) Model {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	nodes := append([]state.Node(nil), m.ws.Nodes...)
	for i := range nodes {
		if wanted[nodes[i].ID] {
			edit(&nodes[i])
		}
	}
	m.ws.Nodes = nodes
	return m
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
	// footLead is the bottom border's left-hand corner and the space after it.
	footLead = "╰─ "
	// minTagWidth is the least a tag can be shown in and still say anything: a
	// hash, a character or two of the tag, and the ellipsis that says there was
	// more. Below it the bottom border carries the kind and the age alone.
	minTagWidth = 4
	// tagOverhead is what the border spends around the tags — the space after
	// the kind and age, one rule, the space before the tags, the space after
	// them, and the corner.
	tagOverhead = 6
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
// other card on the map with it. A note asks for what it has to show, capped by
// its own card size so that one long note cannot cost the whole map its screen.
//
// A note keeps one line at the smallest size, where a shell card keeps none.
// The difference is what the body is: a preview is a snapshot of something the
// session still has, and peek reads it in full — a note's body is the node, and
// a card showing none of it cannot be told from a note with nothing written in
// it.
func (m Model) nodeBodyHeight(n state.Node) int {
	if n.HasSession() {
		return m.previewHeight(n)
	}
	return minInt(len(noteLines(n.Note)), maxInt(m.sizeLines(n), 1))
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
	// Unread is the same dot left hollow: it is the same node in the same
	// state, with something in it you have not seen yet.
	unreadBadge = "○"
)

// cards renders the occupied region of the map, one card per node the filter
// leaves standing, laid out on the cell grid so that a node's position on
// screen is its position on the map.
func (m Model) cards() string {
	minCell, maxCell := m.bounds()
	at := make(map[state.Cell]state.Node, len(m.ws.Nodes))
	for _, n := range m.filtered() {
		at[n.Pos] = n
	}

	body := m.bodyHeight()
	blank := strings.Repeat(" ", cardWidth)
	// The groups are drawn first and the cards over them: the gaps between
	// cells are where a group's walls and its tint live, so laying the cards
	// down last is what puts a group behind its members (§7.1).
	// nil on a map with no groups: the gaps are then spaces and nothing else,
	// and a screenful of styled cells is not worth building to say so.
	frame := m.groupFrame(minCell, maxCell)
	gap := func(col, y int) string {
		if frame == nil {
			return " "
		}
		return frame[y][(col-minCell.Col)*(cardWidth+cellGap)]
	}
	gapRow := func(y int) string {
		if frame == nil {
			return strings.Repeat(" ", (maxCell.Col-minCell.Col+1)*(cardWidth+cellGap)+cellGap)
		}
		return strings.Join(frame[y], "")
	}

	var rows []string
	for row := minCell.Row; row <= maxCell.Row; row++ {
		top := (row - minCell.Row) * m.cardRows()
		rows = append(rows, gapRow(top))
		lines := make([]strings.Builder, m.cardRows()-1)
		for col := minCell.Col; col <= maxCell.Col; col++ {
			cell := state.Cell{Col: col, Row: row}
			node, ok := at[cell]
			drawn := []string(nil)
			if ok {
				drawn = card(m.shown(node), m.drawnSelected(node), m.isSelected(node.ID),
					m.dead[node.ID], m.badgeOf(node), m.groupOf(node), body, m.bodyOf(node))
			}
			for i := range lines {
				lines[i].WriteString(gap(col, top+1+i))
				if !ok {
					lines[i].WriteString(blank)
					continue
				}
				lines[i].WriteString(drawn[i])
			}
		}
		for i := range lines {
			lines[i].WriteString(gap(maxCell.Col+1, top+1+i))
			rows = append(rows, lines[i].String())
		}
	}
	return strings.Join(append(rows, gapRow((maxCell.Row-minCell.Row+1)*m.cardRows())), "\n")
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
//
// Every cell is drawn with its gap in front of it and the grid keeps one more
// at each far edge, so that a group rectangle drawn in the gaps has somewhere
// to put its walls whichever cell the viewport starts at.
func (m Model) viewCells() (cols, rows int) {
	return maxInt((m.width-cellGap)/(cardWidth+cellGap), 1),
		maxInt((m.height-1-1)/m.cardRows(), 1)
}

// bodyOf is what fills a node's card: the preview last captured for a session,
// the rendered markdown for a note — cut to what this node's own card size has
// room for. The cut is here and not only in the capture, because the body every
// card is drawn with is the hungriest node's on the map, and a small card must
// not spend the room a large one asked for. A snapshot keeps its end and a note
// its beginning: the last thing a session said is the news, the first thing a
// note says is the point.
func (m Model) bodyOf(n state.Node) []string {
	room := m.nodeBodyHeight(n)
	if n.HasSession() {
		lines := m.previews[n.ID]
		if len(lines) > room {
			return lines[len(lines)-room:]
		}
		return lines
	}
	lines := noteLines(n.Note)
	if len(lines) > room {
		return lines[:room]
	}
	return lines
}

// card is a node's rendering on the map: a border carrying its title, the body
// lines beneath it, and a border carrying its kind and age. Cards are never
// persisted; nodes are.
//
// The badge arrives already composed (badgeOf): what three sources make of one
// mark is a question about the map's state, and a card only draws it.
func card(n state.Node, cursor, inSelection, dead bool, mark badgeMark, group string, body int, content []string) []string {
	style := cardStyle
	accented, hasAccent := accent(n.Colour)
	switch {
	case cursor && inSelection:
		// Inside a selection the cursor wears the selection's colour and is
		// marked within it, rather than outranking it: see selectionCursorStyle.
		style = selectionCursorStyle
	case cursor:
		// The cursor outranks both the accent and the dimming: a cursor you
		// cannot find is worse than a card drawn in the wrong colour, and the
		// badge still says dead.
		style = selectedStyle
	case inSelection:
		style = selectionStyle
	case dead:
		style = deadStyle
	case hasAccent:
		style = accented
	}
	// A note has no session, so no liveness and no agent status — its card
	// carries no badge at all rather than a badge that means nothing.
	lead, trail := "╭─ "+mark.glyph+" ", n.Kind.Label()+age(n.CreatedAt)
	if !n.HasSession() {
		lead, trail = "╭─ ", n.Kind.Label()
	}
	// Tags sit at the right-hand end of the *bottom* border, after the kind and
	// the age, and take what those leave. The top border is the title's alone:
	// a card is 22 cells wide, the two labels cannot both have one border, and
	// the title is what says which node this is — see
	// docs/adr/0009-tags-live-on-the-bottom-border.md. The kind and the age are
	// the fixed half and never give way; below a few cells there is no room to
	// say anything, so the tags go rather than shrink to a bare ellipsis.
	foot := "─╯"
	if label := footLabel(group, n.Tags,
		cardWidth-lipgloss.Width(footLead)-lipgloss.Width(trail)-tagOverhead); label != "" {
		foot = " " + label + " ─╯"
	}

	lines := make([]string, 0, body+2)
	lines = append(lines, topBorder(style, mark, border(cardWidth, lead, n.Title, "─╮")))
	for i := 0; i < body; i++ {
		text := ""
		if i < len(content) {
			text = content[i]
		}
		lines = append(lines, bodyLine(style, text))
	}
	return append(lines, style.Render(border(cardWidth, footLead, trail, foot)))
}

// topBorder draws the top border, with the badge in the colour the agent's own
// report gives it. The rest of the border keeps the card's style — the badge is
// the one thing on a card that is coloured by something other than the node.
//
// Rendered in one piece where there is no badge colour, so a map with no agents
// on it emits no more escape sequences than it did before there were any.
func topBorder(style lipgloss.Style, mark badgeMark, plain string) string {
	badge, ok := badgeStyles[mark.colour]
	if !ok {
		return style.Render(plain)
	}
	head := "╭─ "
	return style.Render(head) + badge.Render(mark.glyph) + style.Render(strings.TrimPrefix(plain, head+mark.glyph))
}

// badgeStyles is built once, for the reason accentStyles is: a card asks for
// its badge on every frame.
var badgeStyles = func() map[string]lipgloss.Style {
	styles := make(map[string]lipgloss.Style, len(statusColours))
	for _, code := range statusColours {
		styles[code] = lipgloss.NewStyle().Foreground(lipgloss.Color(code))
	}
	return styles
}()

// footLabel is what the bottom border carries after the kind and the age: the
// group the node's cell falls inside, and then its tags. The group goes first
// because it is the one that changes on its own — a card shoved out of a
// rectangle leaves the group without anybody editing anything — and it is held
// to what it can take without costing the card its tags, so that joining a
// group is not a silent way to lose them.
//
// Below room for both, the tags go rather than both shrinking to a pair of
// ellipses. That is ADR 0009's own rule about the kind and the age, one label
// further along: what is left has to be able to say something.
func footLabel(group string, tags []string, room int) string {
	tagged := tagLabel(tags)
	switch {
	case room < minTagWidth:
		return ""
	case group == "":
		return truncate(tagged, room)
	case tagged == "" || room < 2*minTagWidth+1:
		return truncate(group, room)
	}
	group = truncate(group, room-minTagWidth-1)
	return group + " " + truncate(tagged, room-lipgloss.Width(group)-1)
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
	text = closeStyling(ansi.Truncate(text, cardBodyWidth, "…"))
	pad := strings.Repeat(" ", maxInt(cardBodyWidth-lipgloss.Width(text), 0))
	return style.Render("│ ") + text + pad + style.Render(" │")
}

// closeStyling shuts whatever the text left open, so a heading's background
// cannot run out over the card's right-hand wall and a captured hyperlink
// cannot make the rest of the screen clickable. Only worth adding where there
// is styling to close: on a terminal that cannot show any, the renderer emits
// none and a bare reset would be the only escape sequence in the whole frame.
func closeStyling(text string) string {
	if !strings.Contains(text, "\x1b") {
		return text
	}
	text += "\x1b[0m"
	if strings.Contains(text, "\x1b]8;;") {
		// A hyperlink, which a colour reset does not close. capture-pane emits
		// OSC 8 for a pane that printed one, and glamour emits it for a link in
		// a note. Closing what is already closed costs a no-op.
		text += "\x1b]8;;\x1b\\"
	}
	return text
}

// border is one edge of a box: a lead, a label, and a rule filling the rest out
// to width. Cards and group rectangles are both drawn with it, which is what
// makes a group read as a box on the map rather than as a different kind of
// thing.
func border(width int, lead, text, tail string) string {
	room := width - lipgloss.Width(lead) - lipgloss.Width(tail) - 1 // the space before the tail
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

// tagLabel is a node's tags as a card draws them. The hash is the card's, not
// the tag's — it is what makes a label on a border read as a tag.
func tagLabel(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "#" + strings.Join(tags, " #")
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
