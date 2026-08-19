# Attach handoff: manual test matrix

The handoff (SPEC §5.1) is the one mechanism whose failure mode is a terminal left in raw
mode, and terminal emulators disagree about enough of it that a test suite cannot stand in
for running it by hand. SPEC §14 calls residual corruption release-blocking, so this matrix
is run before any release that touched `internal/tui/attach.go` or `tmux.CLI.Handoff`.

## What the suite already covers

`TestHandoffRoundTripOnARealTerminal` (`internal/tui/handoff_pty_test.go`) drives the whole
round trip — create a node, `Enter`, detach key, back to the map, quit — against a real pty
and a real tmux server, and compares the terminal's `termios` either side of it. That is the
release-blocking property, checked on every `go test`. It also covers the nesting case, the
detach binding's installation and removal, and a node whose session died before the attach
(`internal/tmux`, `internal/tui`).

What is left below is only what a pty cannot show: colours as rendered, reflow on a real
window resize, scrollback, and the emulator's own idea of what the alternate screen is.

## Per terminal

Run in each terminal listed in the table, both directly and with `trig` started from inside
a `tmux` session.

1. `trig`, `n`, name a node, `Enter`.
2. Inside the node: run `vim` (or `htop`), confirm it draws and responds; `:q`.
3. `printf '\e[38;5;208mtrue colour\e[0m\n'` — the text is orange, not literal escapes.
4. Resize the terminal window. The session reflows; no stripe of stale content.
5. Prefix `[`, scroll with the arrow keys, `q`. Copy-mode behaves as it does in plain tmux.
6. `M-Escape`. The map returns, redrawn, with the cursor still on the node.
7. Back on the map: `hjkl` move the cursor — proof that Trigpoint has the keyboard again.
8. `q`, then in the shell: `stty -a | grep -o 'icanon\|-icanon'` reports `icanon`, and
   typed characters echo. Scroll up: the scrollback from before `trig` is still there.
9. `tmux ls` still lists the node's session: quitting killed nothing.

Nested runs additionally check that step 6 returns to the map **and leaves the outer tmux
session attached** — the failure this guards against is the outer client detaching instead,
dropping you out of your own tmux.

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

Close this gap before M5 rather than before M1: the handoff code is the same on every
emulator, and what differs is exactly what only an emulator can show. Anyone with the
missing terminals to hand can run the steps above and add a row.
