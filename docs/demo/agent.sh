#!/usr/bin/env bash
# The agent the demo records. A real one reports through the same
# `trig emit-status` the contract documents; this one does it on a timer, so the
# badge changes while the tape is still rolling.
echo "indexer: starting"
sleep 3
trig emit-status running 'indexing the repository'
echo "indexer: 412 files"
sleep 7
trig emit-status needs_you 'two files conflict, which wins?'
echo "indexer: src/a.go and src/b.go both define Handler"
exec bash
