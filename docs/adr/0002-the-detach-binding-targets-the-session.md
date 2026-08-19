# The detach binding targets the session and lives only for the attach

SPEC §5.1 says Trigpoint installs a tmux key binding that runs `detach-client`. Two details
it leaves open are settled here, because getting either wrong breaks the handoff in a way
that is only visible on someone else's machine.

**The binding is installed at the start of a handoff and removed on return**, rather than
once at startup. A tmux key binding is server-global — there is no way to scope one to a
session — so a binding that outlives the attach is a key taken away from every other tmux
session on that server, including the one Trigpoint itself may be sitting in, at a moment
when there is nothing to detach.

**The binding is `detach-client -s =<session>`, naming the node's session, not the client
that pressed the key.** With Trigpoint running inside tmux the outer client sees the key
first and consumes it; a plain `detach-client` would then detach *that* client and drop the
user out of their own tmux, leaving the nested attach still running in a pane they can no
longer see. Naming the session detaches the client attached to the node instead, which is
the right one whether the key was intercepted by an outer server, an inner one, or (not
nested) the only one there is.

## Considered options

**`switch-client` for the nested case.** SPEC offers it as an alternative. Rejected: it
returns immediately instead of blocking, so Trigpoint would keep the terminal it is supposed
to have handed over and would have no event telling it the user had come back. It also
splits the handoff into two code paths that fail differently, when one path covers both.

**A custom key table (`set -t <session> key-table trig`).** Rejected: a session whose key
table is not `root` no longer recognises the prefix key, so copy-mode and every other prefix
binding would stop working inside a node — the opposite of "identical to a plain tmux
attach".

**Refusing to run inside tmux.** Rejected by SPEC, and rightly: nesting is how anyone
already living in tmux would try Trigpoint first.

## Consequences

A key the user has bound themselves in the root table is taken for the duration of an
attach and dropped on the way back, rather than restored. The default `M-Escape` makes this
unlikely; reading the existing binding and putting it back is the fix if it ever bites.

Every client attached to the node's session is detached, not just Trigpoint's. Attaching to
the same session from a second terminal is rare, and detaching it too is defensible.

Because the binding is prepared before the terminal is released, a detach key tmux will not
accept is a failure Trigpoint can still report on the map. Attaching first and discovering
the problem afterwards would trap the user inside the session.
