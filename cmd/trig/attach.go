package main

// `trig attach` is Enter on a card, with no map to press it on (SPEC §11). The
// handoff is the map's own (§5.1): the whole terminal goes to the session, and
// the detach binding installed for the duration hands it back.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

func runAttach(args []string) error {
	fs := flag.NewFlagSet("trig attach", flag.ContinueOnError)
	workspace := fs.String("w", "", "workspace the node is on (default: the configured default workspace)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: trig attach [-w workspace] <node>

Hands the terminal to a node's session without opening the map. The node is
named by its id, or by its title — exactly, or by any run of characters in it.
A name that fits more than one node is refused rather than guessed at.
`)
		fs.PrintDefaults()
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}
	// An empty name is refused with a missing one: it is a subsequence of every
	// title there is, so it would attach to whatever a one-node map happened to
	// hold rather than to anything that was asked for.
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fs.Usage()
		return errors.New("say which node to attach to")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	name := cfg.General.DefaultWorkspace
	if *workspace != "" {
		name = *workspace
	}
	if err := state.ValidName(name); err != nil {
		return err
	}
	stateDir, err := state.Dir()
	if err != nil {
		return err
	}
	ws, err := state.Load(stateDir, name)
	if err != nil {
		return err
	}

	node, err := pick(ws, fs.Arg(0))
	if err != nil {
		return err
	}
	if !node.HasSession() {
		return fmt.Errorf("%q is a note: it holds a body rather than a session, so there is nothing to attach to", node.Title)
	}

	sessions := tmuxCLI()
	session := ws.SessionOf(node)
	// tmux is asked rather than assumed, for the reason the map asks: handing
	// the terminal to a session that is not there would leave the user staring
	// at tmux's own complaint with no way to tell which node it was about.
	alive, err := sessions.Exists(session)
	if err != nil {
		return err
	}
	if !alive {
		return fmt.Errorf("%q has no session left: open the map to start it again", node.Title)
	}

	handoff, release, err := sessions.Handoff(session, cfg.General.DetachKey)
	if err != nil {
		// Attaching with no way back would leave the user inside the session
		// with nothing to return to, so a handoff that cannot be prepared
		// attaches nothing and says why.
		return err
	}
	handoff.Stdin, handoff.Stdout, handoff.Stderr = os.Stdin, os.Stdout, os.Stderr
	attached := handoff.Run()
	// The binding comes out however the attach went: left installed, the detach
	// key would fire on a terminal that has nothing to detach.
	if err := release(); err != nil && attached == nil {
		return err
	}
	return attached
}

// pick is which node a name means. An id, then an exact title, then any run of
// the title's characters — narrowest first, so that a name that is precisely
// right is never made ambiguous by a node it happens to be a subsequence of.
//
// The title alone, where the map's own filter also matches tags and kinds: a
// tag names several nodes by design, and "sh" would be an ambiguous way of
// asking for every shell node on the map.
func pick(ws state.Workspace, query string) (state.Node, error) {
	for _, n := range ws.Nodes {
		if n.ID == query {
			return n, nil
		}
	}

	var exact, fuzzy []state.Node
	for _, n := range ws.Nodes {
		switch {
		case strings.EqualFold(n.Title, query):
			exact = append(exact, n)
		case state.Fuzzy(query, n.Title) >= 0:
			fuzzy = append(fuzzy, n)
		}
	}
	for _, found := range [][]state.Node{exact, fuzzy} {
		if len(found) == 1 {
			return found[0], nil
		}
		if len(found) > 1 {
			return state.Node{}, fmt.Errorf("%q names %d nodes: %s — say which by its id, or by its whole title",
				query, len(found), strings.Join(titles(found), ", "))
		}
	}
	return state.Node{}, fmt.Errorf("no node on workspace %s is called %q", ws.Name, query)
}

func titles(nodes []state.Node) []string {
	named := make([]string, len(nodes))
	for i, n := range nodes {
		named[i] = fmt.Sprintf("%s (%s)", n.Title, n.ID)
	}
	return named
}
