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
it now would be a switch with two unreachable arms. One constant until then. — Landed in M3
(#10); see the update below.

## Consequences

Adding one long note grows every card on the map. This is the honest cost of a grid, and it
is bounded, so a thousand-line note costs ten lines and not a thousand.

Asking a node how tall it wants to be has to be cheap, because the question is asked of every
node on the map on every frame. For notes that means the rendered body is cached rather than
re-parsed — see [ADR 0004](0004-note-bodies-render-through-glamour.md).

Live previews (M2) fill in the body for shell and agent nodes by making `nodeBodyHeight`
answer for those kinds too, and card sizes (M3) replace the constant cap with the node's own
`Size`. Neither moves anything else about card geometry.

## Update — M3, card attributes (#10)

The deferral above is closed. `maxNoteLines` is gone, and the cap is the node's own `Size`
looked up in `preview_lines`, which is the same number a session-backed card asks tmux for —
one card size, one card height, whatever is in the body. Nothing else about the geometry
moved: the map still draws every card at the height of the hungriest node on it.

Two things fell out of it. A card now cuts its own body to its own size as well as being
drawn at the map's shared height, so a small card standing beside a large one shows nothing
rather than spending the room the large one asked for. And a note keeps one line at the
smallest size where a shell card keeps none — a preview is a snapshot of output the session
still has and peek reads in full, but a note's body is the node itself, and a blank card
could not be told from a note with nothing written in it.
