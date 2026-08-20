# Visual select is a selection, not a mode

`v` does not put the map into a mode. It adds the node under the cursor to a list of ids the
view holds, and a non-empty list *is* visual select: the motion keys go on moving the cursor
and now gather what they land on, `H J K L` go on moving nodes and now move all of them, and
`t` and `x` go on tagging and killing and now do it to every one. `Esc` empties the list.

Every action that can take more than one node asks one function for its targets — the
selection when there is one, the node under the cursor when there is not — so no action
branches on whether visual select is on to decide *what it acts on*. Two places do read the
selection: `t`, which needs a different prompt because a bulk tag edit is a different
operation (below), and the status bar, which has a count to report. Neither is a second
implementation of an existing key.

## Why not a mode

Trigpoint is modal, and every other multi-keystroke thing on the map *is* a mode:
`modeConfirmKill`, `modeColour`, `modeFilter`. Visual select looks like the next one on that
list. It is not, and the difference is what a mode is for.

A mode exists to give the keyboard a different meaning: `j` in the colour picker chooses a
colour, `j` in a filter prompt types a `j`. A mode is how those keys stop meaning what they
meant. But `v` changes nothing about what a key means. `l` still moves the cursor right. `x`
still asks before killing. `J` still shoves whatever is in the way. All that changes is how
many nodes the answer is about, which is a fact about the map and not about the keyboard.

Made a mode, `modeVisual` would have had to re-dispatch every key the map already handles —
the motions, the count prefix, `zz`, the attribute keys — and each one would then have had
two implementations that could disagree. That is the same failure the palette avoids by
replaying bindings rather than reimplementing them (§7.2).

## Consequences

**The cursor is marked inside the selection rather than outranking it.** An unselected
cursor keeps its own blue. A selected one takes the selection's colour — a 256-colour code,
so it cannot be confused with the cursor's blue or with any accent — and is underlined
within it. Colour alone would not do: a selection of one node *is* the cursor's card, and
had the cursor outranked the selection there, `v` on a single node would have changed
nothing on the map. The underline is also the only part of this a terminal with no colour
still draws.

**Motion extends only when there is something to extend.** With an empty selection the motion
keys navigate and gather nothing, which is what keeps `v` from being a key you must remember
to press *off*.

**One collision rule, more movers.** `Workspace.Shift` already took a list of ids
([ADR 0001](0001-groups-are-spatial.md)), so bulk movement is the existing rule with a longer
list — the selection steps together and keeps its shape, and bystanders are shoved exactly as
they are for a single node. There is no bulk-movement code to disagree with single-node
movement, because there is no bulk-movement code.

**Bulk tagging adds and removes; it does not set.** The single-node prompt opens on the tags
the node has and commits the list typed, because there is one node's list to show. A
selection has several, and prefilling from one of them would invite committing that one's
tags onto all of them. So the bulk prompt opens empty and reads two verbs: `infra` adds,
`-infra` removes. Setting would silently discard every tag the selected cards do not share,
and nothing on the map undoes.

**The selection is pruned, never stale.** It is dropped on a workspace switch (ids are only
unique against one map), on a filter change (a hidden card is one you cannot see yourself
acting on, §7.1), and when a node leaves the map. It is never written to the workspace file:
a selection is a thing you are in the middle of, and one that survived a restart would be a
map holding several nodes hostage to an intent nobody remembers forming.

**Groups will want this list.** SPEC §7.3 has `g` create a group from the selection. When
groups land, they read the same list and pass the same ids to the same `Shift` — the reason
this is a list of ids and not a rectangle.
