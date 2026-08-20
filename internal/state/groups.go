package state

// Groups are spatial and nothing else: a group is a rectangle of the map, and a
// node is in it exactly when its cell falls inside. There is deliberately no
// membership list to disagree with the picture — see
// docs/adr/0001-groups-are-spatial.md — so everything here is geometry, and
// what is drawn is the whole of what is stored.

import "sort"

// Contains reports whether c falls inside r. Min is inside and Max is not, so
// two rects drawn edge to edge do not both claim the cells along the join.
func (r Rect) Contains(c Cell) bool {
	return c.Col >= r.Min.Col && c.Col < r.Max.Col &&
		c.Row >= r.Min.Row && c.Row < r.Max.Row
}

// Size is how many cells across and down r is. A rect whose Max is behind its
// Min — which only a hand-edited file can produce — is empty rather than
// negative.
func (r Rect) Size() (cols, rows int) {
	cols, rows = r.Max.Col-r.Min.Col, r.Max.Row-r.Min.Row
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	return cols, rows
}

// GroupAt is the group holding a cell, and is the only thing membership ever
// means. The first of them on a map whose rects overlap: a workspace file is a
// file, and two rects hand-edited across one another have to resolve somehow.
func (ws Workspace) GroupAt(c Cell) (Group, bool) {
	for _, g := range ws.Groups {
		if g.Rect.Contains(c) {
			return g, true
		}
	}
	return Group{}, false
}

// NewGroupID draws a short slug no group on this map is using. Group ids never
// reach tmux, but they are drawn from the same alphabet as a node's so that an
// id read off the map is an id whoever reads it can type back.
func (ws Workspace) NewGroupID() string {
	taken := make(map[string]bool, len(ws.Groups))
	for _, g := range ws.Groups {
		taken[g.ID] = true
	}
	return uniqueID(taken)
}

// Gather pulls the nodes named by ids together into the tightest block of cells
// it can find near where they already sit, and hands back the moved nodes with
// the rect that now holds them. It is what makes a group tight rather than a
// rectangle sprawling across everything that happened to lie between its
// members.
//
// The block is placed where no bystander stands, rather than shoving one out:
// a group that formed by displacing whatever was underneath it would move the
// map about far more than the gesture asked for.
func (ws Workspace) Gather(ids []string) ([]Node, Rect) {
	members := ws.pick(ids)
	if len(members) == 0 {
		return ws.Nodes, Rect{}
	}
	cols, rows := block(len(members))
	rect := ws.freeBlock(corner(members), cols, rows, ids)

	gathered := make([]Node, len(ws.Nodes))
	copy(gathered, ws.Nodes)
	index := make(map[string]int, len(gathered))
	for i, n := range gathered {
		index[n.ID] = i
	}
	// Reading order into reading order, so that a run gathered left to right
	// stays in the order it was gathered in.
	next := 0
	for row := rect.Min.Row; row < rect.Max.Row && next < len(members); row++ {
		for col := rect.Min.Col; col < rect.Max.Col && next < len(members); col++ {
			gathered[index[members[next].ID]].Pos = Cell{Col: col, Row: row}
			next++
		}
	}
	return gathered, rect
}

// Join moves the nodes named by ids into r, and grows r when no cell inside it
// is free. A node already inside is already a member, so joining it is nothing
// at all.
func (ws Workspace) Join(r Rect, ids []string) ([]Node, Rect) {
	for _, id := range ids {
		n, ok := ws.node(id)
		if !ok || r.Contains(n.Pos) {
			continue
		}
		cell, free := ws.freeCellIn(r)
		for !free {
			ws, r = ws.grow(r)
			cell, free = ws.freeCellIn(r)
		}
		ws.Nodes = ws.moved(id, cell)
	}
	return ws.Nodes, r
}

// grow adds a column or a row to r — whichever keeps it squarer — and shoves
// whoever is standing where it is about to reach out of the way first. The
// shove is the map's one collision rule (Shift): a rect that grew over a
// bystander would absorb it into the group, which is the one thing containment
// as membership must never do silently.
func (ws Workspace) grow(r Rect) (Workspace, Rect) {
	cols, rows := r.Size()
	d := Cell{Col: 1}
	strip := Rect{Min: Cell{Col: r.Max.Col, Row: r.Min.Row}, Max: Cell{Col: r.Max.Col + 1, Row: r.Max.Row}}
	if cols > rows {
		d = Cell{Row: 1}
		strip = Rect{Min: Cell{Col: r.Min.Col, Row: r.Max.Row}, Max: Cell{Col: r.Max.Col, Row: r.Max.Row + 1}}
	}
	var inTheWay []string
	for _, n := range ws.Nodes {
		if strip.Contains(n.Pos) {
			inTheWay = append(inTheWay, n.ID)
		}
	}
	if len(inTheWay) > 0 {
		ws.Nodes = ws.Shift(inTheWay, d)
	}
	r.Max = Cell{Col: r.Max.Col + d.Col, Row: r.Max.Row + d.Row}
	return ws, r
}

// freeCellIn is the first cell of r no node stands on, in reading order.
func (ws Workspace) freeCellIn(r Rect) (Cell, bool) {
	occupied := make(map[Cell]bool, len(ws.Nodes))
	for _, n := range ws.Nodes {
		occupied[n.Pos] = true
	}
	for row := r.Min.Row; row < r.Max.Row; row++ {
		for col := r.Min.Col; col < r.Max.Col; col++ {
			if cell := (Cell{Col: col, Row: row}); !occupied[cell] {
				return cell, true
			}
		}
	}
	return Cell{}, false
}

// freeBlock is the nearest cols×rows block to anchor holding nobody but the
// nodes named by ids, searched outward ring by ring the way a new node finds
// its cell. The map is infinite and the nodes on it are finite, so a big enough
// ring is always clear.
//
// ponytail: rescans every node per candidate, so it is O(nodes × cells
// searched); the map is a thing a person navigates by hand, and the search
// stops at the first clear block.
func (ws Workspace) freeBlock(anchor Cell, cols, rows int, ids []string) Rect {
	member := make(map[string]bool, len(ids))
	for _, id := range ids {
		member[id] = true
	}
	for radius := 0; ; radius++ {
		for _, c := range ring(anchor, radius) {
			r := Rect{Min: c, Max: Cell{Col: c.Col + cols, Row: c.Row + rows}}
			if ws.clearOf(r, member) {
				return r
			}
		}
	}
}

// clearOf reports whether r holds nobody but the nodes named.
func (ws Workspace) clearOf(r Rect, member map[string]bool) bool {
	for _, n := range ws.Nodes {
		if !member[n.ID] && r.Contains(n.Pos) {
			return false
		}
	}
	return true
}

// block is the shape a count of nodes gathers into: as square as the count
// allows, and wider than it is tall, because a terminal is.
func block(n int) (cols, rows int) {
	cols = 1
	for cols*cols < n {
		cols++
	}
	return cols, (n + cols - 1) / cols
}

// corner is the top-left of the box the nodes already occupy, which is where
// the gathered block is looked for first — a gather that landed somewhere else
// would be a jump as well as a gather.
func corner(nodes []Node) Cell {
	at := nodes[0].Pos
	for _, n := range nodes[1:] {
		at.Col, at.Row = minInt(at.Col, n.Pos.Col), minInt(at.Row, n.Pos.Row)
	}
	return at
}

// pick is the nodes named by ids, in reading order.
func (ws Workspace) pick(ids []string) []Node {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	var picked []Node
	for _, n := range ws.Nodes {
		if wanted[n.ID] {
			picked = append(picked, n)
		}
	}
	sort.Slice(picked, func(i, j int) bool {
		if picked[i].Pos.Row != picked[j].Pos.Row {
			return picked[i].Pos.Row < picked[j].Pos.Row
		}
		return picked[i].Pos.Col < picked[j].Pos.Col
	})
	return picked
}

func (ws Workspace) node(id string) (Node, bool) {
	for _, n := range ws.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// moved returns the nodes with one of them somewhere else, in a fresh slice:
// callers hold Workspace by value all over the app, and writing in place would
// edit the map every older copy is holding.
func (ws Workspace) moved(id string, to Cell) []Node {
	nodes := make([]Node, len(ws.Nodes))
	copy(nodes, ws.Nodes)
	for i := range nodes {
		if nodes[i].ID == id {
			nodes[i].Pos = to
		}
	}
	return nodes
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
