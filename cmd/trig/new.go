package main

// `trig new` is `n` on the map, with no map open (SPEC §11). It decides the
// node's identity and cell exactly as the map view does, starts the session the
// same way, and writes the same workspace file — see
// docs/adr/0017-the-cli-and-the-map-share-one-node-service.md.

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// kinds is what -k accepts, in the order the usage line lists them.
var kinds = []state.Kind{state.KindShell, state.KindAgent, state.KindNote}

func runNew(args []string) error {
	fs := flag.NewFlagSet("trig new", flag.ContinueOnError)
	workspace := fs.String("w", "", "workspace to create the node in (default: the configured default workspace)")
	kind := fs.String("k", string(state.KindShell), "kind of node: "+kindsList())
	title := fs.String("t", "", "the node's title (default: the command's name, or the node's id)")
	command := fs.String("cmd", "", "the command the node runs")
	dir := fs.String("d", "", "the working directory the session starts in (default: the workspace's)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: trig new [-w workspace] [-k %s] [-t title] [--cmd command] [-d dir]

Creates a node and starts the session behind it, without opening the map. The
node is on the map the next time it is opened.

flags:
`, strings.Join(kindNames(), "|"))
		fs.PrintDefaults()
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected argument %q: a title is given with -t", fs.Arg(0))
	}

	nodeKind, err := parseKind(*kind)
	if err != nil {
		return err
	}
	if err := checkText("title", *title, state.MaxTitleLen); err != nil {
		return err
	}
	if err := checkText("command", *command, state.MaxCmdLen); err != nil {
		return err
	}
	// The kinds disagree about the command, so each is asked here rather than
	// discovered later by a session that does something surprising.
	switch {
	case nodeKind == state.KindNote && *command != "":
		return errors.New("a note has no session, so there is nothing for it to run: drop --cmd, or make an agent node with -k agent")
	case nodeKind == state.KindAgent && *command == "":
		return errors.New("an agent node is a node started on a command: say which with --cmd")
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
	// ponytail: a map view open on this workspace holds the whole thing in
	// memory and writes all of it, so it overwrites this node at its next save.
	// The acceptance criterion is the headless case; the fix, if it is ever
	// wanted, is for the map view to reload on a change to the file.
	ws, err := state.Load(stateDir, name)
	if err != nil {
		return err
	}

	node := state.Node{
		ID:    ws.NewNodeID(),
		Kind:  nodeKind,
		Title: strings.TrimSpace(*title),
		Cmd:   *command,
		Dir:   *dir,
		// The cell the map view would have chosen: the nearest free one to
		// where the cursor was left. One node per cell is the map's only layout
		// rule, and it holds whoever placed the node.
		Pos:       ws.NearestFreeCell(ws.Viewport.Cursor),
		CreatedAt: time.Now(),
	}
	if node.Title == "" {
		// Named after what it runs, as the map's own prompt suggests, and after
		// its id when it runs nothing — a card has to say something. Clamped
		// rather than refused: a command may be longer than a title, and this
		// is a default the map would have offered, not something anybody typed
		// into the title.
		node.Title = state.ClampRunes(firstField(*command), state.MaxTitleLen)
	}
	if node.Title == "" {
		node.Title = node.ID
	}

	// The session first: a node reaches the map only once tmux has confirmed
	// the session behind it (§5.2), so a tmux that refuses leaves nothing
	// written rather than a card over nothing.
	if node.HasSession() {
		session := ws.SessionOf(node)
		env := state.Provenance(name, node, status.DirBeside(stateDir))
		if err := tmuxCLI().Create(session, ws.DirOf(node), node.StartCmd(), env); err != nil {
			return err
		}
	}

	ws.Nodes = append(ws.Nodes, node)
	if err := state.Save(stateDir, ws); err != nil {
		return err
	}

	fmt.Printf("created %s: %s %q in workspace %s at cell (%d, %d)\n",
		node.ID, node.Kind, node.Title, name, node.Pos.Col, node.Pos.Row)
	if node.HasSession() {
		fmt.Printf("session %s\n", ws.SessionOf(node))
	}
	return nil
}

func parseKind(s string) (state.Kind, error) {
	for _, k := range kinds {
		if state.Kind(s) == k {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown kind %q: a node is one of %s", s, kindsList())
}

func kindsList() string { return strings.Join(kindNames(), ", ") }

func kindNames() []string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	return names
}

// checkText is what a hand-typed field has to survive to be stored: no control
// characters, and no more than the map holds. Refused rather than trimmed — a
// truncated title is a title, but a truncated command is a different command,
// and a script that had one silently cut would never know.
func checkText(what, s string, max int) error {
	if n := len([]rune(s)); n > max {
		return fmt.Errorf("that %s is too long: %d characters is the most a node holds, and this is %d", what, max, n)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("that %s contains a control character, which a card cannot draw", what)
		}
	}
	return nil
}

func firstField(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
}
