package tui

// The help overlay (SPEC §7.3): `?` answers "what does this key do" for the
// whole tool. It is generated, never written — a hand-maintained help screen
// drifts from the real bindings the first time someone remaps one, and this is
// the one screen that must not.
//
// The map's own keys come from the live keymap. The modal contexts — a gathered
// selection, a held group, a filter, the palette, a peek — read literal keys in
// their own handlers, and reach the overlay as the same small tables the status
// bar renders its contextual hints from.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var helpTitleStyle = lipgloss.NewStyle().Bold(true)

// keyHint is one key and what it does, in two lengths: the word the status bar
// has room for, and the sentence the overlay does.
type keyHint struct {
	keys   string
	short  string
	detail string
}

// contextKeys is one place the keyboard means something other than what it
// means on the map, and every key that means something there.
type contextKeys struct {
	title string
	when  string
	keys  []keyHint
}

// hints is the status bar's rendering: the keys and their short words, in the
// order the table gives them up.
func (c contextKeys) hints() string {
	shown := make([]string, len(c.keys))
	for i, k := range c.keys {
		shown[i] = k.keys + " " + k.short
	}
	return strings.Join(shown, " · ")
}

// contexts is every modal context the overlay covers, in the order a user meets
// them: what the map does with several nodes gathered, what it does with a
// group in hand, and then the three screens that take the keyboard for
// themselves.
var contexts = []contextKeys{selectionKeys, heldKeys, filterKeys, paletteKeys, peekKeys}

func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.mode, m.helpTop = modeHelp, 0
	return m, nil
}

// updateHelp is every key the overlay reads. Esc closes it and the rest scroll;
// there is deliberately nothing here that acts on the map, so a keystroke
// landing while the overlay is up cannot move a card behind it.
func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := maxInt(m.helpRows()-1, 1)
	switch msg.String() {
	case "esc", "?", "q":
		m.mode, m.helpTop = modeNormal, 0
		return m, nil
	case "j", "down":
		m.helpTop++
	case "k", "up":
		m.helpTop--
	case " ", "pgdown", "ctrl+f", "ctrl+d":
		m.helpTop += page
	case "b", "pgup", "ctrl+b", "ctrl+u":
		m.helpTop -= page
	case "g", "home":
		m.helpTop = 0
	case "G", "end":
		m.helpTop = len(m.helpLines())
	}
	return m.clampHelp(), nil
}

// clampHelp keeps the window inside the list: scrolling is a request, and the
// list has ends.
func (m Model) clampHelp() Model {
	m.helpTop = clamp(m.helpTop, 0, maxInt(len(m.helpLines())-m.helpRows(), 0))
	return m
}

// helpRows is how many lines of the list the terminal has room for: everything
// but the status bar.
func (m Model) helpRows() int { return maxInt(m.height-1, 1) }

// helpKeyCol is the width the key column is padded to. Wide enough for the
// longest default binding and narrow enough to leave a sentence beside it in
// eighty columns.
const helpKeyCol = 12

// helpLines is the overlay's whole content, unwindowed: the map's keys read
// from the live keymap, then each modal context.
func (m Model) helpLines() []string {
	lines := []string{helpTitleStyle.Render("The map"), ""}
	for _, c := range commands {
		lines = append(lines, helpRow(m.keys.label(c.name), c.label))
	}
	// Counts are the one part of the keyboard config cannot move, so they are
	// stated rather than listed.
	lines = append(lines, helpRow("1-9", "Repeat the next motion (3l is three presses of l)"))

	for _, ctx := range contexts {
		lines = append(lines, "", helpTitleStyle.Render(ctx.title)+" — "+ctx.when, "")
		for _, k := range ctx.keys {
			what := k.detail
			if what == "" {
				what = k.short
			}
			lines = append(lines, helpRow(k.keys, what))
		}
	}
	return lines
}

// helpRow is one key and what it does, the two held apart in a column so the
// list reads down rather than being hunted through.
func helpRow(keys, what string) string {
	pad := helpKeyCol - lipgloss.Width(keys)
	if pad < 1 {
		pad = 1
	}
	return "  " + hintStyle.Render(keys) + strings.Repeat(" ", pad) + what
}

// helpView is the list itself, drawn from the top left: it is a list being read
// down, not a map being looked at.
func (m Model) helpView() string {
	lines := m.helpLines()
	end := minInt(m.helpTop+m.helpRows(), len(lines))
	shown := make([]string, 0, maxInt(end-m.helpTop, 0))
	for _, line := range lines[clamp(m.helpTop, 0, len(lines)):end] {
		shown = append(shown, truncate(line, m.width))
	}
	return lipgloss.Place(m.width, m.helpRows(), lipgloss.Left, lipgloss.Top, strings.Join(shown, "\n"))
}

// helpBar says where in the list you are, which is the only orientation a
// screen of keys gives you.
func (m Model) helpBar() string {
	lines := len(m.helpLines())
	end := minInt(m.helpTop+m.helpRows(), lines)
	return fmt.Sprintf("help · %d–%d of %d · j/k scroll · esc map", m.helpTop+1, end, lines)
}
