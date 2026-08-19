package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// fakeEditor writes a shell script standing in for $EDITOR and points $EDITOR at
// it. The script is handed the temp file as $1, exactly as a real editor is, so
// a test can say what the user did in there by writing the file or not.
func fakeEditor(t *testing.T, script string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatalf("writing the fake editor: %v", err)
	}
	t.Setenv("EDITOR", path)
}

// mapWithOneNote is a map holding a single note with the cursor on it.
func mapWithOneNote(t *testing.T, body string) (Model, *fakeSessions, string) {
	t.Helper()
	return newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindNote, Title: "todo", Note: body},
	}})
}

func TestNCreatesANoteNodeWithNoSession(t *testing.T) {
	m, sessions, dir := newNodeModel(t, state.Workspace{Name: "main"})

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m, cmd := typeKeys(t, next.(Model), "todo")
	m, cmd = press(t, m, tea.KeyEnter)
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 1 {
		t.Fatalf("N should put a note on the map, got %d nodes (status %q)", len(m.ws.Nodes), m.status)
	}
	if got := m.ws.Nodes[0].Kind; got != state.KindNote {
		t.Errorf("N created a %q node, want %q", got, state.KindNote)
	}
	if got := m.ws.Nodes[0].Title; got != "todo" {
		t.Errorf("note title is %q, want %q", got, "todo")
	}
	if len(sessions.created) != 0 {
		t.Errorf("a note has no session, but tmux was asked for %+v", sessions.created)
	}

	// A note reaches the map with no tmux round trip to wait for, so it is on
	// disk by the time the keystroke is over.
	ws, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the workspace: %v", err)
	}
	if len(ws.Nodes) != 1 || ws.Nodes[0].Kind != state.KindNote {
		t.Errorf("the note should have been saved, got %+v", ws.Nodes)
	}
}

func TestANoteCardShowsItsBody(t *testing.T) {
	m, _, _ := mapWithOneNote(t, "# plan\nship the thing")

	view := m.View()
	for _, want := range []string{"# plan", "ship the thing"} {
		if !strings.Contains(view, want) {
			t.Errorf("the note card should show %q, got:\n%s", want, view)
		}
	}
}

func TestACardWithNothingToShowKeepsItsTwoLines(t *testing.T) {
	// No note to render and no preview lines configured: a card with nothing to
	// put in a body is exactly its two borders.
	cfg := config.Default()
	cfg.General.PreviewLines = config.PreviewLines{}
	sessions := &fakeSessions{}
	m := New(cfg, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindShell, Title: "api"},
	}}, t.TempDir(), sessions)
	if got := m.cardRows(); got != cardRowsNoBody {
		t.Errorf("a card with nothing to show should lay out %d-row cells, got %d", cardRowsNoBody, got)
	}
}

func TestEnterOnANoteOpensTheEditorRatherThanAttaching(t *testing.T) {
	fakeEditor(t, "exit 0")
	m, sessions, _ := mapWithOneNote(t, "hello")

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd == nil {
		t.Fatalf("Enter on a note should hand the terminal to $EDITOR, status: %s", next.status)
	}
	if len(sessions.handoffs) != 0 {
		t.Errorf("a note has no session to attach to, got handoffs %+v", sessions.handoffs)
	}
}

func TestEnterOnANoteWithNoEditorSaysSo(t *testing.T) {
	t.Setenv("EDITOR", "")
	m, _, _ := mapWithOneNote(t, "hello")

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Error("with no $EDITOR there is nothing to hand the terminal to")
	}
	if !strings.Contains(next.status, "EDITOR") {
		t.Errorf("the status should name $EDITOR, got %q", next.status)
	}
}

func TestTheEditedNoteComesBack(t *testing.T) {
	fakeEditor(t, `printf 'edited\n' > "$1"`)

	cmd, collect, err := editorSession("before")
	if err != nil {
		t.Fatalf("preparing the editor: %v", err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the fake editor: %v", err)
	}
	body, err := collect()
	if err != nil {
		t.Fatalf("collecting the note: %v", err)
	}
	if body != "edited" {
		t.Errorf("the note came back as %q, want %q", body, "edited")
	}
	// The body lives in the workspace file; the temp file is scratch and should
	// not be left behind in /tmp after every edit.
	if _, err := os.Stat(cmd.Args[len(cmd.Args)-1]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp file should be gone, stat said %v", err)
	}
}

func TestAbandoningTheEditorLeavesTheBodyUnchanged(t *testing.T) {
	fakeEditor(t, "exit 0") // quit without writing

	cmd, collect, err := editorSession("# plan\nship the thing")
	if err != nil {
		t.Fatalf("preparing the editor: %v", err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the fake editor: %v", err)
	}
	body, err := collect()
	if err != nil {
		t.Fatalf("collecting the note: %v", err)
	}
	if body != "# plan\nship the thing" {
		t.Errorf("an abandoned edit changed the body to %q", body)
	}
}

func TestTheEditedBodyIsSavedToTheNode(t *testing.T) {
	m, _, dir := mapWithOneNote(t, "before")

	next, _ := m.Update(noteEditedMsg{id: "k4f2", body: "after", edited: true})
	m = next.(Model)

	if got := m.ws.Nodes[0].Note; got != "after" {
		t.Errorf("the node body is %q, want %q", got, "after")
	}
	ws, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the workspace: %v", err)
	}
	if len(ws.Nodes) != 1 || ws.Nodes[0].Note != "after" {
		t.Errorf("the edited body should persist across a restart, got %+v", ws.Nodes)
	}
}

func TestAnEditorThatNeverGaveTheBodyBackLeavesTheNoteAlone(t *testing.T) {
	m, _, _ := mapWithOneNote(t, "before")

	next, _ := m.Update(noteEditedMsg{id: "k4f2", err: errors.New("editor exploded")})
	m = next.(Model)

	if got := m.ws.Nodes[0].Note; got != "before" {
		t.Errorf("with nothing read back the body should be untouched, got %q", got)
	}
	if !strings.Contains(m.status, "exploded") {
		t.Errorf("the status should say what went wrong, got %q", m.status)
	}
}

// TestAnEditorThatWroteThenFailedKeepsTheWriting is the data-loss case. vim's
// :cq writes the file and exits non-zero on purpose, and plenty of $EDITOR
// wrappers forward a status of their own — discarding the body on a bad exit
// would throw away writing the user had already saved.
func TestAnEditorThatWroteThenFailedKeepsTheWriting(t *testing.T) {
	m, _, dir := mapWithOneNote(t, "before")

	next, _ := m.Update(noteEditedMsg{
		id: "k4f2", body: "after", edited: true, err: errors.New("exit status 1"),
	})
	m = next.(Model)

	if got := m.ws.Nodes[0].Note; got != "after" {
		t.Errorf("the writing should survive a bad exit status, got %q", got)
	}
	if !strings.Contains(m.status, "exit status 1") {
		t.Errorf("the status should still report the failure, got %q", m.status)
	}
	ws, err := state.Load(dir, "main")
	if err != nil {
		t.Fatalf("loading the workspace: %v", err)
	}
	if len(ws.Nodes) != 1 || ws.Nodes[0].Note != "after" {
		t.Errorf("the writing should be on disk too, got %+v", ws.Nodes)
	}
}

// TestTheEditorIsGivenTheWholeRoundTrip drives a real $EDITOR that writes and
// then exits non-zero, so the whole path is checked rather than the message.
func TestTheEditorIsGivenTheWholeRoundTrip(t *testing.T) {
	fakeEditor(t, `printf 'saved\n' > "$1"; exit 1`)

	cmd, collect, err := editorSession("before")
	if err != nil {
		t.Fatalf("preparing the editor: %v", err)
	}
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatal("the fake editor should have exited non-zero")
	}
	body, err := collect()
	if err != nil {
		t.Fatalf("collecting the note: %v", err)
	}
	if body != "saved" {
		t.Errorf("the written body is %q, wanted it read back regardless of the exit status", body)
	}
}

// TestANoteCardRendersItsMarkdown holds the body to SPEC §6: the card shows
// rendered markdown, not the source. Bullets become bullets, and nesting stays
// nested — which is the half a renderer is easiest to get wrong in a card this
// narrow.
func TestANoteCardRendersItsMarkdown(t *testing.T) {
	lines := noteLines("- ship\n  - notes")

	if len(lines) != 2 {
		t.Fatalf("expected two rendered lines, got %q", lines)
	}
	for _, line := range lines {
		if strings.Contains(line, "- ") {
			t.Errorf("the markdown source should not reach the card: %q", line)
		}
		if !strings.Contains(line, "•") {
			t.Errorf("a list item should render as a bullet, got %q", line)
		}
	}
	if indent(lines[1]) <= indent(lines[0]) {
		t.Errorf("a nested item should sit deeper than its parent, got %q", lines)
	}
}

func indent(line string) int {
	return len(line) - len(strings.TrimLeft(ansi.Strip(line), " "))
}

// TestANoteCardIsNeverWiderThanACard is the property the renderer has to hold
// whatever the markdown does: a body line wider than the card would push the
// column beside it out of the grid.
func TestANoteCardIsNeverWiderThanACard(t *testing.T) {
	for _, body := range []string{
		"# a heading long enough to need wrapping in a card this narrow",
		"a paragraph of ordinary prose that runs well past the width of one card",
		"`onelongunbrokentokenthatcannotbewrappedanywhereatall`",
		"| a | table | with | columns |\n| - | - | - | - |\n| 1 | 2 | 3 | 4 |",
		"日本語の長い行を折り返さなければならない場合はどうなるでしょうか",
	} {
		for _, line := range card(state.Node{Kind: state.KindNote, Title: "todo", Note: body}, false, false, false, maxNoteLines, noteLines(body)) {
			if w := lipgloss.Width(line); w != cardWidth {
				t.Errorf("a card line for %q is %d cells wide, want %d: %q", body, w, cardWidth, line)
			}
		}
	}
}

// TestAControlSequenceInANoteNeverReachesTheCard is the trust boundary: a body
// is pasted as often as it is typed, and glamour passes text it does not
// understand straight through, so an escape sequence would otherwise repaint
// the map from inside a card.
func TestAControlSequenceInANoteNeverReachesTheCard(t *testing.T) {
	const hostile = "harmless \x1b[2Jand \x1b]0;title\x07more\r\n\ttabbed"

	// The escape and the bell go; what is left of them is ordinary text, and
	// deleting that too would be editing the user's writing rather than
	// defusing it.
	scrubbed := scrub(hostile)
	for _, r := range scrubbed {
		if r < 0x20 && r != '\n' || r == 0x7f {
			t.Errorf("control character %q survived scrubbing: %q", r, scrubbed)
		}
	}
	if !strings.Contains(scrubbed, "harmless") || !strings.Contains(scrubbed, "tabbed") {
		t.Errorf("scrubbing should keep the writing, got %q", scrubbed)
	}
	if !strings.Contains(scrubbed, "    tabbed") {
		t.Errorf("a tab should become spaces rather than vanish, got %q", scrubbed)
	}

	for _, line := range noteLines(hostile) {
		if strings.ContainsRune(line, 0x1b) {
			t.Errorf("an escape sequence reached the card: %q", line)
		}
	}
}

// TestRenderedNotesAreCached keeps cursor movement off the markdown parser:
// bodyHeight asks every node on the map how tall it is on every frame.
func TestRenderedNotesAreCached(t *testing.T) {
	body := "# plan\n- ship notes"
	first := noteLines(body)
	if &first[0] != &noteLines(body)[0] {
		t.Error("the same body should come back from the cache, not be re-rendered")
	}
}

// TestANoteCardCarriesNoBadge holds the map to the domain: liveness does not
// apply to notes, so the status dot every other card carries would be a badge
// that means nothing.
func TestANoteCardCarriesNoBadge(t *testing.T) {
	note := card(state.Node{Kind: state.KindNote, Title: "todo"}, false, false, false, 0, nil)
	if strings.Contains(note[0], "●") {
		t.Errorf("a note is never alive or dead, so it carries no badge: %q", note[0])
	}
	shell := card(state.Node{Kind: state.KindShell, Title: "api"}, false, false, false, 0, nil)
	if !strings.Contains(shell[0], "●") {
		t.Errorf("a shell node should still carry its status dot: %q", shell[0])
	}
}

// TestALongNoteDoesNotCostTheWholeMapItsScreen keeps one node from setting the
// height of every card on the map without bound.
func TestALongNoteDoesNotCostTheWholeMapItsScreen(t *testing.T) {
	m, _, _ := mapWithOneNote(t, strings.Repeat("line\n", 200))
	if got := m.bodyHeight(); got != maxNoteLines {
		t.Errorf("a 200-line note asked for %d body lines, want the cap of %d", got, maxNoteLines)
	}
}

func TestASecondEnterDoesNotOpenTwoEditors(t *testing.T) {
	fakeEditor(t, "exit 0")
	m, _, _ := mapWithOneNote(t, "hello")

	next, cmd := press(t, m, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("the first Enter should open the editor")
	}
	if _, cmd = press(t, next, tea.KeyEnter); cmd != nil {
		t.Error("a second Enter before the terminal comes back should do nothing")
	}
}

func TestRemovingANoteKillsNoSession(t *testing.T) {
	m, sessions, dir := mapWithOneNote(t, "hello")

	m, _ = typeKeys(t, m, "x")
	m, cmd := typeKeys(t, m, "y")
	m = settle(t, m, cmd)

	if len(m.ws.Nodes) != 0 {
		t.Errorf("x then y should remove the note, got %+v", m.ws.Nodes)
	}
	if len(sessions.killed) != 0 {
		t.Errorf("a note has no session to kill, tmux was asked to kill %v", sessions.killed)
	}
	ws, _ := state.Load(dir, "main")
	if len(ws.Nodes) != 0 {
		t.Errorf("the removal should persist, got %+v", ws.Nodes)
	}
}

func TestTheKillPromptForANoteDoesNotMentionASession(t *testing.T) {
	m, _, _ := mapWithOneNote(t, "hello")

	m, _ = typeKeys(t, m, "x")
	bar := lastLine(m.View())
	if strings.Contains(bar, "session") {
		t.Errorf("a note has no session, so the prompt should not offer to kill one: %q", bar)
	}
	if !strings.Contains(bar, "todo") {
		t.Errorf("the prompt should name the note, got %q", bar)
	}
}

func TestNotesNeedNoLiveness(t *testing.T) {
	fakeEditor(t, "exit 0")
	m, sessions, _ := mapWithOneNote(t, "hello")
	sessions.existsErr = errors.New("tmux is not running")

	// Nothing about a note goes near tmux, so a tmux that cannot answer at all
	// must not stop a note being opened.
	next, cmd := press(t, m, tea.KeyEnter)
	if cmd == nil {
		t.Errorf("a note should open with no tmux server at all, status: %s", next.status)
	}
}

func TestTheEmptyMapMentionsNotes(t *testing.T) {
	m := newModel(t, config.Default(), state.Workspace{Name: "main"})
	if !strings.Contains(m.View(), "N") {
		t.Errorf("the empty map should say how to make a note, got:\n%s", m.View())
	}
}
