# The demo

[`trigpoint.gif`](trigpoint.gif) is recorded by [vhs](https://github.com/charmbracelet/vhs)
from [`demo.tape`](demo.tape), against a real `trig` and a real tmux inside a container.
Nothing in it is staged: the cards, the badge, and the handoff are the program running.

Re-record it with [`record.sh`](record.sh), which builds `trig` from the working tree, builds
the image, and runs the tape. It needs docker or podman and nothing else.

The tape sets `detach_key = "M-q"` rather than the default `M-Escape`, because vhs's `Alt+`
syntax takes a character and cannot send Escape. Everything else is the default
configuration.

[`agent.sh`](agent.sh) stands in for an agent. It reports through the same
`trig emit-status` an integrator would use, on a timer, so the badge changes from `running`
to `needs_you` while the tape is still rolling.
