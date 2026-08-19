# A workspace switch is a reload

The map view holds one workspace at a time. Switching writes the workspace being left,
reads the one being arrived at, and drops everything the view had derived about the first:
previews, unread marks, the dead set, and the nodes whose sessions tmux was still making.

## Why not hold them all

Workspaces share no nodes, so the obvious alternative — load every workspace file at launch
and keep the maps in memory, switching by moving a pointer — buys only speed, and pays for it
in three places:

- **Node ids are unique against one map, not against the state directory.** They are four
  characters from a 28-character alphabet, drawn against the nodes of the workspace being
  added to. Previews, the unread set, the dead set, and the dirty set are all keyed by id, so
  one shared cache across held maps is a card showing another workspace's output. Per-map
  caches are the same thing this ADR does, with the reload replaced by bookkeeping.
- **Trigpoint is crash-only and more than one can be running.** A workspace file is the
  authority, written atomically after every mutation; a copy held since launch is a copy that
  another `trig` has since made stale. Reading on the switch is how the map is correct
  without anything watching the state directory.
- **Reconciliation is per workspace.** A pass classifies the sessions named
  `trig_<workspace>_<id>` (§9.2) and reconstructs the ones with no card. Held maps would each
  want passes of their own, on a schedule, for sessions nobody is looking at.

The reload costs one file read and one `tmux list-sessions` per switch, on a keystroke a
person presses.

## Consequences

**The workspace being left is written on the way out.** Its viewport is where you were
looking, so arriving back anywhere else would make `Tab` a way of losing your place. It also
puts a workspace opened by name for the first time — `trig -w scratch`, which has no file
until something saves it — into the cycle rather than losing it on the first `Tab`.

**Deleting the open workspace opens another without writing it first**, or the file would come
straight back. Deletion removes the file and nothing else: the sessions its nodes named go on
running, because Trigpoint kills nothing it did not start (§5.2), and the confirmation says so.

**A node whose session was still being created is dropped with the map.** The `nodeCreatedMsg`
that lands afterwards is dropped too, because nothing on the map now on screen is waiting for
it — the alternative, placing it, is another workspace's card appearing on this one. The
session is on its way regardless, so the map that ordered it gets a card back at its next
reconciliation pass, named after the node's id rather than the title that was typed. The
window is one tmux call wide.

**The palette (§7.2) wants to jump to a node in any workspace**, which this ADR says the view
does not hold. That is a read of the workspace files, not a reason to keep the maps in
memory: the palette needs titles and positions to offer, and it gets the map itself by
switching to it — which is what happens when the jump is taken.
