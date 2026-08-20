package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rules for turning a node into a session live here rather than in the map
// view, so that `trig new` and `n` cannot start the same node two different
// ways — see docs/adr/0017-the-cli-and-the-map-share-one-node-service.md.

func TestTheShellOutlivesTheAgent(t *testing.T) {
	// An agent node is defined by having been started as one, not by having a
	// live agent: tmux ends a session whose command exits, so the command it is
	// given has to hand over to a shell rather than be the last thing running.
	got := Node{Kind: KindAgent, Cmd: "claude"}.StartCmd()

	if !strings.Contains(got, "claude") {
		t.Errorf("start command = %q, want it to run the agent", got)
	}
	if !strings.Contains(got, "exec") || !strings.Contains(got, "SHELL") {
		t.Errorf("start command = %q, want the shell to be left behind", got)
	}
}

func TestAQuoteInACommandStaysInsideIt(t *testing.T) {
	// The command reaches tmux as one shell word, so a quote in it must not be
	// able to close that word and turn the rest into commands of its own.
	got := Node{Kind: KindAgent, Cmd: `claude -p 'hi'; rm -rf /`}.StartCmd()

	if strings.Contains(got, "'claude -p 'hi'") {
		t.Errorf("start command = %q, want the quote escaped", got)
	}
	if !strings.HasPrefix(got, "sh -c '") || !strings.HasSuffix(got, "'") {
		t.Errorf("start command = %q, want one quoted shell word", got)
	}
	if want := `claude -p 'hi'; rm -rf /`; !strings.Contains(strings.ReplaceAll(got, `'\''`, "'"), want) {
		t.Errorf("start command = %q, want it to carry %q unchanged", got, want)
	}
}

func TestAShellNodeStillGetsAPlainShell(t *testing.T) {
	if got := (Node{Kind: KindShell}).StartCmd(); got != "" {
		t.Errorf("start command = %q, want a login shell", got)
	}
}

func TestSessionOfNamesANodeAfterItsWorkspaceAndId(t *testing.T) {
	ws := Workspace{Name: "main"}
	if got, want := ws.SessionOf(Node{ID: "kt7m"}), "trig_main_kt7m"; got != want {
		t.Errorf("SessionOf = %q, want %q", got, want)
	}
}

func TestAnAdoptedNodeKeepsTheForeignSessionsOwnName(t *testing.T) {
	// Adoption stores the name rather than imposing a prefix on it (§9.3).
	ws := Workspace{Name: "main"}
	if got, want := ws.SessionOf(Node{ID: "kt7m", Session: "zoo"}), "zoo"; got != want {
		t.Errorf("SessionOf = %q, want the foreign name %q", got, want)
	}
}

func TestProvenanceSaysWhichNodeASessionBelongsTo(t *testing.T) {
	env := Provenance("main", Node{ID: "kt7m", Kind: KindShell}, "")

	for key, want := range map[string]string{"TRIG_WORKSPACE": "main", "TRIG_NODE_ID": "kt7m", "TRIG_NODE_KIND": "shell"} {
		if env[key] != want {
			t.Errorf("%s = %q, want %q", key, env[key], want)
		}
	}
	if _, ok := env["TRIG_STATUS_FILE"]; ok {
		// A shell node has no agent to earn a badge with.
		t.Errorf("a shell node should get no status file, got %q", env["TRIG_STATUS_FILE"])
	}
}

func TestAnAgentNodeIsToldWhereToReportAndTheDirectoryIsThere(t *testing.T) {
	statusDir := filepath.Join(t.TempDir(), "status")
	env := Provenance("main", Node{ID: "kt7m", Kind: KindAgent}, statusDir)

	path := env["TRIG_STATUS_FILE"]
	if !strings.HasPrefix(path, statusDir) || !strings.Contains(path, "main_kt7m") {
		t.Errorf("TRIG_STATUS_FILE = %q, want a file for main_kt7m under %s", path, statusDir)
	}
	// An agent writing the file itself is the documented contract, and not
	// everything has hooks that can shell out to create a directory first.
	if info, err := os.Stat(statusDir); err != nil || !info.IsDir() {
		t.Errorf("the status directory should exist: %v", err)
	}
}

func TestDeadIsEveryNodeWhoseSessionIsNotRunning(t *testing.T) {
	ws := Workspace{Name: "main", Nodes: []Node{
		{ID: "aaaa", Kind: KindShell},
		{ID: "bbbb", Kind: KindShell},
		{ID: "cccc", Kind: KindNote},
		{ID: "dddd", Kind: KindShell, Session: "zoo"},
	}}

	dead := ws.Dead([]string{"trig_main_aaaa", "unrelated"})

	if dead["aaaa"] {
		t.Error("a node whose session is running is alive")
	}
	if !dead["bbbb"] {
		t.Error("a node whose session is gone is dead")
	}
	if _, ok := dead["cccc"]; ok {
		// A note has no session, so it is neither alive nor dead (§6).
		t.Error("a note has no session and cannot be dead")
	}
	if !dead["dddd"] {
		t.Error("an adopted node is judged by the session it stores")
	}
}
