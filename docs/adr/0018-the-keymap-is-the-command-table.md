# The keymap is the command table

Every key the map answers to is remappable, and `?` draws a help overlay that cannot be
wrong about what a key does. Both come from one list.

That list already existed. `internal/tui` held a `commands` table — what the palette offers
and what the status bar hints at — and each entry carried the literal keys it was reached by.
Choosing a command from the palette *replayed* those keystrokes through the map's own key
handling, which is what kept the palette and the keyboard from disagreeing.

Replay cannot survive remapping. A binding read from config is not the binding in the table,
so a replayed keystroke would run whatever the user had since bound that key to; and an
action deliberately left *unbound* — which SPEC §7.3 allows, and which the palette is
supposed to remain the way to reach — has no keystroke to replay at all.

So the table stops carrying keys and starts carrying the action itself:

```go
type command struct {
	name   string                          // what config binds: "cursor_left"
	label  string                          // what the palette and the overlay call it
	hint   string                          // what the status bar calls it, if it offers it
	keys   string                          // the default binding, in config's own grammar
	hidden bool                            // in the keymap, not offered in the palette
	run    func(Model) (tea.Model, tea.Cmd)
}
```

`updateNormal` looks the pressed key up in the resolved keymap and calls `run`. The table is
filled in `init` rather than at its declaration: it holds the handlers, one of which opens the
palette, which reads the table — a cycle Go refuses to order however plainly it terminates. The palette
calls `run` directly. They are the same function value, so they cannot drift — the same
guarantee replay bought, without needing a keystroke to exist.

## The binding grammar

A binding is written the way a user writes it in config: alternatives separated by commas,
each alternative a sequence of key names separated by spaces.

```toml
[keymap]
cursor_left = "h, left"    # two ways to press it
centre      = "z z"        # one way, two keys
peek        = "space"      # the one key written by name rather than typed
kill        = ""           # unbound: reachable from the palette and nowhere else
```

The space bar is spelled, for the obvious reason: it is the key whose own name is the
separator. It is the only such name — everything else is Bubble Tea's own.

The defaults in the table are written in that same grammar, so what ships and what a user
types are the same string, and the overlay renders both through one function.

Sequences replaced the `awaitZ bool` that existed to spell `zz`. The dispatcher holds a
`chord` instead: a key that can only continue a longer binding is kept, an exact match runs,
and anything else drops what was held and is read fresh — pressing `z` and then `l` is a
sequence abandoned and a cursor moved, which is what `awaitZ` did too. `zz` stopped being a
special case and users gained sequences of their own.

Count prefixes stay literal. `3l` is three presses of `l`, and a digit is a digit on every
map — binding one to an action would make counts unreadable. `0` is the one digit an action
claims, and only when no count is being typed, which is how it already worked.

## Validation

`NewKeymap` resolves config over the defaults and refuses three things, naming both sides:

- an action name that does not exist — with the nearest real one suggested, through the same
  subsequence match the filter and the palette rank by;
- one key sequence bound to two actions;
- a binding that is a strict prefix of another, which the prefix machine would make
  unreachable — `z` bound to anything means `z z` can never fire.

It runs at startup, before Bubble Tea owns the terminal, and again in `trig doctor`, so a
config that stops the map opening is a config `doctor` explains rather than one it passes.

## The overlay

`?` opens a scrollable overlay listing every action and the keys it currently answers to,
read from the resolved keymap — so a remap shows up with no other change, and an unbound
action says so rather than being absent.

The modal contexts — a gathered selection, a held group, filter, palette, peek — read literal
keys in their own handlers and stay that way; SPEC §7.3 documents them as fixed. They reach
the overlay as small structured tables sitting beside those handlers, and where the status
bar has room for a context's hints — a gathered selection, a held group, the palette — it
renders them from the same table. The bar of a screen that fills the terminal, peek's and the
filter's, still names the two keys it has width for rather than the whole list.

## Considered options

**A separate `internal/keymap` package.** Rejected: the actions are methods on the map view's
`Model`, so the handlers cannot leave `internal/tui`, and a package holding only their names
and defaults would be a second list to keep equal to the first — bought with a consistency
test, which is a test that exists because of the seam rather than because of the behaviour.

**Keep replay; remap the incoming key to its default before the existing switch.** Rejected:
it is the smallest diff and it fails the requirement. An unbound action has no default key to
replay, so the palette entry would be dead; and the default key keeps working alongside the
remapped one unless separately suppressed, which is two keys doing one thing and no way to
tell from the table.

**Make the modal keys remappable too.** Rejected as scope: five more handlers rewritten, and
duplicate detection would have to become per-context rather than global — `j` means scroll in
a peek and move in a map, and a global check would have to learn that those do not collide.

## Consequences

There is one list of what the map can do. The status bar's hints, the palette's command
entries, the help overlay, and the keyboard all read it, and a new action is one table entry
plus its handler.

`tui.New` now returns an error, because a map view cannot be built on a keymap that does not
resolve. The alternative was falling back to the defaults, which would leave a user looking
at a map whose keys are not the ones in the file they just edited.

An action's `run` is called with the count already parsed onto `Model.repeat`. Only the
motions read it; every other action ignores it, and it is overwritten by the next key.
