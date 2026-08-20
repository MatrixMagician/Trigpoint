# Trigpoint

A keyboard-driven spatial map over long-lived terminal sessions. Trigpoint owns naming,
layout, grouping, status, and discovery; tmux owns PTYs, scrollback, and attachment. The
vocabulary below keeps that ownership line visible — terms on the Trigpoint side of it
never describe terminal mechanics, and terms on the tmux side never acquire Trigpoint
meaning.

## The map

**Workspace**:
An independent map with its own nodes, groups, and default working directory. Workspaces
do not share nodes; switching workspaces changes everything on screen. The map view holds
one at a time — a switch writes the workspace being left and reads the one arrived at; see
[ADR 0010](docs/adr/0010-a-workspace-switch-is-a-reload.md).
_Avoid_: project, board, space

**Map**:
The infinite cell grid belonging to a workspace, on which nodes are placed. Exactly one
map per workspace — the map is the workspace's spatial layer, not a separate thing you
can own or switch independently.
_Avoid_: canvas, board, grid (as a noun for the whole)

**Map view**:
The home screen that renders the map. Distinct from the map itself: the map persists, the
view is what you are currently looking at it through.

**Cell**:
The unit of position on the map. Positions are always cell coordinates, never pixels or
character offsets — this is what makes directional keyboard movement deterministic.

**Cursor**:
The cell you are pointing at. It is a position on the map, not a node — the node under it,
if there is one, is the selection. The motion keys hop it from node to node, so an empty
cell under the cursor is something left behind (a killed node, a fresh map), not somewhere
you steer to.
_Avoid_: selection (for the cursor itself), focus, pointer

**Selection**:
The nodes an action applies to: the one under the cursor, or the several `v` has gathered.
Not a mode — a non-empty selection *is* visual select, and every key goes on meaning what it
meant with more than one node to mean it about. It is never persisted and never crosses a
workspace; see
[ADR 0011](docs/adr/0011-visual-select-is-a-selection-not-a-mode.md).
_Avoid_: multi-select, marks, highlighted nodes, visual mode

**Viewport**:
The window of cells the map view is currently showing. It follows the cursor and is
remembered with the workspace, so reopening Trigpoint puts you back where you were looking.
_Avoid_: camera, scroll, screen

**Shove**:
What a move does to whatever stands in its way: the occupant is pushed on by the same step,
and so is whoever stands behind it. There is exactly one collision rule on the map — node
movement and group movement are the same rule with a different set of movers, never a
variant that refuses the move. See docs/adr/0001-groups-are-spatial.md.
_Avoid_: push, swap, displace, collision handling

**Group**:
A named, coloured rectangular region of the map that contains the nodes sitting inside it,
and moves as a rigid container — carrying its contents, shoving anything in its path.
Membership is containment and nothing else: there is no member list that can disagree with
what is drawn.
_Avoid_: folder, cluster, tag group

**Tag**:
A label marking nodes that belong together regardless of where they sit. The logical
counterpart to a group — tags cross the map freely, groups *are* the map. On a card a tag is
drawn on the bottom border, after the kind and the age; see
[ADR 0009](docs/adr/0009-tags-live-on-the-bottom-border.md).
_Avoid_: label, category, group (for a tag)

## Nodes

**Node**:
One named, placed, long-lived thing on the map — the central entity of the domain. A node
owns its identity (name, colour, tags, position, size); it does not own the terminal
behind it.
_Avoid_: pane, window, tab, item

**Shell node**:
A node backed by a plain login shell in its working directory.

**Agent node**:
A node backed by a shell that immediately runs an agent command line. The only kind that
reports agent status. When the agent process exits the node remains — an agent node is not
defined by having a live agent, only by having been started as one.

**Note node**:
A node holding a markdown body and no session at all. Notes are placed and styled like any
node but are never alive, never dead, and never carry a badge.
_Avoid_: card (for the markdown body), memo

**Card**:
The rendering of a node on the map — border, preview, badge. Strictly visual. A card is
never created, killed, or persisted; nodes are. Say "remove the node", not "remove the card".

**Accent**:
The named colour a node carries, drawn on its card's border. A name from a closed palette
rather than a colour value — the name is what persists and what `c` cycles. See
[ADR 0008](docs/adr/0008-accent-colours-are-a-named-palette.md).
_Avoid_: highlight, theme, colour (for the palette entry itself)

**Card size**:
Small, medium, or large: how many body lines one node asks for. A property of the node, not
of the card — the card is only where the size shows, and a small one shows no preview at all.
_Avoid_: zoom, scale, height

**Preview**:
The recent-output snapshot shown inside a card. Always a snapshot, never a live terminal —
the distinction is the whole v1 architecture.
_Avoid_: output, tail, live view

**Dirty**:
A card whose preview has been overtaken by output, and which is therefore owed a fresh
capture on the next tick. A property of the snapshot, not of your attention — *unread* is
the attention counterpart, and the two clear on entirely different things.
_Avoid_: stale (that is an agent status), unread, changed

**Body**:
The region of a card between its two borders, and what goes in it. A preview is what fills
the body of a session-backed node; a note's markdown fills the body of a note. Every card on
the map has a body of the same height — see
[ADR 0003](docs/adr/0003-every-card-on-the-map-is-the-same-height.md).
_Avoid_: content, contents, card text

## Sessions and liveness

**Session**:
The tmux session behind a node. At most one per node, and Trigpoint's claim over it is a
naming convention, not ownership — a session outlives Trigpoint and survives its exit.
_Avoid_: process, terminal, shell (when the session is meant)

**Monitor**:
The single long-lived tmux control-mode client that says when something happened: a session
appearing or disappearing, a session producing output. It reports *when* to look and never
what was seen — a preview is read with `capture-pane`, never from the monitor. Losing it is
ordinary rather than a failure, because it attaches to one of Trigpoint's own sessions and
killing that node takes it with it. See
[ADR 0005](docs/adr/0005-activity-arrives-as-a-format-subscription.md).
_Avoid_: watcher, listener, event loop, control client (as the domain term)

**Alive** / **Dead**:
A node is alive when its session exists and dead when its state exists but the session is
gone. Liveness is a property of the session only — it says nothing about whether an agent
inside it is working, and it does not apply to note nodes.
_Avoid_: running, stopped, closed

**Respawn**:
Starting a fresh session for a dead node, in its working directory, re-running its command.
The node keeps its identity — respawning is not creating a new node, and in particular keeps
its id, which is what its session is named after.

**Reconciliation**:
The pass that matches persisted nodes against live sessions, deciding what is alive, what is
dead, and what exists in tmux with no node to explain it. It runs at startup, on every change
to the session list, and on the slow tick — liveness is derived every time and never stored,
because a stored flag is stale from the moment the machine reboots. See
[ADR 0006](docs/adr/0006-liveness-is-derived-not-stored.md).

**Adopted session**:
A tmux session Trigpoint did not create, mapped onto the map as a node. Adoption renames
nothing; the foreign session keeps its own name.

**Handoff**:
Giving the whole terminal to a node's session and taking it back on a keystroke. Trigpoint
releases the terminal entirely rather than drawing the session inside itself — the handoff
is why a node is the real thing and a preview is only a snapshot. One node is handed the
terminal at a time; that is the v1 limit the map exists to make bearable.
_Avoid_: open, launch, enter (as a noun), embed

**Detach key**:
The single keystroke that ends a handoff. It is a binding Trigpoint installs into tmux for
the duration of the handoff and takes back on return, not a key Trigpoint reads itself —
during a handoff Trigpoint is not reading anything. See
docs/adr/0002-the-detach-binding-targets-the-session.md.
_Avoid_: escape key, exit key

## Attention

**Badge**:
The composite indicator on a card's border. It is fed by three independent sources — agent
status, liveness, and unread activity — which is why it is one term and they are three.
_Avoid_: status (for the indicator itself), dot, icon

**Agent status**:
The state an agent node reports about itself: running, needs-you, done, or error. Always
self-declared by the agent, never inferred from its output. Absence of a report is not a
state — an unreported agent is simply unknown.
_Avoid_: state, progress, activity

**Status file**:
The file an agent writes its status to. The stable contract between Trigpoint and any
agent; hook plumbing is expected to change around it, the file format is not.

**Unread**:
Output has arrived on a node since you last looked at it. A property of your attention, not
of the node's work — it clears by looking, not by anything the node does.
_Avoid_: activity, new, dirty

**Stale**:
A reported agent status old enough that Trigpoint stops trusting it. Stale is displayed, not
resolved — Trigpoint never guesses what the true state became.

## Interaction

**Attach**:
Handing the terminal to a node so you are working inside it directly, at full fidelity.
The one operation that leaves the map.
_Avoid_: enter, open, focus, zoom

**Handoff**:
The mechanism attach is built on: Trigpoint gives up the terminal, the session takes it, and
Trigpoint takes it back on return. Named separately from attach because its correctness is a
standing risk, not an implementation detail.

**Peek**:
Reading a node's recent output full-screen without attaching. The read-only counterpart to
attach: peek never gives the node your keyboard.
_Avoid_: preview (that is the card's), view, inspect

**Filter**:
A query that narrows the map to the cards matching it, on title, tags, or kind. A way of
looking at the map, not a change to it: nothing moves and nothing is written. A card the
filter hides is hidden from the cursor too, so it cannot be attached to, renamed, or killed
while it is off screen — and the cursor moves to the nearest card that stays rather than
being left pointing at one that has gone. A filter belongs to the map it was typed at and
does not survive a workspace switch.
_Avoid_: search (that is the palette's), hide, query

**Palette**:
The single fuzzy list over every command, every node on every map, every workspace, and
every session there is to adopt. The discoverability backstop — bindings are the fast path,
and an action reachable only by its binding is one nobody can find. Command entries replay
their own bindings rather than reimplementing them.
_Avoid_: menu, launcher, command bar, palette (for the accent colours — those are a *set*)
