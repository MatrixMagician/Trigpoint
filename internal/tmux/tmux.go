// Package tmux is Trigpoint's claim over the sessions behind its nodes. The
// claim is a naming convention and nothing stronger: every session this package
// creates is named under Prefix, and every session it modifies must already be.
// A session outside the prefix belongs to someone else and is never touched.
package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Prefix marks a session as Trigpoint's. See SPEC §5.2.
const Prefix = "trig_"

// SessionName is the tmux name for a node's session. Human-readable titles live
// in Trigpoint's state, not in tmux, so this carries only identity: which
// workspace the node belongs to and which node it is.
func SessionName(workspace, nodeID string) string {
	return Prefix + workspace + "_" + nodeID
}

// Ours reports whether a session name falls inside Trigpoint's prefix.
func Ours(session string) bool { return strings.HasPrefix(session, Prefix) }

// CLI talks to tmux by shelling out to it. Socket names a private tmux server
// (`tmux -L`); empty means the default one, which is what Trigpoint uses.
type CLI struct{ Socket string }

func (c CLI) command(args ...string) *exec.Cmd {
	if c.Socket != "" {
		args = append([]string{"-L", c.Socket}, args...)
	}
	return exec.Command("tmux", args...)
}

// Create starts a detached session in dir, seeding its session environment with
// env so a later reconciliation pass can recognise the session on its own,
// without consulting Trigpoint's state.
func (c CLI) Create(session, dir string, env map[string]string) error {
	if err := mustBeOurs(session, "create"); err != nil {
		return err
	}
	args := []string{"new-session", "-d", "-s", session}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+env[k])
	}
	return c.run(args...)
}

// Kill removes a session. Closing Trigpoint kills nothing; only this does.
//
// A session can die without Trigpoint — a reboot, a `tmux kill-server`, or
// `exit` typed inside it — so killing one that is already gone is the outcome
// that was asked for rather than a failure. That is decided from what
// kill-session itself reports rather than by asking first, because a session
// that dies between the asking and the killing would slip through the gap.
func (c CLI) Kill(session string) error {
	if err := mustBeOurs(session, "kill"); err != nil {
		return err
	}
	err := c.run("kill-session", "-t", "="+session)
	if err != nil && alreadyGone(err) {
		return nil
	}
	return err
}

// alreadyGone recognises tmux complaining about something that is not there:
// the session, or the server that would have held it.
func alreadyGone(err error) bool {
	msg := err.Error()
	for _, absent := range []string{"can't find session", "session not found", "no server running", "error connecting to"} {
		if strings.Contains(msg, absent) {
			return true
		}
	}
	return false
}

// Exists reports whether a session is alive. No tmux server running means
// nothing is alive, which is an answer rather than a failure.
func (c CLI) Exists(session string) (bool, error) {
	err := c.command("has-session", "-t", "="+session).Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, fmt.Errorf("asking tmux about session %q: %w", session, err)
}

func mustBeOurs(session, verb string) error {
	if !Ours(session) {
		return fmt.Errorf("refusing to %s session %q: it is outside Trigpoint's %q prefix", verb, session, Prefix)
	}
	return nil
}

// run reports tmux's own complaint rather than a bare exit status, which on its
// own would send the user hunting. With no complaint to pass on it names the
// subcommand — never the last argument, which for Create is an environment
// value and has no business in a status bar.
func (c CLI) run(args ...string) error {
	cmd := c.command(args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("tmux: %s", detail)
		}
		return fmt.Errorf("tmux %s: %w", args[0], err)
	}
	return nil
}
