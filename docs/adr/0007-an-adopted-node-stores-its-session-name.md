# An adopted node stores its session name

A node adopted from a session Trigpoint did not create carries that session's own name in a
`Session` field on the node, and the workspace file records it. Every other node has the
field empty and its session name derived, as before, from `trig_<workspace>_<nodeID>`. One
accessor — `Model.sessionOf` — answers "which session is behind this node" for both, and
every path that touches a session goes through it: attach, capture, kill, and the
reconciliation pass that decides what is alive.

Adoption renames nothing. The foreign session keeps the name its owner gave it, which is the
whole point — `trig` is a map over a tmux zoo that was there first, and a tool that renamed
what it adopted would be a tool you could not run twice on the same server.

## Considered options

**Rename the session to `trig_<workspace>_<id>` on adoption.** Rejected: it is the one thing
SPEC §9.3 forbids. It would also break every script, window title, and muscle-memory
`tmux attach -t work` the session's owner already has, and it is irreversible from the map —
Trigpoint would have taken over a session it did not create and could not give back.

**Derive the session name from the node's title.** Rejected: titles are editable and node
identity is not. Renaming an adopted card would silently point it at a session that does not
exist, or — worse — at somebody else's that does.

**A `Kind` of its own, `adopted`.** Rejected: an adopted session is a shell, and every
question the map asks about a shell has the same answer for it. Kind is what sits behind a
node, and a kind that differed only in how its name is spelt would have to be special-cased
at every place `KindShell` already appears. The `adopted` tag says the same thing where it
belongs — on the card, for the reader — and nothing branches on it, because tags are the
user's to edit and what may be done to a foreign session is not.

**A separate list of adopted sessions beside the nodes.** Rejected for the reason groups
have no member list (ADR 0001): two places to say the same thing is one place to disagree.

## Consequences

Reconciliation resolves an adopted node by its stored name and needs to know nothing else
about adoption. A foreign session with a node is a placed session, so it is never
reconstructed; a foreign session without one fails the `Ours` check the orphan pass already
makes, so it is left alone. An adopted node whose session has gone is dead exactly like any
other — the dimmed card, the `✗`, and `x` to remove it.

`tmux.Kill` no longer refuses a session outside the prefix. Killing one is an instruction
about a card on the map, given at the same confirmation prompt every other node's kill takes,
and a card whose session refused to die would be worse than no card at all. `Create` keeps
its check: Trigpoint still never invents a name outside its own prefix, and so adoption is
the only way a foreign name ever reaches the tmux package.

An adopted node is not offered a respawn. `Enter` on a dead one says why instead: Trigpoint
did not start the session and knows neither the command that was in it nor a name it is
allowed to create — there is nothing to re-run and nothing to re-run it under. This is what
SPEC's "respawn is offered only where a command is known" comes to, since adoption never
records a command.

A session whose name contains `.` or `:` is not offered for adoption. tmux accepts both in a
session name and then reads them straight back as its own window and pane separators, so
`-t "=my.proj"` is a pane that is not there — `has-session` and `kill-session` fail, and
`capture-pane` quietly answers about a different session. `state.ValidName` refuses the two
characters in a workspace name for exactly this reason; adoption is the other end of the same
string, and the same judgement is made there. Reaching those sessions means addressing tmux by
`#{session_id}` rather than by name, which costs a lookup on every capture — worth doing only
if such names turn out to be common.

An adopted card's preview is refreshed by the slow tick rather than by activity. The event
stream reports activity for sessions under the prefix only, because the subscription reports
every session on the server in one whitespace-separated line and a foreign name may contain
whitespace — half a name is a session that is not there (ADR 0005). The monitor also attaches
only to Trigpoint's own sessions, so a map of nothing but adopted nodes has no event stream
at all. Both are bounded by the slow tick, which re-captures every visible card and
reconciles besides, and both are fixed the same way if it ever grates: a per-session
subscription for the adopted names, which the map already knows.

The `Session` field is absent from every workspace file written before this change, and an
empty one means "named by Trigpoint" — so old files load unchanged and no migration exists.
