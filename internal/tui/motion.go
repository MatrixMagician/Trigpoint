package tui

import (
	"strconv"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// The map is an infinite cell grid, so every motion is one step in one of four
// directions — what changes is whether the step moves the cursor or the node
// under it.
var (
	cursorMotions = map[string]state.Cell{
		"h": {Col: -1}, "left": {Col: -1},
		"l": {Col: 1}, "right": {Col: 1},
		"k": {Row: -1}, "up": {Row: -1},
		"j": {Row: 1}, "down": {Row: 1},
	}
	nodeMotions = map[string]state.Cell{
		"H": {Col: -1}, "L": {Col: 1}, "K": {Row: -1}, "J": {Row: 1},
	}
)

// maxCountDigits bounds a count prefix. A node move repeats the collision rule
// once per step, so an accidental leant-on digit key must not be able to ask
// for a billion of them.
const maxCountDigits = 3

// motion handles the keys that move the cursor, the selected node, or the
// viewport, and reports whether it took the key. Count prefixes and the z of zz
// are consumed here too, so that pressing 3 and then anything else forgets the
// 3 rather than applying it to whatever came next.
func (m Model) motion(key string) (Model, bool) {
	count, awaitZ := m.count, m.awaitZ
	m.count, m.awaitZ = "", false
	was := m.ws.Viewport

	switch {
	case len(key) == 1 && key[0] >= '1' && key[0] <= '9',
		key == "0" && count != "":
		if len(count) < maxCountDigits {
			m.count = count + key
		} else {
			m.count = count // an over-long count keeps what it had rather than resetting
		}
		return m, true

	case key == "0":
		// Origin, centred: jumping somewhere and landing in the corner of the
		// screen hides the half of the map you jumped towards.
		m.ws.Viewport.Cursor = state.Cell{}
		return m.centre().savedIfMoved(was), true

	case key == "z":
		if !awaitZ {
			m.awaitZ = true
			return m, true
		}
		return m.centre().savedIfMoved(was), true
	}

	if d, ok := cursorMotions[key]; ok {
		return m.moveCursor(d, repeat(count)).savedIfMoved(was), true
	}
	if d, ok := nodeMotions[key]; ok {
		return m.moveNode(d, repeat(count)).savedIfMoved(was), true
	}
	return m, false
}

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

// moveNode walks the selected node n cells, shoving whatever it meets. The
// cursor rides along: it is the node that was selected, not the cell, and
// letting the selection fall off after one press would make H a one-shot key.
func (m Model) moveNode(d state.Cell, n int) Model {
	node, ok := m.selected()
	if !ok {
		return m
	}
	for i := 0; i < n; i++ {
		m.ws.Nodes = m.ws.Shift([]string{node.ID}, d)
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
