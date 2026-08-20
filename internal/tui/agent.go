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
	m, act := m.typed(msg, state.MaxCmdLen)
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
		if len([]rune(m.input)) >= state.MaxCmdLen {
			// typed stops taking runes at the limit, so a command this long is
			// one that has already lost its end — a paste, most likely, and
			// silently starting the half of it that fits is worse than saying
			// so.
			m = m.closePicker()
			m.status = fmt.Sprintf("That command is too long: %d characters is the most an agent node holds.", state.MaxCmdLen)
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
	m.input = state.ClampRunes(sanitise(firstWord(cmd)), state.MaxTitleLen)
	return m
}

func firstWord(s string) string {
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return s
}
