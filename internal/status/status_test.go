package status

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if err := Write(Path(dir, "main", "kt7m"), NeedsYou, "waiting for approval"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reports, err := Read(dir, "main")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, ok := reports["kt7m"]
	if !ok {
		t.Fatalf("Read returned %v, want a report for kt7m", reports)
	}
	if got.State != NeedsYou {
		t.Errorf("state = %q, want %q", got.State, NeedsYou)
	}
	if got.Detail != "waiting for approval" {
		t.Errorf("detail = %q, want %q", got.Detail, "waiting for approval")
	}
	if time.Since(got.TS) > time.Minute {
		t.Errorf("ts = %v, want a stamp from just now", got.TS)
	}
}

// A report is written by an agent while the map may be reading it, so it has to
// arrive whole or not at all — the same rename the workspace file is written
// with.
func TestWriteReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir, "main", "kt7m")
	if err := Write(path, Running, "first"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Write(path, Done, "second"); err != nil {
		t.Fatalf("Write again: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("the status directory holds %d files, want the one report: %v", len(entries), entries)
	}
	reports, _ := Read(dir, "main")
	if reports["kt7m"].State != Done {
		t.Errorf("state = %q, want the second write to have won", reports["kt7m"].State)
	}
}

// Node ids are unique against one map and not against every map there is, so
// the workspace is part of the key — see
// docs/adr/0015-agent-status-is-a-directory-of-files-trigpoint-polls.md.
func TestReadIgnoresOtherWorkspaces(t *testing.T) {
	dir := t.TempDir()
	if err := Write(Path(dir, "main", "kt7m"), Running, "mine"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Write(Path(dir, "work", "kt7m"), Error, "theirs"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reports, err := Read(dir, "main")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 1 || reports["kt7m"].Detail != "mine" {
		t.Errorf("Read(main) = %v, want only main's own report", reports)
	}
}

// Absence of a report is not a state (CONTEXT.md, "Agent status"), and a
// directory nobody has written to yet is the most ordinary absence there is.
func TestReadWithoutADirectoryIsNoReports(t *testing.T) {
	reports, err := Read(filepath.Join(t.TempDir(), "never-created"), "main")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("Read = %v, want no reports", reports)
	}
}

// A file Trigpoint cannot make sense of leaves the node unknown rather than
// taking the whole pass down with it: one agent writing rubbish must not cost
// every other card its badge.
func TestReadSkipsFilesItCannotUnderstand(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"main_aaaa.json": "{not json",
		"main_bbbb.json": `{"state": "thinking-hard"}`,
		"main_cccc.json": `{"state": "running"}`,
		"main_dddd.txt":  `{"state": "running"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	reports, err := Read(dir, "main")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("Read = %v, want only the one report that parses", reports)
	}
	if _, ok := reports["cccc"]; !ok {
		t.Errorf("Read = %v, want the report for cccc", reports)
	}
}

// A detail is written by an agent and drawn in Trigpoint's status bar, so it is
// a trust boundary like a typed title is.
func TestReadSanitisesDetail(t *testing.T) {
	dir := t.TempDir()
	if err := Write(Path(dir, "main", "kt7m"), Running, "one\nline\x07"+strings.Repeat("x", 500)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reports, _ := Read(dir, "main")
	detail := reports["kt7m"].Detail
	if strings.ContainsAny(detail, "\n\x07") {
		t.Errorf("detail = %q, want no control characters", detail)
	}
	if len([]rune(detail)) > MaxDetailLen {
		t.Errorf("detail is %d runes, want at most %d", len([]rune(detail)), MaxDetailLen)
	}
}

// Deleting a workspace deletes its nodes, and a node's report belongs to the
// node. Only that workspace's: the directory holds every map's.
func TestRemoveWorkspaceTakesOnlyItsOwnReports(t *testing.T) {
	dir := t.TempDir()
	for _, workspace := range []string{"main", "work"} {
		if err := Write(Path(dir, workspace, "kt7m"), Running, workspace); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if err := RemoveWorkspace(dir, "main"); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}

	if reports, _ := Read(dir, "main"); len(reports) != 0 {
		t.Errorf("main still has %v, want its reports gone", reports)
	}
	if reports, _ := Read(dir, "work"); len(reports) != 1 {
		t.Errorf("work has %v, want its own report untouched", reports)
	}
	// A workspace with nothing to remove, or a directory that was never made,
	// is the outcome that was asked for.
	if err := RemoveWorkspace(dir, "main"); err != nil {
		t.Errorf("RemoveWorkspace of an empty workspace = %v, want nil", err)
	}
	if err := RemoveWorkspace(filepath.Join(dir, "never-created"), "main"); err != nil {
		t.Errorf("RemoveWorkspace without a directory = %v, want nil", err)
	}
}

// An agent writing the minimal report — the state and nothing else — is writing
// something Read accepts, so it must not be read as a report from the epoch.
func TestAReportWithNoTimestampIsReadAsHavingNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main_kt7m.json"), []byte(`{"state":"running"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reports, _ := Read(dir, "main")
	if got, ok := reports["kt7m"]; !ok || !got.TS.IsZero() {
		t.Errorf("report = %+v (found %v), want one with no timestamp at all", got, ok)
	}
}

func TestParseState(t *testing.T) {
	for _, want := range []State{Running, NeedsYou, Done, Error} {
		got, err := ParseState(string(want))
		if err != nil || got != want {
			t.Errorf("ParseState(%q) = %q, %v; want %q, nil", want, got, err, want)
		}
	}
	// The four states are the whole vocabulary: anything else would be a badge
	// with no meaning, and guessing which of the four was meant is the
	// inference this design exists to refuse.
	if _, err := ParseState("needs-you"); err == nil {
		t.Error(`ParseState("needs-you") succeeded, want the four documented states only`)
	}
	if _, err := ParseState(""); err == nil {
		t.Error(`ParseState("") succeeded, want an error`)
	}
}

func TestPathIsKeyedByWorkspaceAndNode(t *testing.T) {
	if got, want := Path("/s", "main", "kt7m"), filepath.Join("/s", "main_kt7m.json"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// The status directory sits beside the workspace files, so the map view can
// find it from the state directory it was given rather than resolving XDG twice.
func TestDirBesideIsASiblingOfTheWorkspaces(t *testing.T) {
	if got, want := DirBeside(filepath.Join("/s", "trig", "workspaces")), filepath.Join("/s", "trig", "status"); got != want {
		t.Errorf("DirBeside = %q, want %q", got, want)
	}
	if DirBeside("") != "" {
		t.Errorf("DirBeside(\"\") = %q, want no directory at all", DirBeside(""))
	}
}

// Ids come back round: a node removed frees its id for the next one, which must
// not inherit the badge the last one left on disk.
func TestRemoveTakesTheReportWithIt(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir, "main", "kt7m")
	if err := Write(path, Running, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if reports, _ := Read(dir, "main"); len(reports) != 0 {
		t.Errorf("Read = %v, want the report gone", reports)
	}
	// Removing what is not there is the outcome that was asked for.
	if err := Remove(path); err != nil {
		t.Errorf("Remove of an absent report = %v, want nil", err)
	}
}

// A workspace name may contain an underscore, so one workspace's file prefix
// can be another workspace's whole name plus the start of a node id. Read
// already refuses to read across that line; removal has to refuse in the same
// place, or deleting a workspace silently clears a live workspace's badges.
func TestRemovingAWorkspaceLeavesTheWorkspaceWhoseNameSharesItsPrefix(t *testing.T) {
	dir := t.TempDir()
	mine := Path(dir, "main", "kt7m")
	theirs := Path(dir, "main_thing", "kt7m")
	for _, path := range []string{mine, theirs} {
		if err := Write(path, Running, ""); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemoveWorkspace(dir, "main"); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	if _, err := os.Stat(mine); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("main's own report survived: %v", err)
	}
	if _, err := os.Stat(theirs); err != nil {
		t.Errorf("deleting main took main_thing's report with it: %v", err)
	}
	reports, err := Read(dir, "main_thing")
	if err != nil || len(reports) != 1 {
		t.Errorf("main_thing reads %v, %v; want its report still there", reports, err)
	}
}
