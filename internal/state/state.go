// Package state holds a workspace — the map of nodes and groups belonging to one
// project — and reads and writes it to disk. Every write is atomic: Trigpoint is
// crash-only, so a workspace file must never be observed half written.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
}

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

// maxNameLen keeps a workspace name comfortably inside every filesystem's limit
// once ".json" and a temp-file suffix are appended.
const maxNameLen = 64

// ValidName reports whether name is safe to use as a workspace file name. Names
// arrive from the command line, so this is a trust boundary: anything that could
// escape the state directory or confuse the filesystem is rejected outright.
func ValidName(name string) error {
	switch {
	case name == "":
		return errors.New("workspace name is empty")
	case len(name) > maxNameLen:
		return fmt.Errorf("workspace name is longer than %d characters", maxNameLen)
	case name == "." || name == "..":
		return fmt.Errorf("workspace name %q is reserved", name)
	case strings.ContainsAny(name, `/\`+"\x00"):
		return fmt.Errorf("workspace name %q may not contain a path separator", name)
	case strings.ContainsAny(name, ".:"):
		// The name is carried into the tmux session name trig_<workspace>_<id>
		// (SPEC §5.2), and tmux uses "." and ":" to address windows and panes.
		return fmt.Errorf("workspace name %q may not contain %q or %q", name, ".", ":")
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
