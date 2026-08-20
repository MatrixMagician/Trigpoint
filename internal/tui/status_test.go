package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// statusModel is a map with one agent node on it, and the status directory the
// map is watching. The node's id is fixed so a test can write its report by
// hand, the way an agent's hook would.
func statusModel(t *testing.T, cfg config.Config) (Model, string) {
	t.Helper()
	// Nested, so the status directory beside it is this model's alone — as it
	// is in production, where the state directory is .../trig/workspaces.
	dir := filepath.Join(t.TempDir(), "workspaces")
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "kt7m", Kind: state.KindAgent, Title: "claude", Cmd: "claude"},
	}}
	m := New(cfg, ws, dir, &fakeSessions{})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model), status.DirBeside(dir)
}

// report is what an agent's hook does: write the file and let the map find it.
func report(t *testing.T, dir, id string, st status.State, detail string) {
	t.Helper()
	if err := status.Write(status.Path(dir, "main", id), st, detail); err != nil {
		t.Fatalf("writing a %s report: %v", st, err)
	}
}

// poll runs one pass of the status tick, the way the runtime would.
func poll(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(statusTickMsg{})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("the status tick should ask for the directory to be read")
	}
	// The tick batches the read with the next tick; only the read carries a
	// message worth applying.
	for _, msg := range collect(cmd) {
		if _, ok := msg.(statusReadMsg); ok {
			after, _ := m.Update(msg)
			m = after.(Model)
		}
	}
	return m
}

// collect runs a command and gathers what it produced, flattening one level of
// batch and giving up on the parts of it that are waiting on a clock — the
// status tick batches the read it wants now with the tick a second away.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { done <- c() }(c)
		select {
		case m := <-done:
			msgs = append(msgs, m)
		case <-time.After(time.Second / 4):
		}
	}
	return msgs
}

// An agent is told where to report by its own session environment (SPEC §8):
// the contract has to be reachable from inside the node and nowhere else.
func TestAnAgentNodeIsGivenItsStatusFile(t *testing.T) {
	m, sessions, stateDir := newNodeModel(t, state.Workspace{Name: "main", Dir: "/tmp/work"})

	m, _ = typeKeys(t, m, "a")
	m, _ = press(t, m, tea.KeyEnter) // claude, the first preset
	m, cmd := press(t, m, tea.KeyEnter)
	m = settle(t, m, cmd)

	if len(sessions.created) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions.created))
	}
	want := status.Path(status.DirBeside(stateDir), "main", m.ws.Nodes[0].ID)
	if got := sessions.created[0].env["TRIG_STATUS_FILE"]; got != want {
		t.Errorf("TRIG_STATUS_FILE = %q, want %q", got, want)
	}
}

// Only an agent node reports agent status (CONTEXT.md, "Agent node"), so only
// an agent node is given somewhere to report it.
func TestAShellNodeIsGivenNoStatusFile(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Dir: "/tmp/work"})

	m, _ = typeKeys(t, m, "n")
	m, _ = typeKeys(t, m, "api")
	_, cmd := press(t, m, tea.KeyEnter)
	settle(t, m, cmd)

	if len(sessions.created) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions.created))
	}
	if got, ok := sessions.created[0].env["TRIG_STATUS_FILE"]; ok {
		t.Errorf("a shell node was given TRIG_STATUS_FILE = %q, want none", got)
	}
}

// The directory is watched, so a file written by an agent reaches the badge
// without Trigpoint being restarted.
func TestAWrittenReportReachesTheBadge(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(1)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	m, dir := statusModel(t, config.Default())
	if got := m.reports["kt7m"].State; got != "" {
		t.Fatalf("the map starts with a report of %q, want none", got)
	}

	report(t, dir, "kt7m", status.NeedsYou, "waiting for approval")
	m = poll(t, m)

	if got := m.reports["kt7m"].State; got != status.NeedsYou {
		t.Fatalf("state = %q, want %q", got, status.NeedsYou)
	}
	if !strings.Contains(m.View(), statusColours[status.NeedsYou]) {
		t.Errorf("a needs-you card should be badged in %s:\n%q", statusColours[status.NeedsYou], m.View())
	}
}

// The four states are drawn differently from one another, or the badge says
// nothing the card did not already say.
func TestTheFourStatesAreDrawnDistinctly(t *testing.T) {
	seen := map[string]status.State{}
	for _, st := range status.States {
		colour, ok := statusColours[st]
		if !ok || colour == "" {
			t.Errorf("state %q has no badge colour", st)
			continue
		}
		if other, clash := seen[colour]; clash {
			t.Errorf("states %q and %q share the badge colour %s", st, other, colour)
		}
		seen[colour] = st
	}
}

// Absence of a report is not a state (CONTEXT.md, "Agent status"): an agent
// that has never reported is unknown, and unknown is drawn as nothing.
func TestAnUnreportedAgentShowsNoAgentStatus(t *testing.T) {
	m, _ := statusModel(t, config.Default())
	m = poll(t, m)

	mark := m.badgeOf(m.ws.Nodes[0])
	if mark.glyph != liveBadge {
		t.Errorf("badge glyph = %q, want the plain live badge %q", mark.glyph, liveBadge)
	}
	if mark.colour != "" {
		t.Errorf("badge colour = %q, want none: nothing has been reported", mark.colour)
	}
}

// A report Trigpoint has stopped trusting is displayed as old, never resolved
// into a guess about what the true state became (CONTEXT.md, "Stale").
func TestAStaleRunningReportIsMarkedRatherThanResolved(t *testing.T) {
	m, _ := statusModel(t, config.Default())
	stale := status.Report{State: status.Running, TS: time.Now().Add(-time.Hour)}
	m.reports = map[string]status.Report{"kt7m": stale}

	mark := m.badgeOf(m.ws.Nodes[0])
	if !strings.HasSuffix(mark.glyph, "?") {
		t.Errorf("badge glyph = %q, want a stale mark on it", mark.glyph)
	}
	if mark.colour != statusColours[status.Running] {
		t.Errorf("badge colour = %q, want it still %q: staleness is displayed, not resolved",
			mark.colour, statusColours[status.Running])
	}
	// Only a running report goes stale: the other three are reports of
	// something that has finished happening.
	m.reports = map[string]status.Report{"kt7m": {State: status.Done, TS: stale.TS}}
	if got := m.badgeOf(m.ws.Nodes[0]).glyph; strings.HasSuffix(got, "?") {
		t.Errorf("a done report went stale (glyph %q); only running does", got)
	}
	// A report with no timestamp has no age to have outlived the window — an
	// agent writing the minimal report is not one reporting from the epoch.
	m.reports = map[string]status.Report{"kt7m": {State: status.Running}}
	if got := m.badgeOf(m.ws.Nodes[0]).glyph; strings.HasSuffix(got, "?") {
		t.Errorf("a report with no timestamp went stale (glyph %q)", got)
	}
}

// Agent status, liveness, and unread are three sources and one badge (§8): the
// dot's shape carries unread, its colour carries agent status, and a session
// that is gone outranks both — see
// docs/adr/0015-agent-status-is-a-directory-of-files-trigpoint-polls.md.
func TestTheBadgeComposesStatusLivenessAndUnread(t *testing.T) {
	node := state.Node{ID: "kt7m", Kind: state.KindAgent, Title: "claude"}
	running := map[string]status.Report{"kt7m": {State: status.Running, TS: time.Now()}}

	for _, c := range []struct {
		name    string
		dead    bool
		unread  bool
		reports map[string]status.Report
		want    badgeMark
	}{
		{"running and read", false, false, running, badgeMark{liveBadge, statusColours[status.Running]}},
		{"running and unread", false, true, running, badgeMark{unreadBadge, statusColours[status.Running]}},
		{"unread with nothing reported", false, true, nil, badgeMark{unreadBadge, ""}},
		{"dead outranks a report", true, true, running, badgeMark{deadBadge, ""}},
	} {
		m, _ := statusModel(t, config.Default())
		m.dead, m.unread, m.reports = map[string]bool{"kt7m": c.dead}, map[string]bool{"kt7m": c.unread}, c.reports
		if got := m.badgeOf(node); got != c.want {
			t.Errorf("%s: badge = %+v, want %+v", c.name, got, c.want)
		}
	}

	// A note has no session, so it has neither liveness nor agent status to
	// report and carries no badge at all.
	m, _ := statusModel(t, config.Default())
	if got := m.badgeOf(state.Node{ID: "nn", Kind: state.KindNote}); got != (badgeMark{}) {
		t.Errorf("a note carries badge %+v, want none", got)
	}
}

// `u` jumps to the next node needing attention, needs-you before unread (§7.3).
func TestUJumpsToNeedsYouBeforeUnread(t *testing.T) {
	dir := t.TempDir()
	ws := state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaaa", Kind: state.KindShell, Title: "quiet", Pos: state.Cell{Col: 0}},
		{ID: "bbbb", Kind: state.KindShell, Title: "noisy", Pos: state.Cell{Col: 1}},
		{ID: "cccc", Kind: state.KindAgent, Title: "claude", Pos: state.Cell{Col: 2}},
	}}
	m := New(config.Default(), ws, dir, &fakeSessions{})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = sized.(Model)
	m.unread = map[string]bool{"bbbb": true, "cccc": true}
	m.reports = map[string]status.Report{"cccc": {State: status.NeedsYou, TS: time.Now()}}

	m, _ = typeKeys(t, m, "u")
	if got := m.ws.Viewport.Cursor.Col; got != 2 {
		t.Fatalf("u landed on column %d, want the needs-you node at 2", got)
	}
	// Pressing it again moves on to the unread one rather than sticking.
	m, _ = typeKeys(t, m, "u")
	if got := m.ws.Viewport.Cursor.Col; got != 1 {
		t.Errorf("u landed on column %d, want the unread node at 1", got)
	}
}

// Nothing needing attention is nowhere to jump to: the cursor stays where the
// user left it rather than being moved to something arbitrary.
func TestUWithNothingNeedingAttentionMovesNothing(t *testing.T) {
	m, _ := statusModel(t, config.Default())
	was := m.ws.Viewport.Cursor

	m, _ = typeKeys(t, m, "u")

	if m.ws.Viewport.Cursor != was {
		t.Errorf("u moved the cursor to %v with nothing to jump to, want it left at %v",
			m.ws.Viewport.Cursor, was)
	}
}

// The bell is off unless it is asked for, and it rings on the transition rather
// than on every pass over a report that has not changed.
func TestTheBellRingsOnceOnNeedsYouAndOnlyWhenConfigured(t *testing.T) {
	var rung strings.Builder
	old := bellOut
	bellOut = &rung
	t.Cleanup(func() { bellOut = old })

	quiet, dir := statusModel(t, config.Default())
	ring(t, quiet)
	report(t, dir, "kt7m", status.NeedsYou, "waiting")
	ring(t, quiet)
	if rung.Len() != 0 {
		t.Errorf("the bell rang %q with bell_on_needs_you off, want silence", rung.String())
	}

	cfg := config.Default()
	cfg.General.BellOnNeedsYou = true
	loud, dir := statusModel(t, cfg)
	loud = ring(t, loud) // the first pass, which is the map catching up

	report(t, dir, "kt7m", status.NeedsYou, "waiting")
	loud = ring(t, loud)
	if got := rung.String(); got != "\a" {
		t.Fatalf("the bell rang %q on a needs-you report, want one \\a", got)
	}
	// The same report on the next pass is not news.
	ring(t, loud)
	if got := rung.String(); got != "\a" {
		t.Errorf("the bell rang %q over two passes of one report, want one \\a", got)
	}
}

// The first pass over the directory is the map catching up with what is already
// there — a launch, or a workspace arrived back at — not an agent asking for
// you while you watched. Ringing for it would ring on every Tab.
func TestTheBellIsSilentForWhatWasAlreadyOnDisk(t *testing.T) {
	var rung strings.Builder
	old := bellOut
	bellOut = &rung
	t.Cleanup(func() { bellOut = old })

	cfg := config.Default()
	cfg.General.BellOnNeedsYou = true
	m, dir := statusModel(t, cfg)
	report(t, dir, "kt7m", status.NeedsYou, "waiting")

	m = ring(t, m)
	if rung.Len() != 0 {
		t.Fatalf("the bell rang %q for a report that was on disk before the map read it", rung.String())
	}
	// And again after a workspace round trip, which reloads everything.
	back, _ := m.open("main")
	ring(t, back)
	if rung.Len() != 0 {
		t.Errorf("the bell rang %q on coming back to the map, want silence", rung.String())
	}
}

// A status file with no node on the map behind it is somebody else's leftover:
// a workspace deleted while its sessions kept running, or a node killed while
// its agent was still writing. The bell is a summons to a card, so a report with
// no card must not ring it — `u` cannot jump to what is not on the map.
func TestTheBellIsSilentForAReportWithNoNodeOnTheMap(t *testing.T) {
	var rung strings.Builder
	old := bellOut
	bellOut = &rung
	t.Cleanup(func() { bellOut = old })

	cfg := config.Default()
	cfg.General.BellOnNeedsYou = true
	m, dir := statusModel(t, cfg)
	m = ring(t, m) // the first pass, which is the map catching up

	report(t, dir, "zzzz", status.NeedsYou, "nobody's node")
	m = ring(t, m)
	if rung.Len() != 0 {
		t.Fatalf("the bell rang %q for a report with no node behind it", rung.String())
	}

	// And a report that does have a node still rings, so this is a filter and
	// not the bell switched off.
	report(t, dir, "kt7m", status.NeedsYou, "waiting")
	ring(t, m)
	if got := rung.String(); got != "\a" {
		t.Errorf("the bell rang %q for a node on the map, want one \\a", got)
	}
}

// ring is poll with the bell command actually run, which is what the runtime
// would do with it.
func ring(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(statusTickMsg{})
	m = next.(Model)
	for _, msg := range collect(cmd) {
		if _, ok := msg.(statusReadMsg); !ok {
			continue
		}
		after, cmd := m.Update(msg)
		m = after.(Model)
		collect(cmd)
	}
	return m
}

// Ids come back round, so a node's report goes with the node — or the next node
// handed that id would wear a badge somebody else earned.
func TestRemovingANodeRemovesItsReport(t *testing.T) {
	m, dir := statusModel(t, config.Default())
	report(t, dir, "kt7m", status.NeedsYou, "waiting")
	m = poll(t, m)

	m, _ = typeKeys(t, m, "x")
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	reports, err := status.Read(dir, "main")
	if err != nil {
		t.Fatalf("reading the status directory: %v", err)
	}
	if _, left := reports["kt7m"]; left {
		t.Errorf("the killed node's report is still on disk: %v", reports)
	}
	if _, held := m.reports["kt7m"]; held {
		t.Errorf("the map is still holding the killed node's report: %v", m.reports)
	}
}

// A respawn starts a fresh session for the node (§9.2), so what the last one's
// agent said about itself is not a report about anything that exists any more.
func TestRespawningDropsTheReportTheOldSessionLeft(t *testing.T) {
	m, dir := statusModel(t, config.Default())
	report(t, dir, "kt7m", status.NeedsYou, "waiting")
	m = poll(t, m)
	m.dead = map[string]bool{"kt7m": true}

	next, _ := m.Update(respawnedMsg{mapStamp: m.stamp(), id: "kt7m"})
	m = next.(Model)

	if _, held := m.reports["kt7m"]; held {
		t.Errorf("the map is still holding the dead agent's report: %v", m.reports)
	}
	// On disk too, or the next poll would put it straight back.
	if reports, _ := status.Read(dir, "main"); len(reports) != 0 {
		t.Errorf("the dead agent's report is still on disk: %v", reports)
	}
	if got := m.badgeOf(m.ws.Nodes[0]); got.colour != "" {
		t.Errorf("the respawned node is badged %+v, want nothing reported about it yet", got)
	}
}

// A dead node's badge is dead and nothing else (badgeOf), and the status bar is
// the same report in words — so it says nothing rather than describing an agent
// whose session has gone.
func TestNothingIsSaidAboutTheAgentOfADeadNode(t *testing.T) {
	m, dir := statusModel(t, config.Default())
	report(t, dir, "kt7m", status.Running, "building the index")
	m = poll(t, m)
	m.dead = map[string]bool{"kt7m": true}

	if strings.Contains(m.View(), "building the index") {
		t.Errorf("the bar describes the agent of a node whose session is gone:\n%s", m.View())
	}
}

// Deleting a workspace deletes its nodes, and their reports go with them.
func TestDeletingAWorkspaceTakesItsReports(t *testing.T) {
	m, dir := statusModel(t, config.Default())
	report(t, dir, "kt7m", status.NeedsYou, "waiting")
	m.candidates, m.choice, m.mode = []string{"main"}, 0, modeConfirmDeleteWorkspace

	m, _ = typeKeys(t, m, "y")

	if reports, _ := status.Read(dir, "main"); len(reports) != 0 {
		t.Errorf("the deleted workspace left %v behind", reports)
	}
}

// A report is keyed by a node id, and an id is only unique against the map it
// belongs to — see docs/adr/0010-a-workspace-switch-is-a-reload.md.
func TestSwitchingWorkspaceDropsTheReports(t *testing.T) {
	m, _ := statusModel(t, config.Default())
	m.reports = map[string]status.Report{"kt7m": {State: status.NeedsYou, TS: time.Now()}}

	next, _ := m.open("other")
	if len(next.reports) != 0 {
		t.Errorf("the new map is holding %v, want nothing carried over", next.reports)
	}
}

// The detail an agent reported is shown, or it is a field nothing ever reads.
func TestTheDetailIsShownForTheNodeUnderTheCursor(t *testing.T) {
	m, dir := statusModel(t, config.Default())
	report(t, dir, "kt7m", status.NeedsYou, "may I run rm")
	m = poll(t, m)

	if !strings.Contains(m.View(), "may I run rm") {
		t.Errorf("the status bar should carry the detail of the node under the cursor:\n%s", m.View())
	}
}
