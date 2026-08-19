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
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

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
	if m.handoff != "" {
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

	m.handoff = node.ID
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

// noteLines is a note body rendered for the card: markdown through glamour,
// broken into the lines the card shows it in.
//
// The result is cached on the body text. bodyHeight asks every node on the map
// how tall it wants to be on every frame, and a markdown parse per node per
// frame would make cursor movement cost more the more notes you have written.
// The returned slice is shared — read it, never write to it.
//
// ponytail: the cache is emptied wholesale once it outgrows noteCacheMax, which
// re-renders every visible note the next frame; reach for an LRU if a map of
// hundreds of notes ever makes that show.
func noteLines(body string) []string {
	if body == "" {
		return nil
	}
	noteMu.Lock()
	defer noteMu.Unlock()
	if lines, ok := noteCache[body]; ok {
		return lines
	}
	lines := renderNote(body)
	if len(noteCache) >= noteCacheMax {
		clear(noteCache)
	}
	noteCache[body] = lines
	return lines
}

// noteCacheMax is generous next to any real map: a workspace of a hundred notes
// still never empties it, and one note edited a hundred times is what it is for.
const noteCacheMax = 256

var (
	noteMu    sync.Mutex
	noteCache = map[string][]string{}
)

// renderNote is the markdown rendering itself (SPEC §6). Glamour is asked for a
// card's worth of width and nothing else — no margin, because two columns of
// eighteen is an eighth of the card.
func renderNote(body string) []string {
	out, err := noteMarkdown().Render(scrub(body))
	if err != nil {
		// A body that will not render is still a body: showing the markdown
		// source beats showing the user an error where their writing was.
		out = scrub(body)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, " ")
		// Glamour separates blocks generously, which is right on a full-width
		// page and not in a ten-line card: two blank lines between a list and
		// the paragraph after it is a fifth of the card spent on the gap.
		if blank(line) && len(lines) > 0 && blank(lines[len(lines)-1]) {
			continue
		}
		lines = append(lines, line)
	}
	return trimBlank(lines)
}

// trimBlank drops the empty lines glamour leaves above and below a document.
// Blank lines *inside* the body stay — they are what separates one paragraph
// from the next.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// blank reports whether a rendered line carries nothing but its own colouring.
func blank(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }

// scrub is what a note body has to survive before it is markdown. A body is
// pasted as often as it is typed, and an escape sequence reaching a card would
// repaint the map — glamour passes text it does not understand straight
// through, so this is the boundary, not the renderer.
func scrub(body string) string {
	return sanitiseKeeping(strings.ReplaceAll(body, "\t", "    "), '\n')
}

// noteMarkdown is the renderer, built once: constructing one compiles a chroma
// theme and a goldmark parser, which is not work to repeat per frame. New calls
// it at startup so that the terminal question markdownStyle asks is asked while
// Trigpoint still owns the terminal to ask it with.
func noteMarkdown() *glamour.TermRenderer {
	noteRendererOnce.Do(func() {
		style := markdownStyle()
		noMargin := uint(0)
		style.Document.Margin = &noMargin

		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(style),
			glamour.WithWordWrap(cardBodyWidth),
			glamour.WithEmoji(),
		)
		if err != nil {
			// NewTermRenderer only fails on a style it cannot parse, and this
			// one is glamour's own; a renderer that is nil here would panic on
			// every frame, so fail loudly at the first note instead.
			panic("trig: cannot build the note renderer: " + err.Error())
		}
		noteRenderer = r
	})
	return noteRenderer
}

var (
	noteRendererOnce sync.Once
	noteRenderer     *glamour.TermRenderer
)

// markdownStyle is the same choice glamour's own auto style makes — dark or
// light by the terminal, and plain text when there is no terminal to ask. It is
// made here rather than through WithAutoStyle because the margin has to come off
// the style, and WithAutoStyle hands back a renderer, not a style.
func markdownStyle() gansi.StyleConfig {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return styles.NoTTYStyleConfig
	}
	if lipgloss.HasDarkBackground() {
		return styles.DarkStyleConfig
	}
	return styles.LightStyleConfig
}
