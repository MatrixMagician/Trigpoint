package main

// `trig ls` is the map, read rather than looked at (SPEC §11). Liveness is
// derived from tmux exactly as reconciliation derives it, and agent status is
// read through the same package the map view reads it with — see
// docs/adr/0017-the-cli-and-the-map-share-one-node-service.md.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// listing is the JSON shape, which is a contract with whatever is parsing it:
// fields are added, never renamed or dropped.
type listing struct {
	Workspace string     `json:"workspace"`
	Nodes     []jsonNode `json:"nodes"`
}

type jsonNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Live is null for a node with no session: a note is neither alive nor dead
	// (§6), and so is every node when tmux could not be asked at all. Absence of
	// an answer is not an answer — CONTEXT.md, "Agent status", says the same of
	// a badge.
	Live      *bool        `json:"live"`
	Session   string       `json:"session"`
	Dir       string       `json:"dir"`
	Cmd       string       `json:"cmd"`
	Tags      []string     `json:"tags"`
	Pos       state.Cell   `json:"pos"`
	CreatedAt time.Time    `json:"created_at"`
	Agent     *agentStatus `json:"agent"`
}

// agentStatus is an agent's own report. It carries no staleness mark, because
// it carries the stamp the mark is derived from — see status.Report.Stale.
type agentStatus struct {
	State  string    `json:"state"`
	Detail string    `json:"detail"`
	TS     time.Time `json:"ts"`
}

func runLs(args []string) error {
	fs := flag.NewFlagSet("trig ls", flag.ContinueOnError)
	workspace := fs.String("w", "", "workspace to list (default: the configured default workspace)")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `usage: trig ls [-w workspace] [--json]

Lists a workspace's nodes with their kind, their liveness, and whatever their
agent last said about itself.

flags:
`)
		fs.PrintDefaults()
	}
	if helped, err := parse(fs, args); helped || err != nil {
		return err
	}
	if fs.NArg() > 0 {
		// `trig ls main` is a natural guess once -w exists, and listing the
		// default workspace instead would answer a question nobody asked.
		fs.Usage()
		return fmt.Errorf("unexpected argument %q: a workspace is named with -w", fs.Arg(0))
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

	// A tmux that cannot be asked leaves liveness unknown rather than marking
	// every node dead: the map is what was asked for, and a listing that
	// condemns everything on the strength of not knowing is worse than one that
	// says it does not know.
	dead, known := map[string]bool{}, true
	if running, err := tmuxCLI().List(); err != nil {
		fmt.Fprintln(os.Stderr, "trig: liveness is unknown:", err)
		known = false
	} else {
		dead = ws.Dead(running)
	}

	reports, err := status.Read(status.DirBeside(stateDir), name)
	if err != nil {
		// Badges are not the point of a listing, so a status directory that
		// cannot be read costs the agent column and nothing else.
		fmt.Fprintln(os.Stderr, "trig:", err)
	}

	if *asJSON {
		return printJSON(ws, dead, known, reports)
	}
	printPlain(ws, dead, known, reports, time.Duration(cfg.General.StatusStaleAfterMin)*time.Minute)
	return nil
}

func printJSON(ws state.Workspace, dead map[string]bool, known bool, reports map[string]status.Report) error {
	out := listing{Workspace: ws.Name, Nodes: make([]jsonNode, 0, len(ws.Nodes))}
	for _, n := range ws.Nodes {
		node := jsonNode{
			ID: n.ID, Kind: string(n.Kind), Title: n.Title,
			Dir: n.Dir, Cmd: n.Cmd, Tags: tags(n), Pos: n.Pos, CreatedAt: n.CreatedAt,
		}
		if n.HasSession() {
			node.Session = ws.SessionOf(n)
			if known {
				live := !dead[n.ID]
				node.Live = &live
			}
		}
		if report, ok := reports[n.ID]; ok {
			node.Agent = &agentStatus{State: string(report.State), Detail: report.Detail, TS: report.TS}
		}
		out.Nodes = append(out.Nodes, node)
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the listing: %w", err)
	}
	fmt.Println(string(raw))
	return nil
}

func printPlain(ws state.Workspace, dead map[string]bool, known bool, reports map[string]status.Report, staleAfter time.Duration) {
	if len(ws.Nodes) == 0 {
		fmt.Printf("workspace %s is empty\n", ws.Name)
		return
	}
	// Written to a buffer rather than straight out, because the last column is
	// empty on most rows and tabwriter pads what it aligns: a listing piped
	// into anything else should not arrive with a tail of spaces on every line.
	var aligned bytes.Buffer
	w := tabwriter.NewWriter(&aligned, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tKIND\tSTATE\tTITLE\tAGENT")
	for _, n := range ws.Nodes {
		agent := ""
		if report, ok := reports[n.ID]; ok {
			agent = string(report.State)
			// Marked the way the map marks it: a report that has outlived its
			// window is what an agent last said rather than what it is doing,
			// and a listing that reads as current where the card reads as stale
			// is the one place a script would be trusted over the map.
			if report.Stale(staleAfter) {
				agent += " ?"
			}
			if report.Detail != "" {
				agent += "  " + report.Detail
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n.ID, n.Kind.Label(), liveness(ws, n, dead, known), n.Title, agent)
	}
	_ = w.Flush()
	for line := range strings.SplitSeq(strings.TrimRight(aligned.String(), "\n"), "\n") {
		fmt.Println(strings.TrimRight(line, " "))
	}
}

// tags is a node's tags as JSON, which is a list and never null: a script
// walking them should not have to check first.
func tags(n state.Node) []string {
	if n.Tags == nil {
		return []string{}
	}
	return n.Tags
}

// liveness is what the state column says. A node with no session has none to
// report, and so has every node when tmux could not be asked.
func liveness(ws state.Workspace, n state.Node, dead map[string]bool, known bool) string {
	switch {
	case !n.HasSession():
		return "-"
	case !known:
		return "?"
	case dead[n.ID]:
		return "dead"
	default:
		return "live"
	}
}
