package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// `trig new`, `trig ls`, and `trig attach` are the map's own actions with no map
// open (SPEC §11). They go through the same functions the map view goes through
// — see docs/adr/0017-the-cli-and-the-map-share-one-node-service.md — so what
// these tests check is the command line over them: what it makes, what it says,
// and what it refuses.

// cli is a built trig, a private tmux server, and a state directory of its own,
// so nothing here can touch the sessions the user is actually working in.
type cli struct {
	bin      string
	socket   string
	stateDir string
	env      []string
}

func newCLI(t *testing.T) cli {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	socket := "trig-test-cli-" + strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })

	stateDir := t.TempDir()
	return cli{
		bin:      build(t),
		socket:   socket,
		stateDir: stateDir,
		env: append(os.Environ(),
			"XDG_STATE_HOME="+stateDir,
			"XDG_CONFIG_HOME="+t.TempDir(),
			"TRIG_TMUX_SOCKET="+socket),
	}
}

// run is trig with these arguments, returning what it said on both streams.
func (c cli) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(c.bin, args...)
	cmd.Env = c.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mustRun is the same for a command that has no business failing.
func (c cli) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := c.run(t, args...)
	if err != nil {
		t.Fatalf("trig %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// workspace reads back what trig wrote, through the same package the map view
// reads it with.
func (c cli) workspace(t *testing.T, name string) state.Workspace {
	t.Helper()
	ws, err := state.Load(c.stateDir+"/trig/workspaces", name)
	if err != nil {
		t.Fatalf("reading workspace %q: %v", name, err)
	}
	return ws
}

func (c cli) tmux(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", c.socket}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestNewCreatesANodeWithNoMapOpenAndSaysWhatItMade(t *testing.T) {
	c := newCLI(t)

	out := c.mustRun(t, "new", "-t", "api-server")

	ws := c.workspace(t, "main")
	if len(ws.Nodes) != 1 {
		t.Fatalf("the workspace should hold one node, holds %d", len(ws.Nodes))
	}
	node := ws.Nodes[0]
	if node.Title != "api-server" || node.Kind != state.KindShell {
		t.Errorf("node = %+v, want a shell node called api-server", node)
	}
	// What it made, so a script can read the id back out of it.
	for _, want := range []string{node.ID, "api-server", "shell", "main"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output should mention %q:\n%s", want, out)
		}
	}
	// And the session behind it is really there, named the way the map names it.
	session := ws.SessionOf(node)
	if !strings.Contains(c.tmux(t, "list-sessions", "-F", "#{session_name}"), session) {
		t.Errorf("tmux has no session %q", session)
	}
	env := c.tmux(t, "show-environment", "-t", "="+session)
	for _, want := range []string{"TRIG_WORKSPACE=main", "TRIG_NODE_ID=" + node.ID, "TRIG_NODE_KIND=shell"} {
		if !strings.Contains(env, want) {
			t.Errorf("the session environment is missing %q:\n%s", want, env)
		}
	}
}

func TestNewPutsTheNodeOnAnyWorkspaceAndInAnyDirectory(t *testing.T) {
	c := newCLI(t)
	dir := t.TempDir()

	c.mustRun(t, "new", "-w", "other", "-t", "elsewhere", "-d", dir)

	ws := c.workspace(t, "other")
	if len(ws.Nodes) != 1 || ws.Nodes[0].Dir != dir {
		t.Fatalf("nodes = %+v, want one node with dir %q", ws.Nodes, dir)
	}
	if len(c.workspace(t, "main").Nodes) != 0 {
		t.Error("the default workspace should be untouched")
	}
	pane := c.tmux(t, "list-panes", "-t", "="+ws.SessionOf(ws.Nodes[0]), "-F", "#{pane_current_path}")
	if !strings.Contains(pane, dir) {
		t.Errorf("the session should have started in %q, started in %q", dir, pane)
	}
}

func TestNewMakesAnAgentNodeThatIsToldWhereToReport(t *testing.T) {
	c := newCLI(t)

	c.mustRun(t, "new", "-k", "agent", "-t", "claude", "--cmd", "sleep 60")

	ws := c.workspace(t, "main")
	node := ws.Nodes[0]
	if node.Kind != state.KindAgent || node.Cmd != "sleep 60" {
		t.Fatalf("node = %+v, want an agent node running the command", node)
	}
	env := c.tmux(t, "show-environment", "-t", "="+ws.SessionOf(node))
	if !strings.Contains(env, "TRIG_STATUS_FILE=") || !strings.Contains(env, "main_"+node.ID) {
		t.Errorf("an agent node should be told where to report:\n%s", env)
	}
}

func TestNewMakesANoteWithNoSessionAtAll(t *testing.T) {
	c := newCLI(t)

	c.mustRun(t, "new", "-k", "note", "-t", "todo")

	ws := c.workspace(t, "main")
	if ws.Nodes[0].Kind != state.KindNote {
		t.Fatalf("node = %+v, want a note", ws.Nodes[0])
	}
	if out, err := exec.Command("tmux", "-L", c.socket, "list-sessions").CombinedOutput(); err == nil && strings.Contains(string(out), "trig_") {
		t.Errorf("a note has no session, but tmux has:\n%s", out)
	}
}

func TestNewRefusesWhatItCannotMake(t *testing.T) {
	c := newCLI(t)
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"new", "-k", "wizard"}, "shell"},             // the kinds it knows
		{[]string{"new", "-k", "note", "--cmd", "ls"}, "note"}, // a note runs nothing
		{[]string{"new", "-k", "agent"}, "command"},            // an agent needs one
		{[]string{"new", "-w", "../escape"}, "separator"},      // and the name is a trust boundary
	} {
		out, err := c.run(t, tc.args...)

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("trig %s should exit non-zero, got %v\n%s", strings.Join(tc.args, " "), err, out)
		}
		if !strings.Contains(out, tc.says) {
			t.Errorf("trig %s should say what is wrong (%q):\n%s", strings.Join(tc.args, " "), tc.says, out)
		}
	}
	if len(c.workspace(t, "main").Nodes) != 0 {
		t.Error("a refused node should not reach the map")
	}
}

func TestNewPlacesEachNodeInACellOfItsOwn(t *testing.T) {
	c := newCLI(t)

	c.mustRun(t, "new", "-t", "one")
	c.mustRun(t, "new", "-t", "two")

	ws := c.workspace(t, "main")
	if len(ws.Nodes) != 2 {
		t.Fatalf("want two nodes, got %d", len(ws.Nodes))
	}
	if ws.Nodes[0].Pos == ws.Nodes[1].Pos {
		t.Errorf("both nodes are at %+v; one node per cell is the map's only layout rule", ws.Nodes[0].Pos)
	}
}

func TestLsSaysWhatEachNodeIsAndWhetherItIsAlive(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "api-server")
	c.mustRun(t, "new", "-k", "note", "-t", "todo")

	out := c.mustRun(t, "ls")

	ws := c.workspace(t, "main")
	for _, n := range ws.Nodes {
		if !strings.Contains(out, n.ID) || !strings.Contains(out, n.Title) {
			t.Errorf("the listing should name %s (%s):\n%s", n.ID, n.Title, out)
		}
	}
	if !strings.Contains(out, "live") {
		t.Errorf("a node whose session is running should be listed as live:\n%s", out)
	}
	if !strings.Contains(out, "sh") || !strings.Contains(out, "note") {
		t.Errorf("the listing should say what kind each node is:\n%s", out)
	}
}

func TestLsShowsAnAgentsOwnReport(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-k", "agent", "-t", "claude", "--cmd", "sleep 60")
	node := c.workspace(t, "main").Nodes[0]

	// Written the way an agent's hooks write it, through the same package the
	// map view reads it with.
	statusDir := status.DirBeside(c.stateDir + "/trig/workspaces")
	if err := status.Write(status.Path(statusDir, "main", node.ID), status.NeedsYou, "waiting for approval"); err != nil {
		t.Fatal(err)
	}

	out := c.mustRun(t, "ls")
	if !strings.Contains(out, "needs_you") || !strings.Contains(out, "waiting for approval") {
		t.Errorf("the listing should carry what the agent said:\n%s", out)
	}
}

func TestLsDeadNodeIsListedDead(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "goner")
	node := c.workspace(t, "main").Nodes[0]
	c.tmux(t, "kill-session", "-t", "="+c.workspace(t, "main").SessionOf(node))

	out := c.mustRun(t, "ls")
	if !strings.Contains(out, "dead") {
		t.Errorf("a node whose session is gone should be listed dead:\n%s", out)
	}
}

func TestLsJSONHasAStableShape(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "api-server")
	c.mustRun(t, "new", "-k", "note", "-t", "todo")

	out := c.mustRun(t, "ls", "--json")

	var listing struct {
		Workspace string `json:"workspace"`
		Nodes     []struct {
			ID      string   `json:"id"`
			Kind    string   `json:"kind"`
			Title   string   `json:"title"`
			Live    *bool    `json:"live"`
			Session string   `json:"session"`
			Tags    []string `json:"tags"`
			Pos     struct {
				Col int `json:"col"`
				Row int `json:"row"`
			} `json:"pos"`
			Agent *struct {
				State  string `json:"state"`
				Detail string `json:"detail"`
			} `json:"agent"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &listing); err != nil {
		t.Fatalf("--json should emit JSON: %v\n%s", err, out)
	}
	if listing.Workspace != "main" || len(listing.Nodes) != 2 {
		t.Fatalf("listing = %+v, want two nodes on main", listing)
	}
	for _, n := range listing.Nodes {
		switch n.Kind {
		case "shell":
			if n.Live == nil || !*n.Live {
				t.Errorf("%s should be live, got %v", n.ID, n.Live)
			}
			if n.Session == "" {
				t.Errorf("%s should name its session", n.ID)
			}
		case "note":
			// A note is neither alive nor dead (§6), and saying either would be
			// a claim about a session it has never had.
			if n.Live != nil {
				t.Errorf("a note's liveness should be null, got %v", *n.Live)
			}
		default:
			t.Errorf("unexpected kind %q", n.Kind)
		}
		if n.Agent != nil {
			t.Errorf("%s has never reported, so it should carry no agent status", n.ID)
		}
	}
}

func TestLsOnAnEmptyMapSaysSoRatherThanFailing(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun(t, "ls")
	if strings.TrimSpace(out) == "" {
		t.Error("an empty map should say it is empty rather than print nothing")
	}
	if out := c.mustRun(t, "ls", "--json"); !strings.Contains(out, "[]") && !strings.Contains(out, "null") {
		t.Errorf("--json on an empty map should still be JSON:\n%s", out)
	}
}

// A tmux that cannot be asked leaves liveness unknown. The map is what was
// asked for, and a listing that condemns every node on the strength of not
// knowing is worse than one that says it does not know.
func TestLsWithoutTmuxStillListsTheMapAndSaysLivenessIsUnknown(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "api-server")

	cmd := exec.Command(c.bin, "ls")
	cmd.Env = append(c.env, "PATH=/nonexistent")
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err != nil {
		t.Fatalf("a missing tmux should not stop the listing: %v\n%s", err, out)
	}
	if !strings.Contains(out, "api-server") {
		t.Errorf("the nodes should still be listed:\n%s", out)
	}
	if strings.Contains(out, "dead") {
		t.Errorf("nothing has been shown to be dead:\n%s", out)
	}
}

// A `running` report that has outlived its window is what an agent last said
// rather than what it is doing, and the listing marks it the way the card does.
func TestLsMarksAReportThatHasOutlivedItsWindow(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-k", "agent", "-t", "claude", "--cmd", "sleep 60")
	node := c.workspace(t, "main").Nodes[0]

	statusDir := status.DirBeside(c.stateDir + "/trig/workspaces")
	path := status.Path(statusDir, "main", node.ID)
	if err := status.Write(path, status.Running, "building the index"); err != nil {
		t.Fatal(err)
	}
	// Backdated past the default window, by rewriting the stamp the way an old
	// report would have carried it.
	stale, err := json.Marshal(status.Report{State: status.Running, TS: time.Now().Add(-24 * time.Hour), Detail: "building the index"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	if out := c.mustRun(t, "ls"); !strings.Contains(out, "running ?") {
		t.Errorf("a report older than the window should be marked:\n%s", out)
	}
}

func TestASubcommandThatWasMistypedDoesNotOpenTheMap(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{{"sl"}, {"ls", "main"}} {
		out, err := c.run(t, args...)

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("trig %s should exit non-zero rather than act on something else, got %v\n%s",
				strings.Join(args, " "), err, out)
		}
		if !strings.Contains(out, "usage:") {
			t.Errorf("trig %s should say what it takes:\n%s", strings.Join(args, " "), out)
		}
	}
}

// An empty name is a subsequence of every title there is, so it names nothing.
func TestAttachRefusesAnEmptyName(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "api-server")

	out, err := c.run(t, "attach", "")

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("an empty name should exit non-zero rather than attach to the only node, got %v\n%s", err, out)
	}
}

// The title a command lends a node is a default the map would have offered, so
// it is clamped like the map's own prefill rather than stored past the bound.
func TestATitleTakenFromACommandStaysInsideTheBound(t *testing.T) {
	c := newCLI(t)
	long := strings.Repeat("x", state.MaxTitleLen+50)

	c.mustRun(t, "new", "-k", "agent", "--cmd", long)

	if got := c.workspace(t, "main").Nodes[0].Title; len([]rune(got)) != state.MaxTitleLen {
		t.Errorf("title is %d characters, want it clamped to %d", len([]rune(got)), state.MaxTitleLen)
	}
}

func TestAttachRefusesAnAmbiguousMatchWithoutGuessing(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "api-server")
	c.mustRun(t, "new", "-t", "api-worker")

	out, err := c.run(t, "attach", "api")

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("an ambiguous match should exit non-zero, got %v\n%s", err, out)
	}
	for _, want := range []string{"api-server", "api-worker"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal should list what matched (%q):\n%s", want, out)
		}
	}
	if clients := c.tmux(t, "list-clients"); strings.TrimSpace(clients) != "" {
		t.Errorf("nothing should have been attached to:\n%s", clients)
	}
}

func TestAttachSaysWhenNothingMatches(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "api-server")

	out, err := c.run(t, "attach", "zqx")

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("no match should exit non-zero, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "zqx") {
		t.Errorf("the refusal should quote what was asked for:\n%s", out)
	}
}

func TestAttachRefusesANoteBecauseThereIsNothingToAttachTo(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-k", "note", "-t", "todo")

	out, err := c.run(t, "attach", "todo")

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("attaching to a note should exit non-zero, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "note") {
		t.Errorf("the refusal should say what a note is:\n%s", out)
	}
}

func TestAttachRefusesADeadNodeRatherThanAttachingToNothing(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "goner")
	ws := c.workspace(t, "main")
	c.tmux(t, "kill-session", "-t", "="+ws.SessionOf(ws.Nodes[0]))

	out, err := c.run(t, "attach", "goner")

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("attaching to a dead node should exit non-zero, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "goner") {
		t.Errorf("the refusal should name the node:\n%s", out)
	}
}

// An exact title, and an id, are how a script says which node it means when a
// subsequence would find several.
func TestAnExactTitleOutranksAFuzzyMatch(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaaa", Kind: state.KindShell, Title: "api"},
		{ID: "bbbb", Kind: state.KindShell, Title: "api-server"},
	}}

	node, err := pick(ws, "api")
	if err != nil {
		t.Fatalf("an exact title should settle it: %v", err)
	}
	if node.ID != "aaaa" {
		t.Errorf("picked %q, want the node whose title is exactly the query", node.Title)
	}
}

func TestAnIdIsAlwaysAWayToNameANode(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaaa", Kind: state.KindShell, Title: "api"},
		{ID: "bbbb", Kind: state.KindShell, Title: "worker"},
	}}

	node, err := pick(ws, "bbbb")
	if err != nil {
		t.Fatalf("an id should name a node: %v", err)
	}
	if node.ID != "bbbb" {
		t.Errorf("picked %q, want the node with that id", node.ID)
	}
}

// The map filters on title, tags, and kind; attach picks on the title alone.
// "sh" would otherwise be an ambiguous way of asking for every shell node, and
// a tag is a name for several nodes by design.
func TestAttachPicksOnTheTitleAndNotTheKindOrTags(t *testing.T) {
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaaa", Kind: state.KindShell, Title: "api", Tags: []string{"infra"}},
	}}

	if _, err := pick(ws, "infra"); err == nil {
		t.Error("a tag names several nodes by design and should not pick one")
	}
	if _, err := pick(ws, "sh"); err == nil {
		t.Error("a kind names several nodes by design and should not pick one")
	}
}
