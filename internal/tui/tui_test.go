package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

func newModel(t *testing.T, cfg config.Config, ws state.Workspace) Model {
	t.Helper()
	m := New(cfg, ws)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return sized.(Model)
}

func TestViewShowsWorkspaceNameAndNodeCount(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main"})
	view := m.View()

	if !strings.Contains(view, "main") {
		t.Errorf("status bar should name the workspace, got:\n%s", view)
	}
	if !strings.Contains(view, "0 nodes") {
		t.Errorf("status bar should show the node count, got:\n%s", view)
	}
}

func TestEmptyMapSaysHowToStart(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main"})
	if !strings.Contains(m.View(), "empty") {
		t.Errorf("an empty map should say so rather than render blank, got:\n%s", m.View())
	}
}

func TestQuitKeysQuit(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		m := newModel(t, config.Default(), state.Workspace{Name: "main"})
		_, cmd := m.Update(key)
		if !isQuit(cmd) {
			t.Errorf("%v should quit", key)
		}
	}
}

func TestConfirmQuitAsksFirst(t *testing.T) {
	cfg := config.Default()
	cfg.General.ConfirmQuit = true
	m := newModel(t, cfg, state.Workspace{Name: "main"})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if isQuit(cmd) {
		t.Fatal("with confirm_quit set, q should ask before quitting")
	}
	asking := next.(Model)
	if !strings.Contains(asking.View(), "Quit") {
		t.Errorf("the confirmation should be visible, got:\n%s", asking.View())
	}

	_, cmd = asking.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !isQuit(cmd) {
		t.Error("y should confirm the quit")
	}

	declined, cmd := asking.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if isQuit(cmd) {
		t.Error("esc should cancel the quit")
	}
	if strings.Contains(declined.(Model).View(), "Quit Trigpoint?") {
		t.Error("cancelling should dismiss the confirmation")
	}
}

func TestViewFitsTheTerminal(t *testing.T) {
	m := New(config.Default(), state.Workspace{Name: "main"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	lines := strings.Split(sized.(Model).View(), "\n")

	if len(lines) > 10 {
		t.Errorf("view rendered %d lines into a 10-line terminal", len(lines))
	}
	for i, line := range lines {
		if n := len([]rune(stripANSI(line))); n > 40 {
			t.Errorf("line %d is %d columns wide in a 40-column terminal: %q", i, n, line)
		}
	}
}

func TestViewBeforeFirstWindowSizeDoesNotPanic(t *testing.T) {
	_ = New(config.Default(), state.Workspace{Name: "main"}).View()
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestViewSurvivesAVeryNarrowTerminal(t *testing.T) {
	m := New(config.Default(), state.Workspace{Name: "a-long-workspace-name"})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 12, Height: 4})
	for i, line := range strings.Split(sized.(Model).View(), "\n") {
		if n := len([]rune(stripANSI(line))); n > 12 {
			t.Errorf("line %d is %d columns wide in a 12-column terminal: %q", i, n, line)
		}
	}
}
