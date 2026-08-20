package tui

// The keymap (SPEC §7.3, §10). One table says what the map can do: what config
// calls each action, what the palette and the help overlay call it, what the
// status bar hints at, what it is bound to by default, and what running it does.
//
// The table is the keymap and the palette and the hints, so the three cannot
// drift. A command entry does not reimplement its action and does not replay a
// keystroke either: the keyboard and the palette call the same function value.
// See docs/adr/0018-the-keymap-is-the-command-table.md.

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// command is one thing the map does.
type command struct {
	name   string // what [keymap] binds: "cursor_left"
	label  string // what the palette and the overlay call it
	hint   string // what the status bar calls it, empty for one it does not offer
	keys   string // the default binding, in config's own grammar
	hidden bool   // not offered in the palette, which is no way to open the palette
	run    func(Model) (tea.Model, tea.Cmd)
}

// commands is the keyboard model. The order is the order the status bar gives
// its hints up in — a terminal too narrow for all of them loses them from the
// end — and the order an unfiltered palette and the help overlay open in, so
// the keys worth knowing come first and the motions, which nobody needs a list
// to find, come last.
//
// Filled in init rather than at its declaration because the table holds the
// handlers, and one of them opens the palette, which reads the table: a
// package-level initialiser that referred to itself through two function calls
// is a cycle Go refuses to order, however plainly it terminates at runtime.
var commands []command

func init() {
	commands = []command{
		{name: "attach", label: "Attach to node / edit note", hint: "attach", keys: "enter", run: attachOrEdit},
		{name: "peek", label: "Peek at a node's output", hint: "peek", keys: "space", run: Model.peek},
		{name: "new_shell", label: "New shell node", hint: "new", keys: "n", run: prompt(state.KindShell)},
		{name: "new_note", label: "New note", hint: "note", keys: "N", run: prompt(state.KindNote)},
		// No status-bar hint: the bar drops its hints from the end, and at eighty
		// columns one more would cost "q quit" — which is the one hint a first run
		// most needs.
		{name: "new_agent", label: "New agent node (picker: the configured presets, or a command of your own)", keys: "a", run: Model.openAgents},
		{name: "adopt", label: "Adopt a session", hint: "adopt", keys: "A", run: askAdoptable},
		// No status-bar hint, for the same reason the agent picker has none: the
		// bar drops hints from the end, and this one would cost "q quit".
		{name: "attention", label: "Jump to the next node needing attention (needs-you, then unread)", keys: "u", run: Model.jumpAttention},
		{name: "kill", label: "Kill node", hint: "kill", keys: "x", run: confirmKill},
		{name: "quit", label: "Quit Trigpoint", hint: "quit", keys: "q", run: quit},
		{name: "rename", label: "Rename node", hint: "name", keys: "r", run: renamePrompt},
		{name: "colour_cycle", label: "Cycle colour", hint: "colour", keys: "c", run: Model.cycleColour},
		{name: "tags", label: "Edit tags", hint: "tags", keys: "t", run: tagsPrompt},
		{name: "size", label: "Cycle card size", hint: "size", keys: "s", run: Model.cycleSize},
		{name: "select", label: "Visual select", keys: "v", run: Model.toggleSelect},
		{name: "group", label: "Group the selection", keys: "g", run: Model.group},
		// The keys a held group answers are not in this table: they are one context
		// down, and are listed in the overlay under heldKeys and on the status bar
		// for as long as a group is held.
		{name: "hold", label: "Hold the group under the cursor (HJKL move · hjkl resize · x delete)", keys: "V", run: Model.hold},
		{name: "workspace_next", label: "Next workspace", hint: "workspace", keys: "tab", run: cycleWorkspace(1)},
		{name: "colour_pick", label: "Choose colour", keys: "C", run: Model.openColours},
		{name: "workspace_prev", label: "Previous workspace", keys: "shift+tab", run: cycleWorkspace(-1)},
		{name: "workspace_picker", label: "Workspace picker", keys: "w", run: Model.openWorkspaces},
		{name: "filter", label: "Filter the map", keys: "/", run: Model.openFilter},
		// The palette is not offered in the palette: it is already open. It is in
		// the table all the same, because it is a key the map answers and so a key
		// the overlay has to name and config has to be able to move.
		{name: "palette", label: "Open the palette", keys: "ctrl+k, :", hidden: true, run: Model.openPalette},
		{name: "help", label: "Help overlay", keys: "?", run: Model.openHelp},
		{name: "clear", label: "Clear the selection, then the filter", keys: "esc", run: escape},
		{name: "cursor_left", label: "Move cursor left", keys: "h, left", run: cursorMove(state.Cell{Col: -1})},
		{name: "cursor_down", label: "Move cursor down", keys: "j, down", run: cursorMove(state.Cell{Row: 1})},
		{name: "cursor_up", label: "Move cursor up", keys: "k, up", run: cursorMove(state.Cell{Row: -1})},
		{name: "cursor_right", label: "Move cursor right", keys: "l, right", run: cursorMove(state.Cell{Col: 1})},
		{name: "node_left", label: "Move node left", keys: "H", run: nodeMove(state.Cell{Col: -1})},
		{name: "node_down", label: "Move node down", keys: "J", run: nodeMove(state.Cell{Row: 1})},
		{name: "node_up", label: "Move node up", keys: "K", run: nodeMove(state.Cell{Row: -1})},
		{name: "node_right", label: "Move node right", keys: "L", run: nodeMove(state.Cell{Col: 1})},
		{name: "origin", label: "Jump to the origin", keys: "0", run: jumpOrigin},
		{name: "centre", label: "Centre on the cursor", keys: "z z", run: centreOnCursor},
	}
}

// Keymap is the bindings a map view is running on: what each key sequence does,
// and what keys each action answers to. It is resolved once, at startup, from
// the defaults above and whatever [keymap] overrides.
type Keymap struct {
	bound  map[string]string     // action -> binding, in config's grammar
	alts   map[string][][]string // action -> the sequences it answers to
	action map[string]int        // a sequence, space-joined -> index into commands
	prefix map[string]bool       // a sequence that can only continue a longer one
}

// NewKeymap layers overrides over the defaults and refuses a keymap that cannot
// work. Three things make one: an action that does not exist, one sequence
// bound to two actions, and a binding that is the start of another — which the
// prefix machine would leave permanently unreachable.
//
// Every check walks the command table rather than the override map, so a keymap
// with two things wrong with it names the same one first on every run.
func NewKeymap(overrides map[string]string) (Keymap, error) {
	km := Keymap{
		bound:  make(map[string]string, len(commands)),
		alts:   make(map[string][][]string, len(commands)),
		action: make(map[string]int, len(commands)),
		prefix: map[string]bool{},
	}

	known := make(map[string]bool, len(commands))
	for _, c := range commands {
		known[c.name] = true
	}
	unknown := make([]string, 0, len(overrides))
	for name := range overrides {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Keymap{}, fmt.Errorf("keymap: %q is not an action%s", unknown[0], didYouMean(unknown[0]))
	}

	for _, c := range commands {
		binding := c.keys
		if override, ok := overrides[c.name]; ok {
			binding = override
		}
		alts := parseBinding(binding)
		// An empty binding is a thing a user writes on purpose — the action
		// stays reachable through the palette (§7.2). A binding that names no
		// key at all is not, and dropping it silently would leave the same
		// unbound action with nothing to say what happened to it.
		if binding != "" && len(alts) == 0 {
			return Keymap{}, fmt.Errorf("keymap: %s is set to %q, which names no key; leave it empty to reach it from the palette alone",
				c.name, binding)
		}
		// 1 to 9 are read before the keymap, so an action bound to one would
		// be an action that never ran — the silently dead key this validation
		// exists to catch. Zero is not among them: it reaches the keymap
		// whenever no count is being typed, which is how the origin has always
		// been pressed.
		for _, alt := range alts {
			for _, key := range alt {
				if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
					return Keymap{}, fmt.Errorf("keymap: %s is bound to %q, and %q is a count prefix rather than a key: 3l is three presses of l on every map",
						c.name, binding, key)
				}
			}
		}
		km.bound[c.name], km.alts[c.name] = binding, alts
	}

	// Ownership first and whole, so that which of two clashing actions is named
	// as the offender does not depend on which the clash was noticed from.
	for i, c := range commands {
		for _, alt := range km.alts[c.name] {
			seq := strings.Join(alt, " ")
			if other, taken := km.action[seq]; taken {
				if other == i {
					return Keymap{}, fmt.Errorf("keymap: %s lists %q twice", c.name, seq)
				}
				return Keymap{}, fmt.Errorf("keymap: %q is bound to both %s and %s",
					seq, commands[other].name, c.name)
			}
			km.action[seq] = i
		}
	}
	for _, c := range commands {
		for _, alt := range km.alts[c.name] {
			for n := 1; n < len(alt); n++ {
				start := strings.Join(alt[:n], " ")
				if other, taken := km.action[start]; taken {
					return Keymap{}, fmt.Errorf("keymap: %q is bound to %s and is also the start of %s (%q); %s could never be pressed",
						start, commands[other].name, c.name, strings.Join(alt, " "), c.name)
				}
				km.prefix[start] = true
			}
		}
	}
	return km, nil
}

// didYouMean offers the nearest real action name, through the same subsequence
// match the filter and the palette rank by — a mistyped action is nearly always
// a typo of one that exists, and the list of all of them is too long for an
// error message.
func didYouMean(name string) string {
	best, bestScore := "", 0
	for _, c := range commands {
		score := state.Fuzzy(name, c.name)
		if score < 0 {
			continue
		}
		if best == "" || score < bestScore {
			best, bestScore = c.name, score
		}
	}
	if best == "" {
		return "; press ? on the map for the list"
	}
	return fmt.Sprintf("; did you mean %q?", best)
}

// parseBinding reads a binding the way it is written in config: alternatives
// separated by commas, each a sequence of key names separated by spaces. An
// empty binding is not a mistake — it leaves the action reachable through the
// palette and nowhere else (§7.2).
//
// The space bar is written "space", for the obvious reason: it is the one key
// whose own name is the separator. Bubble Tea calls it " ", and that is what a
// binding holds once parsed, so a sequence never carries a token the keyboard
// could not deliver.
func parseBinding(s string) [][]string {
	var alts [][]string
	for _, alt := range strings.Split(s, ",") {
		keys := strings.Fields(alt)
		if len(keys) == 0 {
			continue
		}
		for i, key := range keys {
			if key == "space" {
				keys[i] = " "
			}
		}
		alts = append(alts, keys)
	}
	return alts
}

// binding is what an action is bound to, in the grammar config writes it in.
func (k Keymap) binding(action string) string { return k.bound[action] }

// label is an action's keys as the palette and the overlay show them:
// alternatives separated by a slash, the keys of a sequence run together, and
// the space bar drawn rather than spelled — a binding of one space is a column
// of nothing at all, and flatten would drop it.
func (k Keymap) label(action string) string {
	shown := make([]string, 0, len(k.alts[action]))
	for _, alt := range k.alts[action] {
		keys := make([]string, len(alt))
		for i, key := range alt {
			if key == " " {
				key = "␣"
			}
			keys[i] = key
		}
		shown = append(shown, strings.Join(keys, ""))
	}
	if len(shown) == 0 {
		return unboundLabel
	}
	return strings.Join(shown, " / ")
}

// unboundLabel is what an action with no binding says for itself. Not blank:
// the point of listing it is that it is still reachable.
const unboundLabel = "palette only"

// hintSymbols are the keys the status bar draws rather than spells. The bar is
// the narrowest thing in Trigpoint, and these are the three keys whose names
// cost more width than they are worth.
var hintSymbols = map[string]string{"enter": "⏎", " ": "␣", "tab": "⇥"}

// hintKeys is an action's binding as the status bar names it: the first
// alternative only, because the bar is a reminder and not a list. Empty for an
// action nothing is bound to, which the bar then has no hint to give.
func (k Keymap) hintKeys(action string) string {
	alts := k.alts[action]
	if len(alts) == 0 {
		return ""
	}
	keys := make([]string, len(alts[0]))
	for i, key := range alts[0] {
		if symbol, ok := hintSymbols[key]; ok {
			key = symbol
		}
		keys[i] = key
	}
	return strings.Join(keys, "")
}

// dispatch is the map's key handling: a key that can only continue a longer
// binding is held, an exact match runs its action, and anything else drops the
// held prefix and is read fresh.
//
// Read fresh, not swallowed: pressing z and then l is a sequence abandoned and
// a cursor moved, which is what zz did before it was a sequence at all. The
// recursion is one deep — the retry runs with nothing held.
func (m Model) dispatch(key string) (tea.Model, tea.Cmd) {
	held := m.chord
	seq := strings.Join(append(append([]string{}, held...), key), " ")
	if m.keys.prefix[seq] {
		m.chord = append(append([]string{}, held...), key)
		return m, nil
	}
	m.chord = nil
	i, ok := m.keys.action[seq]
	if !ok {
		if len(held) > 0 {
			return m.dispatch(key)
		}
		// A key bound to nothing is still a key, and it ends a half-typed count
		// rather than leaving it to be applied to whatever comes next.
		m.count = ""
		return m, nil
	}
	// The count is parsed here and cleared here, so that an action which does
	// not repeat cannot leave one behind for the next key.
	m.repeat, m.count = repeat(m.count), ""
	return commands[i].run(m)
}

// countPrefix takes the digits of a count (§7.3). They are the one part of the
// keyboard config cannot move: a digit is a digit on every map, and binding one
// to an action would make 3l unreadable. Zero is the exception, and only when
// no count is being typed — that is the origin's own key.
func (m Model) countPrefix(key string) (Model, bool) {
	digit := len(key) == 1 && key[0] >= '1' && key[0] <= '9'
	if key == "0" && m.count != "" {
		digit = true
	}
	if !digit {
		return m, false
	}
	if len(m.count) < maxCountDigits {
		// An over-long count keeps what it had rather than resetting.
		m.count += key
	}
	return m, true
}

// The handlers the table names. The ones that are a method and nothing else are
// written as method expressions above; these are the ones that branch, take an
// argument, or set a mode by hand.

func attachOrEdit(m Model) (tea.Model, tea.Cmd) {
	// A note has no session; Enter opens its body in $EDITOR instead, by the
	// same release-the-terminal mechanism (§6).
	if node, ok := m.selected(); ok && node.Kind == state.KindNote {
		return m.editNote(node)
	}
	return m.attach()
}

func prompt(kind state.Kind) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		m.mode, m.input, m.creating = modeTitle, "", kind
		return m, nil
	}
}

// askAdoptable opens the picker on tmux's answer rather than on the keystroke:
// the question is a subprocess, and a map that froze on it would freeze on the
// one thing adoption is for — a server with a great many sessions.
func askAdoptable(m Model) (tea.Model, tea.Cmd) { return m, m.adoptable() }

func confirmKill(m Model) (tea.Model, tea.Cmd) {
	// The targets are fixed here rather than read again at y, because a create
	// landing while the prompt is up moves the cursor onto the new node — and
	// the user would then confirm one kill and get another.
	if ids := m.targets(); len(ids) > 0 {
		m.mode, m.killing = modeConfirmKill, ids
	}
	return m, nil
}

func quit(m Model) (tea.Model, tea.Cmd) {
	if m.cfg.General.ConfirmQuit {
		m.mode = modeConfirmQuit
		return m, nil
	}
	// Quitting Trigpoint kills nothing: every session outlives it (§5.2).
	return m, tea.Quit
}

func renamePrompt(m Model) (tea.Model, tea.Cmd) {
	return m.editAttr(modeRename, func(n state.Node) string { return n.Title })
}

func tagsPrompt(m Model) (tea.Model, tea.Cmd) {
	if len(m.selection) > 0 {
		return m.openBulkTags()
	}
	return m.editAttr(modeTags, func(n state.Node) string { return strings.Join(n.Tags, " ") })
}

func cycleWorkspace(by int) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) { return m.cycleWorkspace(by) }
}

// escape is Esc on the map. It has two things to clear and one key: the
// selection goes first, being the newer of the two, and the filter is still
// there to Esc again.
func escape(m Model) (tea.Model, tea.Cmd) {
	if next, cleared := m.clearSelection(); cleared {
		return next, nil
	}
	return m.clearFilter()
}

func cursorMove(d state.Cell) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		was := m.ws.Viewport
		return m.moveCursor(d, m.repeat).savedIfMoved(was).afterMotion(was.Offset)
	}
}

func nodeMove(d state.Cell) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		was := m.ws.Viewport
		return m.moveNode(d, m.repeat).savedIfMoved(was).afterMotion(was.Offset)
	}
}

// jumpOrigin lands centred: jumping somewhere and arriving in the corner of the
// screen hides the half of the map you jumped towards.
func jumpOrigin(m Model) (tea.Model, tea.Cmd) {
	was := m.ws.Viewport
	m.ws.Viewport.Cursor = state.Cell{}
	return m.centre().savedIfMoved(was).afterMotion(was.Offset)
}

func centreOnCursor(m Model) (tea.Model, tea.Cmd) {
	was := m.ws.Viewport
	return m.centre().savedIfMoved(was).afterMotion(was.Offset)
}

// afterMotion is the preview bookkeeping every motion shares: cards that have
// never been captured, and — if the viewport itself moved — every card now on
// screen, because off screen is where activity events are dropped and a card
// scrolled back to has been unwatched however recently it was captured. Moving
// the cursor without scrolling is not news about any session, so a map merely
// being navigated does not keep asking tmux the same question.
func (m Model) afterMotion(looking state.Cell) (tea.Model, tea.Cmd) {
	stale := m.unpreviewed()
	if m.ws.Viewport.Offset != looking {
		stale = m.visible()
	}
	next, cmd := m.markDirty(stale...)
	return next, cmd
}
