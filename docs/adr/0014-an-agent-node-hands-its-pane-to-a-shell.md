# An agent node hands its pane to a shell

An agent node's session is started on `sh -c '<cmd>; exec "${SHELL:-/bin/sh}"'` rather than
on `<cmd>` itself. The agent runs first and the shell is what it leaves behind.

tmux ends a session when the command it was given exits. An agent node is defined by having
been started as one, not by having a live agent (CONTEXT.md, "Agent node"), so a session
that died with `claude` would take the node's whole reason for existing with it: the card
would go dead the moment the agent finished, which is exactly when the user wants to look at
what it did. SPEC §6 says it in one line — "when the agent process exits, the shell remains".

Only agent nodes are wrapped. A shell node's optional initial command is given to tmux as it
stands, because a shell node promises a login shell and nothing about what happens after a
command it was handed.

## Considered options

**`set-option remain-on-exit on`.** Rejected: it keeps the pane, not a shell — what is left
is a dead pane with `[exited]` on it, which can be read but not typed into. Re-running the
agent is then a respawn-pane away rather than a command away, and the node's own respawn
already covers the case where the session is genuinely gone.

**Create the session on a login shell, then `send-keys` the command.** Rejected: it is two
tmux calls with a race between them, and the command would land in the shell's history and
on screen as though the user had typed it. The command is also then invisible to
reconciliation, which reads a session's environment and not its scrollback.

**Store the wrapped form in `Node.Cmd`.** Rejected: the node stores what it was asked to
run, and `sh -c '...'` is not that. Re-running an agent by hand, showing the command on a
card, and editing it later all want the command the user chose; wrapping is a detail of
starting a session, so it lives at the one seam that starts sessions (`startCmd`), which
both create and respawn go through.

## Consequences

The command reaches tmux as a single shell word, so it is quoted rather than interpolated:
a custom command line is typed by hand, and a quote in it must not be able to close the word
and make the rest into commands of its own.

`$SHELL` is read inside the session rather than by Trigpoint, so the shell left behind is
the one the user's environment names, falling back to `/bin/sh` where it names none.

A node whose agent has exited is alive, not dead: reconciliation finds a session, the card
keeps its `ag` label, and `Enter` attaches to the shell the agent left. Re-running the agent
is a matter of attaching and doing it.
