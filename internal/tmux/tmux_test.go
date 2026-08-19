package tmux

import (
	"os/exec"
	"strings"
	"testing"
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

// testCLI runs against a private tmux server so a test can never disturb the
// sessions the user is actually working in.
func testCLI(t *testing.T) CLI {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	c := CLI{Socket: "trig-test-" + t.Name()}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", c.Socket, "kill-server").Run() })
	return c
}

func TestCreateMakesALiveSessionCarryingItsProvenance(t *testing.T) {
	c := testCLI(t)
	dir := t.TempDir()
	name := SessionName("main", "k4f2")

	if err := c.Create(name, dir, map[string]string{
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
	if err := c.Create(name, t.TempDir(), nil); err != nil {
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

func TestKillNeverTouchesASessionOutsideThePrefix(t *testing.T) {
	c := testCLI(t)
	const foreign = "someone-elses-work"
	if err := exec.Command("tmux", "-L", c.Socket, "new-session", "-d", "-s", foreign).Run(); err != nil {
		t.Fatalf("setting up a foreign session: %v", err)
	}

	if err := c.Kill(foreign); err == nil {
		t.Error("Kill should refuse a session outside the Trigpoint prefix")
	}
	alive, err := c.Exists(foreign)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !alive {
		t.Fatal("the foreign session was killed")
	}
}

func TestCreateRefusesASessionOutsideThePrefix(t *testing.T) {
	if err := (CLI{Socket: "trig-test-unused"}).Create("scratch", t.TempDir(), nil); err == nil {
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
