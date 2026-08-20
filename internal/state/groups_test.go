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
