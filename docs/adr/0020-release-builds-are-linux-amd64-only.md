# Release builds are linux/amd64 only

[ADR 0019](0019-v1-ships-linux-only.md) rejected a `darwin/arm64` artifact on the grounds
that shipping a binary nobody here can run means owning a platform nobody here can check. It
then kept `linux/arm64`, which is the same binary on the same argument. v0.1.0 shipped it,
and it had never been executed: the machine that cut the release has no aarch64 hardware and
no `binfmt_misc` registration, so `scripts/check-release.sh` could confirm it was a static
aarch64 ELF and nothing further. The release carried a promise its own verification could
not reach.

Release builds are `linux/amd64` and nothing else. `scripts/check-release.sh` loses its
platform argument along with the `binfmt_misc` caveat, because there is now exactly one
artifact and it is always run.

## Considered options

**Keep building `linux/arm64` and label it untested.** Rejected. A caveat on a download page
is not a substitute for running the thing, and SPEC §14 calls the failure this would hide
(residual raw-mode corruption after a handoff) release-blocking. The whole point of
`check-release.sh` is that "a downloaded binary works" is a claim somebody checked.

**Register `qemu-user-static` and run the aarch64 tarball under emulation.** Rejected for
now. It would make the artifact checkable, but qemu is not the machine a user has, and the
handoff is exactly the kind of terminal and signal behaviour where an emulator is the least
convincing witness. It is also work in service of a platform nobody has asked for.

**Cross-compile on demand rather than on release.** This is what happens now, and it needs
no configuration: `go build ./cmd/trig` with `GOARCH=arm64` still works, and anyone on an
arm64 machine can build there directly. What is gone is Trigpoint publishing the result
under its own name.

## Consequences

The README's requirements say x86-64, and its install section points anyone else at
`go install` or a clone. Nothing in `internal/*` changes; no Go code was ever
architecture-specific, and the port stays a build, not a rewrite.

Adding an architecture back is a release-engineering task with one gate. Add the platform to
`scripts/build-release.sh`, and run `scripts/check-release.sh` against the artifact on
hardware of that architecture. Until both are true, it does not go in a release.

v0.1.0's published `linux_arm64` tarball is left where it is. It is a real static binary and
deleting a published asset breaks whoever already has the URL; it simply has no successor.
