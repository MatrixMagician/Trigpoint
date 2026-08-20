package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	for _, want := range []string{"tmux", "control mode", "config", "state directory"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("doctor output does not mention %q:\n%s", want, out)
		}
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
	for _, args := range [][]string{{"-h"}, {"doctor", "-h"}, {"emit-status", "-h"}} {
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
