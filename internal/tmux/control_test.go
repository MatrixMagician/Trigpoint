package tmux

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestChangedNamesOnlyTheSessionsThatMoved(t *testing.T) {
	last := map[string]string{
		SessionName("main", "aaa"): "100",
		SessionName("main", "bbb"): "100",
	}
	moved, now := changed(last, "%subscription-changed act $0 - - - : "+
		SessionName("main", "aaa")+"=100 "+SessionName("main", "bbb")+"=103 ")

	if len(moved) != 1 || moved[0] != SessionName("main", "bbb") {
		t.Errorf("only bbb moved, got %v", moved)
	}
	if now[SessionName("main", "bbb")] != "103" {
		t.Errorf("the new value should be carried forward, got %v", now)
	}
	if _, still := now[SessionName("main", "ccc")]; still {
		t.Error("a session missing from the value should not survive in the diff")
	}
}

func TestChangedIgnoresSessionsOutsideThePrefix(t *testing.T) {
	moved, _ := changed(map[string]string{}, "%subscription-changed act $0 - - - : scratch=100 ")
	for _, name := range moved {
		if !Ours(name) {
			t.Errorf("changed reported %q, which is not Trigpoint's", name)
		}
	}
}

func TestChangedSurvivesALineItCannotRead(t *testing.T) {
	// A session outside the prefix can be named anything at all, and its name
	// travels in the same value as Trigpoint's own.
	for _, line := range []string{
		"%subscription-changed act $0 - - - :",
		"%subscription-changed",
		"%subscription-changed act $0 - - - : a : b=1 " + SessionName("main", "aaa") + "=7 ",
	} {
		if _, now := changed(map[string]string{}, line); now == nil {
			t.Errorf("changed(%q) returned no state to carry forward", line)
		}
	}
}

func TestCaptureReturnsRecentOutputWithTheColourItWasWrittenIn(t *testing.T) {
	c := testCLI(t)
	name := SessionName("main", "k4f2")
	if err := c.Create(name, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Written straight to the pane rather than typed, so the assertion is about
	// the capture and not about whichever shell the machine happens to run.
	if err := c.run("send-keys", "-t", "="+name+":", `printf '\033[31mscarlet\033[0m\n'`, "Enter"); err != nil {
		t.Fatalf("send-keys: %v", err)
	}

	// The colour is what says the shell ran the line rather than merely echoing
	// it: the command as typed contains the word, but only its output is red.
	var out string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := c.Capture(name, 6)
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if strings.Contains(got, "\x1b[31m") {
			out = got
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if out == "" {
		t.Fatal("the capture never kept the colour the output was written in")
	}
	if !strings.Contains(out, "scarlet") {
		t.Errorf("the capture should show the recent output, got %q", out)
	}
}

func TestCaptureRefusesToGuessWhichSessionWasMeant(t *testing.T) {
	c := testCLI(t)
	long := SessionName("main", "k4f2long")
	if err := c.Create(long, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The short name is a prefix of the live one. A prefix match would hand
	// back another node's output under this node's title.
	if _, err := c.Capture(SessionName("main", "k4f2"), 4); err == nil {
		t.Error("capturing a session that does not exist should fail, not match a longer name")
	}
}

func TestCaptureOfNothingAsksTmuxNothing(t *testing.T) {
	// A card with no preview lines — size S, or a note — opts out of being
	// captured at all, so this must not need a tmux server to answer.
	out, err := CLI{Socket: "trig-test-no-server-at-all"}.Capture(SessionName("main", "k4f2"), 0)
	if err != nil || out != "" {
		t.Errorf("Capture(_, 0) = %q, %v; want no output and no failure", out, err)
	}
}

// watching starts a monitor that is stopped when the test ends.
func watching(t *testing.T, c CLI) <-chan Event {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return c.Watch(ctx)
}

// awaitEvent waits for an event the test is looking for, poking tmux on a timer
// meanwhile. The poke is repeated rather than done once because activity is
// reported at one-second resolution: output landing in the same second as the
// subscription's last value is a change tmux has nothing to report.
func awaitEvent(t *testing.T, events <-chan Event, poke func(), want func(Event) bool) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	poking := time.NewTicker(400 * time.Millisecond)
	defer poking.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("the event stream closed before the event arrived")
			}
			if want(ev) {
				return
			}
		case <-poking.C:
			if poke != nil {
				poke()
			}
		case <-deadline:
			t.Fatal("timed out waiting for the event")
		}
	}
}

func TestWatchPushesActivityNamingTheSessionThatMoved(t *testing.T) {
	c := testCLI(t)
	// Named so that the monitor's own choice — first in name order — is the one
	// the test is not watching, which is what proves activity is reported for
	// sessions the control client is not attached to.
	attached, noisy := SessionName("main", "aaa"), SessionName("main", "zzz")
	for _, name := range []string{attached, noisy} {
		if err := c.Create(name, t.TempDir(), "", nil); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	events := watching(t, c)
	awaitEvent(t, events, func() {
		_ = c.run("send-keys", "-t", "="+noisy+":", `printf 'still here\n'`, "Enter")
	}, func(ev Event) bool { return ev.Kind == Activity && ev.Session == noisy })
}

func TestWatchReportsTheSessionListChanging(t *testing.T) {
	c := testCLI(t)
	first := SessionName("main", "aaa")
	if err := c.Create(first, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	events := watching(t, c)
	// The connection itself reports one, so the test waits for a second: the
	// one caused by a session that was not there when the client connected.
	awaitEvent(t, events, nil, func(ev Event) bool { return ev.Kind == Sessions })

	if err := c.Create(SessionName("main", "zzz"), t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitEvent(t, events, nil, func(ev Event) bool { return ev.Kind == Sessions })
}

func TestWatchReconnectsWhenTheSessionItWasWatchingDies(t *testing.T) {
	c := testCLI(t)
	attached, survivor := SessionName("main", "aaa"), SessionName("main", "zzz")
	for _, name := range []string{attached, survivor} {
		if err := c.Create(name, t.TempDir(), "", nil); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	events := watching(t, c)
	awaitEvent(t, events, nil, func(ev Event) bool { return ev.Kind == Sessions })

	// The client is attached to the first session in name order, so killing it
	// takes the control client down with it. Anything heard afterwards can only
	// have come through a connection the monitor made for itself.
	if err := c.Kill(attached); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	awaitEvent(t, events, func() {
		_ = c.run("send-keys", "-t", "="+survivor+":", `printf 'still here\n'`, "Enter")
	}, func(ev Event) bool { return ev.Kind == Activity && ev.Session == survivor })
}

func TestWatchWaitsForASessionToWatchRatherThanGivingUp(t *testing.T) {
	c := testCLI(t)
	// Nothing to attach to yet: the map is empty, which is how Trigpoint starts.
	events := watching(t, c)

	name := SessionName("main", "aaa")
	if err := c.Create(name, t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	awaitEvent(t, events, nil, func(ev Event) bool { return ev.Kind == Sessions })
}

func TestWatchStopsWithItsContext(t *testing.T) {
	c := testCLI(t)
	if err := c.Create(SessionName("main", "aaa"), t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := c.Watch(ctx)
	awaitEvent(t, events, nil, func(ev Event) bool { return ev.Kind == Sessions })

	cancel()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				// The client it left behind must be gone too, or Trigpoint
				// would leak a tmux client per run.
				clients, err := exec.Command("tmux", "-L", c.Socket, "list-clients", "-F", "#{client_name}").Output()
				if err == nil && strings.TrimSpace(string(clients)) != "" {
					t.Errorf("a control client outlived its context:\n%s", clients)
				}
				return
			}
		case <-deadline:
			t.Fatal("the event stream never closed after its context was cancelled")
		}
	}
}

func TestChangedSurvivesASessionNameWithASpace(t *testing.T) {
	// The subscription loops over every session on the server, and a session
	// outside Trigpoint's prefix can be called anything — including something
	// with a space in it, which the value's own separator is.
	ours := SessionName("main", "aaa")
	moved, now := changed(map[string]string{}, "%subscription-changed act $0 - - - : my scratch=100 "+ours+"=103 ")

	if len(moved) != 1 || moved[0] != ours {
		t.Errorf("a stranger's name should not cost us our own entry, got %v", moved)
	}
	if now[ours] != "103" {
		t.Errorf("our session's activity should have been read, got %v", now)
	}
}

func TestFirstSessionNeverReturnsHalfAName(t *testing.T) {
	c := testCLI(t)
	// Created behind Trigpoint's back — ValidName would refuse the workspace
	// name, but a session on the server is not Trigpoint's to police.
	spaced := Prefix + "a b"
	if err := c.run("new-session", "-d", "-s", spaced); err != nil {
		t.Fatalf("new-session: %v", err)
	}
	if err := c.Create(SessionName("main", "zzz"), t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Whatever it picks has to be a session that is actually there. Reading the
	// list as words rather than lines yields "trig_a", which is not.
	got := c.firstSession()
	alive, err := c.Exists(got)
	if err != nil || !alive {
		t.Errorf("firstSession returned %q, which is not a live session (alive=%v err=%v)", got, alive, err)
	}
}

func TestFollowingASessionThatIsNotThereReportsNoConnection(t *testing.T) {
	c := testCLI(t)
	if err := c.Create(SessionName("main", "aaa"), t.TempDir(), "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	out := make(chan Event, 8)

	// tmux answers a doomed attach with a %begin/%error/%exit block, which is
	// output — so a client counted as connected the moment it says anything
	// would report a stream that never carried one, and Watch would retry at
	// the shortest backoff for as long as the session stayed missing.
	if c.follow(t.Context(), SessionName("main", "nosuch"), out) {
		t.Error("an attach that failed should not count as a connection")
	}
	select {
	case ev := <-out:
		t.Errorf("a failed attach should push nothing, got %#v", ev)
	default:
	}
}
