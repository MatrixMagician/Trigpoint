# Attach handoff: manual test matrix

The handoff (SPEC §5.1) is the one mechanism whose failure mode is a terminal left in raw
mode, and terminal emulators disagree about enough of it that a test suite cannot stand in
for running it by hand. SPEC §14 calls residual corruption release-blocking, so this matrix
is run before any release that touched `internal/tui/attach.go` or `tmux.CLI.Handoff`.

Most of what looks wrong on a first run is not. Read **Known and expected** before filing
anything, and use **Is it a Trigpoint bug?** to tell a terminal or window-manager problem
from a Trigpoint one.

## What the suite already covers

`TestHandoffRoundTripOnARealTerminal` (`internal/tui/handoff_pty_test.go`) drives the whole
round trip — create a node, `Enter`, detach key, back to the map, quit — against a real pty
and a real tmux server, and compares the terminal's `termios` either side of it. That is the
release-blocking property, checked on every `go test`. `internal/tmux` additionally attaches
from inside a tmux pane and presses the detach key at it, which is the nesting case, and
`internal/tui` covers a node whose session died before the attach.

What is left below is only what a pty cannot show: colours as rendered, reflow on a real
window resize, copy-mode, scrollback, and the emulator's own idea of the alternate screen.

## Before you start

Build from a clean checkout and check the machine:

```sh
go build -o trig ./cmd/trig
./trig doctor          # four lines, all "ok", exit 0
```

The runs below use your real config, state, and tmux server. To keep them out of the way:

```sh
export XDG_CONFIG_HOME=/tmp/trigtest XDG_STATE_HOME=/tmp/trigtest
```

Sessions still land on the default tmux socket either way; they are all named `trig_*`, and
quitting Trigpoint kills none of them. To clear up afterwards:

```sh
tmux ls | grep '^trig_' | cut -d: -f1 | xargs -r -n1 tmux kill-session -t
rm -rf /tmp/trigtest
```

Keep a **second terminal open** for the whole session. Several of the checks below have to
be run while Trigpoint has the first one.

## The runs

Four in total: Konsole and Ghostty, each of them twice — once started from a plain shell,
and once with `trig` started from inside a `tmux` session (`tmux new -s outer`, then run
`trig` in it). The nested runs are the ones most likely to find something.

## Steps

Each step says what to do, what a pass looks like, and what a failure looks like. Anything
that is neither is a "not sure" — take it to **Is it a Trigpoint bug?** below.

### 1. Start and create a node

Run `trig`, press `n`, type `probe`, press `Enter`.

- **Pass** — the alternate screen takes over, the status bar reads `main · 0 nodes` on the
  left and `⏎ attach · n new · x kill · q quit` on the right. After `Enter` a two-line card
  appears, `╭─ ● probe ───╮` over `╰─ sh · 0s ───╯` padded to a fixed width, and the count
  becomes `1 node`.
- **Fail** — the screen is blank or garbled, the card's box-drawing characters render as
  `?` or mojibake, or the status bar wraps onto a second line.

The card carries no output from the session. That is expected; see **Known and expected**.

### 2. Enter the node

Press `Enter`.

- **Pass** — the map disappears and you are at a shell prompt inside the node, with tmux's
  own status bar along the bottom reading something like `[trig_main_ab12] 0:bash*`.
- **Fail** — nothing happens, or the map stays on screen, or an error appears in
  Trigpoint's status bar instead.

Check from your **second terminal** that the handoff really happened:

```sh
tmux list-sessions -F '#{session_name} attached=#{session_attached}'   # trig_main_… attached=1
tmux list-keys -T root | grep -i escape                               # the detach binding
```

The binding line should read `bind-key -T root M-Escape detach-client -s =trig_main_…`. It
exists only while you are attached — that is deliberate, see
[ADR 0002](adr/0002-the-detach-binding-targets-the-session.md).

### 3. A full-screen application

Inside the node run `vim` (or `htop`), move around, then quit it (`:q`).

- **Pass** — it draws and responds exactly as it would if you had run it in this terminal
  directly, and leaves the shell prompt behind when it quits.
- **Fail** — corrupt drawing, keys landing in the wrong place, or the shell prompt not
  coming back.

### 4. Colour

```sh
printf '\033[38;5;208m256-colour orange\033[0m\n'
printf '\033[1;4;31mbold underlined red\033[0m\n'
```

- **Pass** — orange text, then bold underlined red text.
- **Fail** — the escape sequences printed literally, or everything in one colour.

### 5. Resize

Drag the window to a different size, then back.

- **Pass** — the session reflows to the new size; the shell prompt and any output stay
  legible.
- **Fail** — a stripe of stale content down one side, or content clipped and never redrawn.

Note whether the **map** comes back at the new size in step 7; that is the part specific to
Trigpoint.

### 6. Copy-mode

Run `seq 1 200`, then press your tmux prefix (`Ctrl-b` by default) followed by `[`. Scroll
with the arrow keys or `PageUp`, then press `q`.

- **Pass** — copy-mode enters, scrolls through the numbers, and exits. Identical to plain
  tmux.
- **Fail** — the prefix does nothing, copy-mode will not scroll, or `q` does not leave it.

If the *prefix* does nothing, that is worth reporting: it would mean the handoff has changed
tmux's key handling, which it is not supposed to touch.

### 7. The detach key

Press **Alt+Escape**.

- **Pass** — the map comes back, redrawn, with the cursor still on `probe`, and the card
  showing a larger age (`sh · 2m`). Your keyboard is Trigpoint's again.
- **Fail** — nothing happens, or you are dropped out of tmux entirely, or the map comes
  back visibly broken.

If nothing happens, **do not file it yet** — Alt+Escape not reaching tmux is the single most
likely false alarm on this list. Go to **Is it a Trigpoint bug?**, "the detach key does
nothing".

On a nested run, also check from the second terminal that your **outer** session is still
attached:

```sh
tmux list-sessions -F '#{session_name} attached=#{session_attached}'
```

`outer` must still read `attached=1`. If the outer session detached instead of the inner
one, that is a real bug and the exact failure ADR 0002 exists to prevent — file it.

### 8. The map has the keyboard back

Press `h`, `j`, `k`, `l`, then `zz`.

- **Pass** — with one node there is nowhere to move, so the cursor stays put and nothing
  breaks; `zz` recentres. The point is that keys are being read at all.
- **Fail** — keys are echoed as characters on screen, or nothing responds.

Press `Enter` again and detach again. Twice through the handoff is worth more than once.

### 9. Quit, and the state of the terminal

Press `q`. Then, in the same terminal:

```sh
stty -a | tr ' ;' '\n\n' | grep -Ex '\-?(icanon|echo)'
```

- **Pass** — prints `icanon` and `echo`, neither with a leading `-`. Typed characters echo,
  Ctrl-C works, the prompt behaves.
- **Fail** — either shows a leading `-`. **This is the release-blocking one.** Recover with
  `stty sane` or `reset`.

Then scroll up.

- **Pass** — the scrollback from before you started `trig` is still there. One line reading
  `[detached (from session trig_main_…)]` is expected.
- **Fail** — the earlier scrollback is gone, or the screen is full of escape-sequence
  debris.

### 10. Sessions outlive Trigpoint

```sh
tmux ls
```

- **Pass** — `trig_main_…` is still listed. Quitting Trigpoint kills nothing (SPEC §5.2).
- **Fail** — it is gone.

## Known and expected — not bugs

These all look like defects on a first run and are not. Do not file them.

**The card shows no output from the session.** A card in M1 is a border, a title, the kind
and the age. Previews (`capture-pane` snapshots) are M2 — see #7.

**A node whose shell you exited still draws as a live card.** Type `exit` inside a node and
the session ends, but the card looks exactly as it did. Pressing `Enter` on it reports
`probe has no live session to attach to` rather than attaching. Dimming dead cards, and
offering to respawn them, is M2 (SPEC §9.2). Confirm with `tmux ls`: the session really is
gone, and Trigpoint really did notice — it just has no way to say so on the card yet.

**Quitting leaves tmux sessions running.** That is the design, not a leak.

**Alt+Escape does nothing while you are on the map.** The binding is installed when the
handoff starts and taken back when it ends, so between attaches it does not exist. ADR 0002
explains why: a tmux key binding is server-global, and one that outlived the attach would
take the key away from every other session on the server.

**Nested: two status bars during the attach.** The outer tmux's status line stays where it
is and the node's appears above it. That is what nesting looks like.

**A `[detached (from session trig_main_…)]` line in the scrollback after quitting.** tmux
prints that itself on the way out.

**The map repaints all at once on return, rather than filling in.** Bubble Tea repaints the
whole screen when it takes the terminal back.

**An unnamed node is called after its id.** Pressing `Enter` at an empty title prompt names
the node `ab12` or similar.

## Is it a Trigpoint bug?

### The detach key does nothing

Work down this list; stop at the first one that fails.

**1. Does the terminal send the key at all?** In a plain shell, run `cat -v`, press
Alt+Escape, press Enter, then Ctrl-D.

- `^[^[` — the terminal is sending what tmux calls `M-Escape`. Go to 2.
- `^[` alone, or nothing, or something like `^[[27;3u` — the terminal is not. **Not a
  Trigpoint bug.** Either turn on the terminal's "Alt sends Escape" behaviour, or pick a
  different key (see below).

**2. Does tmux read it as `M-Escape`?** This tests the terminal and tmux with Trigpoint out
of the picture entirely:

```sh
tmux -L keytest new-session -d -s probe
tmux -L keytest bind-key -n M-Escape display-message "the detach key reached tmux"
tmux -L keytest attach -t probe        # press Alt+Escape; the message should appear
# then: tmux -L keytest kill-server
```

- The message appears — tmux sees the key. Go to 3.
- It does not — **not a Trigpoint bug.** If the key is intermittent, try
  `tmux set -sg escape-time 50`: tmux tells a lone Escape from an Alt-prefixed one by
  timing, and 10ms (the default here) is tight over ssh or a loaded machine.

**3. Was the binding installed?** Attach from Trigpoint, and from the second terminal:

```sh
tmux list-keys -T root | grep -i escape
```

- `bind-key -T root M-Escape detach-client -s =trig_main_…` is there, but the key still does
  nothing while attached — **that is a Trigpoint bug**, file it.
- Nothing is there — **that is a Trigpoint bug**, file it. The binding should exist for
  exactly as long as the attach does.

### Choosing a different detach key

Any tmux key name works — `M-q`, `C-Space`, `F12`. Put it in
`$XDG_CONFIG_HOME/trig/config.toml` (or `~/.config/trig/config.toml`):

```toml
[general]
detach_key = "M-q"
```

Trigpoint refuses to attach at all if tmux will not accept the key, and says so on the map
rather than handing over a terminal you cannot get back — a useful check in itself: set
`detach_key = "M-Nonsense"`, press `Enter`, and the map should stay where it is with
`tmux: unknown key: M-Nonsense` in the status bar and nothing attached.

### Anything else

Three questions separate a Trigpoint bug from the layers underneath it:

- **Does plain tmux do it too?** Run `tmux new -s bare` in the same terminal and repeat the
  step. If plain tmux behaves the same way, it is not Trigpoint — the handoff is a real tmux
  client, so it inherits tmux's behaviour exactly.
- **Does it happen in the other terminal?** A fault in one emulator and not the other is
  worth recording either way, but points at the emulator.
- **Does `go test ./...` still pass?** The pty test covers the round trip and the terminal
  restore. Green there plus broken by hand means the difference is something only a real
  emulator does — which is exactly what this matrix is for, and worth filing.

### What to record

On #27, or a new issue if it is a defect rather than a gap:

- terminal and version (`konsole --version`, `ghostty +version`), and whether nested
- `tmux -V`, and `echo $TERM` from inside the node
- the step number above, what you expected, what happened
- output of `tmux list-sessions -F '#{session_name} attached=#{session_attached}'` and
  `tmux list-keys -T root | grep -i escape`, taken **while attached**
- for anything to do with keys, the `cat -v` output from triage step 1
- for raw-mode corruption, the `stty -a` output before running `stty sane`

## Result

| Terminal | Direct | Inside tmux | Notes |
| --- | --- | --- | --- |
| Konsole (Wayland) | | | |
| Ghostty (Wayland) | | | |

Record a run by filling the table in the release PR rather than committing it here.

## Not yet covered

SPEC §14 names kitty, alacritty, wezterm, foot, and Terminal.app. None has been run: the
development machine has Konsole and Ghostty on Linux/Wayland only, and Terminal.app needs
macOS, which M5 targets as a release platform (`darwin/arm64`). Konsole covers the Qt/KDE
family and Ghostty the GPU-accelerated one, so the two are not redundant, but they are not
the whole list either.

Tracked as #27, which blocks the release issue (#21). The handoff code is the same on every
emulator, and what differs is exactly what only an emulator can show.
