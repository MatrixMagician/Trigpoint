# Trigpoint

A keyboard-driven spatial map over long-lived terminal sessions. Trigpoint owns naming,
layout, grouping, status, and discovery; tmux owns PTYs, scrollback, and attachment. The
vocabulary below keeps that ownership line visible — terms on the Trigpoint side of it
never describe terminal mechanics, and terms on the tmux side never acquire Trigpoint
meaning.

## The map

**Workspace**:
An independent map with its own nodes, groups, and default working directory. Workspaces
do not share nodes; switching workspaces changes everything on screen.
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

**Group**:
A named, coloured rectangular region of the map that contains the nodes sitting inside it,
and moves as a rigid container — carrying its contents, shoving anything in its path.
Membership is containment and nothing else: there is no member list that can disagree with
what is drawn.
_Avoid_: folder, cluster, tag group

**Tag**:
A label marking nodes that belong together regardless of where they sit. The logical
counterpart to a group — tags cross the map freely, groups *are* the map.
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

**Preview**:
The recent-output snapshot shown inside a card. Always a snapshot, never a live terminal —
the distinction is the whole v1 architecture.
_Avoid_: output, tail, live view

## Sessions and liveness

**Session**:
The tmux session behind a node. At most one per node, and Trigpoint's claim over it is a
naming convention, not ownership — a session outlives Trigpoint and survives its exit.
_Avoid_: process, terminal, shell (when the session is meant)

**Alive** / **Dead**:
A node is alive when its session exists and dead when its state exists but the session is
gone. Liveness is a property of the session only — it says nothing about whether an agent
inside it is working, and it does not apply to note nodes.
_Avoid_: running, stopped, closed

**Respawn**:
Starting a fresh session for a dead node, in its working directory, re-running its command.
The node keeps its identity — respawning is not creating a new node.

**Reconciliation**:
The startup pass that matches persisted nodes against live sessions, deciding what is
alive, what is dead, and what exists in tmux with no node to explain it.

**Adopted session**:
A tmux session Trigpoint did not create, mapped onto the map as a node. Adoption renames
nothing; the foreign session keeps its own name.

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
