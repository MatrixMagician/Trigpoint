# A group is held with `V`

`V` picks up the group the cursor is standing in and holds it until `Esc`. While one is
held the motion keys act on the rectangle rather than on a card: `H J K L` move it rigidly,
`h j k l` move its far edge, `x` deletes it, `Esc` and `V` let go.

This is the resolution of SPEC §14 risk 6 — rigid group movement (§6) needs a way to name a
group, and `H J K L` were already spoken for by nodes. `V` is `v` one unit larger, which is
what it is in vi, and a held group is the same kind of thing a selection is: not a mode, but
a statement about what the next keystroke is about.

It amends [ADR 0012](0012-g-decides-by-where-the-cursor-is.md), which said there was
nothing to select a group with. `g` still chooses its two jobs by where the cursor is and
still has nothing to do with holding; what has changed is that a group now has a gesture of
its own, because it has something to do that a node cannot do for it.

## Considered options

**A group-select mode reached from `g`.** Rejected: `g` decides by position and does not
branch, and a third job on it would be the mode ADR 0012 exists to avoid. It would also make
the two commonest group gestures — create and move — the same key.

**`v` on a cell inside a rectangle selects the group.** Rejected: `v` on a node inside a
group is how a member is gathered for a bulk action, and that is not a rare thing to want.
One key cannot mean both without the cursor's contents deciding, and what the cursor is
standing on is already how `g` decides. Two keys deciding that way is a map you have to
remember the rules of.

**Its own resize keys (`>` `<` `+` `-`), with `h j k l` still moving the cursor.** Rejected:
while a group is held the cursor has no job — every key is about the rectangle — so the
lowercase motions were free, and pairing them with the uppercase ones (move the thing /
move its edge) is one rule instead of four bindings to learn.

## Consequences

A held group is drawn with bold walls and the status bar names it and its keys, because
this is the one state in which a key means something other than what it means on the map.

The member set is snapshotted when `V` is pressed, and taken again after a resize — a shrink
that drops a node must not carry it on the next move. Nothing else can change it, because
the leading strip of cells the rectangle is about to claim is cleared by the same shove that
node movement uses, so a bystander is always one cell beyond the rectangle's new edge rather
than inside it. That is what lets containment stay the whole of membership.

A held group stops at another group rather than moving onto it, and says so on the status
bar. This is the one refusal on the map, and it is not a collision rule: two rectangles
sharing cells would hand every node in the overlap to whichever was drawn first, which is
membership changing without anything on the map moving. Nodes are shoved because a node can
be pushed somewhere; a group cannot be pushed without pushing its members and theirs.

A bystander shoved out from under a moving group can be shoved into a *different* group's
rectangle, and is then a member of that one. This is not a hole in the rule, it is the rule:
a shove puts a node in a cell, and a node in a cell inside a rectangle is in that group,
exactly as it is when the same shove comes from `H J K L` on a card. The card says which
group it is in, so it is visible rather than silent. Refusing that shove would be the
collision rule refusing a move, which is the one thing it does not do.

Counts do not apply to a held group — `3L` moves a card three cells, `L L L` moves a group
three. Nothing is stopping the count from being read here later; there was no reason to read
it before anyone had asked to move a group very far.
