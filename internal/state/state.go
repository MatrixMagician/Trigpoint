// Package state holds a workspace — the map of nodes and groups belonging to one
// project — and reads and writes it to disk. Every write is atomic: Trigpoint is
// crash-only, so a workspace file must never be observed half written.
package state

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Kind distinguishes what sits behind a node. Note nodes have no session at all,
// so nothing here may assume one exists.
type Kind string

const (
	KindShell Kind = "shell"
	KindAgent Kind = "agent"
	KindNote  Kind = "note"
)

// CardSize selects how many preview lines a card shows; the counts live in config.
type CardSize string

const (
	SizeS CardSize = "s"
	SizeM CardSize = "m"
	SizeL CardSize = "l"
)

// Cell is a position on the map. The map is an infinite cell grid rather than free
// pixels, which is what makes directional keyboard movement deterministic.
type Cell struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

// Rect is a cell-space rectangle, inclusive of Min and exclusive of Max.
type Rect struct {
	Min Cell `json:"min"`
	Max Cell `json:"max"`
}

// Viewport is where the user was last looking, restored on the next launch.
type Viewport struct {
	Cursor Cell `json:"cursor"`
	Offset Cell `json:"offset"`
}

// Node is one named, placed, long-lived thing on the map. A node owns its identity;
// it does not own the tmux session behind it.
type Node struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title"`
	Colour    string    `json:"colour,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	Pos       Cell      `json:"pos"`
	Size      CardSize  `json:"size,omitempty"`
	Cmd       string    `json:"cmd,omitempty"`
	Dir       string    `json:"dir,omitempty"`
	Host      string    `json:"host,omitempty"`
	Note      string    `json:"note,omitempty"`
	Pinned    bool      `json:"pinned,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Session is the name of a session Trigpoint did not create, adopted onto
	// the map (§9.3). Empty for every node whose session Trigpoint named itself,
	// which is what tells the two apart — see
	// docs/adr/0007-an-adopted-node-stores-its-session-name.md.
	Session string `json:"session,omitempty"`
}

// HasSession reports whether a node has a tmux session behind it at all. Notes
// never do (§6), so this — and not the kind — is what every session-touching
// path asks: the session layer stays nil-tolerant, and a future kind with no
// session of its own costs nothing to add.
func (n Node) HasSession() bool { return n.Kind != KindNote }

// Adopted reports whether the session behind a node is one Trigpoint found
// rather than one it made. It is asked of the stored name and not of the
// `adopted` tag: tags are the user's to edit, and what a foreign session may be
// done to is not.
func (n Node) Adopted() bool { return n.Session != "" }

// Group is a named rectangular region of the map. Membership is containment and
// nothing else — there is deliberately no member list to disagree with the picture.
// See docs/adr/0001-groups-are-spatial.md.
type Group struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Colour string `json:"colour,omitempty"`
	Rect   Rect   `json:"rect"`
}

// Workspace is an independent map with its own nodes, groups, and default working
// directory. Workspaces share nothing.
type Workspace struct {
	Name     string   `json:"name"`
	Dir      string   `json:"dir,omitempty"`
	Nodes    []Node   `json:"nodes,omitempty"`
	Groups   []Group  `json:"groups,omitempty"`
	Viewport Viewport `json:"viewport"`
}

// MaxNameLen keeps a workspace name comfortably inside every filesystem's limit
// once ".json" and a temp-file suffix are appended. It is exported because the
// prompt that collects a name bounds what can be typed by it.
const MaxNameLen = 64

// MaxTitleLen keeps a title inside a card and inside a sensible status line.
// Titles are typed by hand — at a prompt or on the command line — so this is a
// trust boundary, not a style rule.
const MaxTitleLen = 48

// MaxCmdLen bounds a hand-typed command line. Like a title it is typed by hand
// and stored, but it is a command rather than a label: it has to hold a real
// invocation with flags and a path or two, and be refused rather than truncated
// — half a command line is a different command line.
const MaxCmdLen = 200

// ClampRunes cuts text to at most n runes. Runes and not cells: this is a bound
// on what is stored, and the card does its own cutting to fit the width it has.
func ClampRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// ValidName reports whether name is safe to use as a workspace file name. Names
// arrive from the command line, so this is a trust boundary: anything that could
// escape the state directory or confuse the filesystem is rejected outright.
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("workspace name is empty")
	case len(name) > MaxNameLen:
		return fmt.Errorf("workspace name is longer than %d characters", MaxNameLen)
	case name == "." || name == "..":
		return fmt.Errorf("workspace name %q is reserved", name)
	case strings.ContainsAny(name, `/\`+"\x00"):
		return fmt.Errorf("workspace name %q may not contain a path separator", name)
	case strings.ContainsAny(name, ".:"):
		// The name is carried into the tmux session name trig_<workspace>_<id>
		// (SPEC §5.2), and tmux uses "." and ":" to address windows and panes.
		return fmt.Errorf("workspace name %q may not contain %q or %q", name, ".", ":")
	case strings.ContainsFunc(name, unicode.IsSpace):
		// Same reason, one step further along: the session name is read back out
		// of tmux's own output — `list-sessions`, and the activity subscription
		// the preview monitor reads — where names are separated by whitespace
		// and a name containing some cannot be told from two names.
		return fmt.Errorf("workspace name %q may not contain whitespace", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("workspace name %q contains a control character", name)
		}
	}
	return nil
}

func path(dir, name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

// Load reads a workspace. A workspace that has never been saved is not an error —
// a first run simply starts with an empty map.
func Load(dir, name string) (Workspace, error) {
	file, err := path(dir, name)
	if err != nil {
		return Workspace{}, err
	}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return Workspace{Name: name}, nil
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("reading workspace %q: %w", name, err)
	}

	var ws Workspace
	if err := json.Unmarshal(raw, &ws); err != nil {
		return Workspace{}, fmt.Errorf("workspace %q is corrupt (%s): %w", name, file, err)
	}
	ws.Name = name
	return ws, nil
}

// Save writes a workspace atomically: a temp file in the same directory, flushed,
// then renamed over the target. There is no save command in the UI — every
// mutation calls this, and a kill at any moment leaves either the old file or the
// new one, never a mixture.
func Save(dir string, ws Workspace) error {
	file, err := path(dir, ws.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	raw, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding workspace %q: %w", ws.Name, err)
	}
	raw = append(raw, '\n')

	// ponytail: a SIGKILL landing between CreateTemp and Rename orphans a .tmp
	// file, and nothing sweeps them. The window is microseconds wide, so this is
	// left alone deliberately; add a glob sweep on Load if they ever show up.
	tmp, err := os.CreateTemp(dir, ws.Name+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file for workspace %q: %w", ws.Name, err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename has succeeded

	if err := writeAndSync(tmp, raw); err != nil {
		tmp.Close()
		return fmt.Errorf("writing workspace %q: %w", ws.Name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file for workspace %q: %w", ws.Name, err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return fmt.Errorf("replacing workspace %q: %w", ws.Name, err)
	}
	// The rename is only durable once the directory entry itself is flushed.
	// Without this the contents fsync above buys nothing across a power loss.
	// ponytail: two fsyncs per mutation; debounce saves if writes at cursor rate
	// ever hurt.
	return syncDir(dir)
}

// List names every workspace in dir, sorted, so that cycling with Tab visits
// them in the same order on every run. A state directory that does not exist yet
// holds no workspaces, which is not a failure to read it.
//
// A file that is not a workspace is not one: an orphaned ".tmp" from an
// interrupted save, and anything whose name Save would have refused to write,
// are skipped rather than offered as maps that cannot be opened.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the state directory: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || ValidName(name) != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Remove deletes a workspace. Only the file: the sessions its nodes named go on
// running, because Trigpoint does not own them and never kills what it did not
// start (SPEC §5.2). A workspace that is already gone is the outcome asked for
// rather than an error.
func Remove(dir, name string) error {
	file, err := path(dir, name)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting workspace %q: %w", name, err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening state directory to flush it: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("flushing state directory: %w", err)
	}
	return nil
}

func writeAndSync(f *os.File, raw []byte) error {
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	return f.Sync()
}

// Dir is where workspace files live. Go's standard library has no equivalent of
// os.UserConfigDir for state, so XDG_STATE_HOME is honoured by hand.
func Dir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating the state directory: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "trig", "workspaces"), nil
}

// idAlphabet leaves out vowels and the digits that read as letters, so a slug
// never spells a word and never has to be read aloud twice.
const idAlphabet = "bcdfghjklmnpqrstvwxz23456789"

const idLen = 4

// NewNodeID draws a short slug that no node on this map is already using. Ids
// are immutable and end up inside the tmux session name, so they keep to
// characters tmux gives no meaning to.
func (ws Workspace) NewNodeID() string {
	taken := make(map[string]bool, len(ws.Nodes))
	for _, n := range ws.Nodes {
		taken[n.ID] = true
	}
	return uniqueID(taken)
}

// uniqueID draws a slug nothing in taken is already using.
func uniqueID(taken map[string]bool) string {
	for {
		if id := newID(); !taken[id] {
			return id
		}
	}
}

func newID() string {
	buf := make([]byte, idLen)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on any supported platform; if it ever does,
		// carrying on with predictable ids would be worse than stopping.
		panic("trig: no randomness available for node ids: " + err.Error())
	}
	for i, b := range buf {
		buf[i] = idAlphabet[int(b)%len(idAlphabet)]
	}
	return string(buf)
}

// NearestFreeCell finds the closest cell to from that no node occupies,
// searching outward ring by ring. One node per cell is the map's only layout
// rule, so this is what "place it near the cursor" means.
func (ws Workspace) NearestFreeCell(from Cell) Cell {
	occupied := make(map[Cell]bool, len(ws.Nodes))
	for _, n := range ws.Nodes {
		occupied[n.Pos] = true
	}
	for radius := 0; ; radius++ {
		for _, c := range ring(from, radius) {
			if !occupied[c] {
				return c
			}
		}
	}
}

// ring lists the cells exactly radius steps from centre, in a fixed order so
// that the same map and the same cursor always place a node in the same cell.
func ring(centre Cell, radius int) []Cell {
	if radius == 0 {
		return []Cell{centre}
	}
	var cells []Cell
	for col := centre.Col - radius; col <= centre.Col+radius; col++ {
		for row := centre.Row - radius; row <= centre.Row+radius; row++ {
			if abs(col-centre.Col) == radius || abs(row-centre.Row) == radius {
				cells = append(cells, Cell{Col: col, Row: row})
			}
		}
	}
	return cells
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Shift returns the nodes with those named by ids moved one step of d, and with
// anything standing in their way shoved the same step. It is the map's only
// collision rule: node movement passes one id, group movement will pass the
// members of the group, and both get the same behaviour because it is the same
// code — there is deliberately no variant that refuses the move instead.
//
// The original nodes are left untouched: a Model is copied by value all over
// Bubble Tea, and moving in place would edit the map every older copy holds.
func (ws Workspace) Shift(ids []string, d Cell) []Node {
	moving := make(map[string]bool, len(ids))
	for _, id := range ids {
		moving[id] = true
	}
	// A cell holds one node, but the file on disk is a file: two nodes stacked
	// by a hand edit both have to be recruited, or a shove can never separate
	// them again.
	at := make(map[Cell][]string, len(ws.Nodes))
	for _, n := range ws.Nodes {
		at[n.Pos] = append(at[n.Pos], n.ID)
	}

	// Whoever stands where a mover is going joins the move, and so does whoever
	// stands in *their* way, until the line ahead has somewhere to go. The map is
	// infinite, so it always does, and each pass recruits at least one node or is
	// the last — so this terminates on the node count.
	// ponytail: O(n²) on a shove that cascades across the whole map; fine at the
	// map sizes a person can navigate by hand.
	for grew := true; grew; {
		grew = false
		for _, n := range ws.Nodes {
			if !moving[n.ID] {
				continue
			}
			for _, id := range at[Cell{Col: n.Pos.Col + d.Col, Row: n.Pos.Row + d.Row}] {
				if !moving[id] {
					moving[id] = true
					grew = true
				}
			}
		}
	}

	moved := make([]Node, len(ws.Nodes))
	copy(moved, ws.Nodes)
	for i := range moved {
		if moving[moved[i].ID] {
			moved[i].Pos = Cell{Col: moved[i].Pos.Col + d.Col, Row: moved[i].Pos.Row + d.Row}
		}
	}
	return moved
}
