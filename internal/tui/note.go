package tui

// Note nodes are the one kind with no tmux session behind them (SPEC §6). They
// are edited by the same mechanism attach uses — the whole terminal is released
// to another program and taken back when it exits — so nothing here talks to
// tmux, and nothing here may assume a node has a session.

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// noteEditedMsg carries a note's body back from $EDITOR, along with whatever
// went wrong while the terminal was not Trigpoint's. The two are independent:
// an editor that wrote the file and then exited non-zero — vim's :cq, or any
// wrapper that forwards a status — has still done the user's writing, and
// throwing it away because of the exit code would lose it for good.
type noteEditedMsg struct {
	id     string
	body   string
	edited bool // the file was read back, whatever the editor's exit status
	err    error
}

// editNote hands the terminal to $EDITOR on the note's body. It is Enter's
// meaning on a note, the way attach is Enter's meaning on a shell node.
func (m Model) editNote(node state.Node) (tea.Model, tea.Cmd) {
	if m.handingOff {
		return m, nil
	}
	editor, collect, err := editorSession(node.Note)
	if err != nil {
		// An editor that cannot be prepared stays on the map and says why,
		// rather than releasing the terminal to nothing.
		m.status = err.Error()
		return m, nil
	}
	// The editor writes its own complaints to stderr, which would otherwise be
	// left under the repainted map.
	var stderr strings.Builder
	editor.Stderr = &stderr

	m.handingOff = true
	return m, tea.ExecProcess(editor, func(err error) tea.Msg {
		body, collectErr := collect()
		return noteEditedMsg{
			id:     node.ID,
			body:   body,
			edited: collectErr == nil,
			err:    cmp.Or(exitErr(err, stderr.String()), collectErr),
		}
	})
}

// editorSession prepares an edit: the command that takes the terminal, and the
// collect to run once it has given the terminal back. The body goes out through
// a temp file because that is the only thing every editor agrees on — an editor
// is a command that is given a path, not a filter.
func editorSession(body string) (*exec.Cmd, func() (string, error), error) {
	// Split on whitespace rather than handed to a shell: EDITOR carrying flags
	// ("emacsclient -nw") is ordinary, and a shell would also give the path a
	// meaning it should not have.
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		return nil, nil, errors.New("$EDITOR is not set, so there is nothing to edit the note with")
	}

	// CreateTemp makes the file 0600: a note body is the user's writing, and it
	// spends this moment somewhere world-readable.
	f, err := os.CreateTemp("", "trig-note-*.md")
	if err != nil {
		return nil, nil, fmt.Errorf("making a scratch file for $EDITOR: %w", err)
	}
	path := f.Name()
	_, writeErr := f.WriteString(body)
	if err := cmp.Or(writeErr, f.Close()); err != nil {
		os.Remove(path)
		return nil, nil, fmt.Errorf("writing the note out for $EDITOR: %w", err)
	}

	args := append(append([]string{}, editor[1:]...), path)
	return exec.Command(editor[0], args...), func() (string, error) {
		defer os.Remove(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading the note back from $EDITOR: %w", err)
		}
		// Editors add a trailing newline; keeping it would show as a blank last
		// line on the card and grow the map by a line for nothing.
		return strings.TrimRight(string(raw), "\n"), nil
	}, nil
}

// noteLines is a note body as the lines a card shows it in. The rendering is
// deliberately the markdown itself rather than a formatted version of it — the
// body is a handful of lines in an 18-column card, where a renderer's headings
// and rules cost more room than they buy.
//
// Tabs become spaces and control characters go, because a body is pasted as
// often as it is typed and an escape sequence reaching a card would repaint the
// map. Leading spaces survive: indentation is what a markdown list means by
// nesting, and collapsing it would make the body less readable than the text
// the user wrote, not more.
//
// ponytail: plain text, no markdown renderer; reach for glamour if bodies ever
// grow past a few lines.
func noteLines(body string) []string {
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = sanitise(strings.ReplaceAll(line, "\t", "    "))
	}
	return lines
}
