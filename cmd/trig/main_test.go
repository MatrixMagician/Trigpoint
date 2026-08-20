package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/Trigpoint/internal/hooks"
	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// build compiles trig once and returns the binary's path, so the tests below
// exercise the real command-line wiring rather than a stand-in for it.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "trig")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("building trig: %v\n%s", err, out)
	}
	return bin
}

func TestDoctorPassesOnThisMachine(t *testing.T) {
	cmd := exec.Command(build(t), "doctor")
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trig doctor failed on a machine with tmux installed: %v\n%s", err, out)
	}
	for _, want := range []string{"tmux", "control mode", "config", "state directory", "keymap"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("doctor output does not mention %q:\n%s", want, out)
		}
	}
}

// TestDoctorRefusesAKeymapThatCannotWork: a keymap that does not resolve stops
// the map opening, so doctor is where the reason lives rather than being found
// by a key that quietly does nothing (§10).
func TestDoctorRefusesAKeymapThatCannotWork(t *testing.T) {
	cfgDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfgDir, "trig"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[keymap]\nnew_note = \"n\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "trig", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(build(t), "doctor")
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+cfgDir)
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("doctor should exit non-zero on a keymap that binds one key twice, got %v\n%s", err, out)
	}
	for _, want := range []string{"keymap", "new_shell", "new_note"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the failure should name %q:\n%s", want, out)
		}
	}
}

// TestTheMapRefusesToOpenOnABrokenKeymap is the same gate at startup, which is
// where a user meets it: the terminal is never taken.
func TestTheMapRefusesToOpenOnABrokenKeymap(t *testing.T) {
	cfgDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfgDir, "trig"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[keymap]\ncursor_lft = \"h\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "trig", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(build(t))
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+cfgDir)
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the map should not open on an unknown action, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cursor_lft") || !strings.Contains(string(out), "cursor_left") {
		t.Errorf("the failure should name the typo and the action meant:\n%s", out)
	}
}

func TestDoctorFailsAndExplainsWhenTmuxIsMissing(t *testing.T) {
	cmd := exec.Command(build(t), "doctor")
	cmd.Env = append(os.Environ(), "PATH=/nonexistent", "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("doctor should exit non-zero without tmux, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "tmux") || !strings.Contains(string(out), "3.2") {
		t.Errorf("the failure should name tmux and the required version:\n%s", out)
	}
}

func TestUnsafeWorkspaceNameIsRejectedBeforeAnythingIsWritten(t *testing.T) {
	stateDir := t.TempDir()
	cmd := exec.Command(build(t), "-w", "../escape")
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+stateDir, "XDG_CONFIG_HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("an unsafe workspace name should be refused, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "separator") {
		t.Errorf("the refusal should say what is wrong with the name:\n%s", out)
	}
}

func TestHelpIsNotAnError(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"new", "-h"}, {"ls", "-h"}, {"attach", "-h"}, {"doctor", "-h"}, {"emit-status", "-h"}, {"init-hooks", "-h"}} {
		cmd := exec.Command(build(t), args...)
		cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()

		if err != nil {
			t.Errorf("trig %v should exit 0: %v\n%s", args, err, out)
		}
		if strings.Contains(string(out), "help requested") {
			t.Errorf("trig %v leaked flag's internal error:\n%s", args, out)
		}
		if !strings.Contains(string(out), "usage:") {
			t.Errorf("trig %v should print usage:\n%s", args, out)
		}
	}
}

func TestUnknownFlagIsReportedOnce(t *testing.T) {
	cmd := exec.Command(build(t), "-nosuchflag")
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("an unknown flag should exit non-zero, got %v\n%s", err, out)
	}
	if n := strings.Count(string(out), "not defined"); n != 1 {
		t.Errorf("the error should appear once, appeared %d times:\n%s", n, out)
	}
}

// `trig emit-status` is the agent side of SPEC §8: the tiny CLI mode an agent's
// hooks call, and the thing a user can run by hand from inside a node to see a
// badge move. It is a convenience over the file format and never a privileged
// path into Trigpoint — these tests read what it wrote back through the same
// package the map view reads it with.

func TestEmitStatusWritesAReportToTheStatusFile(t *testing.T) {
	dir := t.TempDir()
	path := status.Path(dir, "main", "kt7m")

	cmd := exec.Command(build(t), "emit-status", "needs_you", "waiting", "for", "approval")
	cmd.Env = append(os.Environ(), "TRIG_STATUS_FILE="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trig emit-status failed: %v\n%s", err, out)
	}

	reports, err := status.Read(dir, "main")
	if err != nil {
		t.Fatalf("reading back the report: %v", err)
	}
	got, ok := reports["kt7m"]
	if !ok {
		t.Fatalf("read %v, want a report for kt7m", reports)
	}
	if got.State != status.NeedsYou {
		t.Errorf("state = %q, want %q", got.State, status.NeedsYou)
	}
	if got.Detail != "waiting for approval" {
		t.Errorf("detail = %q, want the words after the state", got.Detail)
	}
}

// A hook that fires outside a node has no status file to write to, and saying
// which variable is missing is the whole of what makes that fixable.
func TestEmitStatusWithoutAStatusFileSaysWhichVariableIsMissing(t *testing.T) {
	cmd := exec.Command(build(t), "emit-status", "running")
	cmd.Env = withoutStatusFile(os.Environ())
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("emit-status with no status file should exit non-zero, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "TRIG_STATUS_FILE") {
		t.Errorf("the failure should name the variable:\n%s", out)
	}
}

// A typo is refused where it is made rather than written to a file every later
// reader silently skips.
func TestEmitStatusRefusesAStateOutsideTheVocabulary(t *testing.T) {
	path := status.Path(t.TempDir(), "main", "kt7m")
	cmd := exec.Command(build(t), "emit-status", "needs-you")
	cmd.Env = append(os.Environ(), "TRIG_STATUS_FILE="+path)
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("an unknown state should exit non-zero, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "needs_you") {
		t.Errorf("the failure should list the states an agent may report:\n%s", out)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused state should write nothing, but %s exists", path)
	}
}

func withoutStatusFile(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, v := range env {
		if !strings.HasPrefix(v, "TRIG_STATUS_FILE=") {
			kept = append(kept, v)
		}
	}
	return kept
}

// `trig init-hooks claude` is the only place Trigpoint touches an agent's own
// configuration. It merges, it says what it did, and it never does it twice.

func TestInitHooksInstallsAndSaysWhatItChanged(t *testing.T) {
	claudeDir := t.TempDir()
	cmd := exec.Command(build(t), "init-hooks", "claude")
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+claudeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trig init-hooks claude failed: %v\n%s", err, out)
	}

	settings := filepath.Join(claudeDir, "settings.json")
	if !strings.Contains(string(out), settings) {
		t.Errorf("the output should name the file it changed:\n%s", out)
	}
	for _, e := range hooks.Entries {
		if !strings.Contains(string(out), e.Event) {
			t.Errorf("the output should name the %s entry it added:\n%s", e.Event, out)
		}
	}
	missing, err := hooks.Status(settings)
	if err != nil || len(missing) != 0 {
		t.Errorf("after init-hooks, Status = %v, %v; want everything installed", missing, err)
	}
}

func TestInitHooksASecondTimeSaysThereIsNothingToDo(t *testing.T) {
	claudeDir := t.TempDir()
	bin := build(t)
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+claudeDir)

	first := exec.Command(bin, "init-hooks", "claude")
	first.Env = env
	if out, err := first.CombinedOutput(); err != nil {
		t.Fatalf("first run failed: %v\n%s", err, out)
	}
	before, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	second := exec.Command(bin, "init-hooks", "claude")
	second.Env = env
	out, err := second.CombinedOutput()
	if err != nil {
		t.Fatalf("second run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "already") {
		t.Errorf("the second run should say the hooks are already installed:\n%s", out)
	}
	after, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the second run rewrote the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// Both orders of the flag and the agent name, because the usage line prints
// `init-hooks claude [-n]` and flag stops parsing at the first non-flag word.
func TestInitHooksDryRunSaysWhatItWouldDoAndWritesNothing(t *testing.T) {
	for _, args := range [][]string{{"init-hooks", "-n", "claude"}, {"init-hooks", "claude", "-n"}} {
		claudeDir := t.TempDir()
		cmd := exec.Command(build(t), args...)
		cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+claudeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("trig %v failed: %v\n%s", args, err, out)
		}
		if !strings.Contains(string(out), "would") {
			t.Errorf("trig %v should say it changed nothing:\n%s", args, out)
		}
		if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("trig %v wrote %s/settings.json", args, claudeDir)
		}
	}
}

func TestInitHooksRefusesAnUnknownAgentAndNamesTheKnownOnes(t *testing.T) {
	for _, args := range [][]string{{"init-hooks"}, {"init-hooks", "codex"}, {"init-hooks", "claude", "extra"}} {
		cmd := exec.Command(build(t), args...)
		cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+t.TempDir())
		out, err := cmd.CombinedOutput()

		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("trig %v should exit non-zero, got %v\n%s", args, err, out)
		}
		if !strings.Contains(string(out), "claude") {
			t.Errorf("trig %v should name the agents it knows:\n%s", args, out)
		}
	}
}

func TestDoctorReportsTheClaudeHooks(t *testing.T) {
	cmd := exec.Command(build(t), "doctor")
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(), "CLAUDE_CONFIG_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hooks that are not installed must not fail doctor: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "claude hooks") || !strings.Contains(string(out), "init-hooks") {
		t.Errorf("doctor should report the hooks and how to install them:\n%s", out)
	}
}

// The acceptance criterion end to end, minus Claude Code itself: install the
// hooks, then run the commands that were installed the way Claude Code would —
// with the node's TRIG_STATUS_FILE in the environment — and read the badges
// back off disk through the same package the map view reads them with.
func TestTheInstalledHooksReportEveryStateEndToEnd(t *testing.T) {
	bin := build(t)
	claudeDir := t.TempDir()
	install := exec.Command(bin, "init-hooks", "claude")
	install.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+claudeDir)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("init-hooks failed: %v\n%s", err, out)
	}

	statusDir := t.TempDir()
	nodeEnv := []string{
		"PATH=" + filepath.Dir(bin),
		"TRIG_STATUS_FILE=" + status.Path(statusDir, "main", "kt7m"),
	}
	for _, e := range hooks.Entries {
		command := installedCommand(t, filepath.Join(claudeDir, "settings.json"), e.Event)
		hook := exec.Command("sh", "-c", command)
		hook.Env = nodeEnv
		if out, err := hook.CombinedOutput(); err != nil {
			t.Fatalf("the %s hook failed inside a node: %v\n%s", e.Event, err, out)
		}

		reports, err := status.Read(statusDir, "main")
		if err != nil {
			t.Fatalf("reading the status directory: %v", err)
		}
		if got := reports["kt7m"].State; got != e.State {
			t.Errorf("after the %s hook the node reports %q, want %q", e.Event, got, e.State)
		}
	}
}

// installedCommand digs the command Claude Code would run for one event out of
// the settings file, so the test above runs what was actually installed rather
// than what this package thinks it installed.
func installedCommand(t *testing.T, settingsPath, event string) string {
	t.Helper()
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings file is not JSON: %v\n%s", err, raw)
	}
	for _, group := range settings.Hooks[event] {
		for _, entry := range group.Hooks {
			if strings.Contains(entry.Command, "emit-status") {
				return entry.Command
			}
		}
	}
	t.Fatalf("no emit-status command installed on %s:\n%s", event, raw)
	return ""
}
