package tui

// Agent status on the map (SPEC §8). An agent announces what it is doing by
// writing a file; Trigpoint reads that directory and draws what it finds.
// Nothing here reads an agent's output, and nothing here turns silence into a
// state — an agent that has not reported is unknown, and unknown is drawn as
// nothing at all (CONTEXT.md, "Agent status").
//
// The directory is read on its own tick rather than watched by a file-system
// notifier: see
// docs/adr/0015-agent-status-is-a-directory-of-files-trigpoint-polls.md.

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/status"
)

// statusPoll is how often the status directory is read. It is a badge a human
// is waiting on, so it is fast enough not to be noticed and slow enough that a
// readdir over a handful of small files costs nothing beside the capture-pane
// calls the map already makes.
const statusPoll = time.Second

// staleBadge marks a report Trigpoint has stopped trusting. It is drawn beside
// the state rather than instead of it: staleness says the report is old, never
// what the true state became (CONTEXT.md, "Stale").
const staleBadge = "?"

// statusColours is what each state is drawn in (§8). ANSI codes, so a terminal
// theme still gets a say, and four distinct ones because a badge that cannot be
// told apart from another says nothing the card did not already say.
var statusColours = map[status.State]string{
	status.Running:  "10",  // green: working
	status.NeedsYou: "208", // amber: the one state that is about you
	status.Done:     "244", // grey: finished, nothing to do
	status.Error:    "9",   // red
}

// bellOut is where the terminal bell is rung. Stderr rather than the screen
// Bubble Tea is painting: a BEL written into the frame would ring on every
// repaint, and one written over the rendering would tear it.
var bellOut io.Writer = os.Stderr

// statusTickMsg is the poll falling due.
type statusTickMsg struct{}

// statusReadMsg is what the status directory said, carrying the map it is an
// answer about — reports are keyed by node id, and an id means something else
// on the next map.
type statusReadMsg struct {
	mapStamp
	reports map[string]status.Report
}

// statusTick schedules the next read. It runs for the life of the process, like
// the preview tick: an agent can start reporting at any time, including into a
// directory that did not exist when Trigpoint launched.
func (m Model) statusTick() tea.Cmd {
	return tea.Tick(statusPoll, func(time.Time) tea.Msg { return statusTickMsg{} })
}

// readStatus reads the directory off the update loop, the way every other
// question that touches the disk or tmux is asked.
//
// A read that fails answers with nothing at all rather than with an empty set:
// the reports in hand are the last thing the agents actually said, and clearing
// every badge on the map because one readdir failed would be a worse lie than
// showing a report a second out of date. This is the same judgement the preview
// capture makes about a session it cannot reach.
func (m Model) readStatus() tea.Cmd {
	dir, workspace, stamp := m.statusDir, m.ws.Name, m.stamp()
	if dir == "" {
		return nil
	}
	return func() tea.Msg {
		reports, err := status.Read(dir, workspace)
		if err != nil {
			return nil
		}
		return statusReadMsg{mapStamp: stamp, reports: reports}
	}
}

func (m Model) updateStatusMsg(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case statusTickMsg:
		return m, tea.Batch(m.readStatus(), m.statusTick()), true

	case statusReadMsg:
		if !msg.about(m) {
			return m, nil, true
		}
		// The first read of a map is what is already on disk — a launch, or a
		// workspace arrived back at — rather than an agent asking for you while
		// you watched. It sets the baseline silently, or the bell would ring on
		// every Tab and on every start.
		rings := m.polled && m.cfg.General.BellOnNeedsYou && arrivedNeedingYou(m.ws.Nodes, m.reports, msg.reports)
		m.reports, m.polled = msg.reports, true
		if rings {
			return m, bell(), true
		}
		return m, nil, true
	}
	return m, nil, false
}

// arrivedNeedingYou reports whether any node has just started needing you. The
// bell is for the transition and not for the state: a needs-you report sitting
// unanswered is read off the map, and ringing once a second until it is dealt
// with would train the user to ignore the bell.
//
// Only the nodes on this map. The status directory holds every workspace's
// reports, and a file can outlive the node it was written for — a workspace
// deleted while its sessions kept running, a node killed mid-report. The bell
// is a summons to a card, so a report with no card is not one to ring for:
// `u` walks the map and could not jump to it.
func arrivedNeedingYou(nodes []state.Node, was, now map[string]status.Report) bool {
	for _, n := range nodes {
		report, ok := now[n.ID]
		if ok && report.State == status.NeedsYou && was[n.ID].State != status.NeedsYou {
			return true
		}
	}
	return false
}

// bell rings the terminal, if the user asked for one (§8). Off by default: a
// map is a thing you glance at, and a tool that beeps without being asked is
// one that gets muted.
func bell() tea.Cmd {
	return func() tea.Msg {
		fmt.Fprint(bellOut, "\a")
		return nil
	}
}

// badgeMark is one card's badge: the glyph drawn on its top border and the
// colour it is drawn in. Empty is a card with no badge — a note, which has no
// session and so neither liveness nor agent status to report.
type badgeMark struct{ glyph, colour string }

// badgeOf composes the badge's three sources into one mark (§8). They compose
// rather than overwrite: the glyph's *shape* carries unread, its *colour*
// carries what the agent said about itself, and a session that has gone
// outranks both — a node whose session is dead is in no state an agent could be
// reporting, and has nothing left to produce the output an unread mark is about.
func (m Model) badgeOf(n state.Node) badgeMark {
	if !n.HasSession() {
		return badgeMark{}
	}
	if m.dead[n.ID] {
		return badgeMark{glyph: deadBadge}
	}
	mark := badgeMark{glyph: liveBadge}
	if m.unread[n.ID] {
		mark.glyph = unreadBadge
	}
	report, reported := m.reports[n.ID]
	if !reported {
		// Nothing has been reported, so the badge says nothing about the agent:
		// the card keeps the liveness and unread it would have had anyway.
		return mark
	}
	mark.colour = statusColours[report.State]
	if m.staleReport(report) {
		mark.glyph += staleBadge
	}
	return mark
}

// staleReport is whether a report has outlived the configured window. The rule
// belongs to the status package, so that `trig ls` and the map agree about what
// is still worth believing.
func (m Model) staleReport(r status.Report) bool {
	return r.Stale(time.Duration(m.cfg.General.StatusStaleAfterMin) * time.Minute)
}

// jumpAttention is `u`: the next node needing attention, needs-you before
// unread (§7.3). Pressing it again moves on to the one after, so a map with
// several things waiting is walked rather than stuck on the first of them.
func (m Model) jumpAttention() (tea.Model, tea.Cmd) {
	wanted := m.needingAttention()
	if len(wanted) == 0 {
		// Nowhere to jump to. The cursor stays where the user left it rather
		// than being moved somewhere arbitrary to have moved.
		return m, nil
	}
	at := -1
	for i, n := range wanted {
		if n.Pos == m.ws.Viewport.Cursor {
			at = i
			break
		}
	}
	was := m.ws.Viewport
	m.ws.Viewport.Cursor = wanted[(at+1)%len(wanted)].Pos
	m = m.follow().savedIfMoved(was)
	if m.ws.Viewport.Offset == was.Offset {
		return m, nil
	}
	// The jump scrolled, so cards nothing was watching are on screen now — off
	// screen is where activity events are dropped.
	next, cmd := m.markDirty(m.visible()...)
	return next, cmd
}

// needingAttention is what `u` walks: every node that needs you, then every
// node with output you have not seen, each in map order so the walk is the same
// on every run.
//
// A dead node is in neither list. Its badge says dead and nothing else, so a
// jump to one would be a jump to a card showing no reason for it — and a
// needs-you report on a session that has gone is a question nobody can answer.
//
// Only what the filter leaves standing, like every other motion: a card that is
// not on screen cannot be attached to, and jumping the cursor onto one would be
// pointing at something the user cannot see (§7.1).
func (m Model) needingAttention() []state.Node {
	nodes := append([]state.Node(nil), m.filtered()...)
	sort.Slice(nodes, func(i, j int) bool { return earlier(nodes[i].Pos, nodes[j].Pos) })

	var needsYou, unread []state.Node
	for _, n := range nodes {
		if m.dead[n.ID] {
			continue
		}
		switch {
		case m.reports[n.ID].State == status.NeedsYou:
			needsYou = append(needsYou, n)
		case m.unread[n.ID]:
			unread = append(unread, n)
		}
	}
	return append(needsYou, unread...)
}

// statusLabel is what the status bar says about the node under the cursor: the
// state its agent reported and the detail it reported with it. The badge is a
// colour, and a colour cannot be read out loud — this is where the same report
// says which state it is in words.
//
// Nothing at all for a node that has not reported, which is most of the map.
func (m Model) statusLabel() string {
	node, ok := m.selected()
	if !ok || m.dead[node.ID] {
		// A dead node's badge says dead and nothing else, and this is the same
		// badge in words: describing the agent of a session that has gone would
		// have the bar and the card disagreeing about the same node.
		return ""
	}
	report, reported := m.reports[node.ID]
	if !reported {
		return ""
	}
	label := string(report.State)
	if m.staleReport(report) && report.State == status.Running {
		label += " " + staleBadge
	}
	if report.Detail != "" {
		// Already flattened and bounded when it was read (status.MaxDetailLen),
		// because it is written by an agent rather than by Trigpoint.
		label += " · " + report.Detail
	}
	return label
}

// forgetReport drops a node's report on the way out, on disk as well as in
// hand. Ids are only unique against the nodes on the map, so they come back
// round — a report left behind is a badge the next node handed that id would
// wear as its own.
func (m Model) forgetReport(id string) Model {
	if m.statusDir != "" {
		if err := status.Remove(status.Path(m.statusDir, m.ws.Name, id)); err != nil {
			m.status = err.Error()
		}
	}
	if _, held := m.reports[id]; !held {
		return m
	}
	// A fresh map: a Model is copied by value all over Bubble Tea, and writing
	// through would edit the state every older copy is still holding.
	reports := make(map[string]status.Report, len(m.reports))
	for other, report := range m.reports {
		if other != id {
			reports[other] = report
		}
	}
	m.reports = reports
	return m
}
