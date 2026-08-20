//go:build linux

package main

// The acceptance criterion `trig attach` cannot be shown without a terminal:
// that it performs the same handoff as the map (SPEC §5.1) — the whole terminal
// goes to the session, and the detach binding installed for the duration hands
// it back. So this runs the real command on a real pty against a real tmux
// server. The map's own side of the same handoff is covered by
// internal/tui/handoff_pty_test.go.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAttachHandsTheTerminalOverAndTakesItBack(t *testing.T) {
	c := newCLI(t)
	c.mustRun(t, "new", "-t", "ptytest")
	ws := c.workspace(t, "main")
	session := ws.SessionOf(ws.Nodes[0])

	ptmx, pts := openPty(t)
	attach := exec.Command(c.bin, "attach", "ptytest")
	attach.Env = append(c.env, "TERM=xterm")
	attach.Stdin, attach.Stdout, attach.Stderr = pts, pts, pts
	// The attach needs a controlling terminal of its own, or tmux refuses it
	// the way it refuses one from a pipe.
	attach.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := attach.Start(); err != nil {
		t.Fatalf("starting the attach: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- attach.Wait() }()
	t.Cleanup(func() { _ = attach.Process.Kill() })

	// What tmux draws has to be read, or the pty's buffer fills and stops it
	// being written to at all — and the one question it asks has to be answered,
	// or tmux spends five seconds waiting for the terminal to prove it is there.
	go answerAsATerminal(ptmx)

	waitUntil(t, "the terminal never reached the session", func() bool {
		out, err := exec.Command("tmux", "-L", c.socket, "list-clients", "-t", "="+session).Output()
		return err == nil && strings.TrimSpace(string(out)) != ""
	})

	if _, err := ptmx.WriteString("\x1b\x1b"); err != nil { // M-Escape, the default detach key
		t.Fatalf("typing the detach key: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the detach key should end the attach cleanly: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the detach key did not give the terminal back")
	}

	// The binding comes out with the attach: left installed, the detach key
	// would fire on a terminal that has nothing to detach.
	bound, err := exec.Command("tmux", "-L", c.socket, "list-keys", "-T", "root").Output()
	if err == nil && strings.Contains(string(bound), "M-Escape") {
		t.Errorf("the detach binding outlived the attach:\n%s", bound)
	}
	// And the session is still there: detaching is leaving, not killing.
	if out := c.tmux(t, "list-sessions", "-F", "#{session_name}"); !strings.Contains(out, session) {
		t.Errorf("detaching should leave the session running, got:\n%s", out)
	}
}

// answerAsATerminal reads what is drawn and replies to the queries tmux puts to
// the terminal on the way in. Unanswered, they are not fatal — tmux carries on
// once it has waited them out — but the wait is seconds long, and a terminal
// that never answers is not the thing being tested here.
func answerAsATerminal(ptmx *os.File) {
	replies := []struct{ query, answer string }{
		{"\x1b[6n", "\x1b[1;1R"},   // where is the cursor
		{"\x1b[c", "\x1b[?62;22c"}, // what are you
	}
	buf := make([]byte, 65536)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			drawn := string(buf[:n])
			for _, r := range replies {
				for range strings.Count(drawn, r.query) {
					ptmx.WriteString(r.answer)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// openPty is a bare pty pair; answerAsATerminal is the whole of the emulator
// behind it.
func openPty(t *testing.T) (ptmx, pts *os.File) {
	t.Helper()
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlocking the pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("naming the pty: %v", err)
	}
	pts, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening the pty: %v", err)
	}
	if err := unix.IoctlSetWinsize(int(ptmx.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 120}); err != nil {
		t.Fatalf("sizing the pty: %v", err)
	}
	t.Cleanup(func() { pts.Close(); ptmx.Close() })
	return ptmx, pts
}

func waitUntil(t *testing.T, complaint string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(complaint)
}
