package state

import "testing"

func TestFuzzyMatchesSubsequencesAndScoresTightestFirst(t *testing.T) {
	if Fuzzy("asrv", "api-server") < 0 {
		t.Error("a fuzzy match should take the pattern's characters in order, not together")
	}
	if Fuzzy("API", "api-server") < 0 {
		t.Error("a fuzzy match should ignore case")
	}
	if Fuzzy("zq", "api-server") >= 0 {
		t.Error("characters that are not there are not a match")
	}
	if Fuzzy("", "api-server") != 0 {
		t.Error("an empty pattern matches everything, perfectly")
	}
	if Fuzzy("api", "api-server") >= Fuzzy("api", "an api server") {
		t.Error("a tighter match should score lower than a scattered one")
	}
}

func TestAKindIsLabelledTheWayACardLabelsIt(t *testing.T) {
	// The label is the map's own word for a kind, so `trig ls` and a card say
	// the same thing about the same node.
	for kind, want := range map[Kind]string{KindShell: "sh", KindAgent: "ag", KindNote: "note"} {
		if got := kind.Label(); got != want {
			t.Errorf("%s.Label() = %q, want %q", kind, got, want)
		}
	}
}

func TestANodeMatchesOnItsTitleTagsAndKind(t *testing.T) {
	n := Node{Kind: KindAgent, Title: "api-server", Tags: []string{"infra"}}

	for _, query := range []string{"asrv", "infra", "agent", "ag"} {
		if !Matches(n, query) {
			t.Errorf("%q should match the node's title, tags, or kind", query)
		}
	}
	if Matches(n, "zqx") {
		t.Error("a query matching nothing about the node should not match it")
	}
}

func TestAMatchNeverStartsInOneFieldAndFinishesInAnother(t *testing.T) {
	// Field by field, so the map is never narrowed to cards whose reason for
	// being there cannot be seen.
	n := Node{Kind: KindShell, Title: "api", Tags: []string{"server"}}
	if Matches(n, "apiserver") {
		t.Error("a match should not run across the title and a tag")
	}
}

func TestMatchKeepsTheNodesThatAnswerTheQuery(t *testing.T) {
	ws := Workspace{Nodes: []Node{
		{ID: "aaaa", Title: "api-server"},
		{ID: "bbbb", Title: "notes"},
	}}

	got := ws.Match("asrv")
	if len(got) != 1 || got[0].ID != "aaaa" {
		t.Errorf("Match = %v, want only the node whose title answers it", got)
	}
	if len(ws.Match("")) != len(ws.Nodes) {
		t.Error("an empty query is not a filter and keeps every node")
	}
}
