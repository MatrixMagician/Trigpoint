package state

// How a node maps onto the session behind it. The rules live here rather than
// in the map view because the map view is not their only client: `trig new`
// starts a node with no map open, and `trig ls` derives liveness without one —
// see docs/adr/0017-the-cli-and-the-map-share-one-node-service.md.
//
// Everything here is a pure function of a node and its workspace, apart from
// the one directory Provenance makes. tmux is reached into only for the session
// name, which is the one string both sides have to agree on.

import (
	"os"
	"strings"

	"github.com/MatrixMagician/Trigpoint/internal/status"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// SessionOf is the tmux session behind a node: the one Trigpoint names after
// the node, or — for an adopted node — the foreign session's own name, which
// adoption stores rather than imposes a prefix on (§9.3).
func (ws Workspace) SessionOf(n Node) string {
	if n.Adopted() {
		return n.Session
	}
	return tmux.SessionName(ws.Name, n.ID)
}

// DirOf is where a node's session starts: its own working directory, or the
// workspace's when it has none of its own.
func (ws Workspace) DirOf(n Node) string {
	if n.Dir != "" {
		return n.Dir
	}
	return ws.Dir
}

// StartCmd is what tmux is actually given for a node's session. For an agent
// node it is not the command: tmux ends a session whose command exits, and an
// agent node whose agent has exited is still an agent node with a shell in it
// (§6), so the command hands the pane over to a shell instead of being the last
// thing in it. Every other kind is given its command as it stands — only an
// agent node promises a shell behind whatever it was started with. See
// docs/adr/0014-an-agent-node-hands-its-pane-to-a-shell.md.
//
// The whole thing goes to tmux as one word, so the command is quoted rather
// than interpolated — it is typed by hand, and a quote in it must not be able
// to close the word and turn the rest into commands of tmux's own.
func (n Node) StartCmd() string {
	if n.Cmd == "" || n.Kind != KindAgent {
		return n.Cmd
	}
	return "sh -c " + shellQuote(n.Cmd+`; exec "${SHELL:-/bin/sh}"`)
}

// shellQuote wraps s in single quotes, ending and reopening them around any
// quote of its own — the one escaping a single-quoted shell word allows.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Provenance is what a session carries about the node behind it (§5.2). It is
// what reconciliation reads back when the workspace file has been lost, so a
// session started by the map and a session started by `trig new` have to say
// the same thing.
func Provenance(workspace string, n Node, statusDir string) map[string]string {
	env := map[string]string{
		"TRIG_WORKSPACE": workspace,
		"TRIG_NODE_ID":   n.ID,
		"TRIG_NODE_KIND": string(n.Kind),
	}
	// Where the agent reports what it is doing (§8). Only an agent node gets
	// one: it is the only kind that reports agent status (CONTEXT.md, "Agent
	// node"), and handing a shell node a status file would be inviting a badge
	// onto a card that has no agent to earn it.
	if n.Kind == KindAgent && statusDir != "" {
		// The directory is made here so that an agent writing the file itself —
		// the documented contract, and not everything has hooks that can shell
		// out — does not have to create it first. Best effort: emit-status
		// creates it too, and a status directory that cannot be made is a map
		// without badges rather than a node that refuses to start.
		_ = os.MkdirAll(statusDir, 0o700)
		env["TRIG_STATUS_FILE"] = status.Path(statusDir, workspace, n.ID)
	}
	return env
}

// Dead is which of the workspace's nodes have no session among the ones tmux
// reports running. Liveness is derived and never written to the workspace file
// — see docs/adr/0006-liveness-is-derived-not-stored.md.
//
// A node with no session at all is absent from the answer rather than dead:
// a note is neither alive nor dead (§6), and saying it is dead would put a
// badge on a card that has nothing to earn one.
func (ws Workspace) Dead(running []string) map[string]bool {
	live := make(map[string]bool, len(running))
	for _, name := range running {
		live[name] = true
	}
	dead := make(map[string]bool, len(ws.Nodes))
	for _, n := range ws.Nodes {
		if n.HasSession() && !live[ws.SessionOf(n)] {
			dead[n.ID] = true
		}
	}
	return dead
}
