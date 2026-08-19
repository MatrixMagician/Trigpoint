package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

func TestACardWithNoNoteOnTheMapKeepsItsTwoLines(t *testing.T) {
	m, _, _ := newNodeModel(t, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "k4f2", Kind: state.KindShell, Title: "api"},
	}})
	if got := m.cardRows(); got != cardRowsNoBody {
		t.Errorf("a map with no notes should still lay out %d-row cells, got %d", cardRowsNoBody, got)
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

// TestANoteCardKeepsItsIndentation guards the markdown a note is written in:
// nesting in a list is leading spaces, and collapsing them would make the card
// less readable than the text the user typed.
func TestANoteCardKeepsItsIndentation(t *testing.T) {
	m, _, _ := mapWithOneNote(t, "- ship\n  - notes\n\tthen previews")

	view := m.View()
	for _, want := range []string{"  - notes", "    then previews"} {
		if !strings.Contains(view, want) {
			t.Errorf("the card should keep the indentation of %q, got:\n%s", want, view)
		}
	}
}

// TestANoteCardCarriesNoBadge holds the map to the domain: liveness does not
// apply to notes, so the status dot every other card carries would be a badge
// that means nothing.
func TestANoteCardCarriesNoBadge(t *testing.T) {
	note := card(state.Node{Kind: state.KindNote, Title: "todo"}, false, 0)
	if strings.Contains(note[0], "●") {
		t.Errorf("a note is never alive or dead, so it carries no badge: %q", note[0])
	}
	shell := card(state.Node{Kind: state.KindShell, Title: "api"}, false, 0)
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
