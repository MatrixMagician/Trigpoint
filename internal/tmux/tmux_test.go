package tmux

import (
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionNameEncodesWorkspaceAndNode(t *testing.T) {
	name := SessionName("main", "k4f2")
	if !strings.HasPrefix(name, Prefix) {
		t.Errorf("SessionName(%q) = %q, want the %q prefix", "main", name, Prefix)
	}
	if !strings.Contains(name, "main") || !strings.Contains(name, "k4f2") {
		t.Errorf("SessionName should encode workspace and node id, got %q", name)
	}
	if other := SessionName("other", "k4f2"); other == name {
		t.Error("sessions in different workspaces must not collide")
	}
}

func TestOursRecognisesOnlyThePrefix(t *testing.T) {
	for name, want := range map[string]bool{
		"trig_main_k4f2": true,
		"trig_":          true,
		"scratch":        false,
		"":               false,
		"mytrig_main":    false,
	} {
		if got := Ours(name); got != want {
			t.Errorf("Ours(%q) = %v, want %v", name, got, want)
		}
	}
}

// socketSeq numbers the private servers. Repeating a run (-count=2) would
// otherwise put a fresh server on a socket the last one is still letting go of,
// and the create that lands in that gap fails on a server already on its way out.
var socketSeq atomic.Int64

func testSocket(name string) string {
	return fmt.Sprintf("trig-test-%s-%d", name, socketSeq.Add(1))
}

// testCLI runs against a private tmux server so a test can never disturb the
// sessions the user is actually working in.
func testCLI(t *testing.T) CLI {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	c := CLI{Socket: testSocket(t.Name())}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", c.Socket, "kill-server").Run() })
	return c
}

func TestCreateMakesALiveSessionCarryingItsProvenance(t *testing.T) {
	c := testCLI(t)
	dir := t.TempDir()
	name := SessionName("main", "k4f2")

	if err := c.Create(name, dir, "", map[string]string{
		"TRIG_WORKSPACE": "main",
		"TRIG_NODE_ID":   "k4f2",
		"TRIG_NODE_KIND": "shell",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	alive, err := c.Exists(name)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !alive {
		t.Fatal("Create should leave a live session behind")
	}

	env, err := exec.Command("tmux", "-L", c.Socket, "show-environment", "-t", "="+name).Output()
	if err != nil {
		t.Fatalf("show-environment: %v", err)
	}
	for _, want := range []string{"TRIG_WORKSPACE=main", "TRIG_NODE_ID=k4f2", "TRIG_NODE_KIND=shell"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("session environment is missing %q, got:\n%s", want, env)
		}
	}
}

func TestKillRemovesTheSession(t *testing.T) {
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	if err := c.Create(name, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := c.Kill(name); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	alive, err := c.Exists(name)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if alive {
		t.Error("Kill should remove the session")
	}
}

func TestKillRemovesAnAdoptedSessionToo(t *testing.T) {
	c := testCLI(t)
	// A session outside the prefix, of the kind adoption exists to map (§9.3).
	// Killing it is an instruction about a card on the map, given at a
	// confirmation prompt — so the prefix has nothing left to say here.
	const foreign = "someone-elses-work"
	if err := exec.Command("tmux", "-L", c.Socket, "new-session", "-d", "-s", foreign).Run(); err != nil {
		t.Fatalf("setting up a foreign session: %v", err)
	}

	if err := c.Kill(foreign); err != nil {
		t.Fatalf("Kill of an adopted session: %v", err)
	}
	alive, err := c.Exists(foreign)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if alive {
		t.Error("killing an adopted node should kill the session it was adopted from")
	}
}

func TestCreateRefusesASessionOutsideThePrefix(t *testing.T) {
	if err := (CLI{Socket: "trig-test-unused"}).Create("scratch", t.TempDir(), "", nil); err == nil {
		t.Error("Create should refuse a name outside the Trigpoint prefix")
	}
}

func TestExistsIsFalseWithNoServerRunning(t *testing.T) {
	c := CLI{Socket: "trig-test-no-server-at-all"}
	alive, err := c.Exists(SessionName("main", "k4f2"))
	if err != nil {
		t.Fatalf("Exists with no server should not be an error: %v", err)
	}
	if alive {
		t.Error("nothing is alive when no server is running")
	}
}

func TestKillingAnAbsentSessionSucceeds(t *testing.T) {
	c := testCLI(t)
	// A session can die without Trigpoint: a reboot, a `tmux kill-server`, or
	// `exit` typed inside it. Killing what is already gone is what was asked
	// for, so it is not a failure.
	if err := c.Kill(SessionName("main", "gone")); err != nil {
		t.Errorf("killing a session that does not exist: %v", err)
	}
	if err := (CLI{Socket: "trig-test-never-started"}).Kill(SessionName("main", "gone")); err != nil {
		t.Errorf("killing a session with no tmux server running: %v", err)
	}
}

func TestHandoffInstallsTheDetachBindingAndTakesItBackAgain(t *testing.T) {
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	if err := c.Create(name, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, release, err := c.Handoff(name, "M-Escape")
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}

	bound := rootBindings(t, c)
	if !strings.Contains(bound, "M-Escape") {
		t.Errorf("the detach key should be bound while attached, got:\n%s", bound)
	}
	// The binding names the session rather than whoever pressed the key: with
	// Trigpoint inside tmux the outer client sees the key first, and a plain
	// detach-client would drop the user out of their own tmux.
	if !strings.Contains(bound, "detach-client") || !strings.Contains(bound, name) {
		t.Errorf("the detach binding should detach the node's session, got:\n%s", bound)
	}

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if bound := rootBindings(t, c); strings.Contains(bound, "M-Escape") {
		t.Errorf("the detach key should be unbound once back on the map, got:\n%s", bound)
	}
}

func rootBindings(t *testing.T, c CLI) string {
	t.Helper()
	out, err := exec.Command("tmux", "-L", c.Socket, "list-keys", "-T", "root").Output()
	if err != nil {
		t.Fatalf("list-keys: %v", err)
	}
	return string(out)
}

func TestHandoffAttachDropsTMUXSoNestingIsNeverRefused(t *testing.T) {
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	if err := c.Create(name, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,42,0")
	t.Setenv("TMUX_PANE", "%7")

	attach, _, err := c.Handoff(name, "M-Escape")
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	for _, v := range attach.Env {
		if strings.HasPrefix(v, "TMUX=") || strings.HasPrefix(v, "TMUX_PANE=") {
			t.Errorf("the attach child still carries %q, so tmux will refuse to nest", v)
		}
	}
	if len(attach.Env) == 0 {
		t.Error("the attach child was given no environment at all")
	}
}

func TestHandoffRefusesWithNoDetachKey(t *testing.T) {
	// Attaching with no way back would trap the user inside the session.
	if _, _, err := (CLI{Socket: "trig-test-unused"}).Handoff(SessionName("main", "k4f2"), "  "); err == nil {
		t.Error("Handoff should refuse to attach when no detach key is configured")
	}
}

// TestTheDetachKeyReturnsFromANestedAttach runs the handoff the way Trigpoint
// does it — from inside a tmux pane, which is the nesting case §5.1 says must
// never be refused — and presses the detach key at it.
func TestTheDetachKeyReturnsFromANestedAttach(t *testing.T) {
	c := testCLI(t)
	node := SessionName("main", "k4f2")
	if err := c.Create(node, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := exec.Command("tmux", "-L", c.Socket, "new-session", "-d", "-s", "outer").Run(); err != nil {
		t.Fatalf("setting up the outer session: %v", err)
	}

	attach, release, err := c.Handoff(node, "M-Escape")
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	// The pane's own TMUX is what tmux would refuse the nesting over, so the
	// child is run with exactly the environment Handoff prepared.
	line := "env -u TMUX -u TMUX_PANE " + strings.Join(attach.Args, " ")
	if err := exec.Command("tmux", "-L", c.Socket, "send-keys", "-t", "outer", line, "Enter").Run(); err != nil {
		t.Fatalf("send-keys: %v", err)
	}
	waitForClients(t, c, node, 1, "the nested attach never took hold")

	if err := exec.Command("tmux", "-L", c.Socket, "send-keys", "-t", "outer", "M-Escape").Run(); err != nil {
		t.Fatalf("send-keys M-Escape: %v", err)
	}
	waitForClients(t, c, node, 0, "the detach key did not return from the attach")

	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// waitForClients waits for session to have want clients attached. tmux does its
// own work in its own time, so the count is polled rather than read once.
func waitForClients(t *testing.T, c CLI, session string, want int, complaint string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "-L", c.Socket,
			"list-sessions", "-F", "#{session_name} #{session_attached}").Output()
		if err == nil {
			got = ""
			for _, line := range strings.Split(string(out), "\n") {
				name, clients, _ := strings.Cut(strings.TrimSpace(line), " ")
				if name == session {
					got = clients
				}
			}
			if got == strconv.Itoa(want) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: %s has %q clients attached, want %d", complaint, session, got, want)
}

func TestReleasingTheDetachBindingSurvivesTheServerGoingAway(t *testing.T) {
	// Typing `exit` in the last session takes the tmux server down with it, so
	// a handoff ending in the most ordinary way possible finds nothing left to
	// unbind. That is the outcome that was asked for, not a failure to report
	// on the map the user has just come back to.
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	if err := c.Create(name, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, release, err := c.Handoff(name, "M-Escape")
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	if err := exec.Command("tmux", "-L", c.Socket, "kill-server").Run(); err != nil {
		t.Fatalf("kill-server: %v", err)
	}
	if err := release(); err != nil {
		t.Errorf("release complained about a server that is already gone: %v", err)
	}
}

func TestListNamesEverySessionOnTheServer(t *testing.T) {
	c := testCLI(t)
	ours := SessionName("main", "k4f2")
	if err := c.Create(ours, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	const foreign = "someone-elses-work"
	if err := exec.Command("tmux", "-L", c.Socket, "new-session", "-d", "-s", foreign).Run(); err != nil {
		t.Fatalf("setting up a foreign session: %v", err)
	}

	// Reconciliation has to see the whole server, not only Trigpoint's corner
	// of it: an orphan is recognised by its name, and adoption (§9.3) wants
	// exactly the sessions this prefix excludes.
	names, err := c.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, want := range []string{ours, foreign} {
		if !slices.Contains(names, want) {
			t.Errorf("List() = %v, missing %q", names, want)
		}
	}
}

func TestListIsEmptyWithNoServerRunning(t *testing.T) {
	// No server is an answer — an empty map — rather than a failure.
	names, err := (CLI{Socket: "trig-test-no-server-to-list"}).List()
	if err != nil {
		t.Fatalf("List with no server should not be an error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("List() = %v, want nothing at all", names)
	}
}

func TestEnvReadsBackTheProvenanceCreateSeeded(t *testing.T) {
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	if err := c.Create(name, t.TempDir(), "", map[string]string{
		"TRIG_WORKSPACE": "main",
		"TRIG_NODE_ID":   "k4f2",
		"TRIG_NODE_KIND": "shell",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// This is what makes a session self-describing: a workspace file lost to a
	// crash leaves the session still able to say which node it was.
	env, err := c.Env(name)
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	for key, want := range map[string]string{
		"TRIG_WORKSPACE": "main",
		"TRIG_NODE_ID":   "k4f2",
		"TRIG_NODE_KIND": "shell",
	} {
		if env[key] != want {
			t.Errorf("Env()[%q] = %q, want %q", key, env[key], want)
		}
	}
}

func TestEnvOfAnAbsentSessionIsAFailureRatherThanAnEmptyAnswer(t *testing.T) {
	c := testCLI(t)
	// A session can die between being listed and being asked about. Answering
	// that with an empty environment would be indistinguishable from a session
	// that is there and has had its variables cleared — and the caller does
	// opposite things with the two.
	if _, err := c.Env(SessionName("main", "gone")); err == nil {
		t.Error("Env of a session that is not there should say so")
	}
}

func TestListReportsAFailureRatherThanCallingItAnEmptyServer(t *testing.T) {
	// "No server running" is an empty answer; anything else tmux refuses for is
	// a failure. Reading one as the other would tell reconciliation that
	// nothing is alive and mark every node on the map dead — the one lie the
	// map exists not to tell.
	//
	// The NUL makes the command unrunnable, which stands for every way tmux
	// can fail with sessions still on the server: a protocol mismatch after an
	// upgrade under a live server, or a socket that cannot be read.
	if _, err := (CLI{Socket: "trig-test-\x00-broken"}).List(); err == nil {
		t.Error("a tmux that failed for any other reason should be reported, not read as no sessions")
	}
}

func TestAbsentRecognisesTheWordingsForSomethingThatIsNotThere(t *testing.T) {
	// The wording differs between tmux versions, and a miss here is what turns
	// a live map dead.
	for complaint, want := range map[string]bool{
		"no server running on /tmp/tmux-1000/default":         true,
		"error connecting to /tmp/tmux-1000/x (No such file)": true,
		"no such session: =trig_main_k4f2":                    true,
		"can't find session: trig_main_k4f2":                  true,
		"protocol version mismatch (client 8, server 7)":      false,
		"": false,
	} {
		if got := absent(complaint); got != want {
			t.Errorf("absent(%q) = %v, want %v", complaint, got, want)
		}
	}
}

func TestCreateRunsTheCommandItIsGiven(t *testing.T) {
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	// Respawning an agent node re-runs its command (§9.2); a shell node passes
	// none and gets a login shell.
	if err := c.Create(name, t.TempDir(), "sleep 600", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	out, err := exec.Command("tmux", "-L", c.Socket, "display-message", "-p", "-t", "="+name+":", "#{pane_start_command}").Output()
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	if got := strings.TrimSpace(string(out)); !strings.Contains(got, "sleep 600") {
		t.Errorf("pane start command = %q, want the command Create was given", got)
	}
}
