# The CLI and the map share one node service

`trig new`, `trig ls`, and `trig attach` do what `n`, the map, and `Enter` do, without the
map. They do it by calling the same functions the map view calls, which meant deciding where
those functions live: until now the rules for turning a node into a session — what to name
it, what command to start it on, what environment to hand it — were unexported methods on
the map view's `Model`.

They move down into `internal/state`, beside the node they are rules about. `state` already
owns what a node *is*; it now also owns how a node maps onto a session, as pure functions of
node and workspace:

- `Node.StartCmd()` — the command a session is started on (ADR 0014's wrapping rule).
- `Workspace.SessionOf(Node)` — the session name, honouring adoption (ADR 0007).
- `Workspace.DirOf(Node)` — where the session starts.
- `Provenance(workspace, node, statusDir)` — the environment a session carries about itself.
- `Workspace.Dead(running)` — liveness derived from a session list (ADR 0006).
- `Workspace.Match(query)`, `Fuzzy(pattern, text)` — the subsequence match the filter, the
  palette, and `trig attach` all rank by.
- `Kind.Label()`, `MaxTitleLen`, `MaxCmdLen`, `ClampRunes` — the vocabulary and the bounds a
  card, a prompt, and a flag all have to agree on.

One rule went the other way for the same reason: `Report.Stale(after)` belongs to
`internal/status`, beside the stamp it is judged from, so that `trig ls` marks a report the
map's badge would have marked.

`internal/tmux` is unchanged. It stays the mechanism — a package that knows about sessions
and nothing about nodes — and `state` reaches into it only for `SessionName`, the one string
both sides have to agree on.

## Considered options

**A new `internal/node` package.** Rejected: it would hold `state`'s data with a tmux call
attached to it, so every caller would carry both imports and one more name for the thing it
already had in hand. The functions are pure functions of a `Node` and a `Workspace`; a
package whose whole content is functions of another package's types is a layer, not a seam.

**An `internal/app` service layer, with `app.New`/`app.List`/`app.Attach` the map view calls
too.** Rejected: the map view does not make nodes synchronously. Creation is a `tea.Cmd`
whose answer arrives as a message, precisely so that a slow tmux cannot freeze the map, and a
synchronous service API would either be ignored by the one caller that matters or force the
map back onto a blocking call. The CLI is the synchronous client; that is a property of the
CLI, not a shape to impose on both.

**Leave the rules in `internal/tui` and export them.** Rejected: it makes the map view a
dependency of the command line, so `trig ls` — which draws nothing — would pull in Bubble
Tea, lipgloss, and glamour, and the seam would be named after the one client that does not
need it.

## Consequences

There is one answer to what a node's session is called, what it starts on, and what it
carries, and both clients read it from the same place. `trig new` and `n` cannot drift.

`state` now imports `tmux` and `status`. Neither imports `state`, so there is no cycle, and
the direction is the honest one: the domain knows the names it has to agree with the
mechanism about.

A node created by `trig new` while a map view is open on the same workspace is lost when that
view next saves: the map view holds the whole workspace in memory and writes all of it. The
acceptance criterion is the headless case — the node appears when the map next opens — and
the fix, if it is ever wanted, is for the map view to reload on a change to the file rather
than for the CLI to merge.
