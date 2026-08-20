package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MatrixMagician/Trigpoint/internal/config"
	"github.com/MatrixMagician/Trigpoint/internal/state"
)

// remap builds a map on a keymap of its own, so a test can say what it wants
// bound and press it.
func remap(t *testing.T, keymap map[string]string) Model {
	t.Helper()
	cfg := config.Default()
	cfg.Keymap = keymap
	return newModel(t, cfg, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "one"},
		{ID: "bbb", Kind: state.KindShell, Title: "two", Pos: state.Cell{Col: 1}},
		{ID: "ccc", Kind: state.KindShell, Title: "three", Pos: state.Cell{Col: 2}},
		{ID: "ddd", Kind: state.KindShell, Title: "four", Pos: state.Cell{Col: 3}},
	}})
}

// TestEveryCommandIsAnAction holds the table's own integrity: the keymap is the
// command table, so an entry without a name cannot be bound and an entry
// without a handler cannot be run.
func TestEveryCommandIsAnAction(t *testing.T) {
	names, labels := map[string]bool{}, map[string]bool{}
	for _, c := range commands {
		switch {
		case c.name == "":
			t.Errorf("the command %q has no action name, so config cannot bind it", c.label)
		case names[c.name]:
			t.Errorf("two commands are called %q", c.name)
		case c.run == nil:
			t.Errorf("the command %q has no handler", c.name)
		case c.label == "":
			t.Errorf("the command %q has no label", c.name)
		case labels[c.label]:
			t.Errorf("two commands are labelled %q", c.label)
		}
		names[c.name], labels[c.label] = true, true
	}
}

// TestTheDefaultKeymapIsTheSpecKeymap keeps §7.3 the thing that ships. A
// default that drifted would be a remap nobody asked for.
func TestTheDefaultKeymapIsTheSpecKeymap(t *testing.T) {
	km, err := NewKeymap(nil)
	if err != nil {
		t.Fatalf("the defaults should resolve: %v", err)
	}
	for action, want := range map[string]string{
		"attach":      "enter",
		"peek":        "space",
		"new_shell":   "n",
		"kill":        "x",
		"quit":        "q",
		"cursor_left": "h, left",
		"node_left":   "H",
		"centre":      "z z",
		"origin":      "0",
		"palette":     "ctrl+k, :",
		"help":        "?",
	} {
		if got := km.binding(action); got != want {
			t.Errorf("%s should be bound to %q by default, got %q", action, want, got)
		}
	}
}

func TestARemappedKeyRunsTheAction(t *testing.T) {
	m := remap(t, map[string]string{"new_shell": "ctrl+n"})

	opened, _ := press(t, m, tea.KeyCtrlN)
	if opened.mode != modeTitle {
		t.Errorf("ctrl+n should open the title prompt once new_shell is bound to it, mode = %v", opened.mode)
	}
	// The default is not a second way in: two keys doing one thing is two keys
	// the overlay would have to disagree about.
	stale, _ := typeKeys(t, m, "n")
	if stale.mode != modeNormal {
		t.Errorf("n should do nothing once new_shell has moved off it, mode = %v", stale.mode)
	}
}

func TestARemappedMotionMovesTheCursor(t *testing.T) {
	m := remap(t, map[string]string{"cursor_right": "f"})

	moved := pressKeys(t, m, "f")
	if moved.ws.Viewport.Cursor.Col != 1 {
		t.Errorf("f should step the cursor right, cursor = %v", moved.ws.Viewport.Cursor)
	}
	if still := pressKeys(t, m, "l"); still.ws.Viewport.Cursor.Col != 0 {
		t.Errorf("l should no longer move the cursor, cursor = %v", still.ws.Viewport.Cursor)
	}
}

// TestACountStillPrefixesARemappedMotion holds the one thing that is not
// remappable: a digit is a digit, so 3f is three presses of f.
func TestACountStillPrefixesARemappedMotion(t *testing.T) {
	m := remap(t, map[string]string{"cursor_right": "f"})

	moved := pressKeys(t, m, "3", "f")
	if moved.ws.Viewport.Cursor.Col != 3 {
		t.Errorf("3f should step three nodes right, cursor = %v", moved.ws.Viewport.Cursor)
	}
}

func TestASequenceNeedsEveryKeyInIt(t *testing.T) {
	// o o rather than g o: g is the group key, and a binding that starts with
	// one already bound is refused rather than shadowed.
	m := remap(t, map[string]string{"origin": "o o"})
	m = pressKeys(t, m, "l", "l")
	if m.ws.Viewport.Cursor.Col == 0 {
		t.Fatal("the cursor should have moved off the origin first")
	}

	half := pressKeys(t, m, "o")
	if half.ws.Viewport.Cursor.Col == 0 {
		t.Error("the first key of a sequence should do nothing on its own")
	}
	whole := pressKeys(t, m, "o", "o")
	if whole.ws.Viewport.Cursor != (state.Cell{}) {
		t.Errorf("o o should jump to the origin, cursor = %v", whole.ws.Viewport.Cursor)
	}
	// A key that cannot continue the sequence drops it rather than being eaten.
	abandoned := pressKeys(t, m, "o", "l")
	if abandoned.ws.Viewport.Cursor.Col != m.ws.Viewport.Cursor.Col+1 {
		t.Errorf("o then l should abandon the sequence and move the cursor, cursor = %v",
			abandoned.ws.Viewport.Cursor)
	}
}

// TestZZStillCentres is the sequence that shipped, pressed the way it always
// was: the prefix machine replaced a special case and must not have taken the
// behaviour with it.
func TestZZStillCentres(t *testing.T) {
	m := remap(t, nil)
	m = pressKeys(t, m, "l", "l", "l")
	before := m.ws.Viewport.Offset

	centred := pressKeys(t, m, "z", "z")
	if centred.ws.Viewport.Offset == before {
		t.Errorf("zz should centre the viewport on the cursor, offset = %v", centred.ws.Viewport.Offset)
	}
}

func TestAnUnboundActionIsReachableOnlyFromThePalette(t *testing.T) {
	m := remap(t, map[string]string{"kill": ""})

	if pressed, _ := typeKeys(t, m, "x"); pressed.mode != modeNormal {
		t.Errorf("x should do nothing once kill is unbound, mode = %v", pressed.mode)
	}

	open, cmd := press(t, m, tea.KeyCtrlK)
	open = settle(t, open, cmd)
	open, _ = typeKeys(t, open, "Kill node")
	ran, _ := press(t, open, tea.KeyEnter)
	if ran.mode != modeConfirmKill {
		t.Errorf("the palette should still reach an unbound action, mode = %v", ran.mode)
	}
}

// TestValidationRefusesAKeymapThatCannotWork is the startup gate (§10). Each
// message has to name what is wrong and where, because the alternative is a key
// that quietly does nothing.
func TestValidationRefusesAKeymapThatCannotWork(t *testing.T) {
	cases := map[string]struct {
		keymap map[string]string
		want   []string
	}{
		"an action that does not exist": {
			map[string]string{"cursor_lft": "h"},
			[]string{"cursor_lft", "cursor_left"},
		},
		"one key bound twice": {
			map[string]string{"new_note": "n"},
			[]string{`"n"`, "new_shell", "new_note"},
		},
		"a binding that swallows a sequence": {
			map[string]string{"origin": "z"},
			[]string{`"z"`, "origin", "centre"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewKeymap(tc.keymap)
			if err == nil {
				t.Fatalf("%s should be refused", name)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the message should mention %s, got: %v", want, err)
				}
			}
		})
	}
}

// TestValidationIsDeterministic keeps the message the same on every run: a
// keymap with two things wrong with it reports the same one first each time,
// so a user fixing them does not chase a moving target.
func TestValidationIsDeterministic(t *testing.T) {
	keymap := map[string]string{"new_note": "n", "rename": "n", "tags": "n"}
	first, err := NewKeymap(keymap)
	if err == nil {
		t.Fatalf("a triple binding should be refused, got %v", first)
	}
	for i := 0; i < 20; i++ {
		if _, again := NewKeymap(keymap); again.Error() != err.Error() {
			t.Fatalf("the message changed between runs:\n%v\n%v", err, again)
		}
	}
}

func TestAValidKeymapOpensTheMap(t *testing.T) {
	cfg := config.Default()
	cfg.Keymap = map[string]string{"kill": "d d", "new_shell": "ctrl+n"}
	if _, err := New(cfg, state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{}); err != nil {
		t.Errorf("a keymap that resolves should open a map: %v", err)
	}
}

func TestAnInvalidKeymapRefusesToOpenTheMap(t *testing.T) {
	cfg := config.Default()
	cfg.Keymap = map[string]string{"nonesuch": "d"}
	if _, err := New(cfg, state.Workspace{Name: "main"}, t.TempDir(), &fakeSessions{}); err == nil {
		t.Error("a map should not open on a keymap that does not resolve")
	}
}

// TestTheStatusBarHintsFollowTheKeymap: the bar names keys, so a remap it did
// not follow would be the bar lying about the same thing the overlay exists to
// stop lying about.
func TestTheStatusBarHintsFollowTheKeymap(t *testing.T) {
	m := remap(t, map[string]string{"quit": "Q"})
	bar := lastLine(m.View())

	if !strings.Contains(bar, "Q quit") {
		t.Errorf("the bar should hint the bound key, got %q", bar)
	}
	if strings.Contains(bar, "q quit") {
		t.Errorf("the bar should not hint the key that was moved away from, got %q", bar)
	}
}

func TestThePaletteShowsTheBoundKeys(t *testing.T) {
	m := remap(t, map[string]string{"new_shell": "ctrl+n", "kill": ""})

	open, cmd := press(t, m, tea.KeyCtrlK)
	open = settle(t, open, cmd)

	byLabel := map[string]string{}
	for _, e := range open.palette {
		byLabel[e.label] = e.detail
	}
	if got := byLabel["New shell node"]; got != "ctrl+n" {
		t.Errorf("the palette should show the bound key, got %q", got)
	}
	if got := byLabel["Kill node"]; got != "palette only" {
		t.Errorf("an unbound action should say so, got %q", got)
	}
}

// TestADigitCannotBeBound holds the one thing counts cost: the digits are read
// before the keymap, so an action bound to one would be an action that never
// ran — which is exactly the silently-dead key validation exists to catch.
func TestADigitCannotBeBound(t *testing.T) {
	for _, binding := range []string{"3", "g 3"} {
		_, err := NewKeymap(map[string]string{"kill": binding})
		if err == nil {
			t.Errorf("kill = %q should be refused: a digit is a count", binding)
			continue
		}
		if !strings.Contains(err.Error(), "kill") || !strings.Contains(err.Error(), "count") {
			t.Errorf("the message should name the action and say why, got: %v", err)
		}
	}
	// Zero is not one of them: it reaches the keymap whenever no count is being
	// typed, which is how the origin has always been pressed.
	if _, err := NewKeymap(map[string]string{"origin": "0, ctrl+home"}); err != nil {
		t.Errorf("zero is a key when no count is in hand: %v", err)
	}
}

// TestABindingThatNamesNoKeyIsRefused: an empty binding means "palette only",
// which is a thing a user asks for on purpose. A binding of punctuation that
// parses to nothing is not, and dropping it silently would leave the action
// unbound with no way to tell that is what happened.
func TestABindingThatNamesNoKeyIsRefused(t *testing.T) {
	_, err := NewKeymap(map[string]string{"filter": ","})
	if err == nil {
		t.Fatal(`filter = "," should be refused`)
	}
	if !strings.Contains(err.Error(), "filter") {
		t.Errorf("the message should name the action, got: %v", err)
	}
	// The deliberate case still works.
	if _, err := NewKeymap(map[string]string{"filter": ""}); err != nil {
		t.Errorf(`filter = "" is how an action is left to the palette: %v`, err)
	}
}

func TestAnAlternativeListedTwiceSaysWhatItIs(t *testing.T) {
	_, err := NewKeymap(map[string]string{"kill": "x, x"})
	if err == nil {
		t.Fatal(`kill = "x, x" should be refused`)
	}
	if strings.Contains(err.Error(), "both kill and kill") {
		t.Errorf("naming an action as clashing with itself explains nothing, got: %v", err)
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Errorf("the message should say the key is listed twice, got: %v", err)
	}
}

// TestAPaletteMotionIgnoresAStaleCount: a count is typed at the map, and the
// palette is a screen away from it. Running a motion from the palette with a 3
// still in hand would move three cells with nothing on screen saying so.
func TestAPaletteMotionIgnoresAStaleCount(t *testing.T) {
	m := pressKeys(t, remap(t, nil), "3")
	open, cmd := press(t, m, tea.KeyCtrlK)
	open = settle(t, open, cmd)
	open, _ = typeKeys(t, open, "Move cursor right")

	moved, _ := press(t, open, tea.KeyEnter)
	if moved.ws.Viewport.Cursor.Col != 1 {
		t.Errorf("a motion run from the palette should move one cell, cursor = %v", moved.ws.Viewport.Cursor)
	}
}

// TestTheHintsOnAnEmptyMapFollowTheKeymap: the empty map and the map a filter
// has emptied are the two screens with no card to read a key off, so they are
// the two that most have to name the right one.
func TestTheHintsOnAnEmptyMapFollowTheKeymap(t *testing.T) {
	cfg := config.Default()
	cfg.Keymap = map[string]string{"new_shell": "ctrl+n"}
	empty := newModel(t, cfg, state.Workspace{Name: "main"})

	if line := lineContaining(empty.View(), "empty"); !strings.Contains(line, "ctrl+n") {
		t.Errorf("the empty map should name the key that makes a node, got %q", line)
	}

	cfg.Keymap = map[string]string{"clear": "ctrl+g"}
	m := newModel(t, cfg, state.Workspace{Name: "main", Nodes: []state.Node{
		{ID: "aaa", Kind: state.KindShell, Title: "one"},
	}})
	filtered, _ := typeKeys(t, m, "/zzz")
	if line := lineContaining(filtered.View(), "No card matches"); !strings.Contains(line, "ctrl+g") {
		t.Errorf("the no-match hint should name the key that clears the filter, got %q", line)
	}
}

func lineContaining(view, want string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}
