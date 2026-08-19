package state

import (
	"encoding/json"
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
