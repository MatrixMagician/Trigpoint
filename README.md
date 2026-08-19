# Trigpoint

A keyboard-driven spatial map over long-lived terminal sessions.

Trigpoint (`trig`) lays your shells out on an infinite grid instead of a list of tabs. It
owns naming, layout, grouping, status, and discovery; tmux owns the PTYs, the scrollback,
and the attachment. Quitting Trigpoint kills nothing — every session outlives it.

## Status

Early. The map, its cursor, shell nodes, note cards, the attach handoff, and live previews
work; the rest of the spec does not exist yet.

| Milestone | | |
| --- | --- | --- |
| M0 | Skeleton — config, workspace store, `trig doctor` | done |
| M1 | Nodes on a map — create/kill shell nodes, cursor and node movement, attach handoff | in progress (rename still to come) |
| M2 | Live map — previews, peek, dead nodes, reconciliation, adoption | in progress (previews done) |
| M3 | Organisation — colours, tags, sizes, groups, filter, palette | to do |
| M4 | Agents — agent nodes, status badges, attention jump | to do |
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
```

A workspace is an independent map with its own nodes and its own default working directory.
Workspaces share nothing; the default one is called `main` until you configure otherwise.

## Keys

What is bound today:

| Key | Action |
| --- | --- |
| `h j k l` / arrows | Move the cursor to the nearest node in that direction |
| `3l`, `12j`, … | Count prefix — repeat the motion |
| `H J K L` | Move the selected node one cell, shoving whatever is in the way |
| `zz` | Centre the viewport on the cursor |
| `0` | Jump to the origin |
| `Enter` | Attach to the node under the cursor — the whole terminal, handed over. On a note, edit its body in `$EDITOR` |
| `n` | New shell node at the nearest free cell to the cursor |
| `N` | New note — a rendered markdown card with no session behind it |
| `x` | Kill the node under the cursor and its session (asks first; a note is just removed) |
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
out as themselves, wrapped to the card and capped at ten lines so one long note cannot cost
every other card its screen. See
[ADR 0004](docs/adr/0004-note-bodies-render-through-glamour.md), which also records what
rendering markdown costs the binary.

Movement never refuses. `L` into an occupied cell shoves the occupant one cell right, and
whatever is behind it too: one collision rule, which groups will reuse unchanged.

The cursor position and the viewport are saved with the workspace, so reopening Trigpoint
puts you back where you were looking.

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

Measured on tmux 3.7b: one capture costs ~1.3 ms, and a viewport of 40 nodes ~55 ms for the
whole batch, near enough regardless of preview length — the cost is running `tmux`, not the
lines it hands back.

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
status_stale_after_min = 10

[general.preview_lines]       # preview lines per card size
s = 0
m = 4
l = 10

[agents.claude]               # presets offered when creating an agent node
cmd = "claude"
```

`preview_debounce_ms`, `refresh_tick_s`, and `preview_lines` are live. Settings whose
features have not landed yet (card sizes, agent status) are read and stored, but nothing
consumes them — an unset size takes the `m` line count.

## Files

| Path | What |
| --- | --- |
| `~/.config/trig/config.toml` | Configuration |
| `~/.local/state/trig/workspaces/<name>.json` | One workspace: its nodes, groups, and viewport |

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
