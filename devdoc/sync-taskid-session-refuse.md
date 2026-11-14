<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** `cmd/agent/cli_tasks_sync.go` and the surface tests this page
> lists were DELETED with the cross-node sync family. The refusal semantics it argues for are kept
> here as the record of why; no spelling in this binary reaches another node's ledger.

# `task sync --task-id` on the session faces — the second half of the accepted-and-unread defect

**Branch**: `fix/sync-taskid-session-refuse` (worktree `~/workspace/gopi-sync-taskid-refuse`, off master `a7f47862`).

## The defect (measured 2026-09-21)

```
> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

$ GOPI_SYNC_PEERS=surface-peer GOPI_SYNC_NODE=me gopi task sync --peer surface-peer --task-id t-seed1
gopi task sync: peer surface-peer: memsync sync: peer "surface-peer" is not in the sync_peers whitelist …
exit=1
```

(In this hermetic worktree the peer is unreachable, so the whitelist/ssh leg is what
surfaces first; the defect is upstream of it and needs no peer to be seen — the parse
ACCEPTS the combination and `--task-id` never influences the session.)

Before the fix this invocation **ran a normal delta session and the flag decided nothing**
(the briefing's measurement against a reachable peer: exit 0, exit=1 only once the peer's
own row-count complaint fires): `--task-id` was
admitted by `taskFlagMember`'s Sync case, assigned to `opts.taskID`, and read by exactly
ONE branch — `runTaskSync`'s `--push-only` arm. On every session face (`--peer`, `--peers`,
`--full`, `--status`, `--serve-remote`, `--claim-remote`) the flag reached nobody, so the
session pushed its whole delta window and the named row was never singled out. The
operator's only feedback was the peer's own row-count complaint, which names the peer's
LIMIT and hides the sender's SELECTION — the same diagnostic inversion `b09afc6f` fixed
for the `--push-only` half.

Rule 15: an option is either obeyed or refused **by name** — never accepted and ignored.

## The decision (option (a): refuse by name)

The briefing offered two exits; the smaller honest one was taken, matching how
`--push-only --tags` is already refused and how the family's other combinations behave:

* **(a) refuse the combination by name at parse time** — CHOSEN.
* (b) define a per-row push side for a session — rejected: a session's export window is
  the delta window (`--tags` is its partition), a row selector has no place in it, and
  inventing the semantic would be new behaviour nobody asked for while leaving every
  other session face still silently ignoring the flag.

The refusal is emitted from the Sync parse case **before** the existing composition
checks, so the operator hears their actual mistake (the selector) rather than a downstream
symptom (a missing `--peer`, or `--full` composing with nothing):

```
gopi task sync: task sync: --task-id applies to --push-only only — --peer surface-peer is a
SESSION (its export window is the delta window, so the row would be silently ignored);
to push ONE row use `gopi task sync --push-only --task-id t-seed1`,
to sync the whole window drop the --task-id
```

(exit 1; the `gopi task sync: ` prefix and the family usage blob follow it on stderr.)

## What changed

* `cmd/agent/cli_tasks_sync.go`
  * `syncSessionFaceSpelling(opts)` — the resolver: reports **which** face the argv selected
    (as the spelling the operator typed) or `""` for `--push-only`, the one reading face.
    One switch, so a new session face is named there or it is not a face.
  * `syncTaskIDSessionRefusal(face, taskID)` — the named refusal (both flags, the row, the
    face, the re-runnable correct command).
  * Flag-name constants (`syncPushOnlyFlag`, `syncFullFlag`, `syncPeerFlag`,
    `syncPeersFlag`, `syncStatusFlag`, `syncServeRemoteFlag`, `syncClaimRemoteFlag`,
    `syncTagsFlag`, `syncFlagPrefix`) — Rule 1b/13: one spelling per
    value, referenced by the admission table, the parse case and the messages.
  * `syncUsageHead` — the `--push-only` entry now states that `--task-id` belongs to that
    face alone and that a session refuses it by name.
* `cmd/agent/cli_tasks.go`
  * `taskIDFlagName` (the row selector, referenced by submit's admission row AND the Sync
    face) and the refusal call in the Sync parse case.
  * The Sync admission row and parse assignments now reference the constants instead of
    repeating literals — the two-spellings-of-one-flag shape that let this defect exist.
* `cmd/agent/cli_tasks_sync_taskid_surface_test.go` (new) — the surface matrix (9 face
  spellings incl. the inline `--task-id=` and the selector-before-face order), the accept
  half (`--push-only --task-id T` still parses; sessions without the selector unaffected),
  the resolver's completeness, and a REAL-BINARY leg proving the shipped CLI refuses before
  any session runs.

## RED proof (the test is not a shape formality)

The enforcement block only was deleted (`git show`-style revert of the guard, helpers and
tests kept so the proof is a FAILURE and not a compile error):

```
--- FAIL: TestSyncTaskIDRefusedOnEverySessionFace (all 9 face subtests)
    `gopi task sync --peer surface-peer --task-id t-sync-taskid-row` parsed — the row
    selector is accepted and read by no session face (Rule 15)
--- FAIL: TestSyncTaskIDSessionRefusalRealBinary
    the refusal the operator sees is missing "task sync: --task-id applies to --push-only"
```

and the pre-fix binary, invoked for real, reproduced the original symptom (`exit=1` from
the peer-whitelist error — i.e. the argv was ACCEPTED and the session path was reached).
The fix was restored from a byte copy and rebuilt; the same tests pass.

## Not done / limits

* The `--push-only` single-row push itself is untouched (covered by
  `cli_tasks_sync_pushonly_rowfilter_test.go`).
* The refusal is a parse-time error, so it cannot be reached by any non-CLI caller — the
  serve tick and the claim-broadcast paths pass `nil` options, never a flag set.
* `internal/standards` has PRE-EXISTING reds on master (`Rule 16 NEW=1` at
  `internal/tools/worktree_sweep.go:434` — a file this change does not touch — plus the
  DOC-INDEX coverage/banner rows from in-flight sibling work). Measured identical on
  master in this run's worktree; none is caused by this branch.
