package tui

// The palette (SPEC §7.2): one fuzzy list over everything Trigpoint can do and
// everywhere it can go — every command that has a binding, every node on every
// map, every workspace, and every session there is to adopt.
//
// It is the discoverability backstop, and bindings are the fast path. So a
// command entry does not reimplement its action: it and the key that runs it
// are the same function value, taken from the command table in keymap.go — the
// same table the keyboard, the status bar's hints, and the help overlay are
// read from. An action nothing is bound to is offered here and nowhere else,
// which is what makes an empty binding a thing a user can write.

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// paletteKeys is what the palette itself answers to, for the help overlay: the
// query is typed into the status bar and everything else is these.
var paletteKeys = contextKeys{
	title: "Palette",
	when:  "while the palette is open",
	keys: []keyHint{
		{"↑↓", "choose", "Move through the matches (ctrl+p and ctrl+n do the same)"},
		{"⏎", "run", "Run the chosen entry: go to a node, or do what the map would"},
		{"esc", "cancel", "Close the palette, having done nothing"},
	},
}

// entry is one line of the palette: what it is called, what it is, and what
// choosing it does.
type entry struct {
	label  string
	detail string
	run    func(Model) (tea.Model, tea.Cmd)
}

// detailPenalty is what a match in an entry's detail costs against one in its
// name. Both are searched — a workspace name is how you find a node on another
// map — but what the entry is called is what was most likely typed at.
const detailPenalty = 100

// openPalette builds the list and puts it up. The adoption candidates are a
// subprocess away (§9.3), so they are asked for here and folded in when tmux
// answers: a palette that waited for them would freeze on the busy server
// adoption exists for.
func (m Model) openPalette() (tea.Model, tea.Cmd) {
	m.mode, m.input, m.choice = modePalette, "", 0
	m.palette = m.paletteEntries()
	return m, m.askAdoptable()
}

// askAdoptable is the question A asks, addressed to the palette: the same list
// of strangers, marked as something the palette wanted, so that an answer
// arriving after the palette has closed opens no picker behind it.
func (m Model) askAdoptable() tea.Cmd {
	ask := m.adoptable()
	return func() tea.Msg {
		msg := ask().(adoptableMsg)
		msg.forPalette = true
		return msg
	}
}

// closePalette puts the map back, and the count with it. A count is typed at
// the map and the palette is a screen away from it: an entry run from here does
// what it says once, not three times because a 3 was in hand when it opened.
func (m Model) closePalette() Model {
	m.mode, m.input, m.choice, m.palette = modeNormal, "", 0, nil
	m.repeat, m.count = 1, ""
	return m
}

// updatePalette is every key the palette reads: the arrows choose, Enter runs
// what is chosen, Esc closes, and everything else is the query.
func (m Model) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+p":
		m.choice = maxInt(m.choice-1, 0)
		return m, nil
	case "down", "ctrl+n":
		m.choice = minInt(m.choice+1, maxInt(len(m.paletteMatches())-1, 0))
		return m, nil
	}

	before := m.input
	m, act := m.typed(msg, maxQueryLen)
	switch act {
	case actCancel:
		return m.closePalette(), nil
	case actCommit:
		matches := m.paletteMatches()
		if len(matches) == 0 {
			return m.closePalette(), nil
		}
		chosen := matches[clamp(m.choice, 0, len(matches)-1)]
		return chosen.run(m.closePalette())
	}
	if m.input != before {
		// A different query is a different list, and the cursor was pointing
		// into the old one.
		m.choice = 0
	}
	return m, nil
}

// paletteEntries is everything the palette can offer, in the order it offers it
// with nothing typed (§7.2): the nodes first, because going somewhere is what
// the palette is most often opened for, then what the map does, then where else
// there is to be.
func (m Model) paletteEntries() []entry {
	names, err := m.workspaces()
	if err != nil {
		// The state directory could not be read, so the other maps are not
		// there to be offered. The one on screen still is, and saying so is the
		// switch entry's business rather than this list's.
		names = []string{m.ws.Name}
	}
	entries := make([]entry, 0, len(commands)+len(m.ws.Nodes))
	// The map on screen first: its nodes are the ones being reached for, and the
	// other maps' are what the palette exists to reach at all.
	entries = append(entries, m.nodeEntries(m.ws.Name)...)
	for _, name := range names {
		if name != m.ws.Name {
			entries = append(entries, m.nodeEntries(name)...)
		}
	}
	for _, c := range commands {
		if c.hidden {
			continue
		}
		entries = append(entries, entry{label: c.label, detail: m.keys.label(c.name), run: c.run})
	}
	for _, name := range names {
		if name == m.ws.Name {
			// Switching to the workspace you are on does nothing, so it is not
			// offered as something to choose.
			continue
		}
		workspace := name
		entries = append(entries, entry{
			label:  "Workspace " + name,
			detail: "switch",
			run:    func(m Model) (tea.Model, tea.Cmd) { return m.switchTo(workspace) },
		})
	}
	return entries
}

// nodeEntries is one workspace's nodes. The maps other than the one on screen
// are read off disk on the keystroke rather than held: a list built when the
// palette was last opened would offer nodes that have since been killed.
//
// A workspace that cannot be read offers no nodes rather than an error — its
// switch entry is still there, and going to it is what says what is wrong with
// it.
//
// ponytail: one file read per workspace, on the update loop. Fine for the
// handful a person keeps; cache against the directory's mtime if a hundred ever
// show up.
func (m Model) nodeEntries(name string) []entry {
	ws := m.ws
	if name != m.ws.Name {
		loaded, err := state.Load(m.stateDir, name)
		if err != nil {
			return nil
		}
		ws = loaded
	}
	entries := make([]entry, 0, len(ws.Nodes))
	for _, n := range ws.Nodes {
		workspace, id := name, n.ID
		detail := name + " · " + n.Kind.Label()
		if tags := tagLabel(n.Tags); tags != "" {
			detail += " · " + tags
		}
		entries = append(entries, entry{
			label:  n.Title,
			detail: detail,
			run:    func(m Model) (tea.Model, tea.Cmd) { return m.jumpTo(workspace, id) },
		})
	}
	return entries
}

// withCandidates folds tmux's answer into an open palette. A palette that has
// been closed, or one whose question failed, simply does not gain the entries —
// A is still there to ask again and to say why.
func (m Model) withCandidates(msg adoptableMsg) Model {
	if m.mode != modePalette || msg.err != nil || len(msg.names) == 0 {
		return m
	}
	// A fresh slice, not the one in hand: a Model is copied by value all over
	// Bubble Tea, and appending through would edit the list an older copy is
	// still holding.
	entries := make([]entry, 0, len(m.palette)+len(msg.names))
	entries = append(entries, m.palette...)
	for _, name := range msg.names {
		session := name
		entries = append(entries, entry{
			label:  "Adopt " + name,
			detail: "session",
			run:    func(m Model) (tea.Model, tea.Cmd) { return m.adopt(session) },
		})
	}
	m.palette = entries
	// The list has grown under a cursor pointing into the old one, and the
	// matches are ordered by score rather than appended to. The answer lands in
	// the moment after the palette opened, so the choice this gives up is one
	// nobody has made yet.
	m.choice = 0
	return m
}

// paletteMatches is the entries answering the query, tightest first. Ties keep
// the order the entries were built in, so the list does not reshuffle itself
// under a query that cannot tell two entries apart.
func (m Model) paletteMatches() []entry {
	if m.input == "" {
		return m.palette
	}
	type scored struct {
		entry entry
		score int
	}
	kept := make([]scored, 0, len(m.palette))
	for _, e := range m.palette {
		score := state.Fuzzy(m.input, e.label)
		if score < 0 {
			if inDetail := state.Fuzzy(m.input, e.detail); inDetail >= 0 {
				score = inDetail + detailPenalty
			}
		}
		if score < 0 {
			continue
		}
		kept = append(kept, scored{entry: e, score: score})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].score < kept[j].score })
	matches := make([]entry, 0, len(kept))
	for _, s := range kept {
		matches = append(matches, s.entry)
	}
	return matches
}

// jumpTo is what choosing a node does: the workspace it is on, and then the
// cursor on the node itself. The switch comes first because the cursor is the
// map's, and setting it before the reload would set it on the map being left.
//
// Any filter goes with it. The palette matches every node there is and a filter
// matches only some, so a jump can name a card the filter is hiding — and
// landing the cursor on a card that is not on screen is the one thing a filter
// is not allowed to do (§7.1).
func (m Model) jumpTo(workspace, id string) (tea.Model, tea.Cmd) {
	m.filter = ""
	var switched tea.Cmd
	if workspace != m.ws.Name {
		m, switched = m.switchTo(workspace)
		if m.ws.Name != workspace {
			// The switch failed and the status bar is saying why; there is no
			// map here to put a cursor on.
			return m, switched
		}
	}
	node, ok := m.node(id)
	if !ok {
		// The node was on the map when the palette was built and is not now.
		m.status = "That node is no longer on the map."
		return m, switched
	}
	m.ws.Viewport.Cursor = node.Pos
	m = m.follow().save()
	shown, cmd := m.markDirty(m.visible()...)
	return shown, tea.Batch(switched, cmd)
}

// paletteRows is how many entries the palette shows at once: the screen, less
// the status bar the query is typed into and one line of breathing room.
func (m Model) paletteRows() int { return maxInt(m.height-2, 1) }

// paletteView is the list itself, drawn from the top left: it is a list being
// read down, not a map being looked at.
func (m Model) paletteView() string {
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return hintStyle.Render("Nothing matches " + flatten(m.input) + ".")
	}
	chosen := clamp(m.choice, 0, len(matches)-1)
	// The window is a page rather than a scroll: it turns over when the choice
	// leaves it, and stands still while the choice moves inside it. A window
	// that kept the choice at its edge would scroll the whole list under every
	// press of an arrow key.
	page := m.paletteRows()
	top := chosen / page * page
	end := minInt(top+page, len(matches))

	rows := make([]string, 0, end-top)
	for i := top; i < end; i++ {
		rows = append(rows, m.paletteRow(matches[i], i == chosen))
	}
	return lipgloss.Place(m.width, m.paletteRows(), lipgloss.Left, lipgloss.Top, strings.Join(rows, "\n"))
}

// paletteRow is one entry: what it is called on the left, what it is on the
// right, and the chosen one marked — with the mark in the text and not only in
// the colour, because a terminal without colour still has to say which line
// Enter would run.
func (m Model) paletteRow(e entry, chosen bool) string {
	lead, style := "  ", cardStyle
	if chosen {
		lead, style = "› ", selectedStyle
	}
	detail := flatten(e.detail)
	label := truncate(flatten(e.label), maxInt(m.width-lipgloss.Width(lead)-lipgloss.Width(detail)-2, 1))
	gap := m.width - lipgloss.Width(lead) - lipgloss.Width(label) - lipgloss.Width(detail)
	if gap < 1 {
		return style.Render(lead + label)
	}
	return style.Render(lead+label) + strings.Repeat(" ", gap) + hintStyle.Render(detail)
}

// paletteBar is the query being typed and where in the list it has got to.
func (m Model) paletteBar() string {
	matches := m.paletteMatches()
	where := "no matches"
	if len(matches) > 0 {
		where = fmt.Sprintf("%d of %d", clamp(m.choice, 0, len(matches)-1)+1, len(matches))
	}
	return fmt.Sprintf("› %s▏ · %s · %s", flatten(m.input), where, paletteKeys.hints())
}
