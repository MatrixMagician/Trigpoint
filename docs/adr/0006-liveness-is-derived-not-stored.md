# Liveness is derived from tmux, never stored on the node

A `Node` has no `Alive` or `Dead` field, and the workspace file records none. Whether a node
is alive is worked out by asking tmux which sessions exist and comparing that against the
session names the map's nodes own — at startup, on every change to the session list, and on
the slow tick. The answer lives in the model for as long as the process does and is thrown
away with it.

Nothing else in the workspace file behaves this way: a node's title, colour, tags, and
position are all Trigpoint's to own. Liveness is not. tmux owns the sessions, and a machine
can reboot, a server can be killed, and a shell can be exited without Trigpoint running at
all — so a stored flag is stale from the moment it is written, and the file would confidently
disagree with the truth on every one of those paths.

## Considered options

**A `Dead bool` on `Node`, written when Trigpoint notices.** Rejected: it is a cache with no
invalidation. The common case — the machine was rebooted — is exactly the case where nothing
was running to update it, so the flag would be wrong precisely when the map is most relied on
to be right. It also invites the flag and the session to be checked in different places, which
is the first step towards them disagreeing.

**Ask tmux at render time.** Rejected: `View` runs on every keystroke, and a card's colour
would then cost a subprocess. Reconciliation asks once per pass and the model carries the
answer.

**Reconcile only at startup, as the issue's acceptance criteria describe.** Rejected as too
narrow. The premise — a map that tells the truth about what is still running — stops holding
the moment a node is killed from inside tmux, and the machinery is the same one command
either way. It now also runs on the `Sessions` event and on the slow tick, which is the
fallback SPEC §5.3 already asks for while the control-mode client is down.

## Consequences

A dead node keeps everything else it is. Deadness is a badge and a different answer to
`Enter` and to the `x` prompt; it changes nothing that is written to disk, so the map does not
rearrange itself after a reboot.

Nothing *acts* on the flag where tmux can be asked instead. `Enter` on a card believed dead
still calls `has-session` before deciding, and `x` still routes through `kill-session`, which
already treats an absent session as the outcome that was asked for. Short-circuiting either
on the cached flag would turn a false positive into real damage — a live session abandoned
with no card left to find it by — and the flag is a derived cache precisely because it can be
wrong. What the flag is allowed to decide on its own is what the card looks like and what the
prompts say.

A pass that could not reach tmux changes nothing. `list-sessions` exiting non-zero is only an
empty answer when tmux says there is no server; every other complaint is reported, because
reading a protocol mismatch or an unreadable socket as "no sessions" would mark every node on
the map dead. Passes also carry the number of corrections the map had made when they were
sent, so an answer that predates an attach finding a session gone, or a respawn bringing one
back, is dropped rather than applied.

Respawn keeps the node's id, and therefore its session name. Reconciliation matches sessions
to nodes by `trig_<workspace>_<nodeID>`, so a respawn that minted a new id would leave the
old card orphaned and the new session unrecognised.

Reconstruction lets the session name veto and the session environment decide. The name is
checked first and only ever says no: every session of a map's is called
`trig_<workspace>_<nodeID>`, so one that is not cannot be this map's whatever it claims — and
that keeps every other workspace's sessions off the subprocess path entirely, which matters
now that a pass runs on every session event the whole server produces. The environment is then
authoritative for the ones that get through, because `state.ValidName` permits `_` in a
workspace name: `trig_a_b_c` is ambiguous between node `c` of workspace `a_b` and node `b_c`
of workspace `a`, and `TRIG_WORKSPACE` settles it. The name is the fallback for the id when
the environment was cleared, and only for this workspace's own prefix — where the ambiguity
cannot bite, because the prefix is known.

A session that cannot be asked is left alone rather than guessed at. `show-environment`
failing usually means the session died between being listed and being asked about, and a card
reconstructed for it would be dead the moment it arrived, titled with a raw id, with nothing
but manual removal to clear it.
