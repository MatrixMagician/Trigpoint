package tui

// Visual select and bulk operations (SPEC §7.3): `v` gathers several nodes, the
// motion keys extend what has been gathered, and the actions that take one node
// then take all of them — moved together, tagged together, killed together.
//
// It is deliberately not a mode. A non-empty selection *is* visual select: the
// map's keys go on meaning exactly what they meant, they simply have more than
// one node to mean it about, and every action asks targets() rather than the
// cursor. Esc clears it, which is the only way out that acts on nothing.

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// selectionCode is the colour a selected card's border is drawn in. A colour
// and nothing else, which is the same bargain the cursor's own highlight makes:
// a card is 22 cells wide and has no room for a badge saying "selected" as well
// as the status dot, the kind, the age, and the tags it already carries.
//
// A 256-colour code rather than one of the first sixteen, so that it cannot be
// mistaken for the cursor's blue or for any accent in the palette on a terminal
// that has the colours to tell them apart (§7.1).
const selectionCode = "212"

var selectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(selectionCode)).Bold(true)

// selectionCursorStyle is the cursor's card once it has been selected. The
// cursor cannot simply keep its own colour there: a selection of one node is
// the cursor's card and nothing else, and `v` would then change nothing on the
// map at all. So the selection takes the colour — which is what makes a
// gathered run read as one thing — and the cursor is marked inside it by the
// underline, the one mark a terminal with no colour still draws.
var selectionCursorStyle = selectionStyle.Underline(true)

// toggleSelect is `v`: it gathers the node under the cursor, or lets go of one
// already gathered. Toggling rather than only adding is what makes a selection
// recoverable — an over-shot `l` costs one keystroke rather than the whole
// selection.
func (m Model) toggleSelect() (tea.Model, tea.Cmd) {
	node, ok := m.selected()
	if !ok {
		// An empty cell holds no node to gather, and a selection of nothing
		// would still switch the motion keys over to extending.
		return m, nil
	}
	if m.isSelected(node.ID) {
		// Deleted from a copy, not from the slice itself: a Model is copied by
		// value all over Bubble Tea, and DeleteFunc rewrites in place.
		m.selection = slices.DeleteFunc(slices.Clone(m.selection),
			func(id string) bool { return id == node.ID })
		return m, nil
	}
	return m.gather(node.ID), nil
}

// extend gathers the node the cursor has just landed on, and only while there
// is already a selection to extend: the motion keys are how the map is
// navigated, and every hop adding a node would leave nothing able to move the
// cursor on its own.
func (m Model) extend() Model {
	if len(m.selection) == 0 {
		return m
	}
	node, ok := m.selected()
	if !ok || m.isSelected(node.ID) {
		return m
	}
	return m.gather(node.ID)
}

// gather adds one id, in a fresh slice: a Model is copied by value all over
// Bubble Tea, and appending in place would edit the selection every older copy
// is still holding.
func (m Model) gather(id string) Model {
	m.selection = append(append([]string(nil), m.selection...), id)
	return m
}

// clearSelection is Esc's first job on the map. It reports whether there was
// anything to clear, because Esc's other job — the filter — is what it does
// when there was not.
func (m Model) clearSelection() (Model, bool) {
	if len(m.selection) == 0 {
		return m, false
	}
	m.selection = nil
	return m, true
}

func (m Model) isSelected(id string) bool { return slices.Contains(m.selection, id) }

// targets is what an action applies to: the selection when `v` has gathered
// one, and the node under the cursor when it has not. Every bulk-capable action
// asks this and none of them branches on whether a selection exists — one node
// is a selection of one.
// A fresh slice, for the reason every other list on the Model is one: `x` holds
// its targets from the keystroke until y, and handing it the selection's own
// backing array would let the two grow into each other.
func (m Model) targets() []string {
	if len(m.selection) > 0 {
		return slices.Clone(m.selection)
	}
	if node, ok := m.selected(); ok {
		return []string{node.ID}
	}
	return nil
}

// pruneSelection drops whatever the selection is holding that the map is no
// longer showing — a killed node, a card a filter has hidden. Acting on a card
// that is not on screen is acting blind, which is the same rule that keeps the
// cursor off a hidden card (§7.1).
func (m Model) pruneSelection() Model {
	if len(m.selection) == 0 {
		return m
	}
	shown := make(map[string]bool, len(m.ws.Nodes))
	for _, n := range m.filtered() {
		shown[n.ID] = true
	}
	kept := make([]string, 0, len(m.selection))
	for _, id := range m.selection {
		if shown[id] {
			kept = append(kept, id)
		}
	}
	m.selection = kept
	return m
}

// openBulkTags is `t` with a selection gathered. It opens empty rather than on
// any one node's tags: the prompt is an edit and not a value, and prefilling it
// from one card would invite committing that card's tags onto all of them.
func (m Model) openBulkTags() (tea.Model, tea.Cmd) {
	m.mode, m.input = modeBulkTags, ""
	return m, nil
}

func (m Model) updateBulkTags(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m, act := m.typed(msg, maxTagsLen)
	switch act {
	case actCancel:
		m.mode, m.input = modeNormal, ""
	case actCommit:
		ids, input := m.targets(), m.input
		m.mode, m.input = modeNormal, ""
		add, drop := parseTagEdit(input)
		return m.withNodes(ids, func(n *state.Node) { n.Tags = editTags(n.Tags, add, drop) }).save(), nil
	}
	return m, nil
}

// parseTagEdit reads the bulk prompt: a word adds that tag to every selected
// node, and a word with a "-" in front takes it off. Two verbs in one prompt
// because a selection is a set of nodes and not one node — "set the tags of all
// of these to X" would quietly throw away every tag the cards do not share.
func parseTagEdit(input string) (add, drop []string) {
	for _, f := range strings.Fields(input) {
		if tag, cut := strings.CutPrefix(f, "-"); cut {
			if tag = strings.TrimPrefix(tag, "#"); tag != "" {
				drop = append(drop, tag)
			}
			continue
		}
		// The hash a card draws in front of a tag is punctuation rather than
		// part of it, exactly as the single-node prompt reads it.
		if tag := strings.TrimPrefix(f, "#"); tag != "" {
			add = append(add, tag)
		}
	}
	return add, drop
}

// editTags applies one bulk edit to one node's tags. Removals go first, so that
// "-x x" is not an argument with itself, and a tag a node already carries is
// not added twice.
func editTags(tags, add, drop []string) []string {
	kept := make([]string, 0, len(tags)+len(add))
	for _, tag := range tags {
		if !slices.Contains(drop, tag) {
			kept = append(kept, tag)
		}
	}
	for _, tag := range add {
		if !slices.Contains(kept, tag) {
			kept = append(kept, tag)
		}
	}
	if len(kept) == 0 {
		// nil rather than an empty slice: a node with no tags is one the
		// workspace file does not mention them for.
		return nil
	}
	return kept
}

// selectionHints are the status bar's contextual hints while a selection is
// gathered (§7.1). They replace the general ones rather than joining them: the
// keys that mean something different with several nodes gathered are the ones
// worth the width.
const selectionHints = "HJKL move · t tags · x kill · esc clear"
