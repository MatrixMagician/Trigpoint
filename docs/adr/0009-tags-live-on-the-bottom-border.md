# Tags live on the bottom border

A card's top border carries the status dot and the title, and nothing else. Its tags go on
the bottom border, after the kind and the session age, taking whatever those leave.

This contradicts SPEC §7.1, which draws the tags on the top border. The spec has been
updated to match; this ADR is why.

## The arithmetic that forces it

A card is 22 cells wide. On the top border the lead `╭─ ● ` costs 5 and the corner `─╮` 2,
which leaves 15 for a title, a tag, and the rule between them. `api-server` is 10 and
`#infra` is 6.

Card attributes (#10) first put the tags on the top border, where they took what the title
left and dropped out below four cells. The result was a tag that appeared only for titles of
seven cells or fewer — set on any real node and never seen again (#37).

The bottom border was already half empty: `sh · 2h` is 7 of the same 22, and a note's `note`
is 4. Moving the tags there costs nothing and fits both labels at once:

```
╭─ ● api-server ─────╮
│ $ tail -f log      │
│ 200 GET /health 3… │
╰─ sh · 2h ─ #infra ─╯
```

## Considered options

**Widen the card.** Rejected: 26 cells is the widest that still holds three columns of map
in an 80-column terminal, and it is one cell short of showing `#infra` beside `api-server`.
The width that fits them comfortably is SPEC §7.1's own ~33, which drops an 80-column
terminal to two columns — paying the map's whole reason for existing to place a label.

**Keep the tags on the top and truncate the title to make room.** Rejected: the title is the
node's handle, and `a-very-lo…` beside `#infra` is a card that has kept the decoration and
thrown away the name.

**Mark rather than name — a pip on the border saying "this node is tagged".** Rejected: a
tag exists to be read at a glance across a map. A mark that sends you to the filter to find
out what it says is a tag that has stopped being one.

## Consequences

The kind and the age are the fixed half of the bottom border and never give way; the tags
take what is left and are cut with an ellipsis, or dropped entirely below four cells. So a
node with many tags shows the first of them and a mark that there were more, and a node with
a long age (`sh · 365d`) shows fewer of them than a fresh one — the age is what the card is
sure of, the tag list is not.

The title now has the whole top border — 14 cells rather than the 5 it was left with — so
moving the tags bought the title room as well.

SPEC §6 wants a card to display its group alongside its tags, which will want the same
strip. When groups land, the bottom border is where that competition happens, and this ADR's
rule — fixed labels first, variable labels take what is left — is the one to extend.
