# v1 ships Linux only, and the supported terminal list shrinks to what is run

SPEC §2.7 promised "Linux first, macOS supported", §12 put `darwin/arm64` in M5's release
builds, and §14 named kitty, alacritty, wezterm, foot and Terminal.app as the emulators the
attach handoff had to be checked against. Three of those five are now verified both directly
and nested (#27). The remaining two are dropped rather than carried as a gap:

**wezterm leaves the supported list.** It is not packaged for Fedora, and the Flatpak build
is a different thing to test than a native one. Nothing about the handoff is
wezterm-specific — it is a tmux client like any other — so what its absence costs is a data
point, not a capability.

**macOS is deferred, not abandoned.** Shipping a `darwin/arm64` binary means owning a
platform nobody here can run: Terminal.app's handoff unverified, `trig doctor` unrun on a
clean machine, and a release artifact whose first bug report would be its first test. v1 is
Linux-only, and the release builds `linux/amd64` and `linux/arm64`.

## Considered options

**Ship `darwin/arm64` untested.** Rejected. SPEC §14 calls residual raw-mode corruption
release-blocking, and the corruption a terminal leaves behind is the one failure a user
cannot recover from without knowing to type `stty sane`. Cross-compiling is easy; standing
behind the result is not.

**Keep macOS in the milestone and hold the release until hardware appears.** Rejected: it
blocks a Linux release that is otherwise ready on an acquisition nobody has scheduled.

**Run wezterm from Flathub and record it with a caveat.** Rejected: a sandboxed build tests
the sandbox as much as the emulator, and a row in the matrix that needs a footnote to read
is worse than an honest gap.

## Consequences

`internal/*` keeps its platform-neutral shape — nothing here is a licence to hard-code
Linux paths. `config.Path` resolving `~/.config` by hand rather than through
`os.UserConfigDir` (which is Application Support on macOS) stays as it is: SPEC §9.1 wants
that path on every platform, and it is what makes macOS a build-and-verify job later rather
than a port.

The handoff matrix's remaining gaps are the ones that were never platform-specific: no X11,
and no ssh session, where tmux's `escape-time` is most likely to make `M-Escape`
intermittent. Both are reachable on the hardware here and neither blocks the release.

Adding macOS back is a release-engineering task, not a design change: a `darwin/arm64`
target, `docs/handoff-test-matrix.md` steps 1–10 run by hand on Terminal.app (the harness
drives Wayland and will not help), and `trig doctor` on a clean machine.
