# `g` decides by where the cursor is

`g` has the two jobs SPEC §7.3 gives it — create a group from the selection, and add to the
group under the cursor — and chooses between them by position alone: the cursor inside an
existing rect means join, anywhere else means create. There is no group mode, no second
binding, and nothing to select a group with.

This follows from groups being spatial (ADR 0001). A group *is* a region of the map, so
pointing at it is the whole of naming it, and any other way of choosing one would be a
handle onto something that already has a position.

## Considered options

**A second binding for joining.** Rejected: the two gestures are one intention — "these
belong together" — and the map already knows which one is meant from where the cursor is.
A second key would have to be explained, and would be wrong exactly as often as the user
misremembered which was which.

**A picker listing the groups.** Rejected: a list is how you choose between things that
have no place. Groups have nothing else — a picker would name rectangles by title and then
scroll the map to them, which is the cursor's job done twice.

## Consequences

Adding an outside node to a group is `v` on it, then walking the cursor into the group, then
`g`. The walk gathers what it passes (§7.3), so what joins is the run you walked, which is
what a selection means everywhere else on the map.

`g` on a node that already sits inside the rect does nothing, because containment is
membership and it is already a member. That is a key that appears to do nothing, and it is
the honest answer: there is no state left for it to change.

Creating a group gathers its members into a block of cells that is *clear* — one holding
nobody but them — rather than shoving bystanders out. A gesture that rearranged the far side
of the map to make room would move far more than it was asked to. Growing a group is the
opposite case and does shove, under the map's one collision rule (`Shift`): the alternative
is a rect that reaches over a bystander and silently absorbs it, which is the one thing
membership-by-containment must never do.
