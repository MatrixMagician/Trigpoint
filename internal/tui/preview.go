package tui

// Live previews (SPEC §5.3). A preview is always a snapshot and never a live
// terminal — the map asks tmux what a session looks like, it does not watch one.
//
// What drives the asking is an event stream: activity marks a card dirty, and
// dirty cards are captured together on a short debounce. Nothing here captures
// per event, and nothing here captures a card that is not on screen — dozens of
// nodes times an uncapped capture rate is the scaling risk this design exists to
// avoid (SPEC §14, risk 1).

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// tmuxEventMsg is one push from the control-mode client. It carries the stream
// it arrived on, because the map has to go back for the next one and a Model is
// copied by value everywhere — the channel travels with the message rather than
// being a field every copy would have to keep in step.
type tmuxEventMsg struct {
	ev   tmux.Event
	from <-chan tmux.Event
}

// captureDueMsg is the debounce expiring: whatever went stale since the last one
// is captured now, in one batch.
type captureDueMsg struct{}

// refreshTickMsg is the slow global tick — the fallback for everything the event
// stream missed, and for the whole time there was no event stream to miss it.
type refreshTickMsg struct{}

// capturedMsg carries a batch of snapshots back from tmux.
type capturedMsg struct {
	mapStamp
	previews map[string][]string
}

// listen waits for the next event. A nil stream is a map that was never given
// one — tests drive the model by hand — and waiting on it would wait forever.
func listen(from <-chan tmux.Event) tea.Cmd {
	if from == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-from
		if !ok {
			return nil
		}
		return tmuxEventMsg{ev: ev, from: from}
	}
}

func (m Model) updatePreview(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tmuxEventMsg:
		next, cmd := m.onEvent(msg.ev)
		// Asking for the next event before doing anything else: the buffer the
		// monitor pushes into is small, and it drops rather than waits.
		return next, tea.Batch(listen(msg.from), cmd), true

	case captureDueMsg:
		m.settling = false
		next, cmd := m.capture()
		return next, cmd, true

	case capturedMsg:
		if !msg.about(m) {
			// Snapshots of another map's sessions, keyed by ids that mean
			// something else here.
			return m, nil, true
		}
		return m.withPreviews(msg.previews), nil, true

	case refreshTickMsg:
		// The slow tick is the fallback for everything the event stream missed,
		// and reconciliation is part of that: with the control-mode client down
		// this is the only thing that ever notices a session has gone (§5.3).
		next, cmd := m.markDirty(m.visible()...)
		return next, tea.Batch(cmd, m.slowTick(), m.reconcile()), true

	case reconciledMsg:
		next, cmd := m.applyReconciled(msg)
		return next, cmd, true
	}
	return m, nil, false
}

// onEvent is what tmux said, turned into which cards are now stale. Activity
// names one session; a change to the session list could mean anything, so it
// means everything on screen.
func (m Model) onEvent(ev tmux.Event) (Model, tea.Cmd) {
	if ev.Kind == tmux.Sessions {
		// The session list changed, which is exactly the question
		// reconciliation asks: a node killed from inside tmux goes dead on the
		// map here rather than at the next launch.
		next, cmd := m.markDirty(m.visible()...)
		return next, tea.Batch(cmd, m.reconcile())
	}
	for _, n := range m.ws.Nodes {
		if !n.HasSession() || m.sessionOf(n) != ev.Session {
			continue
		}
		// Output arriving on a node is output you have not seen (§8) — unless
		// you are inside that very node, and an event that arrives during an
		// attach is cleared on the way back from it rather than filtered here:
		// the events tmux pushed while the terminal was away go on arriving
		// after it has come back.
		m = m.unreadOn(n.ID)
		if onScreen(m.visibleNodes(), n.ID) {
			// Off screen there is no card to re-capture for, and the viewport
			// coming back is what marks it stale again.
			return m.markDirty(n.ID)
		}
		return m, nil
	}
	// Another workspace's node, or a session with no card in this map. There is
	// nothing here to mark stale.
	return m, nil
}

// markDirty flags cards for re-capture and starts the debounce if one is not
// already running. Marking is what an event costs; the capture is what the tick
// costs, and however many events arrive in between, there is one of those.
func (m Model) markDirty(ids ...string) (Model, tea.Cmd) {
	if len(ids) == 0 {
		return m, nil
	}
	// A fresh map, not the one in hand: a Model is copied by value all over
	// Bubble Tea, and writing through would edit the state every older copy is
	// still holding.
	dirty := make(map[string]bool, len(m.dirty)+len(ids))
	for id := range m.dirty {
		dirty[id] = true
	}
	for _, id := range ids {
		dirty[id] = true
	}
	m.dirty = dirty
	if m.settling {
		return m, nil // it joins the batch already on its way
	}
	m.settling = true
	return m, tea.Tick(m.debounce(), func(time.Time) tea.Msg { return captureDueMsg{} })
}

// capture takes one batch of snapshots: every dirty card that is on screen, in
// a single command, so that a busy map costs one round of tmux calls per tick
// rather than one per event.
func (m Model) capture() (Model, tea.Cmd) {
	type want struct {
		id      string
		session string
		lines   int
	}
	var wants []want
	// Visibility is checked here and not only when the card was marked: the
	// viewport can move between the event and the tick.
	for _, n := range m.visibleNodes() {
		if !m.dirty[n.ID] || !n.HasSession() || m.dead[n.ID] {
			// A dead node has no session to capture; the card keeps the last
			// snapshot taken, which is the honest thing a snapshot does.
			continue
		}
		if lines := m.previewHeight(n); lines > 0 {
			wants = append(wants, want{n.ID, m.sessionOf(n), lines})
		}
	}
	m.dirty = nil
	if len(wants) == 0 {
		return m, nil
	}

	sessions, stamp := m.sessions, m.stamp()
	// ponytail: one `tmux capture-pane` process per card. Measured at ~1.3 ms
	// each, so a viewport of forty costs ~55 ms per tick, and the cost is the
	// process rather than the lines it returns. If that ever shows, the forty
	// become one invocation — `capture-pane \; display -p <marker> \; …` — and
	// the answer is split back apart on the markers.
	return m, func() tea.Msg {
		previews := make(map[string][]string, len(wants))
		for _, w := range wants {
			text, err := sessions.Capture(w.session, w.lines)
			if err != nil {
				// A capture that fails is a session that is not there any more,
				// which is dead-node business (§9.2) rather than something to
				// put in the status bar on every tick. The card keeps the last
				// snapshot taken, which is the honest thing a snapshot does.
				continue
			}
			previews[w.id] = previewBody(text, w.lines)
		}
		return capturedMsg{mapStamp: stamp, previews: previews}
	}
}

// refreshNow marks every card on screen stale and captures at once, skipping the
// debounce. It is what returning from an attach does: the terminal has been
// somewhere else, and there is nothing to coalesce with.
func (m Model) refreshNow() (Model, tea.Cmd) {
	dirty := make(map[string]bool, len(m.ws.Nodes))
	for _, id := range m.visible() {
		dirty[id] = true
	}
	m.dirty = dirty
	return m.capture()
}

// withPreviews folds a batch of snapshots into the map, in a fresh map for the
// same reason markDirty builds one.
//
// ponytail: batches are unnumbered, so two in flight at once land in whichever
// order they finish and an older snapshot can win. It takes an immediate
// refresh overtaking a debounced batch to happen at all, and costs one card one
// tick. Number the batches and drop the stale one if a card is ever seen going
// backwards.
func (m Model) withPreviews(taken map[string][]string) Model {
	previews := make(map[string][]string, len(m.previews)+len(taken))
	for id, lines := range m.previews {
		previews[id] = lines
	}
	for id, lines := range taken {
		previews[id] = lines
	}
	m.previews = previews
	return m
}

// slowTick is the fallback refresh (§5.3). It is also the only thing that
// refreshes at all while the control-mode client is down, which is why it keeps
// running rather than being cancelled when the stream is healthy.
func (m Model) slowTick() tea.Cmd {
	every := time.Duration(m.cfg.General.RefreshTickS) * time.Second
	if every < minTick {
		every = minTick
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// debounce is how long activity is allowed to settle before the cards that
// moved are re-captured. Zero would capture per event, which is the thing the
// debounce exists to prevent, so it is floored rather than honoured.
func (m Model) debounce() time.Duration {
	if ms := m.cfg.General.PreviewDebounceMs; ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return minDebounce
}

// A configured zero is floored rather than honoured: capturing per event, or
// ticking as fast as the runtime will schedule, is the cost this design exists
// to bound.
const (
	minDebounce = 50 * time.Millisecond
	minTick     = time.Second
)

// previewHeight is how many lines of preview a node's card shows: what its card
// size has room for. A node with no session shows none, and neither does a size
// configured to zero — which is how a card opts out of being captured at all.
func (m Model) previewHeight(n state.Node) int {
	if !n.HasSession() {
		return 0
	}
	return m.sizeLines(n)
}

// previewBody is what capture-pane gave back, cut down to what a card can show.
// capture-pane hands back the whole visible screen however few lines were asked
// for, so the blank screen below the last output is dropped, and what is kept is
// the *end* of the output — a preview of the top of the scrollback would be a
// snapshot of the wrong moment.
func previewBody(text string, lines int) []string {
	all := trimBlank(strings.Split(strings.TrimRight(text, "\n"), "\n"))
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return all
}

// visibleNodes is the nodes with a card on screen. Only these are ever captured
// — a card a filter is hiding is not on screen, and a snapshot nothing is
// showing is a subprocess spent on nothing.
func (m Model) visibleNodes() []state.Node {
	min, max := m.bounds()
	var seen []state.Node
	for _, n := range m.filtered() {
		if n.Pos.Col >= min.Col && n.Pos.Col <= max.Col && n.Pos.Row >= min.Row && n.Pos.Row <= max.Row {
			seen = append(seen, n)
		}
	}
	return seen
}

// onScreen reports whether a node is one of those with a card on screen.
func onScreen(shown []state.Node, id string) bool {
	for _, n := range shown {
		if n.ID == id {
			return true
		}
	}
	return false
}

// visible is visibleNodes by id, which is what marking dirty deals in.
func (m Model) visible() []string {
	nodes := m.visibleNodes()
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

// unpreviewed is the cards on screen with nothing on them yet — what scrolling
// brings into view. Marking only these keeps navigation off the capture path:
// moving the cursor is not news about a session anyone is watching.
func (m Model) unpreviewed() []string {
	var ids []string
	for _, n := range m.visibleNodes() {
		if _, taken := m.previews[n.ID]; !taken && n.HasSession() {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// forget drops what was being held on a node's behalf, on the way out. A
// snapshot that outlived its node is one the next node to be handed that id
// would show as its own: ids are only unique against the nodes on the map, so
// they come back round.
//
// Fresh maps, for the reason markDirty builds one.
func (m Model) forget(id string) Model {
	if _, held := m.previews[id]; held {
		previews := make(map[string][]string, len(m.previews))
		for other, lines := range m.previews {
			if other != id {
				previews[other] = lines
			}
		}
		m.previews = previews
	}
	m = m.alive(id).read(id)
	if m.dirty[id] {
		dirty := make(map[string]bool, len(m.dirty))
		for other := range m.dirty {
			if other != id {
				dirty[other] = true
			}
		}
		m.dirty = dirty
	}
	return m
}
