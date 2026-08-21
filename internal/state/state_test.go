package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadMissingWorkspaceGivesAnEmptyOne(t *testing.T) {
	ws, err := Load(t.TempDir(), "main")
	if err != nil {
		t.Fatalf("a first run has no workspace file and must not fail: %v", err)
	}
	if ws.Name != "main" {
		t.Errorf("Name = %q, want %q", ws.Name, "main")
	}
	if len(ws.Nodes) != 0 {
		t.Errorf("a new workspace should have no nodes, got %d", len(ws.Nodes))
	}
}

func TestWorkspaceRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	want := Workspace{
		Name: "main",
		Dir:  "/home/dev/project",
		Nodes: []Node{{
			ID:        "k4f2",
			Kind:      KindShell,
			Title:     "api-server",
			Colour:    "cyan",
			Tags:      []string{"infra", "api"},
			Pos:       Cell{Col: 3, Row: -2},
			Size:      SizeM,
			Dir:       "/srv/api",
			CreatedAt: time.Now().UTC().Truncate(time.Second),
		}},
		Groups:   []Group{{ID: "g1", Title: "backend", Colour: "green", Rect: Rect{Min: Cell{0, 0}, Max: Cell{4, 4}}}},
		Viewport: Viewport{Cursor: Cell{Col: 3, Row: -2}, Offset: Cell{Col: 1, Row: 0}},
	}

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir, "main")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Dir != want.Dir || got.Viewport != want.Viewport {
		t.Errorf("workspace fields did not survive the round trip:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "k4f2" || got.Nodes[0].Title != "api-server" {
		t.Errorf("nodes did not survive the round trip: %+v", got.Nodes)
	}
	if len(got.Nodes[0].Tags) != 2 {
		t.Errorf("tags did not survive the round trip: %+v", got.Nodes[0].Tags)
	}
	if !got.Nodes[0].CreatedAt.Equal(want.Nodes[0].CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.Nodes[0].CreatedAt, want.Nodes[0].CreatedAt)
	}
	if len(got.Groups) != 1 || got.Groups[0].Rect != want.Groups[0].Rect {
		t.Errorf("groups did not survive the round trip: %+v", got.Groups)
	}
}

// The crash-only guarantee: a reader must never see a half-written workspace.
func TestConcurrentReadNeverSeesAPartialFile(t *testing.T) {
	dir := t.TempDir()
	ws := Workspace{Name: "main"}
	for i := range 400 { // large enough that a non-atomic write would be caught mid-flight
		ws.Nodes = append(ws.Nodes, Node{ID: "n", Kind: KindShell, Title: strings.Repeat("x", 64), Pos: Cell{Col: i}})
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 200 {
			ws.Viewport.Cursor.Row = i
			if err := Save(dir, ws); err != nil {
				t.Errorf("Save: %v", err)
				break
			}
		}
		close(stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			raw, err := os.ReadFile(filepath.Join(dir, "main.json"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Errorf("ReadFile: %v", err)
				return
			}
			if err := json.Unmarshal(raw, &Workspace{}); err != nil {
				t.Errorf("read a partially written workspace (%d bytes): %v", len(raw), err)
				return
			}
		}
	}()

	wg.Wait()
}

func TestSaveLeavesNoTempFilesBehind(t *testing.T) {
	dir := t.TempDir()
	for range 5 {
		if err := Save(dir, Workspace{Name: "main"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "main.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only main.json to remain, got %v", names)
	}
}

func TestWorkspaceNamesThatEscapeTheStateDirAreRejected(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../escape", "a/b", "", ".", "..", "with\x00null", strings.Repeat("n", 300), "my.project", "host:port", "trailing."} {
		t.Run(name, func(t *testing.T) {
			if err := Save(dir, Workspace{Name: name}); err == nil {
				t.Errorf("Save accepted unsafe workspace name %q", name)
			}
			if _, err := Load(dir, name); err == nil {
				t.Errorf("Load accepted unsafe workspace name %q", name)
			}
		})
	}
}

func TestSaveCreatesTheStateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "workspaces")
	if err := Save(dir, Workspace{Name: "main"}); err != nil {
		t.Fatalf("Save should create its directory: %v", err)
	}
	if _, err := Load(dir, "main"); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoadReportsCorruptWorkspaceFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "main"); err == nil {
		t.Fatal("a corrupt workspace file must be reported, not silently replaced")
	}
}

func TestDirHonoursXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/custom/state", "trig", "workspaces"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDirFallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/someone")
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/someone", ".local", "state", "trig", "workspaces"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestNewNodeIDIsShortSessionSafeAndFreeOnThisMap(t *testing.T) {
	ws := Workspace{Name: "main"}
	for i := 0; i < 200; i++ {
		id := ws.NewNodeID()
		if id == "" || len(id) > 8 {
			t.Fatalf("NewNodeID returned %q, which is not a short slug", id)
		}
		// The id is carried into the tmux session name, where "." and ":"
		// address windows and panes.
		if strings.ContainsAny(id, ".:_/ ") {
			t.Fatalf("NewNodeID returned %q, which is not safe in a tmux session name", id)
		}
		for _, n := range ws.Nodes {
			if n.ID == id {
				t.Fatalf("NewNodeID returned %q, which is already on the map", id)
			}
		}
		ws.Nodes = append(ws.Nodes, Node{ID: id})
	}
}

func TestNearestFreeCellPrefersTheCursorItself(t *testing.T) {
	ws := Workspace{Name: "main"}
	if got := ws.NearestFreeCell(Cell{Col: 3, Row: 4}); got != (Cell{Col: 3, Row: 4}) {
		t.Errorf("on an empty map the cursor's own cell is free, got %+v", got)
	}
}

func TestNearestFreeCellStepsAsideWhenOccupied(t *testing.T) {
	ws := Workspace{Name: "main", Nodes: []Node{{ID: "a", Pos: Cell{Col: 0, Row: 0}}}}
	got := ws.NearestFreeCell(Cell{})

	if got == (Cell{}) {
		t.Fatal("the cursor's cell is taken; NearestFreeCell returned it anyway")
	}
	if d := max(abs(got.Col), abs(got.Row)); d != 1 {
		t.Errorf("NearestFreeCell returned %+v, %d cells away when a neighbour was free", got, d)
	}
}

func TestNearestFreeCellSkipsAFullRing(t *testing.T) {
	ws := Workspace{Name: "main"}
	for col := -1; col <= 1; col++ {
		for row := -1; row <= 1; row++ {
			ws.Nodes = append(ws.Nodes, Node{ID: "n", Pos: Cell{Col: col, Row: row}})
		}
	}
	got := ws.NearestFreeCell(Cell{})

	for _, n := range ws.Nodes {
		if n.Pos == got {
			t.Fatalf("NearestFreeCell returned occupied cell %+v", got)
		}
	}
	if d := max(abs(got.Col), abs(got.Row)); d != 2 {
		t.Errorf("NearestFreeCell returned %+v, %d cells away when the second ring was free", got, d)
	}
}

func TestNearestFreeCellIgnoresTheCellsCoordinatesOfOtherWorkspaces(t *testing.T) {
	// Placement is a pure function of this workspace's nodes: same map, same
	// cursor, same answer, so a restart puts a node where the user expects.
	ws := Workspace{Name: "main", Nodes: []Node{{ID: "a", Pos: Cell{}}}}
	if first, second := ws.NearestFreeCell(Cell{}), ws.NearestFreeCell(Cell{}); first != second {
		t.Errorf("NearestFreeCell is not deterministic: %+v then %+v", first, second)
	}
}

func nodesAt(cells ...Cell) []Node {
	nodes := make([]Node, len(cells))
	for i, c := range cells {
		nodes[i] = Node{ID: fmt.Sprintf("n%d", i), Kind: KindShell, Pos: c}
	}
	return nodes
}

func posOf(nodes []Node, id string) Cell {
	for _, n := range nodes {
		if n.ID == id {
			return n.Pos
		}
	}
	return Cell{Col: -9999, Row: -9999}
}

func TestShiftMovesIntoAFreeCell(t *testing.T) {
	ws := Workspace{Nodes: nodesAt(Cell{Col: 0, Row: 0})}

	moved := ws.Shift([]string{"n0"}, Cell{Col: 1})

	if got := posOf(moved, "n0"); got != (Cell{Col: 1}) {
		t.Errorf("node moved to %+v, want one cell right", got)
	}
}

func TestShiftShovesTheOccupantOutOfTheWay(t *testing.T) {
	ws := Workspace{Nodes: nodesAt(Cell{Col: 0, Row: 0}, Cell{Col: 1, Row: 0})}

	moved := ws.Shift([]string{"n0"}, Cell{Col: 1})

	if got := posOf(moved, "n0"); got != (Cell{Col: 1}) {
		t.Errorf("mover ended at %+v, want the occupied cell it pushed into", got)
	}
	if got := posOf(moved, "n1"); got != (Cell{Col: 2}) {
		t.Errorf("occupant ended at %+v, want to be shoved on by the same step", got)
	}
}

func TestShiftCascadesThroughARowOfNodes(t *testing.T) {
	ws := Workspace{Nodes: nodesAt(
		Cell{Col: 0, Row: 0}, Cell{Col: 0, Row: 1}, Cell{Col: 0, Row: 2}, Cell{Col: 0, Row: 4},
	)}

	moved := ws.Shift([]string{"n0"}, Cell{Row: 1})

	for id, want := range map[string]Cell{
		"n0": {Row: 1}, "n1": {Row: 2}, "n2": {Row: 3}, "n3": {Row: 4},
	} {
		if got := posOf(moved, id); got != want {
			t.Errorf("%s ended at %+v, want %+v", id, got, want)
		}
	}
}

// A group moves as one, so the rule has to take a set: the members shift
// together and only non-members are shoved, or a group would eat a bystander.
func TestShiftMovesASetTogetherAndShovesOnlyOutsiders(t *testing.T) {
	ws := Workspace{Nodes: nodesAt(Cell{Col: 0}, Cell{Col: 1}, Cell{Col: 2})}

	moved := ws.Shift([]string{"n0", "n1"}, Cell{Col: 1})

	for id, want := range map[string]Cell{"n0": {Col: 1}, "n1": {Col: 2}, "n2": {Col: 3}} {
		if got := posOf(moved, id); got != want {
			t.Errorf("%s ended at %+v, want %+v", id, got, want)
		}
	}
}

func TestShiftLeavesTheOriginalNodesAlone(t *testing.T) {
	ws := Workspace{Nodes: nodesAt(Cell{Col: 0}, Cell{Col: 1})}

	ws.Shift([]string{"n0"}, Cell{Col: 1})

	if got := posOf(ws.Nodes, "n0"); got != (Cell{Col: 0}) {
		t.Errorf("the workspace's own nodes moved to %+v; every Model copy shares that slice", got)
	}
}

// A workspace file is a file, and a hand-edited one can stack two nodes on a
// cell. Shoving that cell must take both of them, or the pair is stuck on top
// of each other for good.
func TestShiftUnstacksNodesThatShareACell(t *testing.T) {
	ws := Workspace{Nodes: nodesAt(Cell{Col: 0}, Cell{Col: 1}, Cell{Col: 1})}

	moved := ws.Shift([]string{"n0"}, Cell{Col: 1})

	for id, want := range map[string]Cell{"n0": {Col: 1}, "n1": {Col: 2}, "n2": {Col: 2}} {
		if got := posOf(moved, id); got != want {
			t.Errorf("%s ended at %+v, want %+v", id, got, want)
		}
	}
}

// TestValidNameRejectsWhitespace holds the boundary that the tmux side depends
// on: the name is carried into the session name `trig_<workspace>_<id>`, and
// tmux's own output — `list-sessions`, and the activity subscription — is read
// with the name as one whitespace-free word.
func TestValidNameRejectsWhitespace(t *testing.T) {
	for _, name := range []string{"my ws", "ws\t2", "ws\nx", " main", "main "} {
		if err := ValidName(name); err == nil {
			t.Errorf("ValidName(%q) allowed whitespace, which tmux output cannot be read back through", name)
		}
	}
	if err := ValidName("my-ws_2"); err != nil {
		t.Errorf("ValidName(%q) should still be fine: %v", "my-ws_2", err)
	}
}

func TestListNamesEveryWorkspaceOnDisk(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"work", "main", "scratch"} {
		if err := Save(dir, Workspace{Name: name}); err != nil {
			t.Fatalf("Save(%q): %v", name, err)
		}
	}
	// Whatever else is in the directory is not a workspace: an orphaned temp
	// file from an interrupted save, and a file whose name Trigpoint would
	// never have written.
	for _, other := range []string{"main.4f2.tmp", "notes.md", "bad name.json"} {
		if err := os.WriteFile(filepath.Join(dir, other), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	names, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := strings.Join(names, ","), "main,scratch,work"; got != want {
		t.Errorf("List = %q, want %q — sorted, and workspace files only", got, want)
	}
}

// A first run has no state directory at all, and having no workspaces yet is
// not a failure to list them.
func TestListOfAMissingDirectoryIsEmpty(t *testing.T) {
	names, err := List(filepath.Join(t.TempDir(), "never-made"))
	if err != nil {
		t.Fatalf("List of a missing directory should not fail: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("List = %v, want nothing", names)
	}
}

func TestRemoveDeletesTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Workspace{Name: "scratch", Nodes: []Node{{ID: "k4f2"}}}); err != nil {
		t.Fatal(err)
	}

	if err := Remove(dir, "scratch"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	names, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("the workspace is still listed after Remove: %v", names)
	}
	// Removing what is not there is the outcome that was asked for: two clients
	// deleting the same workspace must not turn the second into an error.
	if err := Remove(dir, "scratch"); err != nil {
		t.Errorf("Remove of a workspace that is already gone: %v", err)
	}
}

// Remove takes a name from the same place Load does, so it holds the same
// boundary: a name that could reach outside the state directory is refused
// rather than obeyed.
func TestRemoveRefusesANameThatEscapesTheDirectory(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "keep.json")
	if err := os.WriteFile(victim, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Remove(dir, "../keep"); err == nil {
		t.Error("Remove should refuse a name containing a path separator")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("Remove followed the path out of the state directory: %v", err)
	}
}

func TestPlacementCellTakesTheCursorsOwnCellWhenNothingIsOnIt(t *testing.T) {
	ws := Workspace{Name: "main", Nodes: []Node{{ID: "a", Pos: Cell{Col: 1, Row: 1}}}}
	view := Viewport{Cursor: Cell{Col: 3, Row: 4}}

	if got := ws.PlacementCell(view); got != view.Cursor {
		t.Errorf("placed at %+v; a cursor pointing at empty space is where the card goes", got)
	}
}

func TestPlacementCellFillsWhatTheViewportShows(t *testing.T) {
	// Ten cards made in a row, each landing on the cursor that the last one
	// moved. Every one of them has to be inside the window the map opens on,
	// which is five cells by five from the offset.
	const window = 5
	ws := Workspace{Name: "main"}
	view := Viewport{}
	for i := 0; i < 10; i++ {
		pos := ws.PlacementCell(view)
		for _, n := range ws.Nodes {
			if n.Pos == pos {
				t.Fatalf("card %d placed on top of %s at %+v", i, n.ID, pos)
			}
		}
		ws.Nodes = append(ws.Nodes, Node{ID: string(rune('a' + i)), Pos: pos})
		view.Cursor = pos
	}
	for _, n := range ws.Nodes {
		if n.Pos.Col < view.Offset.Col || n.Pos.Col >= view.Offset.Col+window ||
			n.Pos.Row < view.Offset.Row || n.Pos.Row >= view.Offset.Row+window {
			t.Errorf("%s is at %+v, outside the %dx%d window at %+v", n.ID, n.Pos, window, window, view.Offset)
		}
	}
}

func TestPlacementCellGrowsDownAndRightOfTheViewport(t *testing.T) {
	// The viewport is what the map is showing, so a card that cannot go on the
	// cursor goes somewhere the user is already looking. Up and left of the
	// offset is off screen by definition.
	ws := Workspace{Name: "main", Nodes: []Node{{ID: "a", Pos: Cell{Col: 4, Row: 4}}}}
	view := Viewport{Cursor: Cell{Col: 4, Row: 4}, Offset: Cell{Col: 4, Row: 4}}

	got := ws.PlacementCell(view)
	if got.Col < view.Offset.Col || got.Row < view.Offset.Row {
		t.Errorf("placed at %+v, up or left of the offset %+v and so off screen", got, view.Offset)
	}
}
