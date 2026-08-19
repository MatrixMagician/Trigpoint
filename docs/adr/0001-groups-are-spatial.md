# Groups have no membership list

A `Group` holds only a `Rect`, and a node is in that group iff its cell falls inside the
rect. There is no `Members` field and no `GroupID` on `Node` — this is deliberate, not an
oversight. Trigpoint is a spatial tool: "these belong together" is expressed by putting
things together, and derived membership cannot desynchronise from what is drawn on the map.
Nodes that need associating without sitting together use tags, which already carry filter
and bulk-selection behaviour.

## Considered options

**A `Members` list, with the rect drawn to fit them.** Rejected: it creates a state that can
contradict the picture — a node listed as a member but rendered outside the box, or inside
the box and not listed. That is the exact confusion a visual grouping exists to prevent, and
it also lets scattered members produce enormous overlapping rects.

**A `Members` list, with the rect hand-positioned as a hint.** Rejected for the same reason,
more acutely: the rect becomes free to lie about membership outright.

## Consequences

Rigid group movement is what makes containment sufficient. Moving a group carries the nodes
inside it at fixed relative offsets (member set snapshotted when the gesture starts) and
shoves non-members aside under the same collision rule node movement already uses — so a
group can never drift over a bystander and absorb it, which is the failure mode that would
otherwise force a membership list back into the model.

Two nodes that sit apart on the map cannot be grouped. This is intended; it is the boundary
between `Group` (spatial) and `Tag` (logical).

Moving a node out of a rect, or shrinking a rect past a node, removes it from the group.
Cards display their group alongside their tags so this is visible rather than silent.
