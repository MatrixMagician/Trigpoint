package tui

import (
	"strconv"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// The map is an infinite cell grid, so every motion is one step in one of four
// directions — what changes is whether the step moves the cursor or the node
// under it. Which key asks for which is the keymap's business (keymap.go); this
// is what the four directions do.
//
// nodeMotions survives as a map because a held group reads it directly: HJKL
// move a rectangle rather than a card, and those keys are one context down from
// the map's own and are not remappable (§7.3).
var nodeMotions = map[string]state.Cell{
	"H": {Col: -1}, "L": {Col: 1}, "K": {Row: -1}, "J": {Row: 1},
}

// maxCountDigits bounds a count prefix. A node move repeats the collision rule
// once per step, so an accidental leant-on digit key must not be able to ask
// for a billion of them.
const maxCountDigits = 3

// savedIfMoved persists the map only when the motion actually went somewhere.
// h at the left edge of the map moves nothing, and nothing moved should not
// cost a write — and every motion that does move a node moves the cursor with
// it, so the viewport is enough to tell.
func (m Model) savedIfMoved(before state.Viewport) Model {
	if m.ws.Viewport == before {
		return m
	}
	return m.save()
}

func repeat(count string) int {
	n, err := strconv.Atoi(count)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// moveCursor steps the cursor from node to node, n times, stopping early when
// there is nothing further in that direction — a count is a request, not a
// promise that the map has that many nodes to offer.
func (m Model) moveCursor(d state.Cell, n int) Model {
	for i := 0; i < n; i++ {
		next, ok := m.nearest(d)
		if !ok {
			break
		}
		m.ws.Viewport.Cursor = next
		// Every node the cursor passes joins a selection being gathered, not
		// only the one a count lands on: 3l is three presses of l.
		m = m.extend()
	}
	return m.follow()
}

// nearest is the cell of the closest node in direction d. Sideways drift counts
// double, so pressing l along a row of nodes tracks the row instead of wandering
// off it at the first node that happens to be a cell nearer diagonally.
func (m Model) nearest(d state.Cell) (state.Cell, bool) {
	cursor := m.ws.Viewport.Cursor
	best, bestCost, found := state.Cell{}, 0, false
	for _, n := range m.filtered() {
		col, row := n.Pos.Col-cursor.Col, n.Pos.Row-cursor.Row
		along := col*d.Col + row*d.Row
		if along <= 0 {
			continue
		}
		cost := along + 2*(abs(col*d.Row)+abs(row*d.Col))
		// Two nodes can be equally near on opposite sides of the line of travel;
		// ordering them by cell keeps the choice the same on every run.
		if !found || cost < bestCost || (cost == bestCost && earlier(n.Pos, best)) {
			best, bestCost, found = n.Pos, cost, true
		}
	}
	return best, found
}

func earlier(a, b state.Cell) bool {
	if a.Row != b.Row {
		return a.Row < b.Row
	}
	return a.Col < b.Col
}

// moveNode walks the selection n cells, shoving whatever it meets. The cursor
// rides along: it is the node that was selected, not the cell, and letting the
// selection fall off after one press would make H a one-shot key.
//
// One node or several is the same code and the same collision rule: Shift takes
// the ids it is given, so a gathered selection moves as a unit — every member
// steps together, which is what keeps their relative positions — and shoves
// every bystander in its path (§7.3).
func (m Model) moveNode(d state.Cell, n int) Model {
	ids := m.targets()
	if len(ids) == 0 {
		return m
	}
	for i := 0; i < n; i++ {
		m.ws.Nodes = m.ws.Shift(ids, d)
		m.ws.Viewport.Cursor = state.Cell{
			Col: m.ws.Viewport.Cursor.Col + d.Col,
			Row: m.ws.Viewport.Cursor.Row + d.Row,
		}
	}
	return m.follow()
}

// follow scrolls the viewport by as little as it can to keep the cursor on
// screen, which is what makes the map feel infinite rather than paged.
func (m Model) follow() Model {
	cols, rows := m.viewCells()
	cursor := m.ws.Viewport.Cursor
	m.ws.Viewport.Offset = state.Cell{
		Col: clamp(m.ws.Viewport.Offset.Col, cursor.Col-cols+1, cursor.Col),
		Row: clamp(m.ws.Viewport.Offset.Row, cursor.Row-rows+1, cursor.Row),
	}
	return m
}

// centre puts the cursor in the middle of the viewport (zz).
func (m Model) centre() Model {
	cols, rows := m.viewCells()
	cursor := m.ws.Viewport.Cursor
	m.ws.Viewport.Offset = state.Cell{Col: cursor.Col - (cols-1)/2, Row: cursor.Row - (rows-1)/2}
	return m
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
