package tui

// Card attributes (SPEC §6, §7.3): the four things about a node the user sets
// by hand — its title, its accent colour, its tags, and how big its card is.
// All four are node identity rather than anything about the session behind it,
// so all four persist and all four are drawn on the card.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// The accent colours a card can wear. A closed, named set rather than free
// colour: a name is what the workspace file carries, so it has to mean the same
// thing on the next machine and under the next terminal theme — and `c` cycles,
// which needs the set to have an order. See
// docs/adr/0008-accent-colours-are-a-named-palette.md.
var colours = []struct {
	Name string
	Code string // an ANSI colour, so a terminal theme still gets a say
}{
	{"red", "9"},
	{"orange", "208"},
	{"yellow", "11"},
	{"green", "10"},
	{"cyan", "14"},
	{"blue", "12"},
	{"violet", "141"},
	{"pink", "13"},
}

// accentStyles is built once: a card asks for its accent on every frame, and
// lipgloss styles are not free enough to build one per card per frame.
var accentStyles = func() map[string]lipgloss.Style {
	styles := make(map[string]lipgloss.Style, len(colours))
	for _, c := range colours {
		styles[c.Name] = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Code))
	}
	return styles
}()

// accent is what to draw a card's border in, and whether the node named a
// colour at all. A name nobody knows — a hand-edited workspace file, a palette
// that has since changed — draws as no colour rather than as a guess.
func accent(name string) (lipgloss.Style, bool) {
	style, ok := accentStyles[name]
	return style, ok
}

// colourIndex is where a name sits in the palette, and -1 for a card that has
// no colour or one the palette does not know.
func colourIndex(name string) int {
	for i, c := range colours {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// cycleColour steps a card's accent on by one, and off the end of the palette
// back to no colour at all — which is how a border is made plain again without
// opening the picker.
func (m Model) cycleColour() (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok {
		return m, nil
	}
	next := ""
	if i := colourIndex(node.Colour); i+1 < len(colours) {
		next = colours[i+1].Name
	}
	return m.withNode(node.ID, func(n *state.Node) { n.Colour = next }).save(), nil
}

// openColours opens the picker on the colour the node already has, so that C is
// a way to change a choice rather than a way to lose it.
func (m Model) openColours() (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok {
		return m, nil
	}
	m.mode, m.editing, m.choice = modeColour, node.ID, maxInt(colourIndex(node.Colour), 0)
	return m, nil
}

func (m Model) updateColour(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.choice = (m.choice + 1) % len(colours)
	case "k", "up":
		m.choice = (m.choice + len(colours) - 1) % len(colours)
	case "enter":
		name := colours[m.choice].Name
		m, id, _ := m.done()
		return m.withNode(id, func(n *state.Node) { n.Colour = name }).save(), nil
	case "esc", "q":
		m, _, _ = m.done()
	}
	return m, nil
}

// shown is a node as its card draws it: itself, except while the colour picker
// is open on it — then the card wears the colour under the picker's cursor, so
// that choosing a colour means seeing it rather than reading its name.
func (m Model) shown(n state.Node) state.Node {
	if m.mode == modeColour && n.ID == m.editing {
		n.Colour = colours[m.choice].Name
	}
	return n
}

// drawnSelected reports whether a card is drawn as the one under the cursor.
// It is, except while the colour picker is open on it: the selection style is
// a colour too, and it would paint over the very thing being chosen. Nothing is
// lost by dropping it for those few keystrokes — the card wearing a colour that
// changes under j and k is unmistakably the one being coloured.
func (m Model) drawnSelected(n state.Node) bool {
	if m.mode == modeColour && n.ID == m.editing {
		return false
	}
	return n.Pos == m.ws.Viewport.Cursor
}

// colourBar names the colour under the picker's cursor. The name and not a
// swatch: the card behind the bar is already showing the colour itself.
func (m Model) colourBar() string {
	return fmt.Sprintf("Colour %s (%d of %d) · j/k choose · ⏎ set · esc cancel",
		colours[m.choice].Name, m.choice+1, len(colours))
}

// action is what a keystroke did to a one-line prompt. The three prompts differ
// only in what they collect and what they do with it, so the editing itself is
// one piece of code and this is how it answers.
type action int

const (
	actTyping action = iota
	actCommit
	actCancel
)

// typed edits the prompt's text, and reports whether the key ended it. The
// limit is the prompt's own: text arrives from the keyboard, reaches a card, a
// status line, and the workspace file, so what it may contain and how much of
// it there may be is decided here rather than wherever it lands.
func (m Model) typed(msg tea.KeyMsg, limit int) (Model, action) {
	switch msg.Type {
	case tea.KeyEnter:
		return m, actCommit
	case tea.KeyEsc:
		return m, actCancel
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.input = clampRunes(m.input+sanitise(string(msg.Runes)), limit)
	}
	return m, actTyping
}

// editAttr opens a prompt on an attribute of the node under the cursor. The
// node is fixed here rather than read again at Enter: a create landing while
// the prompt is up moves the cursor onto the new node, and the user would then
// be editing a node they never chose.
func (m Model) editAttr(at mode, text func(state.Node) string) (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok {
		return m, nil
	}
	m.mode, m.editing, m.input = at, node.ID, text(node)
	return m, nil
}

// done closes a prompt, whatever it was collecting, and hands back the node it
// was opened on.
func (m Model) done() (Model, string, string) {
	id, input := m.editing, m.input
	m.mode, m.editing, m.input = modeNormal, "", ""
	return m, id, input
}

func (m Model) updateRename(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m, act := m.typed(msg, maxTitleLen)
	switch act {
	case actCancel:
		m, _, _ = m.done()
	case actCommit:
		m, id, title := m.done()
		title = strings.TrimSpace(title)
		if title == "" {
			// A card whose border says nothing is a node you cannot pick out of
			// a map; the id is what it was called before it was named.
			title = id
		}
		return m.withNode(id, func(n *state.Node) { n.Title = title }).save(), nil
	}
	return m, nil
}

// sizes are the card sizes in the order s cycles them (SPEC §7.3, S→M→L). An
// unset size reads as medium, so the first press of s on a fresh card asks for
// a bigger one.
var sizes = []state.CardSize{state.SizeS, state.SizeM, state.SizeL}

// sizeLines is how many body lines a card of this node's size has room for,
// looked up in config. It is the preview line count for a session-backed node
// and the cap on a note's rendered body — one card size, one card height.
func (m Model) sizeLines(n state.Node) int {
	lines := m.cfg.General.PreviewLines
	switch n.Size {
	case state.SizeS:
		return maxInt(lines.S, 0)
	case state.SizeL:
		return maxInt(lines.L, 0)
	default:
		return maxInt(lines.M, 0)
	}
}

// cycleSize steps a card through the three sizes and round again.
func (m Model) cycleSize() (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok {
		return m, nil
	}
	next := sizes[(sizeIndex(node.Size)+1)%len(sizes)]
	m = m.withNode(node.ID, func(n *state.Node) { n.Size = next }).save().follow()
	// A size is both how many lines every card on the map has room for and how
	// many tmux is asked for, so a change to one leaves the viewport arithmetic
	// and every snapshot on screen out of date at once.
	shown, cmd := m.markDirty(m.visible()...)
	return shown, cmd
}

// sizeIndex is where a size sits in the cycle. A card that has never been sized
// — and one sized by hand to something the palette of sizes does not have — is
// medium, which is what its card is already being drawn at.
func sizeIndex(s state.CardSize) int {
	for i, c := range sizes {
		if c == s {
			return i
		}
	}
	return 1
}

// maxTagsLen bounds the tag prompt. Several tags on one node is the point, so
// it holds more than a title does — and it is still a bound, because this is
// keyboard text on its way to a card and to the workspace file.
const maxTagsLen = 96

func (m Model) updateTags(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m, act := m.typed(msg, maxTagsLen)
	switch act {
	case actCancel:
		m, _, _ = m.done()
	case actCommit:
		m, id, input := m.done()
		tags := parseTags(input)
		return m.withNode(id, func(n *state.Node) { n.Tags = tags }).save(), nil
	}
	return m, nil
}

// parseTags reads the prompt as a list of tags: whitespace separates them, and
// the hash a card draws in front of one is punctuation rather than part of it.
func parseTags(input string) []string {
	var tags []string
	for _, f := range strings.Fields(input) {
		if f = strings.TrimPrefix(f, "#"); f != "" {
			tags = append(tags, f)
		}
	}
	return tags
}

// clampRunes cuts text to at most n runes. Runes and not cells: this is a bound on
// what is stored, and the card does its own cutting to fit the width it has.
func clampRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
