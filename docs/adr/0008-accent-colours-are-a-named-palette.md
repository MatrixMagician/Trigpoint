# Accent colours are a named palette

A node's colour is a name from a fixed, ordered set that Trigpoint defines — `red`,
`orange`, `yellow`, `green`, `cyan`, `blue`, `violet`, `pink`. The name is what the
workspace file stores and what `c` cycles; the ANSI code it draws as is Trigpoint's, and a
name the palette does not know draws as no colour at all.

## Considered options

**Free colour — a hex string per node.** Rejected: the map is a terminal, and the honest
range of a terminal is what the user's theme says it is. A stored `#7f3fbf` also has no
answer to "what is the next colour" — and `c`, the fast path, is exactly that question.

**Named colours configurable in `config.toml`.** Deferred, not rejected: the palette is a
table of names to codes, so it is a `[colours]` section away. Nothing yet asks for it, and
a per-machine palette makes a workspace file mean different things on two machines — the
name has to survive the trip.

**The eight ANSI names, and nothing else.** Rejected as too few once groups also carry a
colour (SPEC §6): two adjacent nodes and the group behind them want to be told apart, and
`orange` and `violet` cost a 256-colour code each.

## Consequences

A colour the palette does not know — a hand-edited file, or a palette that changes under a
workspace written by an older build — is drawn as no colour rather than as a guess, and `c`
walks it back into the palette on the next press. Nothing about a node is lost by this: the
colour is decoration, and the border it decorates is still there.

Selection and death outrank the accent on a card. A cursor you cannot find is worse than a
card drawn in the wrong colour, and a dead node reads as dead before it reads as green — so
the accent is what a card wears when it is neither.

The picker (`C`) shows the colour on the card rather than a swatch in the status bar: the
card is already on screen, and a colour is a thing you choose by seeing it.
