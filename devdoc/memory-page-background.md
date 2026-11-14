<!-- doc-class: historical -->
# The `gopi memory` page — background moved off the page

**Class: historical** (no current artifact cites this file, so the generator
classes it a past run's record; the banner above is the machine-checkable
half of the same claim). This is the standing background of the `gopi memory`
group page (`memoryUsage`, `cmd/agent/cli_memory.go`). Slice 3/5 of the "one
page = one level" epic (`01a10bb7`) moved the reasoning off the page and left a
pointer. Nothing was deleted — this file IS the moved prose, verbatim.

The page is the group index: it lists the SUBCOMMANDS the reader can type, one
synopsis each, and defers the semantics of each to that subcommand's own page
(`gopi memory sync -h`, `gopi memory backup -h`, `gopi memory memsync -h`,
`gopi memory brew -h`). Its surface is machine-read: the `gopi memory <verb> …`
synopsis heads are the input to `scripts/g3-memory-help-surface.sh`, which
generates `devdoc/gopi-memory-help-surface-inventory.md`, and
`TestMemoryHelpSurfaceMatchesInventory` re-derives the same list in Go so a verb
added to the help without a row fails the build.

## `sync` — the whole paragraph the page carried

Cross-node memory-asset sync (unique-only): ONE bidirectional session that
copies each side's unique `distilled_qa` rows to the other, so both nodes
converge on the superset. Exact-question identity; rows present on both sides
are never touched. `--every` turns that one-shot session into the PERIODIC leg:
a `--push` round toward every declared memory peer each cadence, until stopped —
quiet on a converged round, one line per state change on a failed one.
`--dry-run` reports what the push toward that peer would carry without
contacting it (the pull side is unknowable without a session; the whitelist
check still runs). `--serve-assets` is the PEER side of that leg — stdin carries
the initiator's export, stdout carries our reply, ssh'd by an initiator and
never run by a human.

Different command, same subject: `gopi memory memsync` is the patch-FILE
channel (an export/import patch you move by hand, plus the task and procedural
row channels) — it is a sibling, not a deprecated spelling. Pick by transport:
`gopi memory sync` for a live session, `gopi memory memsync` when the transfer
must be a file.

## `backup` — the peer side

`--serve` is the PEER side of the ship leg: stdin carries an envelope, stdout
answers a JSON receipt, ssh'd by an initiator and never run by a human.
RESTORING is documented, never performed here — the full page, including the
restore procedure, is `gopi memory backup -h`.

## `provenance --latent*` — the measurement behind the harvest

Latent provenance (Rule 16 follow-up): a row with EMPTY `sources` whose TOPIC
declares a file-shaped path that its own question/answer repeats. Measured: 100%
of such declared paths resolve to a real file, versus 63% of the RECORDED paths.
The signal is the same "the addressable field names the artifact" test a
human-written `sources` passes. Rows that merely MENTION a path are NOT
harvested — the rule refuses what it cannot defend. Writes only the additive
columns `source_type_latent`, `provenance` and `provenance_evidence`; `sources`
and `source_type` are never touched.

## `memsync` and `brew` — the routing detail

`gopi memory brew` is the producing half of the memory dimension; the transfer
half is sync/memsync. The same pipeline the flat `gopi brew` spelling runs —
`gopi memory brew -h` carries the flag set and the exit codes (0 ok / 1 usage /
2 partial).

`gopi memory memsync` is the patch-FILE channel: move knowledge rows node→node as
a reviewable JSON patch (the LIVE ssh sibling is `gopi memory sync`). The flat
`gopi memsync` spelling still works — the demotion moved the door, not the
command.

## Why the page is the shape it is

The rule this slice applies: **a help page lists what the reader can type, one
line each, and defers the reasoning to a document that is read on purpose.**
Every moved paragraph was either a measurement (the latent-provenance rates), a
decision record (why memsync is a sibling and not a deprecated spelling), or a
description of a subcommand's own flags — all of which the subcommand's own page
already carries, and none of which the group index needs to decide whether to
run something.

Measured on this slice (real binary, `./bin/gopi memory -h | wc -l`, at branch
tip `048ef7a2`): **150 lines before, 138 after** — the pointer paragraph below
the last synopsis is the block this file is named in. The before figure is the
same binary built at that commit without the four page edits (`git worktree add
--detach <path> HEAD` + build), so both numbers are real renders of the same
compiler, not estimates.
