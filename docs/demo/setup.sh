#!/usr/bin/env bash
# The machine the tape records on: a config naming the demo agent, and a detach
# key vhs can type. Its Alt+ syntax takes a character, so the default M-Escape
# cannot be sent from a tape.
set -e
mkdir -p ~/.config/trig
cat > ~/.config/trig/config.toml <<'TOML'
[general]
detach_key = "M-q"

[agents.indexer]
cmd = "bash /vhs/agent.sh"
TOML
