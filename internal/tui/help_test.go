package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// The contextual hints as the status bar has always read them. They are the
// golden the tables are held to: the tables are what the bar and the overlay
// are both rendered from now, and a table edited without meaning to would show
// up here rather than on somebody's status bar.
const (
	selectionHints = "HJKL move · g group · t tags · x kill · esc clear"
	heldHints      = "HJKL move · hjkl resize · x delete · esc done"
)

func helpMap(t *testing.T, keymap map[string]string) Model {
	t.Helper()
	m := remap(t, keymap)
	opened, _ := typeKeys(t, m, "?")
	if opened.mode != modeHelp {
		t.Fatalf("? should open the help overlay, mode = %v", opened.mode)
	}
	return opened
}

func TestQuestionMarkOpensTheHelpOverlay(t *testing.T) {
	view := helpMap(t, nil).View()

	for _, want := range []string{"Attach", "enter", "Kill node", "x"} {
		if !strings.Contains(view, want) {
			t.Errorf("the overlay should list %q, got:\n%s", want, view)
		}
	}
}

// TestTheOverlayIsGeneratedFromTheLiveKeymap is the one the overlay exists for
// (§7.3): a remapped key shows its new binding with nothing else changed.
func TestTheOverlayIsGeneratedFromTheLiveKeymap(t *testing.T) {
	view := helpMap(t, map[string]string{"kill": "d d"}).View()

	if !strings.Contains(view, "dd") {
		t.Errorf("the overlay should show the key kill is actually bound to, got:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Kill node") && strings.Contains(line, "x") {
			t.Errorf("the overlay should not still offer the old key: %q", line)
		}
	}
}

func TestTheOverlaySaysWhenAnActionIsUnbound(t *testing.T) {
	view := helpMap(t, map[string]string{"kill": ""}).View()

	found := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Kill node") {
			found = true
			if !strings.Contains(line, "palette only") {
				t.Errorf("an unbound action should say where it is still reachable: %q", line)
			}
		}
	}
	if !found {
		t.Errorf("an unbound action should still be listed, got:\n%s", view)
	}
}

// TestTheOverlayCoversTheModalContexts: the keys that mean something only while
// a selection is gathered, a group is held, or a prompt is up are the ones a
// user is least likely to guess (§7.3).
func TestTheOverlayCoversTheModalContexts(t *testing.T) {
	view := allOfHelp(t, helpMap(t, nil))

	for _, want := range []string{"Selection", "Group held", "Filter", "Palette", "Peek"} {
		if !strings.Contains(view, want) {
			t.Errorf("the overlay should cover %s, got:\n%s", want, view)
		}
	}
}

// allOfHelp scrolls the overlay to the bottom and returns everything it drew on
// the way, so a test can assert on a section below the fold.
func allOfHelp(t *testing.T, m Model) string {
	t.Helper()
	var seen strings.Builder
	for i := 0; i < 100; i++ {
		seen.WriteString(m.View())
		next := pressKeys(t, m, "j")
		if next.helpTop == m.helpTop {
			break
		}
		m = next
	}
	return seen.String()
}

func TestTheOverlayScrolls(t *testing.T) {
	m := helpMap(t, nil)
	top := m.View()

	scrolled := pressKeys(t, m, "j", "j", "j")
	if scrolled.helpTop == 0 {
		t.Fatal("j should scroll the overlay")
	}
	if scrolled.View() == top {
		t.Error("scrolling should change what is on screen")
	}
	// The window has ends: a scroll is a request, not a promise the list is
	// that long.
	far := m
	for i := 0; i < 200; i++ {
		far = pressKeys(t, far, "j")
	}
	if strings.Contains(far.View(), "\n\n\n\n") {
		t.Errorf("scrolling past the end should stop at it, got:\n%s", far.View())
	}
	if back := pressKeys(t, scrolled, "k", "k", "k", "k"); back.helpTop != 0 {
		t.Errorf("k should scroll back to the top, helpTop = %d", back.helpTop)
	}
}

func TestEscCloses(t *testing.T) {
	closed, _ := press(t, helpMap(t, nil), tea.KeyEsc)
	if closed.mode != modeNormal {
		t.Errorf("esc should close the overlay, mode = %v", closed.mode)
	}
}

// TestTheOverlayDoesNothingToTheMap: a key that means scroll here must not also
// mean move there, and closing must put back what was on screen.
func TestTheOverlayLeavesTheMapAlone(t *testing.T) {
	m := remap(t, nil)
	before := m.ws.Viewport.Cursor

	opened, _ := typeKeys(t, m, "?")
	scrolled := pressKeys(t, opened, "j", "l", "x")
	if scrolled.ws.Viewport.Cursor != before {
		t.Errorf("the overlay should not move the cursor, cursor = %v", scrolled.ws.Viewport.Cursor)
	}
	if scrolled.mode != modeHelp {
		t.Errorf("x should not open the kill prompt from inside the overlay, mode = %v", scrolled.mode)
	}
}

func TestTheOverlayFitsTheTerminal(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 40, Height: 10}, {Width: 14, Height: 4}} {
		m, err := New(config.Default(), state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{})
		if err != nil {
			t.Fatal(err)
		}
		sized, _ := m.Update(size)
		opened, _ := typeKeys(t, sized.(Model), "?")

		lines := strings.Split(opened.View(), "\n")
		if len(lines) != size.Height {
			t.Errorf("the overlay is %d lines in a %d-line terminal", len(lines), size.Height)
		}
		for i, line := range lines {
			if n := len([]rune(stripANSI(line))); n > size.Width {
				t.Errorf("line %d is %d columns wide in a %d-column terminal: %q", i, n, size.Width, line)
			}
		}
	}
}

// TestTheStatusBarAndTheOverlayAgreeAboutAContext holds the modal tables' one
// job: the bar's hints and the overlay's section are two renderings of one list.
func TestTheStatusBarAndTheOverlayAgreeAboutAContext(t *testing.T) {
	for _, keys := range []contextKeys{selectionKeys, heldKeys, peekKeys, filterKeys, paletteKeys} {
		if keys.title == "" {
			t.Error("a context needs a title for the overlay to put it under")
		}
		if len(keys.keys) == 0 {
			t.Errorf("%s has no keys", keys.title)
		}
		for _, k := range keys.keys {
			if k.keys == "" || k.short == "" {
				t.Errorf("%s has a key with nothing to say about it: %+v", keys.title, k)
			}
		}
	}
	if got := selectionKeys.hints(); got != selectionHints {
		t.Errorf("the selection hints should come from the table, got %q want %q", got, selectionHints)
	}
	if got := heldKeys.hints(); got != heldHints {
		t.Errorf("the held hints should come from the table, got %q want %q", got, heldHints)
	}
}

// TestResizingReclampsTheOverlay: a taller terminal shows more of the list, so
// the line it starts at has a lower ceiling — one the overlay would otherwise
// keep until the next scroll key, drawing dead space under a short page.
func TestResizingReclampsTheOverlay(t *testing.T) {
	m := helpMap(t, nil)
	bottom := m
	for i := 0; i < 200; i++ {
		bottom = pressKeys(t, bottom, "j")
	}
	if bottom.helpTop == 0 {
		t.Fatal("the overlay should have scrolled")
	}

	grown, _ := bottom.Update(tea.WindowSizeMsg{Width: 80, Height: 200})
	if top := grown.(Model).helpTop; top != 0 {
		t.Errorf("a terminal tall enough for the whole list should start at its top, helpTop = %d", top)
	}
}
