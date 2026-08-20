package tui

// Filter (SPEC §7.1): `/` narrows the map to the cards that answer it, live as
// each character is typed. A filter is a way of looking at the map and not a
// change to it — nothing moves, nothing is written, and Esc puts every card
// back.
//
// A hidden card is hidden from the cursor too, not merely from the screen:
// selecting one would be attaching to, renaming, or killing a node with nothing
// on screen to say which.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// maxQueryLen bounds what the filter and the palette collect. Both are
// keyboard text, and neither is stored — but a query is drawn in the status
// bar, and a bound is cheaper than a bar that spends its width on a leant-on
// key.
const maxQueryLen = 48

// openFilter puts the prompt up on the filter already active, so `/` is a way
// of refining a narrowed map rather than a way of starting again.
func (m Model) openFilter() (tea.Model, tea.Cmd) {
	m.mode, m.input = modeFilter, m.filter
	return m, nil
}

// updateFilter narrows as it collects. Enter leaves the filter on with the
// prompt gone; Esc clears it — the two ways out of a prompt are the two things
// a user can mean by it.
func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.filter
	m, act := m.typed(msg, maxQueryLen)
	switch act {
	case actCancel:
		m.mode, m.input, m.filter = modeNormal, "", ""
	case actCommit:
		m.mode, m.input, m.filter = modeNormal, "", m.input
	default:
		m.filter = m.input
	}
	return m.refilter(before)
}

// clearFilter is Esc on the map: the only way back from a filter that has
// outlived its prompt.
func (m Model) clearFilter() (tea.Model, tea.Cmd) {
	if m.filter == "" {
		return m, nil
	}
	before := m.filter
	m.filter = ""
	return m.refilter(before)
}

// refilter settles the map on a filter that has just changed: the cursor onto a
// card that is still there, and a capture for the cards that have come back.
func (m Model) refilter(before string) (Model, tea.Cmd) {
	m = m.snapToMatch().pruneSelection()
	// The cards on screen with nothing on them — and, when the filter has
	// widened, every card on screen: hidden is off screen, and off screen is
	// where activity events are dropped, so a card that was hidden has been
	// unwatched for as long as it was. Appending to a filter can only hide
	// more of them, so narrowing costs nothing.
	stale := m.unpreviewed()
	if !strings.HasPrefix(m.filter, before) {
		stale = m.visible()
	}
	return m.markDirty(stale...)
}

// snapToMatch puts the cursor on a card the filter has left standing, when the
// one it was on has just gone. A cursor on a hidden card draws no cursor at all
// and leaves every key that acts on a node quietly doing nothing.
//
// The nearest match, so that narrowing the map moves the cursor as little as it
// can. It is not written to the workspace here: a filter is typed a character
// at a time, and the map is not worth two fsyncs a keystroke — the next motion
// saves it.
func (m Model) snapToMatch() Model {
	if m.filter == "" {
		// An empty cell is a perfectly good place for the cursor to be on a map
		// nobody is filtering.
		return m
	}
	if _, ok := m.selected(); ok {
		return m
	}
	shown := m.filtered()
	if len(shown) == 0 {
		return m
	}
	cursor, best := m.ws.Viewport.Cursor, shown[0]
	for _, n := range shown[1:] {
		near, far := cellDist(n.Pos, cursor), cellDist(best.Pos, cursor)
		// Two matches can be equally near on opposite sides of the cursor;
		// ordering them by cell keeps the choice the same on every run.
		if near < far || (near == far && earlier(n.Pos, best.Pos)) {
			best = n
		}
	}
	m.ws.Viewport.Cursor = best.Pos
	return m.follow()
}

// cellDist is how far apart two cells are, in steps of the grid.
func cellDist(a, b state.Cell) int { return abs(a.Col-b.Col) + abs(a.Row-b.Row) }

// filtered is the nodes the map is showing: all of them, or — while a filter is
// active — the ones that answer it. Everything that draws a card, captures one,
// or moves the cursor between them asks this rather than the workspace, which
// is what makes a filtered-out node absent rather than merely invisible.
func (m Model) filtered() []state.Node {
	if m.filter == "" {
		return m.ws.Nodes
	}
	kept := make([]state.Node, 0, len(m.ws.Nodes))
	for _, n := range m.ws.Nodes {
		if matchesNode(n, m.filter) {
			kept = append(kept, n)
		}
	}
	return kept
}

// matchesNode reports whether a node answers a filter: its title, its tags, or
// its kind (§7.1). Field by field rather than against the three run together,
// so a match cannot start in the title and finish in a tag — which would narrow
// the map to cards a user cannot see the reason for.
//
// The kind is matched both as it is stored and as a card labels it, because
// "note" and "sh" are both what the map calls a node.
func matchesNode(n state.Node, query string) bool {
	if fuzzy(query, n.Title) >= 0 || fuzzy(query, string(n.Kind)) >= 0 || fuzzy(query, kindLabel(n.Kind)) >= 0 {
		return true
	}
	for _, tag := range n.Tags {
		if fuzzy(query, tag) >= 0 {
			return true
		}
	}
	return false
}

// fuzzy scores pattern against text: the lower the tighter, 0 for a run of
// characters at the very start, and -1 for no match at all. A match is a
// subsequence — the pattern's characters in order but not necessarily together
// — which is what lets "asrv" find "api-server". Case is ignored: a query is
// typed in a hurry.
//
// The score is where the match starts plus everything it had to jump over, so
// the entry that spends the least text on the query sorts first.
//
// ponytail: leftmost-greedy, so it scores the first subsequence it finds rather
// than the best one. That costs a little ranking accuracy and saves a matrix;
// swap in the full scan if the ordering ever reads wrong.
func fuzzy(pattern, text string) int {
	if pattern == "" {
		return 0
	}
	want := []rune(strings.ToLower(pattern))
	at, score, last := 0, 0, -1
	for i, r := range []rune(strings.ToLower(text)) {
		if r != want[at] {
			continue
		}
		if last < 0 {
			score = i // how far in the match starts
		} else {
			score += i - last - 1 // and what it jumped over to carry on
		}
		last, at = i, at+1
		if at == len(want) {
			return score
		}
	}
	return -1
}
