# A new card fills the map you are looking at

Placing a card at the nearest free cell to the cursor, and then moving the cursor onto the
card, is a loop. Each create starts its search one ring further out than the last, and
because `ring` offers the cell up and to the left first, ten cards in a row land on `(0,0)`
through `(-9,-9)`. The map draws five of them and the rest trail off screen behind the
cursor. #63.

Neither half is wrong on its own. Moving the cursor onto the new card is what lets `n` be
followed by Enter, and it is what made the CLI and the map agree in #61. Placing near the
cursor is what "put it where I am working" means. What is wrong is using the cursor as both
the origin of the search and the thing the search moves.

**Placement searches from the viewport, not from the cursor.** `Workspace.PlacementCell`
takes the whole `Viewport`. It returns the cursor's own cell when nothing is on it, and
otherwise the nearest free cell down and right of the viewport's corner. The viewport does
not move while the cards still fit in it, so the search origin is stable and the cards pack
instead of walking. The cursor still follows each new card.

Up and left of the offset is off screen by definition, so the search never offers it. The map
is infinite the other way, so there is always an answer.

## Considered options

Measured with a throwaway probe over `internal/state`, ten cards in a row, counting how many
land inside the five-by-five window a 120x34 terminal opens on.

| | rule | visible |
| --- | --- | --- |
| A | ring up-left first, cursor follows (what shipped in v0.1.0) | 5/10 |
| B | ring down-right first, cursor follows | 5/10 |
| C | ring up-left first, cursor stays put | 4/10 |
| D | ring down-right first, cursor stays put | 5/10 |
| E | A, and the map centres the cursor when it opens | 3/10 |
| F | B, and the map centres the cursor when it opens | 3/10 |
| G | search the quadrant down-right of the cursor, cursor stays put | 10/10 |
| H | search the quadrant down-right of the viewport, cursor follows | **10/10** |

Reordering the ring cannot fix it (A, B, D). A following cursor walks whichever way the ring
points, so the change only picks the direction of the trail. Centring the cursor on open (E,
F) makes it worse, because it hands half the viewport to the empty space the cards have
already left.

G and H both pack the cards into a three-by-four block. **H is chosen over G** because G
needs the cursor to stay where it was, which costs `n` then Enter, the most-used sequence on
the map. The cost of H is that a card can land somewhere other than under the cursor, which
G would not do. In practice this is not a real cost: the motion keys hop from node to node,
so the cursor only ever sits on empty space on a fresh map or on a cell a killed node left
behind, and both of those are cases where `PlacementCell` returns the cursor's cell anyway.

## Consequences

`NearestFreeCell` stays, with one caller and a narrower job: settling a cell a node was
already given and may have lost to a shove while its session was being made
(`internal/tui/nodes.go`, on `nodeCreatedMsg`). Every caller that decides where a *new* card
goes uses `PlacementCell`, which is `trig new`, `n`/`N` on the map, adoption, and the
reconciliation pass that finds a session with no node.

Adoption and reconciliation get the fix for free, and they had the same bug: a session found
with no node was placed up and left of a cursor that never moved, which is how #60 looked
before anyone tied it to `trig new`.

The rule depends on `Viewport.Offset` being saved, which it already is. The CLI has no
terminal and no idea how many cells fit, and does not need one: it reads the offset the map
last wrote and searches from that cell, so `trig new` and `n` place identically. Verified by
making ten notes each way and diffing the cells.
