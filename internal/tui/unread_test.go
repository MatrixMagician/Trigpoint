package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
)

// Unread is a property of your attention, not of the node's work (CONTEXT.md):
// it is set by output arriving while you are elsewhere, and cleared by looking —
// attaching or peeking — and by nothing else.

func TestActivityOnADetachedNodeMarksItsCardUnread(t *testing.T) {
	m, _ := mapWithDeepOutput(t)

	m = run(t, m, activity("k4f2"))

	if !m.unread["k4f2"] {
		t.Fatal("output arriving while you are on the map is output you have not seen")
	}
	if view := m.View(); !strings.Contains(view, unreadBadge) {
		t.Errorf("an unread node's card should say so, got:\n%s", view)
	}
}

func TestPeekingClearsTheUnreadMark(t *testing.T) {
	m, _ := mapWithDeepOutput(t)
	m = run(t, m, activity("k4f2"))

	m = peeked(t, m)

	if m.unread["k4f2"] {
		t.Error("peeking is looking, and looking is what clears unread")
	}
	back, _ := press(t, m, tea.KeyEsc)
	if view := back.View(); strings.Contains(view, unreadBadge) {
		t.Errorf("the card should be read once the peek is over, got:\n%s", view)
	}
}

func TestAttachingClearsTheUnreadMark(t *testing.T) {
	m, _ := mapWithOneNode(t, config.Default())
	m = run(t, m, activity("k4f2"))
	if !m.unread["k4f2"] {
		t.Fatal("the node should start unread for this to say anything")
	}

	next, _ := press(t, m, tea.KeyEnter)
	if next.unread["k4f2"] {
		t.Error("attaching to a node is looking at it")
	}
}

// TestActivityDuringAnAttachDoesNotOutliveIt is the stale-unread case: the
// events tmux pushes while the terminal is out at a session arrive on the map
// afterwards, and marking the node you were just inside as unread would leave a
// mark nothing but a second visit could clear.
func TestActivityDuringAnAttachDoesNotOutliveIt(t *testing.T) {
	m, _ := mapWithOneNode(t, config.Default())

	away, _ := press(t, m, tea.KeyEnter) // the terminal is now at the session
	away = update(t, away, activity("k4f2"))
	back := update(t, away, attachedMsg{})

	if back.unread["k4f2"] {
		t.Error("output produced while you were inside the node has been seen")
	}
	if view := back.View(); strings.Contains(view, unreadBadge) {
		t.Errorf("the card should be read on the way back from an attach, got:\n%s", view)
	}
}

func TestADeadCardIsNotAlsoAnUnreadOne(t *testing.T) {
	m, _ := mapWithDeepOutput(t)
	m = run(t, m, activity("k4f2"))
	m = m.markDead("k4f2")

	view := m.View()
	if !strings.Contains(view, deadBadge) {
		t.Errorf("a node whose session has gone is dead, got:\n%s", view)
	}
	if strings.Contains(view, unreadBadge) {
		t.Errorf("one badge per card: dead outranks unread, got:\n%s", view)
	}
}

// TestUnreadDoesNotOutliveTheNode guards the id being handed out again: ids are
// unique against the nodes on the map, so they come back round.
func TestUnreadDoesNotOutliveTheNode(t *testing.T) {
	m, _ := mapWithDeepOutput(t)
	m = run(t, m, activity("k4f2"))

	m = run(t, m, nodeKilledMsg{id: "k4f2"})

	if m.unread["k4f2"] {
		t.Error("a killed node's unread mark should go with it")
	}
}
