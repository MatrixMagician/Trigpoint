# Trigpoint — SPEC.md

**A keyboard-driven terminal workspace map. nodeterm's spatial model, rebuilt as a TUI.**

Working title: **Trigpoint** (binary `trig`) — after an Ordnance Survey trig point: a fixed,
named reference mark on high ground you navigate terrain by. Clash-checked Aug 2026: no
existing dev tool, product, or company; nearest uses are an R data set of literal trig points
and a hobbyist trig-point-finder app. (Previous candidates rejected: Vantage → vantage.sh,
Cairn → multiple dev tools.)

Status: draft v1 · Language: Go · UI: Bubble Tea + Lip Gloss · Substrate: tmux

---

## 1. Overview

Stacked terminal tabs and windows hide context: you lose track of what is running where.
[nodeterm](https://github.com/eneskirca/nodeterm) solves this with a GUI canvas where every
shell is a draggable node. Trigpoint solves the same problem entirely inside the terminal:

- Every long-lived shell, agent session, or note is a **node** with a name, colour, tags,
  and a position on a **2D map**.
- The **map view** is a zoomed-out overview: a grid of node cards showing live output
  previews and status badges, navigated with the keyboard.
- **Entering** a node gives you the real, full-fidelity terminal (a tmux client), and one
  keystroke returns you to the map.
- Sessions **persist across restarts** of Trigpoint, your terminal emulator, and (with a
  remote host) your laptop — because every node is a tmux session.

Trigpoint is not a terminal multiplexer and not a terminal emulator. It is an
**orchestrator and map** over tmux. tmux owns PTYs, scrollback, and attachment; Trigpoint
owns naming, layout, grouping, status, discovery, and workflow.

## 2. Goals

1. Keyboard-first: every operation reachable without a mouse; mouse optional, never required.
2. Spatial persistence: a node's position, name, colour, and tags survive restarts; the map
   looks the same tomorrow as it does today.
3. Zero-fidelity-loss terminals: entering a node is indistinguishable from a plain
   `tmux attach` — TUI apps, colours, resize, copy-mode all just work.
4. First-class agent sessions: Claude Code / Codex / custom agent CLIs as a node kind, with
   live RUNNING / NEEDS YOU / DONE badges driven by hooks, not output scraping.
5. Workspaces: independent maps per project, each with its own default working directory.
6. Crash-only design: Trigpoint can be killed at any time; state is recovered from disk and
   from live tmux sessions on next launch.
7. Single static binary, Linux first, macOS supported.

## 3. Non-goals (v1)

- No embedded terminal emulation (no live interactive terminals rendered inside the TUI).
  Previews are text snapshots; interaction happens via attach handoff. (See §14 for v2.)
- No editor, diff viewer, web, or video nodes. `$EDITOR`, `git`, and the browser exist.
  A node can *launch* these; Trigpoint does not embed them.
- No built-in source-control panel.
- No SDK chat node — agents are CLI processes in terminals, full stop.
- No daemon. tmux is the daemon.
- No Windows (tmux dependency). WSL2 works implicitly.
- No coupling to any other tool or project — standalone binary, plain-text state.

## 4. Language and stack

**Go.** Rationale: mature TUI ecosystem (Bubble Tea, Lip Gloss, Bubbles), single-binary
distribution, and the v1 architecture never needs an in-process terminal emulator — which
is the main argument for Rust (ratatui + wezterm's `termwiz`/Zellij-style embedding). If
v2 pursues embedded panes, revisit; do not pre-pay that cost now.

| Concern | Choice |
| --- | --- |
| TUI framework | `github.com/charmbracelet/bubbletea` |
| Styling/layout | `github.com/charmbracelet/lipgloss` |
| Fuzzy matching (palette) | `github.com/sahilm/fuzzy` or equivalent |
| Config/state | TOML via `github.com/BurntSushi/toml`; state as JSON |
| tmux integration | shell-out to `tmux` CLI + one long-lived control-mode (`tmux -C`) client for events |
| File watching (agent status) | `github.com/fsnotify/fsnotify` |
| Testing | stdlib `testing`; `teatest` for TUI snapshot tests |

Minimum tmux version: 3.2 (control mode, `capture-pane -e`, session environment). Probe at
startup and fail with a clear message.

## 5. Core architecture

```
┌────────────────────────────────────────────────────────┐
│ trig (Bubble Tea TUI)                                   │
│                                                        │
│  Map view ── Palette ── Workspace switcher ── Help     │
│      │                                                 │
│  ┌───┴────────────┐   ┌──────────────┐  ┌───────────┐  │
│  │ Workspace store │   │ tmux service │  │ Status svc│  │
│  │ (JSON on disk)  │   │ (CLI + -C)   │  │ (fsnotify)│  │
│  └────────────────┘   └──────┬───────┘  └─────┬─────┘  │
└──────────────────────────────┼────────────────┼────────┘
                               │                │
                        tmux server      ~/.local/state/trig/status/*.json
                        (owns all PTYs)  (written by agent hooks)
```

### 5.1 The attach handoff

The defining mechanism. When the user enters a node:

1. Bubble Tea releases the terminal (`tea.ReleaseTerminal()` or programme exit into exec).
2. Trigpoint runs `tmux attach-session -t <session>` as a child process in the foreground,
   inheriting the TTY.
3. A tmux key binding installed by Trigpoint (default: `M-Esc`, configurable) runs
   `tmux detach-client`.
4. The child exits; Bubble Tea restores the terminal; the map re-renders with fresh previews.

Consequences: perfect terminal fidelity; zero emulation code; native tmux copy-mode,
mouse, and resize semantics inside a node. Only one node is interactive at a time — this
is accepted in v1 (see §14).

Edge case: if Trigpoint itself is running inside tmux, use `switch-client` semantics or
attach with `TMUX` unset in the child environment (`tmux -L` default socket, nested attach
is fine with `unset TMUX`). Detect and handle both cases; never refuse to run inside tmux.

### 5.2 tmux naming and ownership

- Every node maps to one tmux session named `trig_<workspace>_<nodeID>` where `nodeID` is a
  short random slug (e.g. `k4f2`). Human-readable names live in Trigpoint state, not tmux.
- Session environment carries provenance: `TRIG_WORKSPACE`, `TRIG_NODE_ID`, `TRIG_NODE_KIND`.
- Trigpoint never touches tmux sessions outside its prefix, except to offer **adoption**
  (§9.3).
- Killing a node = `tmux kill-session`. Closing Trigpoint kills nothing.

### 5.3 Events and previews

- One persistent control-mode client (`tmux -C attach`) provides push events:
  session created/closed/renamed, layout changes, activity. This drives dirty-flagging.
- Previews come from `tmux capture-pane -p -e -t <session> -S -<n>` — last N lines with
  ANSI colours, rendered into the node card. Refresh policy: on activity event with
  debounce (default 500 ms), plus on returning from attach, plus a slow global tick
  (default 5 s) as a fallback. All capture calls are batched per render tick.
- If the control-mode connection drops, fall back to polling `tmux list-sessions` on the
  slow tick and attempt reconnection with backoff.

## 6. Data model

```go
type Workspace struct {
    Name       string    // unique, filesystem-safe
    Dir        string    // default working directory for new nodes
    Nodes      []Node
    Groups     []Group
    Viewport   Viewport  // last cursor position / scroll offset
}

type Node struct {
    ID        string    // short slug, immutable
    Kind      Kind      // shell | agent | note
    Title     string
    Colour    string    // named accent colour
    Tags      []string
    Pos       Cell      // {Col, Row} on the map grid
    Size      CardSize  // S | M | L  (preview lines: 0 / 4 / 10)
    Cmd       string    // agent: command line; shell: optional initial command
    Dir       string    // working directory override
    Host      string    // "" = local; otherwise an ssh host alias (v1.1, §13)
    Note      string    // note kind: markdown body
    Pinned    bool
    CreatedAt time.Time
}

type Group struct {
    ID     string
    Title  string
    Colour string
    Rect   Rect   // cell-space rectangle drawn behind member nodes
}
```

- The map is an **infinite cell grid**, not free pixels: nodes occupy grid cells, groups
  are rectangles of cells. This makes keyboard navigation deterministic (hjkl moves
  cursor between occupied cells by nearest-in-direction) and layout trivially serialisable.
- **Groups are spatial; tags are logical.** A node is in a group iff its cell falls inside
  that group's `Rect` — there is no membership list. Moving a group moves the nodes inside it
  rigidly (member set snapshotted at gesture start, fixed relative offsets) and shoves
  non-members aside using the same collision rule node movement already uses, so a group can
  never drift over a bystander and absorb it. Moving a node out of a rect, or shrinking a rect
  past a node, removes it from the group — cards display their group alongside their tags, so
  this is visible rather than silent. To associate nodes that sit apart, use tags.
- **Node kinds:**
  - `shell` — plain login shell in `Dir`.
  - `agent` — shell that immediately runs `Cmd` (presets: `claude`, `codex`, custom).
    Carries status badge behaviour (§8). When the agent process exits, the shell remains.
  - `note` — no tmux session; a markdown card rendered on the map (glamour-style plain
    rendering; keep it simple). Editing opens `$EDITOR` on a temp file via the same
    release-terminal mechanism as attach.

## 7. UI specification

### 7.1 Map view (home screen)

- Grid of node cards; viewport scrolls to follow the cursor. Card anatomy:

```
╭─ ● api-server ─────╮
│ $ tail -f /var/log │
│ 200 GET /health 3… │
│ 200 GET /v2/items… │
╰─ sh · 2h ─ #infra ─╯
```

  Top border: status dot (§8), title. Body: preview lines (per `Size`). Bottom border:
  kind, session age, tags. Accent colour on the border. Dead nodes (state exists, tmux
  session gone) render dimmed with a `✝ dead` badge.

  Tags are on the bottom border and not beside the title: a card is 22 cells wide, which is
  not enough for both, and the kind and the age leave room where the title does not. The
  kind and the age never give way; the tags take what is left and are cut, or dropped, when
  that runs out. See
  [ADR 0009](docs/adr/0009-tags-live-on-the-bottom-border.md).

- Groups render as a background-tinted rectangle with a title in its top border, beneath
  member cards.
- A **status bar** (bottom): workspace name, node count, running/attention counts, current
  filter, pending-key indicator, and contextual hints.
- **Filter mode** (`/`): live-narrow cards by fuzzy match on title/tags/kind; `Esc` clears.

### 7.2 Palette (`Ctrl-K` and `:`)

Single fuzzy palette over: jump-to-node (any workspace), commands (every action below has
a palette entry), workspace switch, and adoption candidates. Palette is the discoverability
backstop — bindings are the fast path.

### 7.3 Keyboard model

Modal, vim-flavoured. No leader key for core motions; count prefixes supported (`3l`).

| Key | Action |
| --- | --- |
| `h j k l` / arrows | Move cursor between nodes (nearest in direction) |
| `H J K L` | **Move the selected node** one cell (auto-resolves collisions by shifting) |
| `Enter` | Attach to node (handoff §5.1) / edit note |
| `n` | New shell node at nearest free cell to cursor |
| `a` | New agent node (picker: claude / codex / custom) |
| `N` | New note |
| `r` | Rename node |
| `c` | Cycle colour · `C` colour picker |
| `t` | Edit tags |
| `s` | Cycle card size S→M→L |
| `x` | Kill node (confirm; `x` on dead node = remove card) |
| `g` | Group: create from selection (**gathers** members together first) / add to group under cursor (grows the rect if no cell is free) |
| `v` | Visual select (multi-node: move, group, tag, kill in bulk) |
| `V` | Hold the group under the cursor: `H J K L` move it rigidly, `h j k l` resize it, `x` deletes it (nodes survive), `Esc` lets go |
| `Space` | Peek: full-screen scrollable snapshot (`capture-pane -S -2000`) without attaching |
| `/` | Filter · `Ctrl-K` or `:` palette |
| `Tab` / `S-Tab` | Next / previous workspace · `w` workspace picker |
| `u` | Jump to next node needing attention (NEEDS YOU, then unread) |
| `zz` | Centre viewport on cursor · `0` jump to origin |
| `?` | Help overlay (generated from the live keymap) |
| `q` | Quit Trigpoint (sessions keep running; confirm only if `confirm_quit=true`) |

All bindings user-remappable in config; help overlay always reflects actual bindings.

## 8. Agent status (hook-driven)

Design principle inherited from nodeterm: **no output scraping**. Agents announce state.

- Status directory: `$XDG_STATE_HOME/trig/status/` (default `~/.local/state/trig/status/`).
- On agent-node creation, Trigpoint exports `TRIG_STATUS_FILE=<dir>/<nodeID>.json` into the
  session environment and (for known agents) ensures hook configuration:
  - **Claude Code**: a `trig init-hooks claude` subcommand installs/merges hook entries
    (e.g. on stop / notification / permission-request) that run `trig emit-status`, a tiny
    CLI mode that writes `{state, ts, detail}` to `TRIG_STATUS_FILE`. Installation is
    idempotent, explicit (never silent config mutation), and documented.
  - **Custom agents**: documented contract — write JSON `{"state": "running" | "needs_you"
    | "done" | "error", "detail": "..."}` to `$TRIG_STATUS_FILE`.
- Trigpoint watches the directory with fsnotify; badge states:
  - `●` green **running** · `●` amber **NEEDS YOU** (also triggers terminal bell,
    optional) · `●` grey idle/done · `●` red error · `○` unread activity (tmux activity
    event while not attached; cleared on attach or peek).
- Staleness: a `running` state older than `status_stale_after` (default 10 min) renders
  with a `?`. No inference beyond that.

## 9. Persistence, startup, reconciliation

### 9.1 Files

```
~/.config/trig/config.toml            # settings + keymap
~/.local/state/trig/workspaces/<name>.json   # one file per workspace, written atomically
~/.local/state/trig/status/<nodeID>.json
```

Atomic writes (temp file + rename) on every mutation; no save command exists.

### 9.2 Startup reconciliation

1. Load workspace files.
2. `tmux list-sessions -F ...` → live sessions with `trig_` prefix.
3. Node with live session → **alive**. Node without → **dead** (dimmed card; `Enter`
   offers respawn in `Dir`, re-running `Cmd` for agents). Session without node (state file
   lost) → reconstruct a card from session environment metadata.

### 9.3 Adoption

`A` / palette "Adopt session": lists non-`trig` tmux sessions; adopting renames nothing —
Trigpoint stores the foreign session name and maps it like any node (kind `shell`, tagged
`adopted`). This makes Trigpoint useful on day one with an existing tmux zoo.

## 10. Configuration (config.toml sketch)

```toml
[general]
default_workspace = "main"
preview_lines = { s = 0, m = 4, l = 10 }
preview_debounce_ms = 500
refresh_tick_s = 5
status_stale_after_min = 10
confirm_quit = false
detach_key = "M-Escape"          # binding installed into tmux for handoff return

[agents.claude]
cmd = "claude"
[agents.codex]
cmd = "codex"
[agents.custom.example]
cmd = "aider --model ..."

[keymap]
# action = "key"; every action in §7.3 remappable
```

## 11. CLI surface

```
trig                       # open TUI (default workspace)
trig -w <workspace>        # open TUI in workspace
trig new [-w ws] [-k kind] [-t title] [--cmd ...]   # create node headlessly
trig ls [-w ws]            # list nodes + states (plain / --json)
trig attach <node>         # skip the map, attach directly (fuzzy match on title)
trig emit-status ...       # hook helper (§8)
trig init-hooks claude     # install agent hooks
trig doctor                # check tmux version, control mode, status dir, config validity
```

Everything scriptable; the TUI is a client of the same internal services.

## 12. Milestones

- **M0 — Skeleton.** Bubble Tea app, config load, workspace store with atomic writes,
  `trig doctor`. Empty map renders; quit works.
- **M1 — Nodes on a map.** Create/rename/kill shell nodes; tmux session lifecycle; grid
  cursor movement; node movement with collision shift; attach handoff and return.
  *Exit criterion: daily-drivable for plain shells.*
- **M2 — Live map.** Control-mode event client; capture-pane previews with debounce;
  peek view; dead-node handling; startup reconciliation; adoption.
- **M3 — Organisation.** Colours, tags, sizes, groups, visual select, filter, palette,
  workspaces + switcher, help overlay from keymap.
- **M4 — Agents.** Agent node kind, status-file contract, fsnotify watcher, badges,
  `u` attention jump, `trig init-hooks claude`, bell notification.
- **M5 — Polish & release.** Full keymap remapping, `trig new/ls/attach` CLI, docs,
  goreleaser static builds (linux/amd64, linux/arm64, darwin/arm64), demo GIF (vhs).

Each milestone lands with tests and an updated README section, in the usual
milestone-by-milestone Claude Code flow.

## 13. v1.1 candidate: remote nodes

`Host` field on a node = SSH host alias. Sessions run under tmux **on the remote host**
(`ssh -t host tmux ...`); previews via `ssh host tmux capture-pane ...` on a longer tick;
attach handoff wraps the ssh command. ControlMaster strongly recommended and checked by
`doctor`. Deliberately out of v1 to keep M1–M5 honest.

## 14. Risks and open questions

1. **Preview cost at scale.** Dozens of nodes × capture-pane could add tmux-server load.
   Mitigation: capture only viewport-visible cards, debounce, batch per tick. Measure in M2.
2. **Attach handoff jank.** Terminal mode restore after child exit must be bulletproof
   across kitty/alacritty/wezterm/foot/Terminal.app and inside-tmux nesting. Dedicated
   M1 test matrix; treat any residual raw-mode corruption as release-blocking.
3. **Claude Code hook config drift.** Hook file formats change; `init-hooks` must merge,
   not overwrite, and `doctor` must validate. Keep the emit contract (JSON file) as the
   stable layer so hook plumbing can evolve independently.
4. **One-interactive-node-at-a-time.** The honest v1 limitation vs nodeterm. v2 options,
   in ascending cost: (a) split handoff — attach to a tmux session that itself tiles
   several trig sessions via `link-window`; (b) embedded emulation via a Go vt library for
   2–4 live tiles. Decide only after v1 usage data.
5. **Note nodes without tmux.** Slight model asymmetry (nodes that never have sessions).
   Accepted; keep the session layer nil-tolerant from M1 so notes don't special-case late.
6. ~~**No binding for moving a group.**~~ Resolved in M3: `V` holds the group under the
   cursor, and while one is held `H J K L` move it rigidly and `h j k l` resize it. See
   docs/adr/0013-a-group-is-held-with-V.md.
7. **Naming.** "Trigpoint" survived an initial web/GitHub clash check (Aug 2026); before M5,
   re-verify against Homebrew formulae, apt/Fedora package names, and pkg.go.dev, and check
   `trig` specifically as a binary name (Kiln → Fettle precedent).
