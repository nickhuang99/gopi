<!-- doc-class: historical -->
# `task sync --push-only --task-id T` pushed the whole delta window — the flag was admitted and never assigned

**Status:** fixed on `fix/push-only-task-id-row-filter` (worktree off master `abf073b3`).
**Measured defect:** 2026-09-21, peerbox — 2181 rows left for one requested row; the peer's
`--claim-remote` answered `want 1..8 row(s), got 2181`.

## The defect, and why the obvious suspects were innocent

The ticket's own "where to look" list proposed three hypotheses, all of them downstream of
`SyncTaskPushOnly`: (a) `rowFilter` returns 1 row and something re-expands the patch, (b) the
exported `TaskID` representation differs from argv, (c) the full-export fallback ran
unfiltered. **None of them was true.** `rowFilter` (`internal/tools/memory_sync_claim.go`) is
correct, `patch.Rows = rows` happens before marshal, and the library's own tests were green —
which is exactly why they were green: the library was never handed a task id at all.

The defect is one missing ASSIGNMENT in the CLI parser:

```go
case taskverbs.Sync:
    ...
    opts.pushOnly = fs.has("push-only")
    opts.syncFull = fs.has("full")
    // ... and nothing read fs.get("task-id")
```

`taskFlagMember`'s Sync case **admits** `task-id` (`cli_tasks.go:1282`), so the flag parses,
passes the unknown-flag gate, and appears in every help page — while `opts.taskID` keeps its
zero value. `runTaskSync` then calls `tools.SyncTaskPushOnly("")`, and `""` has a
long-standing, documented meaning: *the whole current delta window*. The row filter never ran.

This is Rule 15's defect in its purest form: **an option accepted and ignored**. Every sibling
case in that switch assigns what it admits (`opts.peer = fs.get("peer")`, `opts.peersRaw =
fs.get("peers")`, …), which is why the omission in one case is invisible on review — the
pattern reads as complete.

### The reason it survived: nothing tested ASSIGNMENT

The help-text gates (`cli_tasks_advert_gate_test.go`, `cli_tasks_test.go`) assert the flag is
*advertised* and *admitted*; the library tests assert the filter is *correct*. Both halves were
green while the wire between them carried nothing. The regression test added here closes that
gap by asserting the VALUE the CLI computes for itself
(`opts.taskID != T` → fail) **and** the resulting bytes on the wire.

## The fix (surgical, one assignment)

`cmd/agent/cli_tasks.go`, Sync parse case:

```go
opts.taskID = fs.get("task-id")
```

A comment records the incident, the measured number, and the Rule 15 framing, so the next
reader of that switch knows why an assignment that looks redundant is asserted by a test.

## The second half: the refusal named the wrong side

With the flag ignored, the operator's evidence was
`claim-remote: want 1..8 row(s), got 2181` — a **peer-side row LIMIT** naming an error that is a
**sender-side row SELECTION**. That wording is what sent the operator hunting in the wrong
repository (the ticket's own text says so). The cap is a protocol safety property and was left
untouched (`claimLockMaxRows = 8`); what changed is which side explains itself:

- the peer still refuses an over-wide patch, in its own terms (unchanged);
- a genuinely-absent `--task-id` fails **on the sender** with
  `memsync push: task <id> not found in the ledger (window AND full export)` — no ssh leg
  leaves the process (asserted by test), and the message carries no row-count complaint;
- `syncUsageHead` now states the asymmetry instead of implying `--task-id` is an optional
  decoration: without it you are deliberately shipping the window, and the peer's claim-sized
  cap will (correctly) refuse it.

The first clause was already true in the library; it is now pinned by a test so a future
change cannot move the refusal peer-side, where it reads as someone else's bug.

## Red-then-green (the proof, not the claim)

`cmd/agent/cli_tasks_sync_pushonly_rowfilter_test.go` — three cases, all at the CLI seam, all
measuring **the bytes on the wire** (a PATH-shadowed `ssh` shim captures the patch it receives;
the test counts the rows in it — no peer ledger, no merge, is consulted).

| case | un-fixed (fix reverted in the working tree) | fixed |
|---|---|---|
| `--task-id T` over a 13-row ledger | FAIL — `opts.taskID = ""` (assignment assert), wire would be 13 rows | PASS — 1 row, peer accepted |
| `T` outside the peer patch, inside the full export | FAIL — fallback shipped **12 row(s)** | PASS — 1 row |
| unknown `--task-id` | FAIL — pushed 13 rows to the peer instead of refusing | PASS — sender-side not-found, no ssh leg |

The second case's fixture needed a correction worth recording: **the anchor window cannot be
simulated on this channel.** `syncAnchorMarginBackdate` is 5s, so an aged row is inside the
window again ~5s later — a timing race, and my first version failed its own precondition. The
*real* way a requested row is absent from the peer patch while present in the full export is the
**M4.24 anti-reflow gate**: a LIVE row tagged `from-<peer>` is excluded from the patch destined
for that peer, but the full export (no peer → the gate keys on this node) still carries it. That
fixture is deterministic and asserts both preconditions before the run. It is also the exact
case `rowFilter`'s full-export fallback was written for.

## Measured end-to-end acceptance (real binary, real peer handler)

`scripts/evidence-pushonly-taskid.sh` — a two-node harness on one host (GOPI_HOME isolation,
the repo's established sim), where the receiving side is the **real** `gopi … --claim-remote`
handler, so the 1..8 cap under test is the production check:

```
SENDER LEDGER: 13 rows (1 requested + 12 siblings — far above the peer's 1..8 cap)

=== ACCEPTANCE 1: --push-only --task-id t-proof-wanted (13-row ledger) ===
output: push gopi-proof-peer: accepted (1 row(s))                       → PASS

=== ACCEPTANCE 2: the whole window (what the bug shipped) vs the 1..8 cap ===
output: ... claim-remote: want 1..8 row(s), got 13                      → PASS (cap unchanged)

=== ACCEPTANCE 3: unknown --task-id refuses on the SENDER ===
output: gopi task sync: memsync push: task t-proof-absent not found in the ledger
        (window AND full export)                                        → PASS

PEER SIDE: task show t-proof-wanted → status: in_progress (the claim landed)
RESULT: 3/3 PASS
```

Note the peer-side `status: in_progress`: that is the claim verb actually being APPLIED on the
receiver, not merely accepted — the "1 row" is a real one-row claim broadcast.

## Pitfalls hit (recorded so the next reader does not repeat them)

1. **`--claim-remote` is not a carrier.** The claim channel applies to rows the peer ALREADY
   has; it has no insert path (a pending verb is the revert leg). A consequence worth stating:
   the manual push moves a row's *state*, not its *existence* — the acceptance harness must
   deliver the row by the full channel first. A row with no row on the peer answers
   `accepted` + `row-not-found` (uncontested) and changes nothing.
2. **Sender-side node role decides `task submit`.** A sandbox pinned `GOPI_NODE_ROLE=client`
   with no `sync_to` REFUSES to write locally (it forwards to the authoritative ledger instead) —
   so a fixture node must pin the role EMPTY (undeclared) to get a local ledger at all. My first
   harness run produced 0 rows for this reason and said so loudly, which is the rule working.
3. **A shim `ssh` must skip ssh's own options** (`-o BatchMode=yes -o ConnectTimeout=…`): the
   first version treated `-o` as the host and tried to run `-o` as a command.
4. **Do not build proof binaries under `/tmp`** — `/tmp` is on the `/` partition on this host
   (`/` = 66307, `/home` = 66309). The proof build lives under `~/workspace/` (Rule 22).

## Boundary: what is NOT fixed here (filed separately)

A push of a row the peer already has in `pending` answers
`t-…: no longer reclaimable (terminal — left as-is)` — for a row that is not terminal.
`syncClaimRemoteRevert`'s 0-rows-affected branch (`memory_sync_claim.go:623`) assumes the only
cause is a terminal/raced row, but an **already-pending** row also affects 0 rows. That is a
sender-visible misdirection of its own and it cost me a detour in this very run; it is filed as
its own bug (task `01a0c2ce-e9dd-7796-bfbb-afc4fb56567f`) rather than folded in here.

Also explicitly out of scope per the ticket: peerbox's `sync_peers: []` topology, and the
two-ledger divergence.

## Verification record

- Build: `CGO_ENABLED=0 go build -o /home/user/workspace/gopi-test-scratch/gopi-pushonly-evid ./cmd/agent/` → OK (exit 0)
- Tests (uncontended, one package at a time — see the trap below):
  - `go test ./internal/tools -count=1` → **ok 207.429s**
  - `go test ./cmd/agent -count=1` → 11 failures whose names are byte-for-byte the 11
    that fail on the UNMODIFIED base commit `abf073b3` (verified in a detached
    baseline worktree); none of the three new `TestPushOnlyTaskID*` cases is among
    them (they pass). So the package carries pre-existing reds; this diff adds none.
- **Trap measured while verifying (worth knowing): do not run `./cmd/agent` twice
  concurrently on this host.** A smoke run and a name-collection run overlapped and
  reported 24 failures; 13 of them (all child-spawn / session-dir tests:
  `TestTaskRun*`, `TestWorker*`, `TestBareRun*`, `TestRawPromptRun*`,
  `TestFrontRun*`, `TestCLIPositionalSubmit*`) **pass in isolation** and are absent
  from the uncontended run. They are contention flakes from the second concurrent
  suite, not regressions — an attributed to this diff they are not.
- Red proof: revert ONLY the two production files (`git checkout abf073b3 -- cmd/agent/cli_tasks.go
  cmd/agent/cli_tasks_sync.go` — the explicit pre-fix commit; `HEAD~N` is fragile here because
  this branch has three commits, two of which postdate the fix, and `HEAD~2` is the fix itself,
  the new test kept in the tree),
  cmd/agent/cli_tasks_sync.go`, the new test kept in the tree), re-run the three cases → 3/3 FAIL
  with the messages quoted in the table above; `git checkout HEAD -- <those two files>` → 3/3 PASS.
  (A `git stash`-based revert is NOT used here: `git stash`/`pop` is repo-wide and a *foreign*
  stash entry — `WIP on (no branch)`, another session's uncommitted chat work — was already
  present, so `pop` applied it and conflicted in `cmd/agent/cli_chat.go`. Reverting named files
  is scoped to this worktree's own paths and cannot touch shared stash state.)
- E2E: `scripts/evidence-pushonly-taskid.sh` → 3/3 PASS (quoted above)
