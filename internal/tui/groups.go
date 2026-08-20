package tui

// Groups (SPEC §6, §7.1): a group is a named, coloured rectangle of the map,
// and a node is in it exactly when its cell falls inside. There is no
// membership list anywhere here — see docs/adr/0001-groups-are-spatial.md — so
// moving a card out of a rectangle takes it out of the group, and the card says
// so on its bottom border rather than changing silently.
//
// `g` creates one from the selection, gathering its nodes together first so the
// rectangle is tight; with the cursor inside a group it adds the selection to
// that group instead, growing the rectangle when no cell inside is free.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// maxGroupTitleLen bounds the name prompt. A group title is drawn in the
// rectangle's top border and again on every member card's bottom border, so it
// has less room than a node's title rather than more.
const maxGroupTitleLen = 32

// group is `g`. Which of its two jobs it does is decided by where the cursor
// is, not by a mode: inside a group, `g` adds to that group; anywhere else it
// makes a new one.
func (m Model) group() (tea.Model, tea.Cmd) {
	ids := m.targets()
	if len(ids) == 0 {
		return m, nil
	}
	if g, ok := m.ws.GroupAt(m.ws.Viewport.Cursor); ok {
		return m.joinGroup(g, ids), nil
	}
	// The targets are fixed here rather than read again at Enter, for the same
	// reason the kill prompt fixes its own: a node arriving while the prompt is
	// up moves the cursor, and the user would then group something they never
	// chose.
	m.mode, m.input, m.grouping = modeGroupTitle, "", ids
	return m, nil
}

func (m Model) updateGroupTitle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m, act := m.typed(msg, maxGroupTitleLen)
	switch act {
	case actCancel:
		// Nothing has moved yet: the gather happens on commit, so Esc leaves the
		// map exactly as it was.
		m.mode, m.input, m.grouping = modeNormal, "", nil
	case actCommit:
		ids, title := m.grouping, strings.TrimSpace(m.input)
		m.mode, m.input, m.grouping = modeNormal, "", nil
		return m.createGroup(ids, title), nil
	}
	return m, nil
}

// createGroup gathers the nodes together and draws the rectangle round them.
// Gathering first is what keeps a group tight: two nodes at opposite ends of
// the map would otherwise make a rectangle swallowing everything between them,
// and containment is membership.
func (m Model) createGroup(ids []string, title string) Model {
	under, _ := m.selected()
	nodes, rect := m.ws.Gather(ids)
	if cols, rows := rect.Size(); cols == 0 || rows == 0 {
		// Nothing named is on the map any more — killed while the prompt was
		// up, or left behind by a workspace switch. A rect with no cells in it
		// would be a group nothing can ever be in and nothing can draw.
		return m
	}
	id := m.ws.NewGroupID()
	if title == "" {
		// A rectangle whose border says nothing is a group you cannot name on a
		// card; the id is what it was called before it was named.
		title = id
	}
	m.ws.Nodes = nodes
	m.ws.Groups = append(append([]state.Group(nil), m.ws.Groups...), state.Group{
		ID: id, Title: title, Colour: nextGroupColour(m.ws.Groups), Rect: rect,
	})
	return m.chase(under.ID).save()
}

// joinGroup moves the targets into an existing group. A target already inside
// is already a member, so it is left where it is.
func (m Model) joinGroup(g state.Group, ids []string) Model {
	under, _ := m.selected()
	nodes, rect := m.ws.Join(g.Rect, ids)
	groups := append([]state.Group(nil), m.ws.Groups...)
	for i := range groups {
		if groups[i].ID == g.ID {
			groups[i].Rect = rect
		}
	}
	m.ws.Nodes, m.ws.Groups = nodes, groups
	return m.chase(under.ID).save()
}

// chase puts the cursor back on a node that has just been moved out from under
// it. Gathering and joining both move cards, and a cursor left on the cell they
// used to occupy is a cursor on nothing.
func (m Model) chase(id string) Model {
	if n, ok := m.node(id); ok {
		m.ws.Viewport.Cursor = n.Pos
	}
	return m.follow()
}

// nextGroupColour steps through the accent palette, so that two groups drawn
// side by side are told apart without being coloured by hand.
func nextGroupColour(groups []state.Group) string {
	return colours[len(groups)%len(colours)].Name
}

// groupOf is the title of the group a node's cell falls inside, which is the
// only thing membership ever is. Empty for a node in none — including one that
// has just been moved out of a rectangle, which is what makes that visible.
func (m Model) groupOf(n state.Node) string {
	g, ok := m.ws.GroupAt(n.Pos)
	if !ok {
		return ""
	}
	return g.Title
}

// groupStyles are how a group's rectangle is drawn: its walls in the accent
// colour it carries, and the region inside them tinted with the same colour, so
// the cards standing on it read as sitting on something rather than beside it.
func groupStyles(g state.Group) (wall, tint lipgloss.Style) {
	code, ok := colourCode(g.Colour)
	if !ok {
		return cardStyle, lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(code)),
		lipgloss.NewStyle().Background(lipgloss.Color(code))
}

// colourCode is the ANSI code behind an accent name, and whether the palette
// knows the name at all.
func colourCode(name string) (string, bool) {
	for _, c := range colours {
		if c.Name == name {
			return c.Code, true
		}
	}
	return "", false
}

// groupFrame draws every group into the one-cell gaps the card grid leaves
// between and around its cards, as a grid of one styled string per screen cell.
// The cards are drawn over the top of it, which is what puts a group behind its
// members without anything having to composite two layers of colour.
//
// ponytail: one styled string per cell, so a tinted group costs an escape
// sequence per space it covers. It is a screenful at most, once per frame.
func (m Model) groupFrame(min, max state.Cell) [][]string {
	cols, rows := max.Col-min.Col+1, max.Row-min.Row+1
	width, height := cols*(cardWidth+cellGap)+cellGap, rows*m.cardRows()+1
	grid := make([][]string, height)
	for y := range grid {
		grid[y] = make([]string, width)
		for x := range grid[y] {
			grid[y][x] = " "
		}
	}
	for _, g := range m.ws.Groups {
		m.drawGroup(grid, min, max, g)
	}
	return grid
}

// drawGroup paints one rectangle into the frame. Everything is clipped to the
// grid rather than skipped: a group hanging off the side of the screen is drawn
// as far as the screen goes, which is what makes scrolling into one look like
// arriving at it.
func (m Model) drawGroup(grid [][]string, min, max state.Cell, g state.Group) {
	// A cell either side of what is drawn is enough to put the walls in, and
	// clipping to it is what keeps a rect the size of the map — or one a hand
	// edit has stretched across a million cells — costing no more than the
	// screen it covers. What is clipped away lands outside the frame, so a wall
	// or a corner cut off here is simply not drawn.
	rect := state.Rect{
		Min: state.Cell{Col: maxInt(g.Rect.Min.Col, min.Col-1), Row: maxInt(g.Rect.Min.Row, min.Row-1)},
		Max: state.Cell{Col: minInt(g.Rect.Max.Col, max.Col+2), Row: minInt(g.Rect.Max.Row, max.Row+2)},
	}
	if cols, rows := rect.Size(); cols == 0 || rows == 0 {
		return
	}
	x0 := (rect.Min.Col - min.Col) * (cardWidth + cellGap)
	x1 := (rect.Max.Col - min.Col) * (cardWidth + cellGap)
	y0 := (rect.Min.Row - min.Row) * m.cardRows()
	y1 := (rect.Max.Row - min.Row) * m.cardRows()
	wall, tint := groupStyles(g)

	for y := y0 + 1; y < y1; y++ {
		for x := x0 + 1; x < x1; x++ {
			place(grid, x, y, " ", tint)
		}
	}
	// The title goes in the top border, exactly as a card's does — a group is
	// a box on the map and reads like one.
	place(grid, x0, y0, border(x1-x0+1, "╭─ ", g.Title, "─╮"), wall)
	place(grid, x0, y1, "╰"+strings.Repeat("─", maxInt(x1-x0-1, 0))+"╯", wall)
	for y := y0 + 1; y < y1; y++ {
		place(grid, x0, y, "│", wall)
		place(grid, x1, y, "│", wall)
	}
}

// place writes text into the frame one cell at a time, styled, and skips
// whatever falls outside it. A rune the terminal draws two cells wide takes two
// cells of the grid, or a CJK group title would shear every line it sits on.
func place(grid [][]string, x, y int, text string, style lipgloss.Style) {
	if y < 0 || y >= len(grid) {
		return
	}
	for _, r := range text {
		width := lipgloss.Width(string(r))
		if x >= 0 && x < len(grid[y]) {
			grid[y][x] = style.Render(string(r))
			for i := 1; i < width && x+i < len(grid[y]); i++ {
				// The cell the wide rune spills into is already drawn; anything
				// left in it would push the rest of the line along.
				grid[y][x+i] = ""
			}
		}
		x += width
	}
}
