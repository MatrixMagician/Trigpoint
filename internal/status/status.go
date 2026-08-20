// Package status is the contract between Trigpoint and any agent (SPEC §8).
//
// An agent announces its own state by writing a small JSON file; Trigpoint
// reads the directory those files land in and never reads an agent's output to
// work out what it is doing. The hook plumbing that calls `trig emit-status` is
// expected to change with every agent release — this file format is not, which
// is why it is a package of its own and documented in the README.
//
// Absence of a report is not a state. A node with no file here is unknown, and
// unknown is drawn as nothing rather than as a guess (CONTEXT.md, "Agent
// status"). See
// docs/adr/0015-agent-status-is-a-directory-of-files-trigpoint-polls.md.
package status

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// State is what an agent says about itself. Four values and no others: a badge
// is a colour on a card, and a fifth state would be one nothing knows how to
// draw.
type State string

const (
	Running  State = "running"
	NeedsYou State = "needs_you"
	Done     State = "done"
	Error    State = "error"
)

// States is the whole vocabulary, in the order `trig emit-status` lists it.
var States = []State{Running, NeedsYou, Done, Error}

// Report is one agent's announcement, as it is written to disk.
type Report struct {
	State State `json:"state"`
	// TS is when the agent said it. It is what staleness is measured against,
	// and it is the agent's own stamp rather than the file's modification time:
	// a file copied or touched has not been reported again.
	TS     time.Time `json:"ts"`
	Detail string    `json:"detail,omitempty"`
}

// MaxDetailLen bounds a detail. It is written by an agent and drawn in
// Trigpoint's status bar, which makes it a trust boundary rather than a style
// rule — and a status bar has one line to spend on it.
const MaxDetailLen = 120

// ext is what marks a file in the status directory as one of ours. Anything
// else in there is somebody else's business and is left alone.
const ext = ".json"

// ParseState checks a state against the vocabulary. It exists so that
// `trig emit-status` refuses a typo at the point it is made, rather than
// writing a file that every later reader silently skips.
func ParseState(s string) (State, error) {
	for _, known := range States {
		if State(s) == known {
			return known, nil
		}
	}
	names := make([]string, len(States))
	for i, s := range States {
		names[i] = string(s)
	}
	return "", fmt.Errorf("unknown agent state %q: the states an agent may report are %s", s, strings.Join(names, ", "))
}

// DirBeside is the status directory (SPEC §9.1), as a sibling of the workspace
// files — which is what the map view already has in hand. Resolved from the
// state directory rather than from XDG a second time, so there is one answer to
// where Trigpoint keeps its state and not two that can drift.
//
// Nothing beside an empty path, so a model built without a state directory
// watches nothing rather than watching the working directory.
func DirBeside(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(stateDir), "status")
}

// Path is the file one node's agent reports to. The workspace is part of the
// name because node ids are unique against one map and not against every map
// there is — two workspaces can each hold a node called kt7m, and under a name
// of the id alone their agents would share one file.
func Path(dir, workspace, nodeID string) string {
	return filepath.Join(dir, workspace+"_"+nodeID+ext)
}

// Write records a report, stamped now. The write is atomic — temp file and
// rename, like the workspace file — because Trigpoint reads this directory on a
// tick and a half-written report must never be a report at all.
func Write(path string, state State, detail string) error {
	if _, err := ParseState(string(state)); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating the status directory: %w", err)
	}
	raw, err := json.Marshal(Report{State: state, TS: time.Now(), Detail: detail})
	if err != nil {
		return fmt.Errorf("encoding the status report: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating a temp file for the status report: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename has succeeded

	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing the status report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing the status report: %w", err)
	}
	// No fsync, unlike the workspace file: a report is a claim about a process
	// that is running now, so one lost to a power cut is one that would have
	// been wrong on the way back up anyway.
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing the status report: %w", err)
	}
	return nil
}

// Read is every report belonging to one workspace, by node id. A directory
// nobody has written to yet is no reports rather than a failure — it is what a
// map with no agents on it looks like from here.
//
// A file that does not parse, or that names a state outside the vocabulary, is
// skipped rather than reported: one agent writing rubbish must not cost every
// other card its badge, and a state Trigpoint cannot draw is the same as no
// report at all.
func Read(dir, workspace string) (map[string]Report, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the status directory %s: %w", dir, err)
	}

	prefix := workspace + "_"
	reports := make(map[string]Report, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ext) || !strings.HasPrefix(name, prefix) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		// Ids carry no underscore (state.NewNodeID), so anything left holding
		// one is another workspace's file whose name happens to start with this
		// workspace's — "main" against "main_thing_kt7m.json".
		if id == "" || strings.Contains(id, "_") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var report Report
		if err := json.Unmarshal(raw, &report); err != nil {
			continue
		}
		if _, err := ParseState(string(report.State)); err != nil {
			continue
		}
		report.Detail = clean(report.Detail)
		reports[id] = report
	}
	return reports, nil
}

// Remove takes a node's report with the node. Ids come back round — a node
// removed frees its id for the next one — so a report left behind is a badge
// the next node to be handed that id would wear as its own.
//
// A report that is not there is the outcome that was asked for.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing the status report %s: %w", path, err)
	}
	return nil
}

// RemoveWorkspace takes every report belonging to one workspace, for when the
// workspace itself is deleted: its nodes go with it, and a report is about a
// node. Only its own — the directory holds every map's.
//
// A directory that is not there, or a workspace that never reported, is the
// outcome that was asked for.
func RemoveWorkspace(dir, workspace string) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading the status directory %s: %w", dir, err)
	}
	prefix := workspace + "_"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ext) || !strings.HasPrefix(name, prefix) {
			continue
		}
		if err := Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// clean is what a detail has to survive to reach a status bar: no control
// characters, no newlines, and no more than one line's worth of it.
func clean(detail string) string {
	detail = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, detail)), " ")
	if runes := []rune(detail); len(runes) > MaxDetailLen {
		return string(runes[:MaxDetailLen-1]) + "…"
	}
	return detail
}
