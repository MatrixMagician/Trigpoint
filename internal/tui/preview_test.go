package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
	"github.com/MatrixMagician/Trigpoint/internal/tmux"
)

// mapWithTwoShellNodes puts two live nodes side by side, which is the smallest
// map that can tell "captured the one that moved" from "captured everything".
func mapWithTwoShellNodes(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Pos: state.Cell{Col: 0, Row: 0}},
		{ID: "bbb", Kind: state.KindShell, Title: "web", Pos: state.Cell{Col: 1, Row: 0}},
	}})
	// The first resize is what gives a map a viewport, and so what sets its
	// first capture going. Letting it finish leaves each test asserting about
	// the captures it caused itself.
	m = run(t, m, captureDueMsg{})
	sessions.captured = nil
	return m, sessions
}

func activity(id string) tea.Msg {
	return tmuxEventMsg{ev: tmux.Event{Kind: tmux.Activity, Session: tmux.SessionName("main", id)}}
}

// update runs one message through the model, dropping the command.
func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

// run applies a message and then drives whatever it asked for to completion,
// the way the Bubble Tea runtime would.
func run(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	return settle(t, next.(Model), cmd)
}

// errTestCapture stands for tmux refusing a capture — a session that died
// between the event and the tick.
var errTestCapture = errors.New("no such session")

func TestActivityMarksTheCardDirtyRatherThanCapturingOnEveryEvent(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)

	for i := 0; i < 5; i++ {
		m = update(t, m, activity("aaa"))
	}
	if len(sessions.captured) != 0 {
		t.Fatalf("an activity event should not capture on its own, got %v", sessions.captured)
	}

	m = run(t, m, captureDueMsg{})
	if len(sessions.captured) != 1 || sessions.captured[0].session != tmux.SessionName("main", "aaa") {
		t.Errorf("five events should have coalesced into one capture, got %v", sessions.captured)
	}
}

func TestCapturesAreBatchedIntoOneTick(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)

	m = update(t, m, activity("aaa"))
	m = update(t, m, activity("bbb"))
	m = update(t, m, activity("aaa"))
	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 2 {
		t.Errorf("two cards moved, so one tick should capture twice, got %v", sessions.captured)
	}
}

func TestOnlyCardsInTheViewportAreCaptured(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Pos: state.Cell{Col: 0, Row: 0}},
		// Far enough away that no terminal this test runs in can show both.
		{ID: "far", Kind: state.KindShell, Title: "distant", Pos: state.Cell{Col: 200, Row: 200}},
	}})

	m = update(t, m, activity("aaa"))
	m = update(t, m, activity("far"))
	m = run(t, m, captureDueMsg{})

	for _, c := range sessions.captured {
		if c.session == tmux.SessionName("main", "far") {
			t.Errorf("a card outside the viewport was captured: %v", sessions.captured)
		}
	}
	if len(sessions.captured) != 1 {
		t.Errorf("the visible card should still have been captured, got %v", sessions.captured)
	}
}

func TestTheSlowTickRefreshesWithNoEventToPromptIt(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)

	// Nothing has been heard from tmux at all — this is the fallback that
	// catches whatever the event stream missed, or never delivered.
	m = update(t, m, refreshTickMsg{})
	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 2 {
		t.Errorf("the slow tick should refresh every visible card, got %v", sessions.captured)
	}
}

func TestTheSlowTickKeepsTicking(t *testing.T) {
	m, _ := mapWithTwoShellNodes(t)
	if _, cmd := m.Update(refreshTickMsg{}); cmd == nil {
		t.Error("the slow tick should schedule the next one, or it fires once and stops")
	}
}

func TestReturningFromAttachRefreshesImmediately(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)

	// The terminal has been out at a session; whatever happened there is not on
	// the card, and waiting a debounce to find out is a visible stale flash.
	m = run(t, m, attachedMsg{})

	if len(sessions.captured) != 2 {
		t.Errorf("returning from an attach should capture at once, got %v", sessions.captured)
	}
}

func TestALostEventStreamDoesNotDisturbTheMap(t *testing.T) {
	m, _ := mapWithTwoShellNodes(t)
	before := len(m.ws.Nodes)

	// A dropped connection reconnects and says so; a reconnect storm must cost
	// the map nothing but captures.
	for i := 0; i < 20; i++ {
		m = update(t, m, tmuxEventMsg{ev: tmux.Event{Kind: tmux.Sessions}})
		m = update(t, m, activity("aaa"))
	}
	m = run(t, m, captureDueMsg{})

	if len(m.ws.Nodes) != before {
		t.Errorf("the node set changed under an event storm: %d nodes, want %d", len(m.ws.Nodes), before)
	}
	if m.status != "" {
		t.Errorf("a reconnect is ordinary, not an error to report: %q", m.status)
	}
}

func TestAnEventForANodeThatIsNotOnTheMapIsIgnored(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)

	// Another workspace's node, or one adopted into a map this process is not
	// looking at. Nothing here has a card to mark stale.
	m = update(t, m, tmuxEventMsg{ev: tmux.Event{Kind: tmux.Activity, Session: tmux.SessionName("other", "zzz")}})
	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 0 {
		t.Errorf("a session with no card should capture nothing, got %v", sessions.captured)
	}
}

func TestThePreviewShowsOnTheCardInTheColourItWasCapturedIn(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)
	sessions.output = map[string]string{
		tmux.SessionName("main", "aaa"): "\x1b[31m200 GET /health\x1b[0m\n",
	}

	m = update(t, m, activity("aaa"))
	m = run(t, m, captureDueMsg{})

	view := m.View()
	if !strings.Contains(view, "200 GET /health") {
		t.Errorf("the card should show the captured output, got:\n%s", view)
	}
	if !strings.Contains(view, "\x1b[31m") {
		t.Errorf("the preview should keep the colour it was captured in, got:\n%q", view)
	}
}

func TestThePreviewIsCutToTheCardsLineCount(t *testing.T) {
	lines := config.Default().General.PreviewLines.M
	var out strings.Builder
	for i := 0; i < lines+20; i++ {
		fmt.Fprintf(&out, "line %d\n", i)
	}

	body := previewBody(out.String(), lines)
	if len(body) != lines {
		t.Fatalf("a preview of %d lines should be cut to %d, got %d", lines+20, lines, len(body))
	}
	// The last lines are the recent ones — a preview showing the top of the
	// scrollback would be a snapshot of the wrong moment.
	if want := fmt.Sprintf("line %d", lines+19); body[len(body)-1] != want {
		t.Errorf("the preview should end at the most recent line %q, got %q", want, body[len(body)-1])
	}
}

func TestThePreviewDropsTheBlankScreenBelowTheOutput(t *testing.T) {
	// capture-pane hands back the whole visible screen however few lines were
	// asked for, and most of a fresh pane is empty.
	body := previewBody("$ make\nok\n"+strings.Repeat("\n", 20), 4)
	if len(body) != 2 || body[1] != "ok" {
		t.Errorf("the blank screen below the output should not fill the card, got %q", body)
	}
}

func TestACardWithNoPreviewLinesAsksTmuxForNothing(t *testing.T) {
	cfg := config.Default()
	cfg.General.PreviewLines.M = 0
	sessions := &fakeSessions{}
	m := New(cfg, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api"},
	}}, t.TempDir(), sessions)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m = update(t, m, activity("aaa"))
	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 0 {
		t.Errorf("a card showing no preview should not be captured, got %v", sessions.captured)
	}
	if got := m.cardRows(); got != cardRowsNoBody {
		t.Errorf("a card with no body should still be %d rows, got %d", cardRowsNoBody, got)
	}
}

func TestEveryLiveCardGetsRoomForItsPreview(t *testing.T) {
	m, _ := mapWithTwoShellNodes(t)
	if got, want := m.bodyHeight(), config.Default().General.PreviewLines.M; got != want {
		t.Errorf("a map of shell nodes should lay out %d body lines, got %d", want, got)
	}
}

func TestACaptureThatFailsLeavesTheLastSnapshotStanding(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)
	sessions.output = map[string]string{tmux.SessionName("main", "aaa"): "still here\n"}
	m = update(t, m, activity("aaa"))
	m = run(t, m, captureDueMsg{})

	// The session has since died. A preview is a snapshot, so the honest thing
	// is the last one taken rather than a blank card.
	sessions.captureErr = errTestCapture
	m = update(t, m, activity("aaa"))
	m = run(t, m, captureDueMsg{})

	if !strings.Contains(m.View(), "still here") {
		t.Errorf("a failed capture should leave the last snapshot on the card, got:\n%s", m.View())
	}
}

func TestListenDeliversWhatTheMonitorPushed(t *testing.T) {
	ch := make(chan tmux.Event, 1)
	ch <- tmux.Event{Kind: tmux.Activity, Session: tmux.SessionName("main", "aaa")}

	msg, ok := listen(ch)().(tmuxEventMsg)
	if !ok || msg.ev.Session != tmux.SessionName("main", "aaa") {
		t.Fatalf("listen should carry the event through, got %#v", msg)
	}
	// The map has to keep listening, and the only way back to the channel is
	// the message it arrived in.
	if msg.from == nil {
		t.Error("the message should carry the stream it came from, or the map hears one event and no more")
	}
}

func TestAnEventKeepsTheMapListening(t *testing.T) {
	m, _ := mapWithTwoShellNodes(t)
	ch := make(chan tmux.Event, 1)

	_, cmd := m.Update(tmuxEventMsg{ev: tmux.Event{Kind: tmux.Sessions}, from: ch})
	if cmd == nil {
		t.Fatal("handling an event should ask for the next one")
	}
}

func TestAHyperlinkInAPreviewDoesNotEscapeItsCard(t *testing.T) {
	// capture-pane emits OSC 8 for a pane that printed a hyperlink, and the cut
	// that fits a line into a card can land inside one. A link left open makes
	// every card after it clickable.
	line := bodyLine(cardStyle, "\x1b]8;;https://example.com/a/very/long/url\x1b\\a link with far too much text in it")
	if !strings.Contains(line, "\x1b]8;;\x1b\\") {
		t.Errorf("the card should close the hyperlink it opened, got %q", line)
	}
	// And nothing is added to a line that never opened one.
	if plain := bodyLine(cardStyle, "200 GET /health"); strings.Contains(plain, "\x1b]8;;") {
		t.Errorf("a line with no hyperlink should gain no escape sequence, got %q", plain)
	}
}

func TestScrollingBackToACardRefreshesWhatWentStaleOffScreen(t *testing.T) {
	// Off-screen output is the one case where events are guaranteed to have been
	// missed, so a card coming back into view is not to be trusted, preview or
	// no preview.
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "api", Pos: state.Cell{Col: 0, Row: 0}},
		{ID: "far", Kind: state.KindShell, Title: "distant", Pos: state.Cell{Col: 40, Row: 0}},
	}})
	m = run(t, m, captureDueMsg{})

	// l hops the cursor to the far node, dragging the viewport with it; h brings
	// it back. Both cards have been captured by the time we return, so nothing
	// is missing — only overtaken, by whatever ran while nobody was looking.
	scroll := func(key rune) {
		t.Helper()
		before := m.ws.Viewport.Offset
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if m.ws.Viewport.Offset == before {
			t.Fatalf("%q should have scrolled the viewport", key)
		}
		m = run(t, m, captureDueMsg{})
	}
	scroll('l')
	sessions.captured = nil
	scroll('h')

	if len(sessions.captured) != 1 || sessions.captured[0].session != tmux.SessionName("main", "aaa") {
		t.Errorf("scrolling back should refresh the card it brought into view, got %v", sessions.captured)
	}
}

func TestMovingTheCursorWithinTheViewportCapturesNothing(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)
	m = run(t, m, captureDueMsg{})
	sessions.captured = nil

	// Both cards are already on screen and already captured. Moving between
	// them is not news about either session.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = run(t, m, captureDueMsg{})

	if len(sessions.captured) != 0 {
		t.Errorf("navigating a captured viewport should ask tmux nothing, got %v", sessions.captured)
	}
}

func TestKillingANodeForgetsItsPreview(t *testing.T) {
	m, sessions := mapWithTwoShellNodes(t)
	sessions.output = map[string]string{tmux.SessionName("main", "aaa"): "still here\n"}
	m = update(t, m, activity("aaa"))
	m = run(t, m, captureDueMsg{})
	if _, taken := m.previews["aaa"]; !taken {
		t.Fatal("the node should have a preview to forget")
	}

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Ids are only unique against the nodes on the map, so a snapshot outliving
	// its node is a snapshot the next node to be given that id would show.
	if _, kept := m.previews["aaa"]; kept {
		t.Errorf("a killed node's snapshot should go with it, got %q", m.previews["aaa"])
	}
}
