# Agent status is a directory of files Trigpoint polls

An agent writes its own state to `~/.local/state/trig/status/<workspace>_<nodeID>.json`, and
Trigpoint reads that whole directory on a one-second tick. There is no watcher goroutine, no
inotify, and nothing anywhere that reads an agent's output to work out what it is doing.

Three decisions are stacked here, and they are recorded together because each only makes
sense given the others.

## The file is the contract, not the hook

SPEC §8's principle is inherited from nodeterm: agents announce state, Trigpoint never
scrapes it. So the seam has to be something an agent can write with no library and no
knowledge of Trigpoint — a JSON file at a path handed to it in its environment. Hook
plumbing changes with every agent release; `{"state": …, "ts": …, "detail": …}` does not.

`trig emit-status` exists so that the plumbing has something to call, and so that the
contract can be exercised by hand from inside a node. It is a convenience over the file
format, never a privileged path into Trigpoint: an agent that writes the JSON itself is
exactly as integrated as one that shells out.

Absence of a file is not a state. An agent that has never reported is unknown, and an
unknown agent's card says nothing about its agent status rather than guessing "running" from
the fact that the session is alive (CONTEXT.md, "Agent status").

## Polling, not fsnotify

SPEC §8 names fsnotify. This uses a `tea.Tick` that reads the directory instead.

fsnotify would be the first non-Charm runtime dependency, a watcher goroutine to wire and
cancel alongside the tmux monitor, and — because every writer of a status file renames it
into place — a directory watch with rename handling rather than the per-file watch it looks
like it should be. It would also still need a poll fallback where inotify is unavailable.
What it buys is roughly nine hundred milliseconds of badge latency on a notification a human
reads.

The map already has a tick loop for exactly this shape of question, and a `ReadDir` over a
handful of small files costs less than the `capture-pane` calls the same loop already makes
per frame.

## The file is keyed by workspace and node, not by node alone

SPEC §9.1 writes `status/<nodeID>.json`. Node ids are drawn unique against one map
(`state.NewNodeID`), not against every map there is, so two workspaces can each hold a node
called `kt7m` — and under the spec's filename their agents would write to one file, each
card showing whichever of them reported last.

The name used is `<workspace>_<nodeID>.json`, which is the key the tmux session names already
use (`trig_<workspace>_<nodeID>`). This is a deliberate deviation from SPEC §9.1 and is what
the documented contract says, because the contract is what an agent integrates against.

## Consequences

A status report is read from disk on every tick and never persisted into the workspace file.
It is derived state, for the same reason liveness is
([ADR 0006](0006-liveness-is-derived-not-stored.md)): a state that survived a reboot would be
a claim about a process that did not.

Removing a node removes its status file. Ids are only unique against the nodes on the map, so
they come back round, and a report left on disk is a badge the next node handed that id would
wear as its own.

Staleness is displayed, never resolved. A `running` report older than `status_stale_after_min`
draws a `?` beside its dot and stays `running`: Trigpoint does not know what happened, and
the honest badge is one that says the report is old.

The badge composes three sources into one glyph rather than three: dead outranks everything
and draws `✗`; otherwise the dot's shape carries unread (`●` read, `○` unread) and its colour
carries agent status. A card is 22 cells wide and the title needs them.
