# Every card on the map is the same height

Cards have a body — preview lines for a session-backed node, the markdown body for a note —
and the number of body lines is one number for the whole map: the most any node currently on
the map asks for, capped so that one node cannot cost every other card its screen.

A map with nothing to put in a body draws cards as their two borders, exactly as before
notes existed.

## Considered options

**Per-node height, cards packed.** Rejected: the map is a cell grid, and a node's position
on screen has to be its position on the map. A taller card in one column pushes the rest of
its row down and the columns stop lining up, which is the one property that makes `h j k l`
mean anything.

**Per-row height — the tallest card in each map row.** Rejected as the same trade in a
smaller package: rows would change height as you scroll, so the viewport arithmetic in
`follow` and `centre` would need the heights of rows it has not drawn to know how many fit.

**A fixed height from config, independent of content.** Rejected: with the default four
preview lines every shell card on the map would carry four blank lines until live previews
land, and a map of two-line notes would waste half its screen.

**Per-node card size deciding the cap.** Deferred, not rejected: `Size` is the field this
cap belongs on (SPEC §7.3, `s`), but nothing sets it until card attributes land, so wiring
it now would be a switch with two unreachable arms. One constant until then.

## Consequences

Adding one long note grows every card on the map. This is the honest cost of a grid, and it
is bounded by `maxNoteLines`, so a thousand-line note costs ten lines and not a thousand.

Asking a node how tall it wants to be has to be cheap, because the question is asked of every
node on the map on every frame. For notes that means the rendered body is cached rather than
re-parsed — see [ADR 0004](0004-note-bodies-render-through-glamour.md).

Live previews (M2) fill in the body for shell and agent nodes by making `nodeBodyHeight`
answer for those kinds too, and card sizes (M3) replace the constant cap with the node's own
`Size`. Neither moves anything else about card geometry.
