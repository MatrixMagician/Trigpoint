package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// read is the settings file as a map, so a test can look at what Install left
// behind without going back through this package's own reader.
func read(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings file is not JSON: %v\n%s", err, raw)
	}
	return settings
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInstallAddsEveryEntryWhereThereIsNoSettingsFileAtAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")

	added, err := Install(path, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(added) != len(Entries) {
		t.Fatalf("added %d entries, want all %d", len(added), len(Entries))
	}

	missing, err := Status(path)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("after installing, %v are still missing", missing)
	}
}

// The user's settings file is theirs. Trigpoint adds three hook entries to it
// and must leave everything else — its own keys, and hooks belonging to
// somebody else — exactly as it found them.
func TestInstallPreservesUnrelatedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{
	  "model": "opus",
	  "env": {"FOO": "bar"},
	  "hooks": {
	    "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}],
	    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "audit.sh"}]}]
	  }
	}`)

	if _, err := Install(path, false); err != nil {
		t.Fatalf("Install: %v", err)
	}

	settings := read(t, path)
	if settings["model"] != "opus" {
		t.Errorf("model = %v, want it left alone", settings["model"])
	}
	if env, _ := settings["env"].(map[string]any); env["FOO"] != "bar" {
		t.Errorf("env = %v, want it left alone", settings["env"])
	}
	for _, want := range []string{"notify-send done", "audit.sh"} {
		if !strings.Contains(commandsIn(t, path), want) {
			t.Errorf("hook %q was lost", want)
		}
	}
	// And the entry that shares an event with somebody else's hook is still added.
	missing, err := Status(path)
	if err != nil || len(missing) != 0 {
		t.Errorf("Status = %v, %v; want everything installed alongside the existing hooks", missing, err)
	}
}

func TestInstallASecondTimeChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(path, false); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	added, err := Install(path, false)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("second Install added %v, want nothing", added)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("second Install rewrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// Drift is the case this command exists for: a file that has some of the
// entries gets the rest, and keeps the one it already had exactly once.
func TestInstallFillsInOnlyWhatIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "trig emit-status done"}]}]}}`)

	added, err := Install(path, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(added) != len(Entries)-1 {
		t.Fatalf("added %v, want every entry but the one already there", added)
	}
	for _, e := range added {
		if e.Event == "Stop" {
			t.Errorf("added a second Stop entry over the one already installed")
		}
	}
	if n := strings.Count(commandsIn(t, path), "emit-status done"); n != 1 {
		t.Errorf("the done entry appears %d times, want 1", n)
	}
}

// An entry a user has wrapped in their own shell is still their installation of
// it, and re-running the command must not staple a second copy underneath.
func TestAnEntryWrappedByTheUserStillCountsAsInstalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	write(t, path, `{"hooks": {"Notification": [{"hooks": [{"type": "command",
	  "command": "cd /tmp && trig emit-status needs_you \"$(date)\""}]}]}}`)

	added, err := Install(path, false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, e := range added {
		if e.Event == "Notification" {
			t.Errorf("added a Notification entry over the user's own wrapping of it")
		}
	}
}

func TestDryRunReportsWhatItWouldDoAndWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	added, err := Install(path, true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(added) != len(Entries) {
		t.Errorf("a dry run reported %d entries, want the %d it would add", len(added), len(Entries))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a dry run created %s", path)
	}
}

func TestStatusOnAFileThatIsNotThereIsEverythingMissing(t *testing.T) {
	missing, err := Status(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("a missing settings file is not an error: %v", err)
	}
	if len(missing) != len(Entries) {
		t.Errorf("missing = %v, want all %d entries", missing, len(Entries))
	}
}

// Refusing to guess is what keeps a malformed file from being replaced by one
// Trigpoint invented.
func TestMalformedSettingsAreRefusedRatherThanOverwritten(t *testing.T) {
	cases := map[string]string{
		"not JSON at all":          `{"hooks":`,
		"hooks is not an object":   `{"hooks": ["Stop"]}`,
		"an event is not a list":   `{"hooks": {"Stop": "notify-send"}}`,
		"a group is not an object": `{"hooks": {"Stop": ["notify-send"]}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			write(t, path, body)

			if _, err := Status(path); err == nil {
				t.Errorf("Status accepted %s", name)
			}
			if _, err := Install(path, false); err == nil {
				t.Fatalf("Install accepted %s", name)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != body {
				t.Errorf("a refused Install rewrote the file:\n%s", raw)
			}
		})
	}
}

func TestSettingsPathHonoursClaudeConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/somewhere/else")
	path, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath: %v", err)
	}
	if want := filepath.Join("/somewhere/else", "settings.json"); path != want {
		t.Errorf("SettingsPath = %q, want %q", path, want)
	}
}

// The hooks fire in every Claude Code session, not only in the ones running
// inside a node. Outside one there is no status file, and the entry has to be a
// quiet no-op rather than an error Claude Code shows the user on every prompt.
func TestTheInstalledCommandIsSilentOutsideANode(t *testing.T) {
	for _, e := range Entries {
		cmd := exec.Command("sh", "-c", e.Command)
		cmd.Env = []string{"PATH=/nonexistent"} // no trig, and no TRIG_STATUS_FILE
		if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
			t.Errorf("%s hook outside a node: err = %v, output = %q; want silence", e.Event, err, out)
		}
	}
}

// Claude Code's Notification hook fires for two different things: a permission
// request, which is needs-you, and the prompt having sat idle for a minute,
// which is not. Without the second one filtered out, every node that reported
// done would turn amber a minute later — and `done` would be a state nothing
// could ever be seen in.
func TestTheNotificationHookIgnoresAnIdlePrompt(t *testing.T) {
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "main_kt7m.json")
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// A stand-in for trig on PATH, so the test sees whether the hook decided to
	// call it without needing the real binary built.
	stub := "#!/bin/sh\necho \"$@\" > " + statusFile + "\n"
	if err := os.WriteFile(filepath.Join(bin, "trig"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}

	command := ""
	for _, e := range Entries {
		if e.Event == "Notification" {
			command = e.Command
		}
	}
	cases := []struct {
		name    string
		payload string
		emits   bool
	}{
		{"a permission request", `{"message":"Claude needs your permission to use Bash"}`, true},
		{"an idle prompt", `{"message":"Claude is waiting for your input"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(statusFile)
			cmd := exec.Command("sh", "-c", command)
			cmd.Stdin = strings.NewReader(c.payload)
			// The stub first, then the real PATH: the hook uses grep, and a
			// PATH without it would send every case down the emit branch.
			cmd.Env = []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "TRIG_STATUS_FILE=" + statusFile}
			out, err := cmd.CombinedOutput()
			if err != nil || len(out) > 0 {
				t.Fatalf("the hook failed: %v\n%s", err, out)
			}
			_, err = os.Stat(statusFile)
			if emitted := err == nil; emitted != c.emits {
				t.Errorf("%s emitted = %v, want %v", c.name, emitted, c.emits)
			}
		})
	}
}

// commandsIn is every command string in the file, flattened, for tests that
// only care whether some text survived.
func commandsIn(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Knowledge of an agent's configuration format lives here and nowhere else, and
// installing it is something the user does rather than something node creation
// does on their behalf. Both of those are one fact about the import graph: the
// map view cannot reach this package, so it cannot write to an agent's
// settings even by accident.
func TestTheMapViewCannotReachThisPackage(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Deps \"\\n\"}}", "../tui").Output()
	if err != nil {
		t.Fatalf("listing the map view's dependencies: %v", err)
	}
	const self = "github.com/MatrixMagician/Trigpoint/internal/hooks"
	for _, dep := range strings.Split(string(out), "\n") {
		if dep == self {
			t.Errorf("internal/tui imports %s; hooks are installed by `trig init-hooks`, never by creating a node", self)
		}
	}
}
