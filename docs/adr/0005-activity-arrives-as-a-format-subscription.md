# Activity arrives as a format subscription, not as `%output`

One long-lived control-mode client (`tmux -C attach -f ignore-size,no-output`) supplies the
events that mark a card's preview stale. It learns about activity by subscribing to a format
that loops over every session:

```
refresh-client -B "act::#{S:#{session_name}=#{window_activity} ,#{session_name}=#{window_activity} }"
```

tmux answers with `%subscription-changed` whenever that string changes, and diffing it
against the last one names exactly which sessions produced output. Session lifecycle arrives
separately as `%sessions-changed`.

The client attaches to whichever live `trig_` session it finds. It never creates one of its
own, and it never affects one: `ignore-size` keeps it out of the window-size calculation, and
`no-output` keeps every pane's bytes off the wire, because the preview is read with
`capture-pane` and the event only says *when* to read it.

## Considered options

**`%output`, the mechanism SPEC §5.3 names.** Rejected on measurement. tmux forwards
`%output` only for panes in windows linked to the control client's own session — a client
attached to `trig_main_aaa` never sees a byte from `trig_main_bbb` (verified, tmux 3.7b). The
spec's "one persistent control-mode client provides push events … activity" and `%output`
cannot both be true; the subscription is what makes the single-client half true.

**One control client per session.** Rejected: it is the scaling risk the milestone exists to
avoid (SPEC §14, risk 1), traded from capture cost into process count — dozens of nodes
becomes dozens of long-lived tmux clients and dozens of goroutines parsing them.

**Polling `list-sessions -F '#{window_activity}'` on a fast tick.** Rejected as the design,
kept as the fallback. It is what runs when there is no control client to be had, which is a
state the design expects rather than an error.

**A dedicated `trig_control` session to attach to.** Rejected: it buys a client whose life
does not depend on a node, at the cost of a permanent session in the user's `tmux ls` that
Trigpoint has to remember to clean up and that reconciliation has to learn to ignore.
Attaching to a real node's session instead makes the connection mortal — and the drop is not
a failure path bolted on afterwards, it is the ordinary consequence of killing that node.

## Consequences

`#{window_activity}` has one-second resolution, which is coarser than the 500 ms debounce it
feeds. A card can therefore be a debounce late rather than a debounce early — acceptable for
a snapshot, and the slow tick catches whatever the resolution loses.

Inside `#{S:…}` the format sees each session's *current* window. A node's session has one
window, so this is exact; an adopted session with several windows reports activity for the
window `capture-pane` would read anyway, which is the right answer for a preview.

The subscription needs tmux 3.2 (2021) or newer, which `doctor` already requires for control
mode and `capture-pane -e`, so it costs no new floor. Nothing enforces it at runtime: on an
older tmux the subscription is simply refused, the monitor never delivers activity, and
previews refresh on the slow tick.

The monitor counts as an attached client on whichever session it picked, so one node's
session shows as attached in `tmux ls` even with nobody in it. Accepted as the honest price
of not owning a session of our own — Trigpoint really is watching — but it means
`#{session_attached}` is not the question to ask about whether *the user* is inside a node.
Ask the clients themselves and skip the control-mode one, as `waitForClients` does.

It also means the detach key can take the monitor down with it, because the binding detaches
the session rather than the client that pressed it (ADR 0002) — so leaving a node the monitor
happened to be watching drops the connection. This costs nothing: returning from an attach
refreshes every card at once anyway, and the monitor is back within a backoff.

Reconnection lives in `internal/tmux`, not in the update loop: the monitor picks its own
session and backs off on its own clock, so the map holds no connection state and the model
stays drivable from a plain channel in tests.
