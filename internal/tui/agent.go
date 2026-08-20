package tui

// Agent nodes (SPEC §6, §7.3): a shell that immediately runs an agent command
// line, chosen from the presets config offers or typed by hand. The node stores
// what it was started with, so a respawn re-runs the agent rather than dropping
// to a bare shell — an agent node is defined by having been started as one, not
// by having a live agent (CONTEXT.md, "Agent node").

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// maxCmdLen bounds a hand-typed command line. Like a title it is typed by hand
// and stored, so it is a trust boundary; unlike a title it is handed to a shell,
// which is what startCmd's quoting is for — and why a command that reaches the
// limit is refused rather than cut: half a command is a different command, and
// one that can end mid-quote.
const maxCmdLen = 200

// customAgent is the picker's last entry: the way to an agent config knows
// nothing about. It is chosen by position rather than by name, so a preset
// actually called "custom" is still an entry of its own.
const customAgent = "custom…"

// openAgents puts the picker up. Presets are read from config every time it
// opens, and there is no list of them anywhere else — adding one is an edit to
// config, never a code change (§6).
func (m Model) openAgents() (tea.Model, tea.Cmd) {
	names := make([]string, 0, len(m.cfg.Agents)+1)
	for name := range m.cfg.Agents {
		names = append(names, name)
	}
	// Sorted, so that the same config offers the same first preset every time
	// rather than whichever one the map happened to range over first.
	sort.Strings(names)
	m.mode, m.candidates, m.choice = modeAgent, append(names, customAgent), 0
	return m, nil
}

// updateAgent is the picker's own keys, the same ones adoption and the
// workspace list answer.
func (m Model) updateAgent(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.choice = (m.choice + 1) % len(m.candidates)
	case "k", "up":
		m.choice = (m.choice + len(m.candidates) - 1) % len(m.candidates)
	case "enter":
		if m.choice == len(m.candidates)-1 {
			// The candidates are kept while the command is collected, so Esc
			// there goes back to the list rather than out to the map.
			m.mode, m.input = modeAgentCmd, ""
			return m, nil
		}
		name := m.candidates[m.choice]
		cmd := m.cfg.Agents[name].Cmd
		if strings.TrimSpace(cmd) == "" {
			// A preset with no command would make an agent node that runs
			// nothing — a shell node wearing the wrong label. Naming the preset
			// says which line of the config file to go and look at.
			m = m.closePicker()
			m.status = "The agent preset " + flatten(name) + " has no command."
			return m, nil
		}
		return m.closePicker().promptAgentTitle(cmd), nil
	case "esc", "q":
		return m.closePicker(), nil
	}
	return m, nil
}

// updateAgentCmd collects a command line for the custom entry.
func (m Model) updateAgentCmd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m, act := m.typed(msg, maxCmdLen)
	switch act {
	case actCancel:
		m.mode, m.input = modeAgent, ""
	case actCommit:
		cmd := strings.TrimSpace(m.input)
		if cmd == "" {
			// Nothing to run, so nothing to name: the prompt keeps collecting,
			// and Esc is still the way out of it.
			return m, nil
		}
		if len([]rune(m.input)) >= maxCmdLen {
			// typed stops taking runes at the limit, so a command this long is
			// one that has already lost its end — a paste, most likely, and
			// silently starting the half of it that fits is worse than saying
			// so.
			m = m.closePicker()
			m.status = fmt.Sprintf("That command is too long: %d characters is the most an agent node holds.", maxCmdLen)
			return m, nil
		}
		return m.closePicker().promptAgentTitle(cmd), nil
	}
	return m, nil
}

// promptAgentTitle carries the chosen command into the title prompt n and N
// already use, so an agent node is named the same way every other node is.
func (m Model) promptAgentTitle(cmd string) Model {
	m.mode, m.creating, m.creatingCmd = modeTitle, state.KindAgent, cmd
	// Prefilled with the command's own name, so ⏎ names the card after what it
	// runs. The other prompts open empty because they have nothing to suggest.
	m.input = clampRunes(sanitise(firstWord(cmd)), maxTitleLen)
	return m
}

func firstWord(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return s
}

// startCmd is what tmux is actually given for a node's session. For an agent
// node it is not the command: tmux ends a session whose command exits, and an
// agent node whose agent has exited is still an agent node with a shell in it
// (§6), so the command hands the pane over to a shell instead of being the last
// thing in it. Every other kind is given its command as it stands — only an
// agent node promises a shell behind whatever it was started with. See
// docs/adr/0014-an-agent-node-hands-its-pane-to-a-shell.md.
//
// The whole thing goes to tmux as one word, so the command is quoted rather
// than interpolated — it is typed by hand, and a quote in it must not be able
// to close the word and turn the rest into commands of tmux's own.
func startCmd(n state.Node) string {
	if n.Cmd == "" || n.Kind != state.KindAgent {
		return n.Cmd
	}
	return "sh -c " + shellQuote(n.Cmd+`; exec "${SHELL:-/bin/sh}"`)
}

// shellQuote wraps s in single quotes, ending and reopening them around any
// quote of its own — the one escaping a single-quoted shell word allows.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
