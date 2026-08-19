// Package tui renders the map view: the home screen a workspace's map is seen
// through. The map persists; the view is only what you are looking at it with.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

var (
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238")).Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// Model is the map view's state. The workspace it renders is owned here; the
// terminal size arrives from Bubble Tea and is zero until the first resize.
type Model struct {
	cfg config.Config
	ws  state.Workspace

	width, height int
	confirmQuit   bool
}

func New(cfg config.Config, ws state.Workspace) Model {
	return Model{cfg: cfg, ws: ws}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.confirmQuit {
			return m.updateConfirmQuit(msg)
		}
		if msg.String() == "q" {
			if m.cfg.General.ConfirmQuit {
				m.confirmQuit = true
				return m, nil
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) updateConfirmQuit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, tea.Quit
	case "n", "N", "esc":
		m.confirmQuit = false
	}
	return m, nil
}

// View renders the map above the status bar, filling the terminal exactly. It is
// called before the first resize, when the size is still unknown.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 1 {
		return ""
	}
	body := lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height - 1).
		Render(lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, m.mapView()))
	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())
}

func (m Model) mapView() string {
	if len(m.ws.Nodes) == 0 {
		return hintStyle.Render("The map is empty.")
	}
	// Cards land in the next ticket; until then a placed node is only a count.
	return hintStyle.Render(fmt.Sprintf("%d nodes placed.", len(m.ws.Nodes)))
}

func (m Model) statusBar() string {
	bar := statusStyle.Width(m.width).MaxWidth(m.width)
	if m.confirmQuit {
		return bar.Render("Quit Trigpoint? Sessions keep running. (y/n)")
	}
	left := fmt.Sprintf("%s · %s", m.ws.Name, pluralise(len(m.ws.Nodes), "node"))
	right := "q quit"

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2 // statusStyle's padding
	if gap < 1 {
		return bar.Render(left)
	}
	return bar.Render(left + strings.Repeat(" ", gap) + right)
}

func pluralise(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
