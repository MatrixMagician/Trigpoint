# Attach handoff: manual test matrix

The handoff (SPEC §5.1) is the one mechanism whose failure mode is a terminal left in raw
mode, and terminal emulators disagree about enough of it that no automated test can stand in
for running it. SPEC §14 calls residual corruption release-blocking, so this matrix is run
before any release that touched `internal/tui/attach.go` or `tmux.CLI.Handoff`.

What the automated suite already covers, and what this matrix does not repeat:

- the detach binding is installed for the attach and removed on return (`internal/tmux`)
- the binding detaches the node's session from inside a nested tmux (`internal/tmux`)
- the attach child is handed an environment with `TMUX` unset (`internal/tmux`)
- a node whose session died between render and attach is reported, not attached to
  (`internal/tui`)

## Per terminal

Run in each of **kitty**, **alacritty**, **wezterm**, **foot**, and **Terminal.app**, both
directly and with `trig` started from inside a `tmux` session (ten runs).

1. `trig`, `n`, name a node, `Enter`.
2. Inside the node: run `vim` (or `htop`), confirm it draws and responds; `:q`.
3. `printf '\e[38;5;208mtrue colour\e[0m\n'` — the text is orange, not literal escapes.
4. Resize the terminal window. The session reflows; no stripe of stale content.
5. Prefix `[`, scroll with the arrow keys, `q`. Copy-mode behaves as it does in plain tmux.
6. `M-Escape`. The map returns, redrawn, with the cursor still on the node.
7. Back on the map: `hjkl` move the cursor — proof that Trigpoint has the keyboard again.
8. `q`, then in the shell: `stty -a | grep -o 'icanon\|-icanon'` reports `icanon`, and
   typed characters echo. This is the raw-mode check.
9. `tmux ls` still lists the node's session: quitting killed nothing.

Nested runs additionally check that step 6 returns to the map **and leaves the outer tmux
session attached** — the failure this guards against is the outer client detaching instead,
dropping you out of your own tmux.

## Result

| Terminal | Direct | Inside tmux | Notes |
| --- | --- | --- | --- |
| kitty | | | |
| alacritty | | | |
| wezterm | | | |
| foot | | | |
| Terminal.app | | | |

Record a run by filling the table in the release PR rather than committing it here.
