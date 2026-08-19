package tmux

// The event stream and the previews it drives (SPEC §5.3). One long-lived
// control-mode client says *when* a card went stale; capture-pane says what is
// now on it. The two are deliberately separate: the client carries no pane
// output at all, so the bytes a busy node produces never travel through
// Trigpoint — only the fact that it produced some.
//
// See docs/adr/0005-activity-arrives-as-a-format-subscription.md for why
// activity arrives as a format subscription rather than as %output.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Capture is a preview: the recent output of a session, with the colour it was
// written in. It reads a session rather than changing it, so there is no prefix
// check — adopted sessions (§9.3) are previewed the same way Trigpoint's own are.
//
// Asking for no lines asks tmux nothing: a card that shows no preview — size S,
// or a note — opts out of being captured at all.
func (c CLI) Capture(session string, lines int) (string, error) {
	if lines <= 0 {
		return "", nil
	}
	// "=name:" is the exact-match form capture-pane takes. A bare "=name" is
	// refused, because this is a pane target and not a session one; a bare name
	// is a prefix match, which after a kill can quietly resolve to a longer-named
	// session and show one node's output under another node's title.
	cmd := c.command("capture-pane", "-p", "-e", "-t", "="+session+":", "-S", "-"+strconv.Itoa(lines))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("tmux: %s", detail)
		}
		return "", fmt.Errorf("capturing %s: %w", session, err)
	}
	return string(out), nil
}

// EventKind is what tmux had to say.
type EventKind int

const (
	// Activity says a session produced output, so the card over it is a
	// snapshot of something that has moved on.
	Activity EventKind = iota
	// Sessions says the set of sessions changed — or that the event stream
	// itself did. A fresh connection reports one, because everything that
	// happened while there was no client to hear it is indistinguishable from
	// everything.
	Sessions
)

// Event is one push from the control-mode client.
type Event struct {
	Kind    EventKind
	Session string // the session that moved; empty for Sessions
}

const (
	// subscribe asks tmux to push a line whenever any session's activity time
	// moves. The loop body appears twice because #{S:a,b} uses the second form
	// for the session the client is attached to — with one form the client's
	// own session is the one node whose activity is never reported.
	subscribe = `refresh-client -B "act::#{S:#{session_name}=#{window_activity} ,#{session_name}=#{window_activity} }"`

	// clientFlags are what make a monitor harmless. ignore-size keeps it out of
	// the window-size calculation, so attaching does not resize the work going
	// on inside the session; no-output keeps every pane's bytes off the wire,
	// which is the difference between a monitor and a terminal emulator.
	clientFlags = "ignore-size,no-output"

	// events buffers what the map has not read yet. It is never waited on — see
	// push — so this is how much history survives a busy moment, not a queue.
	events = 64

	minBackoff = 500 * time.Millisecond
	maxBackoff = 10 * time.Second
)

// Watch keeps one control-mode client alive for as long as ctx runs and pushes
// what it hears onto the returned channel, which is closed when ctx is done.
//
// The connection is expected to drop: the client attaches to one of Trigpoint's
// own sessions, and killing that node takes the client with it. Losing it is
// therefore ordinary rather than exceptional — the monitor picks another session
// and reconnects, backing off while there is nothing to attach to, which is also
// the state a map with no live nodes starts in.
func (c CLI) Watch(ctx context.Context) <-chan Event {
	out := make(chan Event, events)
	go func() {
		defer close(out)
		for wait := time.Duration(0); ctx.Err() == nil; {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			if session := c.firstSession(); session != "" && c.follow(ctx, session, out) {
				// A connection that was made and then lost is worth retrying
				// straight away: the usual cause is a node being killed, and
				// the other nodes are still there to watch.
				wait = minBackoff
				continue
			}
			wait = backoff(wait)
		}
	}()
	return out
}

func backoff(d time.Duration) time.Duration {
	switch {
	case d < minBackoff:
		return minBackoff
	case d*2 > maxBackoff:
		return maxBackoff
	default:
		return d * 2
	}
}

// follow runs one control-mode client on session until it ends, reporting
// whether it ever connected — which is what tells a client that died apart from
// one that never took, and so how long Watch waits before trying again.
func (c CLI) follow(ctx context.Context, session string, out chan<- Event) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := c.commandContext(ctx, "-C", "attach-session", "-t", "="+session, "-f", clientFlags)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	// The pipe stays open for the client's whole life: tmux reads commands from
	// it, and closing it is how the client is told to exit.
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()
	if _, err := io.WriteString(stdin, subscribe+"\n"); err != nil {
		return false
	}

	last, seeded, connected := map[string]string{}, false, false
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		line := lines.Text()
		switch {
		case strings.HasPrefix(line, "%session-changed"):
			// The one line that says the attach took. An attach that did not
			// still talks — tmux answers a missing session with a
			// %begin/%error/%exit block — so counting any output as a
			// connection would report a stream that never carried one, and
			// would leave Watch retrying at the shortest backoff for as long as
			// the session stayed missing.
			//
			// A client that has only just connected knows nothing about what
			// changed while there was none, so it says so rather than guessing.
			connected = true
			push(out, Event{Kind: Sessions})

		case !connected:
			// Anything said before the attach took is about the attaching.

		case strings.HasPrefix(line, "%subscription-changed "):
			moved, now := changed(last, line)
			last = now
			if !seeded {
				// The first value is the state of the world on connecting, not
				// news about it — the Sessions event above already said that
				// everything is worth a fresh look.
				seeded = true
				continue
			}
			for _, session := range moved {
				push(out, Event{Kind: Activity, Session: session})
			}
		case strings.HasPrefix(line, "%sessions-changed"):
			push(out, Event{Kind: Sessions})
		case strings.HasPrefix(line, "%exit"):
			return connected
		}
	}
	// However the reading ended — end of stream, or a broken pipe — the client
	// is over, and reconnecting is the same answer to both.
	_ = lines.Err()
	return connected
}

// changed diffs one %subscription-changed line against the values last seen,
// naming the sessions whose activity moved and returning the state to carry
// forward. Sessions absent from the line are dropped: they are gone, and a map
// that only grows would outlive every node a long-running map ever had.
func changed(last map[string]string, line string) (moved []string, now map[string]string) {
	now = make(map[string]string, len(last))
	// The header is a fixed five fields, so the first " : " is the separator —
	// whatever a session outside Trigpoint's prefix happens to be called.
	_, value, ok := strings.Cut(line, " : ")
	if !ok {
		return nil, now
	}
	for pair := range strings.FieldsSeq(value) {
		name, at, split := strings.Cut(pair, "=")
		// The subscription loops over every session on the server, including
		// the ones that are none of Trigpoint's business.
		if !split || !Ours(name) {
			continue
		}
		if last[name] != at {
			moved = append(moved, name)
		}
		now[name] = at
	}
	return moved, now
}

// push offers an event without ever waiting for it to be taken. The map stops
// reading while the terminal is out at an attached session, and a monitor that
// blocked there would block the tmux client it is reading from, and through it
// the server everyone else's nodes are running on. A dropped event costs one
// card one slow tick.
func push(out chan<- Event, ev Event) {
	select {
	case out <- ev:
	default:
	}
}

// firstSession is a session to attach the control client to: one of Trigpoint's
// own, chosen in name order so that reconnecting is not a coin toss. No server,
// or no sessions of ours on it, is an answer rather than a failure — it is what
// an empty map looks like from here.
func (c CLI) firstSession() string {
	out, err := c.command("list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	// Split by line and not by word: a session name may hold spaces. Trigpoint
	// will not make one (state.ValidName refuses them), but the server carries
	// everyone's sessions, and half a stranger's name can still start with the
	// prefix — which would send the monitor attaching to something that is not
	// there, over and over.
	ours := []string(nil)
	for line := range strings.SplitSeq(string(out), "\n") {
		if name := strings.TrimRight(line, "\r"); Ours(name) {
			ours = append(ours, name)
		}
	}
	if len(ours) == 0 {
		return ""
	}
	sort.Strings(ours)
	return ours[0]
}

func (c CLI) commandContext(ctx context.Context, args ...string) *exec.Cmd {
	if c.Socket != "" {
		args = append([]string{"-L", c.Socket}, args...)
	}
	return exec.CommandContext(ctx, "tmux", args...)
}
