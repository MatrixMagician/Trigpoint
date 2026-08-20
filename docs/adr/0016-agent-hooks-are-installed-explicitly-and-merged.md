# Agent hooks are installed explicitly, and merged

`trig init-hooks claude` is a command the user runs. Nothing else in Trigpoint writes to an
agent's configuration, and creating an agent node does not install anything.

SPEC §8 says node creation "ensures hook configuration" for known agents. It does not, and
this records why.

## Explicit, because the file is not ours

`~/.claude/settings.json` belongs to Claude Code and to the user, and it is read by every
Claude Code session on the machine — not only by the ones running inside a node. A tool that
silently edits it the first time you press `a` is one you stop leaving alone with it, and the
edit would arrive with no output, at the moment your attention is on the agent you were
starting rather than on your settings.

The cost is one command, run once, and `trig doctor` says when it has not been.

## Merged, never written over

The command reads the file, adds only the entries that are not already there, and writes
everything else back untouched — other people's hooks on the same events included. An entry
counts as installed when *some* command on its event invokes `trig emit-status` with its
state, so a user who has wrapped the emit in their own shell keeps their wrapping and does not
get a second copy stapled underneath.

A settings file that does not parse is refused rather than replaced. Trigpoint cannot merge
into a file it has misread, and the failure mode of guessing is the user's configuration
silently rewritten into what Trigpoint imagined.

## The entries are guarded

Each installed command is `[ -z "$TRIG_STATUS_FILE" ] || trig emit-status <state>`.

The hooks fire in every Claude Code session, and outside a node there is no status file to
write to. Unguarded, `trig emit-status` would exit non-zero on every prompt in every Claude
Code session the user has anywhere — an error shown to a user who has done nothing wrong. The
guard makes the hook a silent no-op where it does not apply, without swallowing a genuine
failure where it does.

Three events, and no more: `UserPromptSubmit` is running, `Notification` is needs-you, `Stop`
is done. There is no hook for `error` — a hook that fires is a hook whose session is still
working, so a report of `error` has to come from the agent itself.

`Notification` carries a second clause, because Claude Code fires it for two different things:
a permission request, which is needs-you, and the prompt having sat idle for a minute, which
is not. Unfiltered, every node that reported `done` would turn amber a minute later, ring the
bell, and pull `u` to itself — and grey would be a state no card could ever be seen in. So the
entry reads the notification's own message off stdin and stays quiet for the idle one.

Matching Claude Code's wording is the brittle part of this, and it is brittle on purpose: the
worst a change to that wording can do is let an idle notification through as needs-you, which
is where this started rather than a hook that has stopped working.

## One package knows the format

Hook formats change with every agent release; the status file format
([ADR 0015](0015-agent-status-is-a-directory-of-files-trigpoint-polls.md)) does not. So
`internal/hooks` is the only package that knows what a settings file looks like, `internal/tui`
cannot import it, and a test asserts that it cannot — which is the same fact as node creation
never installing anything, expressed where it can be checked.

## Consequences

Drift is a `trig doctor` failure rather than a badge that quietly stops moving. Nothing
installed is not drift and passes: a user who never runs a Claude Code node never needs the
hooks. Some of them installed is, because that is a card that reports `running` and then never
reports again.

Adding a second known agent is a second entry table in this package and a second word the
command accepts. Every other agent integrates through the file format, which needs no command
and no knowledge of Trigpoint at all.
