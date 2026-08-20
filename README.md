# Trigpoint

A keyboard-driven spatial map over long-lived terminal sessions.

Trigpoint (`trig`) lays your shells out on an infinite grid instead of a list of tabs. It
owns naming, layout, grouping, status, and discovery; tmux owns the PTYs, the scrollback,
and the attachment. Quitting Trigpoint kills nothing — every session outlives it.

## Status

Early. The map, its cursor, shell nodes, note cards, the attach handoff, live previews, peek,
adopting sessions you already had, the four card attributes — name, colour, tags, size —
several workspaces to switch between, and agent nodes reporting their own status work; the
rest of the spec does not exist yet.

| Milestone | | |
| --- | --- | --- |
| M0 | Skeleton — config, workspace store, `trig doctor` | done |
| M1 | Nodes on a map — create/kill shell nodes, cursor and node movement, attach handoff | done |
| M2 | Live map — previews, peek, dead nodes, reconciliation, adoption | done |
| M3 | Organisation — colours, tags, sizes, workspaces, groups, filter, palette | in progress (group movement to come) |
| M4 | Agents — agent nodes, status badges, attention jump | in progress (`trig init-hooks claude` to come) |
| M5 | Polish and release | to do |

[`SPEC.md`](SPEC.md) is the whole design; this README describes only what is built.

## Requirements

- tmux 3.2 or newer (control mode, `capture-pane -e`, session environment)
- Go 1.26 or newer, to build

## Install

```sh
go install github.com/MatrixMagician/Trigpoint/cmd/trig@latest
```

Or from a clone:

```sh
go build -o trig ./cmd/trig
```

Then check the machine can actually run it:

```sh
trig doctor
```

`doctor` reports on tmux, tmux control mode, the config file, and the state directory, and
exits non-zero if any of them would stop Trigpoint working.

## Usage

```sh
trig              # open the default workspace
trig -w scratch   # open a named workspace
trig doctor       # check this machine

trig emit-status running "building the index"   # from inside an agent node
```

A workspace is an independent map with its own nodes and its own default working directory.
Workspaces share nothing; the default one is called `main` until you configure otherwise. The
name travels into the tmux session name `trig_<workspace>_<id>`, so it may not contain
whitespace, a path separator, `.`, or `:` — everything tmux reads a session name back through
uses one of those to mean something else.

## Keys

What is bound today:

| Key | Action |
| --- | --- |
| `h j k l` / arrows | Move the cursor to the nearest node in that direction |
| `3l`, `12j`, … | Count prefix — repeat the motion |
| `H J K L` | Move the selection one cell — the node under the cursor, or every node `v` has gathered — shoving whatever is in the way |
| `zz` | Centre the viewport on the cursor |
| `0` | Jump to the origin |
| `Enter` | Attach to the node under the cursor — the whole terminal, handed over. On a dead node, offer to respawn it. On a note, edit its body in `$EDITOR` |
| `Space` | Peek: read the node's recent output full-screen, without attaching — `j`/`k`, `Space`/`b`, `g`/`G` scroll, `Esc` returns |
| `n` | New shell node at the nearest free cell to the cursor |
| `N` | New note — a rendered markdown card with no session behind it |
| `a` | New agent node — `j`/`k` to choose a preset from config or `custom…` to type a command, `Enter` to start it. The node stores its command, so a respawn re-runs it; when the agent exits the shell remains |
| `r` | Rename the node under the cursor |
| `c` | Cycle its accent colour · `C` pick one by name — `j`/`k` to choose, `Enter` to set |
| `t` | Edit its tags — space-separated, several to a node. With a selection gathered, `tag` adds to every selected node and `-tag` removes |
| `s` | Cycle its card size, small → medium → large |
| `A` | Adopt a tmux session Trigpoint did not create — `j`/`k` to choose, `Enter` to adopt |
| `x` | Kill the node under the cursor and its session (asks first; a note or a dead node is just removed). With a selection gathered, one confirmation names the count and kills all of them |
| `v` | Visual select — gather the node under the cursor, or let go of one already gathered. The motion keys then extend the selection, and `H J K L`, `g`, `t`, and `x` act on all of it at once; `Esc` clears it |
| `g` | Group — gathers the selection together and draws a named rectangle round it. With the cursor already inside a group, adds the selection to that one instead |
| `V` | Hold the group under the cursor. While one is held, `H J K L` move the whole rectangle and everything inside it, `h j k l` move its far edge, `x` deletes it, `Esc` lets go |
| `Tab` / `Shift-Tab` | Next / previous workspace, in name order |
| `w` | Workspace picker — `j`/`k` to choose, `Enter` to open, `n` new, `x` delete |
| `/` | Filter the map — narrows as you type, `Enter` keeps it, `Esc` clears it |
| `u` | Jump to the next node needing attention — every agent reporting NEEDS YOU first, then every unread card, in map order. Press it again for the one after |
| `Ctrl-K` / `:` | Palette — every command, every node on every map, every workspace, every session to adopt |
| `q` / `Ctrl-C` | Quit — sessions keep running |

`Enter` hands the entire terminal to that node's tmux session, so TUI apps, 256-colour
output, resize, and copy-mode behave exactly as they do under a plain `tmux attach` —
Trigpoint emulates none of it. `M-Escape` (configurable as `detach_key`) brings you back and
the map redraws. Trigpoint installs that binding for the duration of the attach only, and
points it at the node's session, so running Trigpoint inside tmux works rather than being
refused; see [ADR 0002](docs/adr/0002-the-detach-binding-targets-the-session.md).

A note has no tmux session at all. `Enter` on one edits its body in `$EDITOR` by the same
mechanism attach uses — the terminal is released and taken back when the editor exits — and
the card redraws with what you wrote. Quitting the editor without writing leaves the body as
it was; writing and then exiting non-zero keeps the writing. Notes are never alive and never
dead, so nothing about liveness applies to them — a note's card carries no status dot.

The card shows the markdown rendered, not its source: headings, bullets, and emphasis come
out as themselves, wrapped to the card and capped at the note's card size so one long note
cannot cost every other card its screen. See
[ADR 0004](docs/adr/0004-note-bodies-render-through-glamour.md), which also records what
rendering markdown costs the binary.

## Card attributes

Four things about a node are yours to set, and all four are the node rather than the session
behind it — so all four persist, and all four are on the card.

`r` renames. `c` steps the accent colour on by one and off the end of the palette back to a
plain border; `C` picks one by name, and the card wears the colour while you choose, because
a colour is a thing you pick by seeing it. `t` edits the tags: space-separated, hash
optional, several to a node. See
[ADR 0008](docs/adr/0008-accent-colours-are-a-named-palette.md) for why the colours are a
closed set of names.

`s` cycles the card size, small → medium → large, which is how many preview lines the card
shows — `preview_lines` in config, `0 / 4 / 10` by default. A small card shows none and is
never captured at all, which is how a node you have finished watching stops costing a
`capture-pane` per tick. A note is sized the same way, except that a small one keeps its
first line: a preview is a snapshot of output the session still has, and `Space` reads it in
full, but a note's body is the node itself.

Every card on the map is the same height — the hungriest node's — so one large card grows
the rest, and the small one beside it still shows nothing. See
[ADR 0003](docs/adr/0003-every-card-on-the-map-is-the-same-height.md).

The border carries the lot: the status dot and the title along the top, the kind, the
session's age and the tags along the bottom, and the accent colour on the border itself.

```
╭─ ● api-server ─────╮
│ $ tail -f log      │
│ 200 GET /health 3… │
╰─ sh · 2h ─ #infra ─╯
```

The tags are on the bottom border rather than beside the title because a card is 22 cells
wide and that is not enough for both — the kind and the age leave room where a real title
does not. The kind and the age never give way; the tags take what is left, are cut with an
ellipsis when that is not enough, and go entirely below a few cells. See
[ADR 0009](docs/adr/0009-tags-live-on-the-bottom-border.md). A card inside a group carries
the group's name there too, in front of the tags.

Movement never refuses. `L` into an occupied cell shoves the occupant one cell right, and
whatever is behind it too: one collision rule, which groups reuse unchanged.

The cursor position and the viewport are saved with the workspace, so reopening Trigpoint
puts you back where you were looking.

## Groups

A group is a named, coloured rectangle of the map, and a node is in it exactly when its cell
falls inside. There is no membership list: what is drawn is the whole of what is stored, so
nothing can be listed as a member and rendered outside the box. See
[ADR 0001](docs/adr/0001-groups-are-spatial.md).

`g` on a selection asks for a name and draws the rectangle. It gathers the selected nodes
together first — into the nearest block of cells nobody else is standing on — so the
rectangle is tight rather than sprawling across everything that happened to lie between its
members. With the cursor already inside a group, `g` adds the selection to that group
instead, moving each node into a free cell inside the rectangle and growing the rectangle
when there is none, shoving whoever stood where it grew to. Which of the two `g` does is
decided by where the cursor is and nothing else; see
[ADR 0012](docs/adr/0012-g-decides-by-where-the-cursor-is.md).

`V` picks the group under the cursor up and holds it. While it is held the motion keys act
on the rectangle instead of on a card: `H J K L` move it one cell, carrying every node that
was inside it when you picked it up and shoving whoever stood in its path out from under it
first — so a group can never drift over a bystander and quietly absorb it. `h j k l` move
its far edge, growing the rectangle or shrinking it. `x` deletes the group and leaves its
nodes exactly where they are; `Esc` lets go. A held group stops at another group rather than
overlapping it, and the status bar says so. See
[ADR 0013](docs/adr/0013-a-group-is-held-with-V.md).

Because membership is containment, moving a node out of a rectangle takes it out of the
group, and so does shrinking a rectangle past it. Member cards carry the group's name on
their bottom border beside their tags, so that happens visibly rather than silently.

To associate nodes that sit apart, use tags: groups *are* the map, tags cross it freely.

## Workspaces

More than one map. `Tab` and `Shift-Tab` step through the workspaces in name order, and `w`
opens a picker on the one you are in — `Enter` opens the one under it, `n` makes a new one,
`x` deletes it. `trig -w <name>` opens straight into one, making it if it is not there yet.

Switching changes everything on screen, because workspaces share nothing: their own nodes,
their own groups, their own default working directory for new nodes. Each keeps its own cursor
and viewport too, so switching away and back returns you to what you were looking at rather
than to the origin — across restarts as well, since leaving a workspace writes it.

Deleting a workspace deletes the file and nothing else. The tmux sessions its nodes named go
on running: Trigpoint kills nothing it did not start, so its `x` says so before it does it,
and `trig -w <name>` on the same name gets the cards back through reconciliation. The last
workspace cannot be deleted — there would be no map to be on.

The workspace on screen is read from its file on every switch rather than held in memory
alongside the others. See
[ADR 0010](docs/adr/0010-a-workspace-switch-is-a-reload.md) for what that buys and what it
drops.

## Finding things

`/` narrows the map as you type, fuzzy-matching each card's title, its tags, and its kind —
so `asrv` finds `api-server` and `infra` finds everything tagged with it. `Enter` closes the
prompt and leaves the map narrowed; the status bar says how many cards of how many are left
and what the filter is. `Esc` clears it, from the prompt or from the map.

A filter is a way of looking at the map rather than a change to it: nothing moves and
nothing is written. Cards it hides are hidden from the cursor as well as from the screen, so
`Enter`, `r`, and `x` cannot reach a node with nothing on screen to say which one it is —
and a hidden card costs no `capture-pane` while it is gone. If the card under the cursor is
one of the ones that goes, the cursor moves to the nearest one that stays. Switching
workspace clears the filter: it was typed at one map, and the next is not it.

`Ctrl-K` or `:` opens the palette: one fuzzy list over every command the map has, every node
on every workspace, every workspace to switch to, and every session there is to adopt.
`↑`/`↓` (or `Ctrl-P`/`Ctrl-N`) choose, `Enter` runs, `Esc` closes. Choosing a node on
another map switches to that workspace and puts the cursor on it.

The palette is the discoverability backstop and the bindings are the fast path, so every
action with a key has an entry — and a command entry replays its own binding rather than
reimplementing it, which is what stops the two drifting apart. The same table names the keys
the status bar offers as hints.

## Dead nodes and reconciliation

Trigpoint reopens on a map that tells the truth about what is still running. Every node with
a session is checked against tmux: a node whose session is there is alive, and a node whose
session is gone — a reboot, a `tmux kill-server`, an `exit` typed inside it — is **dead**. A
dead card is dimmed and carries a `✗` instead of the status dot, and keeps everything else it
is: name, colour, tags, and position, so the map does not rearrange itself after a reboot.

`Enter` on a dead node offers to respawn it: a fresh session in the node's own working
directory, falling back to the workspace's, re-running its command if it has one. The node
keeps its id, so it keeps its session name too — respawning is not creating a new node. `x`
on a dead node asks to remove the card rather than to kill anything, because there is nothing
left to kill.

A session under the `trig_` prefix with no node behind it is reconstructed as a card, from
the `TRIG_WORKSPACE` / `TRIG_NODE_ID` / `TRIG_NODE_KIND` that every session Trigpoint starts
carries in its own environment. That is what gets your map back after a lost or corrupted
state file. Sessions belonging to another workspace are left to the map that owns them, and
sessions outside the prefix are left alone entirely until you adopt one with `A`.

Liveness is worked out from tmux every time and never written to disk: a stored flag would be
stale from the moment the machine rebooted, which is exactly when the map is most relied on.
The same pass runs at startup, whenever tmux says the session list changed, and on the slow
tick — so a node killed from inside tmux goes dead on the map without waiting for a restart.
See [ADR 0006](docs/adr/0006-liveness-is-derived-not-stored.md).

Notes are excluded from all of it. A note has no session, so it is never alive, never dead,
and carries no badge.

## Adopting a session you already had

Trigpoint is useful on day one against a tmux zoo you have been running for months. `A` lists
the sessions on the server that Trigpoint did not create and that are not already on this map;
`j` and `k` choose among them and `Enter` adopts one, which puts a shell node on the map at the
nearest free cell, tagged `adopted`, titled after the session.

Adoption creates nothing and renames nothing. The session was already running, the card simply
appears over it, and the name stays the one its owner gave it — so `tmux attach -t work` still
finds it and anything scripted around the name still works. Trigpoint stores that name on the
node instead of imposing its own prefix, which is what lets an adopted card do everything any
other shell node does: `Enter` attaches to it, its card previews it, reconciliation matches it
by the stored name, and `x` kills the foreign session after the usual confirmation.

Sessions whose names contain `.` or `:` are not offered. tmux accepts both and then reads them
back as its own window and pane separators, so it cannot be asked about such a session by name
at all — which is also why a workspace name may not contain them.

Two things differ, both because Trigpoint did not start the session. `Enter` on an adopted node
whose session has gone says so rather than offering a respawn — there is no command to re-run
and no name Trigpoint may create — so `x` removes the card. And its preview refreshes on the
slow tick rather than the moment output lands, because the activity subscription reports
sessions under the `trig_` prefix only. See
[ADR 0007](docs/adr/0007-an-adopted-node-stores-its-session-name.md).

## Live previews

Each card shows a snapshot of its node's recent output, with its colour, cut to the line
count for that card size. A preview is always a snapshot and never a live terminal — that
distinction is the whole architecture, and it is why a map of dozens of nodes costs what one
does.

One long-lived tmux control-mode client pushes the events that say a card has gone stale.
Activity marks it dirty rather than capturing on the spot; every dirty card that is actually
on screen is then captured together, once per `preview_debounce_ms`. A slow
`refresh_tick_s` catches whatever the events missed, and returning from an attach refreshes
at once rather than waiting for either.

The event connection is expected to drop — it attaches to one of your own nodes, so killing
that node takes it with it. When it goes, the slow tick carries on refreshing and the client
reconnects to another session, backing off while there is nothing to attach to. The monitor
never resizes or reads a byte from your panes; it only says *when* to look. See
[ADR 0005](docs/adr/0005-activity-arrives-as-a-format-subscription.md).

## Peek and unread

`Space` reads a node's recent output full-screen: two thousand lines of it, against the
four a medium card's body shows. It is the read-only counterpart to attach — peek never gives the
node your keyboard, so the keys scroll the snapshot and reach nothing else, and `Esc`
returns to the map. What you are reading is a snapshot taken when the peek opened, not a
live terminal; peeking again is what asks the session what it has said since. A dead node
still peeks, showing the last snapshot taken of it before its session went, or saying
plainly that there was never one.

Output arriving on a node while you are looking somewhere else marks its card unread — a
hollow dot in place of the live one. Unread is a property of your attention rather than of
the node's work, so it clears by looking: attaching or peeking, and nothing else. It is
never written to disk, because a mark that survived a restart would be one nothing had
learned anything about.

Measured on tmux 3.7b: one capture costs ~1.3 ms, and a viewport of 40 nodes ~55 ms for the
whole batch, near enough regardless of preview length — the cost is running `tmux`, not the
lines it hands back.

## Agent status

An agent node's card is badged with what the agent says it is doing. Trigpoint never reads an
agent's output to work it out: agents announce, Trigpoint draws.

| Badge | |
| --- | --- |
| `●` green | running |
| `●` amber | **needs you** — a question, a permission prompt, something only you can answer |
| `●` grey | done |
| `●` red | error |
| `●?` | the report is older than `status_stale_after_min` — displayed as old, never resolved into a guess |
| `○` | unread output, in whatever colour the agent last reported |
| `✗` | the session is gone |

The three sources compose into one mark: the dot's shape is unread, its colour is agent
status, and a session that has died outranks both. An agent that has never reported has no
colour at all — absence of a report is not a state, and Trigpoint says nothing rather than
inferring one.

`u` walks the cards that want you: everything reporting needs-you first, then everything
unread. Set `bell_on_needs_you = true` to have the terminal bell ring when an agent starts
needing you. It rings on the transition and nothing else: not on every pass over a report that
has not changed, and not for what was already on disk when the map opened — so starting
Trigpoint, or coming back to a workspace with `Tab`, is silent.

### The contract, for any agent

Every agent node is created with `TRIG_STATUS_FILE` in its session environment — a path in
`~/.local/state/trig/status/`. Write this JSON to that path and the badge follows within a
second:

```json
{"state": "needs_you", "ts": "2026-08-20T11:04:05Z", "detail": "may I run rm -rf build?"}
```

`state` is one of `running`, `needs_you`, `done`, `error`, and nothing else. `ts` is when the
agent said it, which is what staleness is measured against; `detail` is optional and is shown
in the status bar beside the card under the cursor. Write it atomically — a temp file and a
rename — because Trigpoint reads the directory while agents are writing to it.

`trig emit-status <state> [detail...]` does all of that for you, and is what an agent's hooks
should call:

```sh
trig emit-status needs_you "may I run rm -rf build?"
trig emit-status done
```

It reads `TRIG_STATUS_FILE` from the environment, so it works from inside an agent node and
nowhere else — try it by hand in one and watch the badge change. It is a convenience over the
file format and never a privileged path: an agent that writes the JSON itself is exactly as
integrated as one that shells out.

The file format is the stable part of this. Hook plumbing changes with every agent release;
`{state, ts, detail}` does not. See
[ADR 0015](docs/adr/0015-agent-status-is-a-directory-of-files-trigpoint-polls.md) for why the
directory is polled rather than watched, and why the file is named after the workspace as
well as the node.

## Configuration

`~/.config/trig/config.toml`, or `$XDG_CONFIG_HOME/trig/config.toml`. It is optional — the
defaults are enough to run, and a partial file keeps every default it does not mention. A
key Trigpoint does not recognise is an error rather than a silent no-op.

```toml
[general]
default_workspace = "main"
confirm_quit = false          # ask before quitting
detach_key = "M-Escape"       # single keystroke back to the map from an attached node
preview_debounce_ms = 500
refresh_tick_s = 5
status_stale_after_min = 10   # a running report older than this is badged as stale
bell_on_needs_you = false     # ring the terminal when an agent starts needing you

[general.preview_lines]       # preview lines per card size
s = 0
m = 4
l = 10

[agents.claude]               # presets `a` offers when creating an agent node
cmd = "claude"

[agents.codex]                # adding one is an edit here, never a code change
cmd = "codex"
```

The `agents` tables are the presets, not additions to them: a file that names any replaces
`claude` and `codex` entirely, so a preset for an agent you do not have installed can be got
rid of. A file with no `agents` table keeps both defaults.

Every setting above is live; a node that has never been sized takes the `m` line count. A
`status_stale_after_min` of `0` turns the stale mark off rather than staling everything at
once.

## Files

| Path | What |
| --- | --- |
| `~/.config/trig/config.toml` | Configuration |
| `~/.local/state/trig/workspaces/<name>.json` | One workspace: its nodes, groups, and viewport |
| `~/.local/state/trig/status/<workspace>_<node id>.json` | What one agent last said about itself |

`XDG_CONFIG_HOME` and `XDG_STATE_HOME` are honoured. Workspace writes are atomic, so a kill
at any moment leaves either the old map or the new one, never a mixture.

tmux sessions Trigpoint creates are named `trig_<workspace>_<node id>`. That naming is a
convention, not ownership: attach to one with plain `tmux attach` whenever you like.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

The TUI is tested without a tmux server — everything tmux does sits behind one small
interface — so the suite needs no special environment. Tests that do want tmux (session
lifecycle, the attach handoff) run against a private `tmux -L` socket and skip themselves
when tmux is absent, so they can never touch the sessions you are working in.

The attach handoff is exercised end to end on a real pty and a real tmux server, including
the terminal-settings comparison either side of the round trip. What is left for a person —
colours as rendered, reflow, scrollback, one emulator at a time — is
[`docs/handoff-test-matrix.md`](docs/handoff-test-matrix.md), which also records which
terminals have not been tried yet.

Before changing anything, read [`CONTEXT.md`](CONTEXT.md) for the vocabulary the code and
the issues use, and [`docs/adr/`](docs/adr) for the decisions already taken.

## Licence

Apache 2.0. See [`LICENSE`](LICENSE).
