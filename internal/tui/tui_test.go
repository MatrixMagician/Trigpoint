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
	m := New(cfg, ws, t.TempDir(), &fakeSessions{})
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
	m := New(config.Default(), state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{})
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
	_ = New(config.Default(), state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{}).View()
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
	m := New(config.Default(), state.Workspace{Name: "a-long-workspace-name"}, t.TempDir(), &fakeSessions{})
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 12, Height: 4})
	for i, line := range strings.Split(sized.(Model).View(), "\n") {
		if n := len([]rune(stripANSI(line))); n > 12 {
			t.Errorf("line %d is %d columns wide in a 12-column terminal: %q", i, n, line)
		}
	}
}

// TestTheStatusBarIsAlwaysExactlyOneLine guards the view's only hard invariant:
// it fills the terminal and never overruns it. The bar carries text from tmux
// and from the user, so both are able to break it.
func TestTheStatusBarIsAlwaysExactlyOneLine(t *testing.T) {
	long := strings.Repeat("a-very-long-node-title ", 8)
	cases := map[string]struct {
		width, height int
		ws            state.Workspace
		status        string
		mode          mode
		killing       string
	}{
		"a multi-line tmux error":          {80, 24, state.Workspace{Name: "main"}, "tmux: no server running\nand a second line", modeNormal, ""},
		"an error wider than the terminal": {40, 10, state.Workspace{Name: "main"}, long, modeNormal, ""},
		"a long title being typed":         {40, 10, state.Workspace{Name: "main"}, "", modeTitle, ""},
		"a kill confirmation in a narrow terminal": {
			20, 8,
			state.Workspace{Name: "main", Nodes: []state.Node{{ID: "k4f2", Title: long}}},
			"", modeConfirmKill, "k4f2",
		},
		"a workspace name wider than the terminal": {12, 6, state.Workspace{Name: "a-long-workspace-name"}, "", modeNormal, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := New(config.Default(), tc.ws, t.TempDir(), &fakeSessions{})
			sized, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			view := sized.(Model)
			view.status, view.mode, view.input, view.killing = tc.status, tc.mode, long, tc.killing

			lines := strings.Split(view.View(), "\n")
			if len(lines) != tc.height {
				t.Errorf("view is %d lines in a %d-line terminal", len(lines), tc.height)
			}
			for i, line := range lines {
				if n := len([]rune(stripANSI(line))); n > tc.width {
					t.Errorf("line %d is %d columns wide in a %d-column terminal: %q", i, n, tc.width, line)
				}
			}
		})
	}
}

// TestTheStatusBarRightAlignsItsHints holds the two halves of the bar apart. The
// gap is computed from the terminal width, so anything that collapses runs of
// spaces silently undoes it and the hints drift back against the workspace name.
func TestTheStatusBarRightAlignsItsHints(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main"})
	bar := lastLine(m.View())

	if !strings.HasSuffix(strings.TrimRight(bar, " "), "q quit") {
		t.Errorf("the key hints should sit at the right-hand end, got %q", bar)
	}
	if !strings.Contains(bar, "main") {
		t.Errorf("the bar should still name the workspace, got %q", bar)
	}
	if !strings.Contains(bar, "  ") {
		t.Errorf("the two halves should be pushed apart, got %q", bar)
	}
}

func lastLine(view string) string {
	lines := strings.Split(view, "\n")
	return lines[len(lines)-1]
}
