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

// deepOutput is more output than any card could ever show — the whole point of
// peek is that it reaches past the card.
func deepOutput(lines int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// mapWithDeepOutput is one live node with a long scrollback behind it.
func mapWithDeepOutput(t *testing.T) (Model, *fakeSessions) {
	t.Helper()
	m, sessions := mapWithOneNode(t, config.Default())
	sessions.output = map[string]string{tmux.SessionName("main", "k4f2"): deepOutput(300)}
	return m, sessions
}

// peeked opens the peek view on whatever the cursor is on.
func peeked(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	return settle(t, next.(Model), cmd)
}

func TestSpaceOpensASnapshotReachingWellPastTheCard(t *testing.T) {
	m, sessions := mapWithDeepOutput(t)
	sessions.captured = nil

	m = peeked(t, m)

	if len(sessions.captured) != 1 {
		t.Fatalf("space should take one snapshot, got %v (status %q)", sessions.captured, m.status)
	}
	if got := sessions.captured[0].lines; got != peekLines {
		t.Errorf("peek asked for %d lines, want the deep %d a card cannot show", got, peekLines)
	}
	view := m.View()
	// The end of the output is what a peek opens on: the newest lines are what
	// "what has this been doing" means.
	if !strings.Contains(view, "line 300") {
		t.Errorf("peek should open on the end of the output, got:\n%s", view)
	}
	// Far more than the four lines config.Default gives a card's body.
	if !strings.Contains(view, "line 290") {
		t.Errorf("peek should fill the screen, not a card's body, got:\n%s", view)
	}
}

func TestPeekScrollsThroughTheSnapshot(t *testing.T) {
	m := peeked(t, first(mapWithDeepOutput(t)))

	up, _ := typeKeys(t, m, "kkk")
	if up.peekTop >= m.peekTop {
		t.Errorf("k should scroll back through the output, went from %d to %d", m.peekTop, up.peekTop)
	}
	down, _ := typeKeys(t, up, "jjj")
	if down.peekTop != m.peekTop {
		t.Errorf("j should scroll forward again, want back at %d, got %d", m.peekTop, down.peekTop)
	}

	top, _ := typeKeys(t, m, "g")
	if top.peekTop != 0 {
		t.Errorf("g should go to the start of the output, got line %d", top.peekTop)
	}
	if !strings.Contains(top.View(), "line 1") {
		t.Errorf("the start of the output should be the start of the snapshot, got:\n%s", top.View())
	}
	bottom, _ := typeKeys(t, top, "G")
	if bottom.peekTop != m.peekTop {
		t.Errorf("G should return to the end, want %d, got %d", m.peekTop, bottom.peekTop)
	}
}

func TestPeekScrollingStopsAtTheEndsOfTheSnapshot(t *testing.T) {
	m := peeked(t, first(mapWithDeepOutput(t)))

	up, _ := typeKeys(t, m, strings.Repeat("k", 500))
	if up.peekTop != 0 {
		t.Errorf("scrolling past the start should stop at it, got line %d", up.peekTop)
	}
	down, _ := typeKeys(t, up, strings.Repeat("j", 500))
	if down.peekTop != m.peekTop {
		t.Errorf("scrolling past the end should stop at it, want %d, got %d", m.peekTop, down.peekTop)
	}
}

// TestPeekNeverGivesTheNodeYourKeyboard is the whole difference between peek
// and attach: the keys that would act on the map, or reach a session, do
// neither.
func TestPeekNeverGivesTheNodeYourKeyboard(t *testing.T) {
	m, sessions := mapWithDeepOutput(t)
	m = peeked(t, m)
	sessions.captured = nil

	for _, keys := range []string{"n", "x", "q", "N", "A"} {
		m, _ = typeKeys(t, m, keys)
	}
	m, _ = press(t, m, tea.KeyEnter)

	if len(sessions.handoffs)+len(sessions.created)+len(sessions.killed)+len(sessions.captured) != 0 {
		t.Errorf("peek is read-only, but tmux was asked to do something: %+v", sessions)
	}
	if m.mode != modePeek {
		t.Errorf("nothing but esc leaves the peek, got mode %v", m.mode)
	}
	if len(m.ws.Nodes) != 1 {
		t.Errorf("keys pressed in a peek should not change the map, got %d nodes", len(m.ws.Nodes))
	}
}

func TestEscReturnsFromAPeekToTheMap(t *testing.T) {
	m := peeked(t, first(mapWithDeepOutput(t)))

	back, _ := press(t, m, tea.KeyEsc)
	if back.mode != modeNormal {
		t.Fatalf("esc should return to the map, got mode %v", back.mode)
	}
	view := back.View()
	if strings.Contains(view, "line 300") {
		t.Errorf("the snapshot should be gone, got:\n%s", view)
	}
	if !strings.Contains(view, "api") || !strings.Contains(view, "attach") {
		t.Errorf("the map should be back, got:\n%s", view)
	}
}

// TestPeekOnADeadNodeShowsWhatWasLastCaptured holds peek to the one thing it
// can still do for a node whose session has gone (§9.2): the last snapshot
// taken is all there is, and it is better than a blank screen.
func TestPeekOnADeadNodeShowsWhatWasLastCaptured(t *testing.T) {
	m, sessions := mapWithDeepOutput(t)
	m = run(t, m, captureDueMsg{}) // a snapshot taken while the node was alive
	m = m.markDead("k4f2")
	sessions.captured = nil

	m = peeked(t, m)

	if len(sessions.captured) != 0 {
		t.Errorf("a dead node has no session to capture from, but one was asked: %v", sessions.captured)
	}
	if m.mode != modePeek {
		t.Fatalf("peek should still open on a dead node, got mode %v (status %q)", m.mode, m.status)
	}
	if !strings.Contains(m.View(), "line 300") {
		t.Errorf("a dead node's peek should show its last capture, got:\n%s", m.View())
	}
}

func TestPeekOnADeadNodeWithNothingCapturedSaysSo(t *testing.T) {
	m, _ := mapWithDeepOutput(t)
	m = m.markDead("k4f2") // dead before anything was ever captured from it

	m = peeked(t, m)

	if m.mode != modePeek {
		t.Fatalf("peek should open and explain itself, got mode %v", m.mode)
	}
	if !strings.Contains(m.View(), "Nothing") {
		t.Errorf("a peek with no output should say so plainly, got:\n%s", m.View())
	}
}

func TestPeekReportsACaptureTmuxRefused(t *testing.T) {
	m, sessions := mapWithDeepOutput(t)
	sessions.captureErr = errors.New("tmux: no such session")

	m = peeked(t, m)

	if m.mode == modePeek {
		t.Error("a snapshot that could not be taken should not open an empty peek")
	}
	if !strings.Contains(m.View(), "no such session") {
		t.Errorf("the map should say why the peek failed, got:\n%s", m.View())
	}
}

// TestPeekOnANoteDoesNothing keeps the domain line: peek reads a session's
// output, and a note has never had one (§6).
func TestPeekOnANoteDoesNothing(t *testing.T) {
	m, sessions, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindNote, Title: "todo", Note: "buy milk"},
	}})

	m = peeked(t, m)

	if m.mode != modeNormal {
		t.Errorf("a note has no output to peek at, got mode %v", m.mode)
	}
	if len(sessions.captured) != 0 {
		t.Errorf("a note should never be captured, got %v", sessions.captured)
	}
}

func TestPeekOnAnEmptyCellDoesNothing(t *testing.T) {
	m := mapWith(t)
	if next := peeked(t, m); next.mode != modeNormal {
		t.Errorf("there is nothing under the cursor to peek at, got mode %v", next.mode)
	}
}

// first drops the second value of a two-value helper, so a peek can be opened
// on a map in one expression.
func first(m Model, _ *fakeSessions) Model { return m }
