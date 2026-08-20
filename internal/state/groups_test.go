package state

import "testing"

func TestRectHoldsItsMinAndNotItsMax(t *testing.T) {
	r := Rect{Min: Cell{Col: 1, Row: 1}, Max: Cell{Col: 3, Row: 3}}
	for _, c := range []Cell{{Col: 1, Row: 1}, {Col: 2, Row: 2}} {
		if !r.Contains(c) {
			t.Errorf("%+v should be inside %+v", c, r)
		}
	}
	for _, c := range []Cell{{Col: 0, Row: 1}, {Col: 3, Row: 2}, {Col: 1, Row: 3}} {
		if r.Contains(c) {
			t.Errorf("%+v should be outside %+v", c, r)
		}
	}
}

func TestGroupAtIsMembershipByContainment(t *testing.T) {
	ws := Workspace{Groups: []Group{{ID: "g1", Title: "api", Rect: Rect{Max: Cell{Col: 2, Row: 2}}}}}
	if g, ok := ws.GroupAt(Cell{Col: 1, Row: 1}); !ok || g.ID != "g1" {
		t.Errorf("a cell inside the rect is in the group, got %+v %v", g, ok)
	}
	if _, ok := ws.GroupAt(Cell{Col: 2, Row: 2}); ok {
		t.Error("a cell outside the rect is in no group")
	}
}

func TestGatherPullsScatteredNodesIntoATightBlock(t *testing.T) {
	ws := Workspace{Nodes: []Node{
		{ID: "a"},
		{ID: "b", Pos: Cell{Col: 9, Row: 4}},
		{ID: "c", Pos: Cell{Col: 3, Row: 8}},
	}}
	nodes, rect := ws.Gather([]string{"a", "b", "c"})

	cols, rows := rect.Size()
	if cols*rows > 4 {
		t.Errorf("three nodes should gather into a 2x2 block, got %dx%d", cols, rows)
	}
	for _, n := range nodes {
		if !rect.Contains(n.Pos) {
			t.Errorf("node %s at %+v was left outside the group rect %+v", n.ID, n.Pos, rect)
		}
	}
}

func TestGatherLeavesEveryoneElseWhereTheyAre(t *testing.T) {
	ws := Workspace{Nodes: []Node{
		{ID: "a"},
		{ID: "b", Pos: Cell{Col: 4}},
		{ID: "x", Pos: Cell{Col: 1}},
		{ID: "y", Pos: Cell{Col: 2}},
		{ID: "z", Pos: Cell{Col: 3}},
	}}
	nodes, rect := ws.Gather([]string{"a", "b"})

	for _, n := range nodes {
		if n.ID != "a" && n.ID != "b" {
			if before, _ := ws.node(n.ID); before.Pos != n.Pos {
				t.Errorf("bystander %s moved from %+v to %+v", n.ID, before.Pos, n.Pos)
			}
			if rect.Contains(n.Pos) {
				t.Errorf("bystander %s at %+v was swallowed by the rect %+v", n.ID, n.Pos, rect)
			}
		}
	}
}

func TestGatherIsIdempotentOnAlreadyTightNodes(t *testing.T) {
	ws := Workspace{Nodes: []Node{{ID: "a"}, {ID: "b", Pos: Cell{Col: 1}}}}
	_, rect := ws.Gather([]string{"a", "b"})
	if cols, rows := rect.Size(); cols*rows != 2 {
		t.Errorf("two adjacent nodes should already be a tight block, got %dx%d", cols, rows)
	}
}

func TestJoinMovesANodeIntoAFreeCellInside(t *testing.T) {
	rect := Rect{Max: Cell{Col: 2, Row: 2}}
	ws := Workspace{Nodes: []Node{
		{ID: "a"},
		{ID: "b", Pos: Cell{Col: 1}},
		{ID: "j", Pos: Cell{Col: 8, Row: 8}},
	}}
	nodes, grown := ws.Join(rect, []string{"j"})

	if grown != rect {
		t.Errorf("the rect had a free cell, so it should not have grown: %+v", grown)
	}
	joined, _ := Workspace{Nodes: nodes}.node("j")
	if !rect.Contains(joined.Pos) {
		t.Errorf("j at %+v is not inside %+v", joined.Pos, rect)
	}
}

func TestJoinGrowsTheRectWhenNoCellInsideIsFree(t *testing.T) {
	rect := Rect{Max: Cell{Col: 2, Row: 1}}
	ws := Workspace{Nodes: []Node{
		{ID: "a"},
		{ID: "b", Pos: Cell{Col: 1}},
		{ID: "j", Pos: Cell{Col: 8, Row: 8}},
	}}
	nodes, grown := ws.Join(rect, []string{"j"})

	if grown == rect {
		t.Fatalf("the rect was full, so it should have grown: %+v", grown)
	}
	after := Workspace{Nodes: nodes}
	for _, id := range []string{"a", "b", "j"} {
		n, _ := after.node(id)
		if !grown.Contains(n.Pos) {
			t.Errorf("%s at %+v is outside the grown rect %+v", id, n.Pos, grown)
		}
	}
}

func TestGrowingShovesTheBystanderInItsWayRatherThanSwallowingIt(t *testing.T) {
	rect := Rect{Max: Cell{Col: 2, Row: 1}}
	ws := Workspace{Nodes: []Node{
		{ID: "a"},
		{ID: "b", Pos: Cell{Col: 1}},
		// Exactly where the rect grows to, in the row it grows along.
		{ID: "x", Pos: Cell{Col: 1, Row: 1}},
		{ID: "j", Pos: Cell{Col: 8, Row: 8}},
	}}
	nodes, grown := ws.Join(rect, []string{"j"})

	after := Workspace{Nodes: nodes}
	x, _ := after.node("x")
	if grown.Contains(x.Pos) {
		t.Errorf("bystander x at %+v was swallowed by the grown rect %+v", x.Pos, grown)
	}
	if x.Pos == (Cell{Col: 1, Row: 1}) {
		t.Error("bystander x should have been shoved out of the way")
	}
	for _, n := range nodes {
		for _, o := range nodes {
			if n.ID != o.ID && n.Pos == o.Pos {
				t.Fatalf("%s and %s are stacked on %+v", n.ID, o.ID, n.Pos)
			}
		}
	}
}

func TestJoinLeavesANodeAlreadyInsideAlone(t *testing.T) {
	rect := Rect{Max: Cell{Col: 2, Row: 2}}
	ws := Workspace{Nodes: []Node{{ID: "a", Pos: Cell{Col: 1, Row: 1}}}}
	nodes, grown := ws.Join(rect, []string{"a"})

	if grown != rect {
		t.Errorf("nothing joined, so nothing should have grown: %+v", grown)
	}
	if nodes[0].Pos != (Cell{Col: 1, Row: 1}) {
		t.Errorf("a node already in the group moved to %+v", nodes[0].Pos)
	}
}

func TestNewGroupIDIsNotOneAlreadyTaken(t *testing.T) {
	ws := Workspace{Groups: []Group{{ID: "g1"}, {ID: "g2"}}}
	for i := 0; i < 50; i++ {
		if id := ws.NewGroupID(); id == "g1" || id == "g2" {
			t.Fatalf("NewGroupID handed out %q, which is already in use", id)
		}
	}
}

func TestGatherKeepsClearOfAGroupAlreadyDrawn(t *testing.T) {
	ws := Workspace{
		Nodes:  []Node{{ID: "a", Pos: Cell{Col: 5, Row: 5}}, {ID: "b", Pos: Cell{Col: 6, Row: 5}}},
		Groups: []Group{{ID: "g1", Rect: Rect{Max: Cell{Col: 10, Row: 10}}}},
	}
	_, rect := ws.Gather([]string{"a", "b"})
	if rect.Overlaps(ws.Groups[0].Rect) {
		t.Errorf("the gathered rect %+v shares cells with %+v", rect, ws.Groups[0].Rect)
	}
}

func TestGrowingTurnsAsideFromAnotherGroup(t *testing.T) {
	// Full, and squarest by growing right — but another group is drawn in the
	// column it would grow into, so it has to go down instead.
	rect := Rect{Max: Cell{Col: 1, Row: 1}}
	ws := Workspace{
		Nodes: []Node{{ID: "a"}, {ID: "j", Pos: Cell{Col: 8, Row: 8}}},
		Groups: []Group{
			{ID: "g1", Rect: rect},
			{ID: "g2", Rect: Rect{Min: Cell{Col: 1}, Max: Cell{Col: 3, Row: 1}}},
		},
	}
	_, grown := ws.Join(rect, []string{"j"})

	if grown.Overlaps(ws.Groups[1].Rect) {
		t.Errorf("the grown rect %+v reaches into the group at %+v", grown, ws.Groups[1].Rect)
	}
}

// Rigid group movement (SPEC §6, docs/adr/0013-a-group-is-held-with-V.md): the
// rect and the nodes it held move as one, and whatever the rect is about to
// reach over is shoved out from under it first.

func heldMap() Workspace {
	return Workspace{
		Nodes: []Node{
			{ID: "a"},
			{ID: "b", Pos: Cell{Col: 1, Row: 1}},
			{ID: "x", Pos: Cell{Col: 3, Row: 1}},
		},
		Groups: []Group{{ID: "g1", Title: "infra", Rect: Rect{Max: Cell{Col: 3, Row: 3}}}},
	}
}

func TestMovingAGroupCarriesItsMembersAtFixedOffsets(t *testing.T) {
	ws := heldMap()
	nodes, rect, ok := ws.MoveGroup("g1", []string{"a", "b"}, Cell{Col: 1})
	if !ok {
		t.Fatal("nothing is in the way, so the move should have happened")
	}
	if want := (Rect{Min: Cell{Col: 1}, Max: Cell{Col: 4, Row: 3}}); rect != want {
		t.Errorf("the rect is %+v, want %+v", rect, want)
	}
	ws.Nodes = nodes
	for id, want := range map[string]Cell{"a": {Col: 1}, "b": {Col: 2, Row: 1}} {
		if n, _ := ws.node(id); n.Pos != want {
			t.Errorf("%s is at %+v, want %+v — members keep their offsets", id, n.Pos, want)
		}
	}
}

func TestAMovingGroupShovesTheBystanderItWouldOtherwiseSwallow(t *testing.T) {
	ws := heldMap()
	// x stands in an empty cell of the column the rect is about to reach into,
	// so no member is walking at it: only the rect is.
	nodes, rect, ok := ws.MoveGroup("g1", []string{"a", "b"}, Cell{Col: 1})
	if !ok {
		t.Fatal("a bystander is shoved, not refused")
	}
	ws.Nodes = nodes
	n, _ := ws.node("x")
	if n.Pos == (Cell{Col: 3, Row: 1}) {
		t.Errorf("x was left standing at %+v while the rect moved over it", n.Pos)
	}
	if rect.Contains(n.Pos) {
		t.Errorf("x at %+v was absorbed by the rect %+v", n.Pos, rect)
	}
}

func TestAGroupNeverAbsorbsABystanderHoweverFarItGoes(t *testing.T) {
	ws := heldMap()
	for i := 0; i < 5; i++ {
		nodes, rect, ok := ws.MoveGroup("g1", []string{"a", "b"}, Cell{Col: 1})
		if !ok {
			t.Fatalf("step %d was refused with nothing but a node in the way", i)
		}
		ws.Nodes, ws.Groups[0].Rect = nodes, rect
		if n, _ := ws.node("x"); rect.Contains(n.Pos) {
			t.Fatalf("after %d steps x at %+v is inside %+v", i+1, n.Pos, rect)
		}
	}
}

func TestAGroupStopsAtAnotherGroupRatherThanOverlappingIt(t *testing.T) {
	ws := heldMap()
	ws.Groups = append(ws.Groups, Group{ID: "g2", Title: "web",
		Rect: Rect{Min: Cell{Col: 3}, Max: Cell{Col: 5, Row: 3}}})

	nodes, _, ok := ws.MoveGroup("g1", []string{"a", "b"}, Cell{Col: 1})
	if ok {
		t.Fatal("moving onto another group would hand its nodes to whichever rect was drawn first")
	}
	if nodes != nil {
		t.Error("a refused move should leave the map alone")
	}
}

func TestGrowingAGroupShovesWhoeverStandsInTheNewColumn(t *testing.T) {
	ws := heldMap()
	ws.Nodes[2].Pos = Cell{Col: 3} // x in the column the rect is about to take
	nodes, rect, ok := ws.ResizeGroup("g1", Cell{Col: 1})
	if !ok {
		t.Fatal("a node in the way is shoved, not a refusal")
	}
	ws.Nodes = nodes
	if n, _ := ws.node("x"); rect.Contains(n.Pos) {
		t.Errorf("x at %+v was absorbed by the grown rect %+v", n.Pos, rect)
	}
}

func TestShrinkingAGroupPastANodeDropsItWithoutMovingIt(t *testing.T) {
	ws := heldMap()
	nodes, rect, ok := ws.ResizeGroup("g1", Cell{Col: -1})
	if !ok {
		t.Fatal("a rect with cells left in it can always shrink")
	}
	ws.Nodes, ws.Groups[0].Rect = nodes, rect
	if n, _ := ws.node("b"); n.Pos != (Cell{Col: 1, Row: 1}) {
		t.Errorf("b moved to %+v; shrinking drops a node, it does not push one", n.Pos)
	}
	if _, ok := ws.GroupAt(Cell{Col: 2, Row: 1}); ok {
		t.Error("the cells the rect gave up are in no group")
	}
}

func TestAGroupNeverShrinksToNoCellsAtAll(t *testing.T) {
	ws := Workspace{Groups: []Group{{ID: "g1", Rect: Rect{Max: Cell{Col: 1, Row: 1}}}}}
	if _, _, ok := ws.ResizeGroup("g1", Cell{Col: -1}); ok {
		t.Error("a rect with no cells in it is a group nothing can ever be in")
	}
}

func TestAGroupDoesNotGrowIntoAnotherGroup(t *testing.T) {
	ws := heldMap()
	ws.Groups = append(ws.Groups, Group{ID: "g2",
		Rect: Rect{Min: Cell{Col: 3}, Max: Cell{Col: 5, Row: 3}}})
	if _, _, ok := ws.ResizeGroup("g1", Cell{Col: 1}); ok {
		t.Error("growing over another rect would draw one group inside another")
	}
}

func TestMembersAreTheNodesInsideTheRect(t *testing.T) {
	ws := heldMap()
	got := ws.Members(ws.Groups[0].Rect)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("the members are %v, want a and b — x sits outside", got)
	}
}
