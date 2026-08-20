package state

// Finding a node by typing at it. The filter narrows the map with this, the
// palette ranks with it, and `trig attach` picks a node with it — one rule, so
// that what the map would have shown you is what the command line attaches to.

import "strings"

// Label is the map's own word for a kind, as a card's bottom border draws it.
// It lives beside the kind rather than in the renderer because it is
// vocabulary: `trig ls` and a card have to call the same node the same thing,
// and the filter matches on both this and the stored name.
func (k Kind) Label() string {
	switch k {
	case KindShell:
		return "sh"
	case KindAgent:
		return "ag"
	case KindNote:
		return "note"
	default:
		return string(k)
	}
}

// Match is the nodes that answer a query. An empty query is not a filter and
// keeps everything.
func (ws Workspace) Match(query string) []Node {
	if query == "" {
		return ws.Nodes
	}
	kept := make([]Node, 0, len(ws.Nodes))
	for _, n := range ws.Nodes {
		if Matches(n, query) {
			kept = append(kept, n)
		}
	}
	return kept
}

// Matches reports whether a node answers a query: its title, its tags, or its
// kind (§7.1). Field by field rather than against the three run together, so a
// match cannot start in the title and finish in a tag — which would narrow the
// map to cards a user cannot see the reason for.
//
// The kind is matched both as it is stored and as a card labels it, because
// "note" and "sh" are both what the map calls a node.
func Matches(n Node, query string) bool {
	if Fuzzy(query, n.Title) >= 0 || Fuzzy(query, string(n.Kind)) >= 0 || Fuzzy(query, n.Kind.Label()) >= 0 {
		return true
	}
	for _, tag := range n.Tags {
		if Fuzzy(query, tag) >= 0 {
			return true
		}
	}
	return false
}

// Fuzzy scores pattern against text: the lower the tighter, 0 for a run of
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
func Fuzzy(pattern, text string) int {
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
