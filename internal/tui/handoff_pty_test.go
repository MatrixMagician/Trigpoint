//go:build linux

package tui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/unix"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// TestHandoffRoundTripOnARealTerminal runs the whole handoff against a real
// pty and a real tmux server: create a node, Enter into it, press the detach
// key, come back, quit. SPEC §14 calls residual raw-mode corruption
// release-blocking, so the terminal's own settings are compared either side of
// the round trip — the one property that cannot be checked by looking at the
// screen. What is left for docs/handoff-test-matrix.md is what only a real
// emulator can show: colours, reflow, and scrollback.
func TestHandoffRoundTripOnARealTerminal(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	cli := tmux.CLI{Socket: "trig-test-pty"}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", cli.Socket, "kill-server").Run() })

	term := openTerminal(t)
	settled, err := unix.IoctlGetTermios(int(term.pts.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("reading the terminal's settings: %v", err)
	}

	ws := state.Workspace{Name: "main", Dir: t.TempDir()}
	stateDir := t.TempDir()
	prog := tea.NewProgram(New(config.Default(), ws, stateDir, cli),
		tea.WithAltScreen(), tea.WithInput(term.pts), tea.WithOutput(term.pts))
	done := make(chan error, 1)
	go func() { _, err := prog.Run(); done <- err }()

	term.waitFor(t, "map is empty", "the map never appeared")
	term.type_("n")
	term.waitFor(t, "Title", "n did not prompt for a title")
	term.type_("ptytest\r")
	term.waitFor(t, "ptytest", "the node never reached the map")

	// The node's id is a random slug, so it is read back from the workspace the
	// map has just written rather than guessed.
	saved, err := state.Load(stateDir, "main")
	if err != nil || len(saved.Nodes) != 1 {
		t.Fatalf("reading back the workspace: %v, %d nodes", err, len(saved.Nodes))
	}

	term.type_("\r") // Enter: the whole terminal goes to the node's session
	waitForClients(t, cli, tmux.SessionName("main", saved.Nodes[0].ID), 1, "the handoff never took hold")

	term.forget()
	term.type_("\x1b\x1b") // M-Escape, the default detach key
	term.waitFor(t, "ptytest", "the detach key did not bring the map back")

	term.type_("q")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the programme ended badly: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("q did not quit")
	}

	after, err := unix.IoctlGetTermios(int(term.pts.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatalf("reading the terminal's settings back: %v", err)
	}
	if *after != *settled {
		t.Errorf("the terminal was not handed back as it was found:\n before %+v\n after  %+v", *settled, *after)
	}
}

// terminal is a pty with just enough of an emulator behind it to answer the
// questions Bubble Tea asks on startup — without an answer it waits, and the
// programme never draws anything.
type terminal struct {
	ptmx, pts *os.File

	mu   sync.Mutex
	seen bytes.Buffer
}

func openTerminal(t *testing.T) *terminal {
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
	pts, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening the pty: %v", err)
	}
	if err := unix.IoctlSetWinsize(int(ptmx.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 120}); err != nil {
		t.Fatalf("sizing the pty: %v", err)
	}
	term := &terminal{ptmx: ptmx, pts: pts}
	t.Cleanup(func() { pts.Close(); ptmx.Close() })
	go term.answer()
	return term
}

// answer reads what is drawn and replies to the queries a real terminal would.
func (term *terminal) answer() {
	replies := []struct{ query, answer string }{
		{"\x1b]11;?\x1b\\", "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\"}, // background colour
		{"\x1b]11;?\x07", "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\"},
		{"\x1b[6n", "\x1b[1;1R"},   // cursor position
		{"\x1b[c", "\x1b[?62;22c"}, // device attributes
	}
	buf := make([]byte, 65536)
	for {
		n, err := term.ptmx.Read(buf)
		if n > 0 {
			drawn := string(buf[:n])
			term.mu.Lock()
			term.seen.WriteString(drawn)
			term.mu.Unlock()
			for _, r := range replies {
				for range strings.Count(drawn, r.query) {
					term.ptmx.WriteString(r.answer)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (term *terminal) type_(keys string) { term.ptmx.WriteString(keys) }

// plainText drops the escape sequences, leaving what is actually on the screen.
func plainText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		for i++; i < len(s); i++ { // a sequence ends at its final byte
			if c := s[i]; c >= 0x40 && c <= 0x7e && c != '[' && c != ']' {
				break
			}
		}
	}
	return b.String()
}

// forget drops what has been drawn so far, so that waiting for something to
// appear cannot be satisfied by an older frame that already said it.
func (term *terminal) forget() {
	term.mu.Lock()
	defer term.mu.Unlock()
	term.seen.Reset()
}

func (term *terminal) waitFor(t *testing.T, text, complaint string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		term.mu.Lock()
		drawn := term.seen.String()
		term.mu.Unlock()
		if strings.Contains(plainText(drawn), text) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: waiting for %q", complaint, text)
}

// waitForClients waits for session to have want clients attached; tmux does its
// own work in its own time.
func waitForClients(t *testing.T, cli tmux.CLI, session string, want int, complaint string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "-L", cli.Socket,
			"list-sessions", "-F", "#{session_name} #{session_attached}").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				name, clients, _ := strings.Cut(strings.TrimSpace(line), " ")
				if name == session && clients == fmt.Sprint(want) {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(complaint)
}
