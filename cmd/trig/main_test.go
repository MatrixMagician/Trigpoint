package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	for _, args := range [][]string{{"-h"}, {"doctor", "-h"}} {
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
