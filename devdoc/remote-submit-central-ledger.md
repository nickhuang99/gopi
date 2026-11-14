<!-- doc-class: historical -->
# Remote submit → the authoritative ledger (ledger ADDRESS half)

**Epic:** remote-submit-central-ledger. **Branch:** `fix/remote-submit-ledger`
(worktree `~/workspace/gopi-remote-submit-ledger`), base `master` @ `5cdae25e`.
**Date:** 2026-09-18.

## The defect

`task_submit` issued on a REMOTE leg (HP / RPi5) wrote its row into that node's
PROCESS-LOCAL ledger and reported success:

```
Task queued: 01a0… (status=pending, source=human, tags=—)
```

but the node declares itself a **client** — its ledger is a MIRROR of the
central one on peerbox (`config` `sync_to`, resolved through
`config.ResolveNodeRole`). The row therefore existed only in the mirror: never
claimed, never notified, never verified. A black hole with a success message —
exactly the silent failure Rule 15b bans, and precisely what the epic's
reproduction showed.

## The changed call site

`internal/tools/tasks.go`, `TaskSubmit` — the single site that opened the ledger:

```go
// BEFORE (master 5cdae25e, tasks.go:4260 — the memoryDBPath() literal is on
// that line)
ctx := context.Background()
db, err := openTasksDB(ctx, memoryDBPath())

// AFTER
ctx := context.Background()
localDBPath := memoryDBPath()
fwdPeer, fwdErr := guardTaskSubmitLedgerAddress(localDBPath) // ← the ADDRESS decision
if fwdErr != nil {
    return "", fwdErr        // loud refusal; the ledger is NOT opened
}
if fwdPeer != "" {
    return taskSubmitRemote(opts, ctx, localDBPath, fwdPeer)
}
db, err := openTasksDB(ctx, localDBPath)   // unchanged server/single-machine arm
```

New module `internal/tools/task_submit_forward.go` (the address policy + the
forward), new arm `taskSubmitRemote` in `tasks.go`. Nothing else in the submit
engine changed: the server arm is byte-identical to the pre-change behaviour.

## The decision (Rule 2 / Rule 14 — one definition, one reader)

`taskSubmitNeedsForward()` reads `config.ResolveNodeRole()` — the ONE resolver
(env `GOPI_SYNC_TO` > settings.json `sync_to`) — never an inline host literal:

| resolved role | meaning | action |
|---|---|---|
| `server` | this node IS the authoritative ledger | local insert (unchanged) |
| `""` (undeclared) | single-machine default | local insert (unchanged) |
| anything else (e.g. `client`) | mirror leg — HP / RPi5 | **forward** to `sync_to`, or refuse |

The legacy hostname fallback of `IsServerNode` is deliberately NOT consulted: a
hostname that merely equals a `sync_peers` entry would silently turn a real
forward into a local write — the very defect this change removes.

## The forward, and why this verb

1. **Mint/verify the id** — a uuid v7 that collides with neither ledger
   (`taskSubmitMintForwardID`; the local ledger is sampled only if it already
   exists, never created just to sample).
2. **Forward** the row as a version-2 patch through the peer's SESSION verb
   (`ssh <peer> gopi task sync --serve-remote` → `SyncRemoteServe` →
   `SyncTaskImportDelta` → **insert** an unknown row).
   *Measured pitfall:* the CLAIM verb does not work here — its handler only
   inserts a TERMINAL ghost row (a lock verb must never conjure a pending row),
   so a pending submit pushed over it was accepted and stored nowhere.
3. **Confirm on the peer** — `ssh <peer> gopi task show <task_id>` must return
   the id. The success evidence is the row's PRESENCE, never the ssh leg's exit
   code (an empty envelope answering 0 is the failure mode being caught).
4. **Echo into the local mirror** — the same row bytes through
   `SyncTaskImportDelta`, tagged `forwarded-to:<peer>`, so `task show` answers
   on the remote node too. Refused echo rows are reported on stderr (the peer
   does carry the row, so it is a warning, not a refusal).

Every failure arm above renders through ONE format string
(`taskSubmitRefusalFmt`, Rule 14) naming: the authoritative target, the
would-be local path, the reason, and the remedy (`fix/verify the sync_to leg on
this node, or submit on the authoritative node itself`).

## Rule 15a/15b — no silent failure

* A forward that cannot be delivered or confirmed → **loud refusal**, and the
  local ledger is never opened, so no black-hole row can exist.
* A remote-leg node with role `client` but NO configured peer → **loud
  refusal** naming that combination (the case the seam-free repro pins).
* An unforwardable `parent_task_id` → **loud refusal**: the sync row carries no
  parent column, so the child would arrive on the authoritative ledger without
  its tree edge (Rule 15b — name it instead of forwarding a maimed row).
* An unforwardable explicit `--priority` → **loud refusal**, the SAME class
  found by auditing `SyncTaskRow`'s columns: the wire row carries no priority
  column and the peer's import re-derives the tier from the tags
  (`taskTagPriority`), so a non-tag-backed priority would be silently dropped
  on the very ledger that dispatches the row. The guard compares against the
  tag-derived tier, not against `> 0` — `TaskSubmit` already defaulted the
  field from the tags, so an equal value forwards (and the test pins that
  control arm alongside the refusal).
* `stats.RowsRefused > 0` on the local echo → warning on stderr naming both
  ledgers.

## Not Done (honest)

* The refusal is a plain error, not a retry/queue-on-this-node fallback: an
  offline mirror leg cannot park the row for later. The epic asked for
  "forward **or** fail loudly", so this is the accepted half — a spool-and-retry
  leg is a separate task.
* The forward carries the work definition only (summary/payload/tags/source/
  status/deliverable) — no `session_id`, `model`, `priority` or `reason`, since
  `SyncTaskRow` does not carry them (the peer derives priority from the tags).
  A parented row is refused rather than forwarded (above).
* Only `internal/tools` is touched; `cmd/agent` needed no change (the CLI and
  the tool share the engine), so the branch-scoped suite is `internal/tools`.

## Verification

**Red → green (the repro test).** Control worktree
`/home/user/workspace/gopi-prefix-control` (detached at `master` `5cdae25e`,
carrying ONLY the seam-free repro file) versus the branch. The two repro files
are byte-identical (`diff` → no output; `git hash-object` =
`57a3f048441569a572705573edba4a4a5142e83a` on both sides), so the ONLY variable
between red and green is the fix:

```
# pre-fix (control @ master 5cdae25e):
$ go test ./internal/tools -run TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently -count=1
    zz_remote_submit_repro_test.go:48: BLACK-HOLE: the submit reported success on a mirror leg
    with no authoritative peer: "Task queued: 01a0b4c6-55e1-7154-b726-1c3adca3b81e
    (status=pending, source=human, tags=—)" (row written to the node-local ledger
    …/TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently2484111040/001/mirror-memory.db)
--- FAIL: TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently (7.33s)
FAIL	github.com/nick/gopi/internal/tools	7.809s        RED_EXIT=1

# post-fix (branch, the SAME repro file):
$ go test ./internal/tools -run TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently -count=1
ok  	github.com/nick/gopi/internal/tools	1.538s         GREEN_EXIT=0

# the named address tests, post-fix:
$ go test ./internal/tools -run 'TestTaskSubmitRemote|TestTaskSubmitDeclaredServer|TestReproRemoteLeg' -count=1
ok  	github.com/nick/gopi/internal/tools	14.652s
```

The user decided to preserve the original phrasing of the messages. The Chinese version goes like this:

**Build / vet:**

```
$ go build ./...            → BUILD_OK
$ go vet ./internal/tools   → (no output)
$ gofmt -l <the four files> → (no output)
$ go test ./cmd/agent -run 'Submit|TaskVerb' -count=1 → ok 1.761s
```


**Tests** (`internal/tools`, the branch-scoped package):

| test | what it pins |
|---|---|
| `TestTaskSubmitRemoteLegWritesTheAuthoritativeLedger` | central ledger carries the row, mirror carries the `forwarded-to:<peer>` echo, confirmation names the peer |
| `TestTaskSubmitRemoteLegRefusalWritesNoRow` | undeliverable forward → `REFUSED` naming peer + local path + `sync_to`; **zero** rows in either ledger |
| `TestTaskSubmitRemoteLegRefusesUnforwardableParent` | parented remote submit → refusal, no row anywhere |
| `TestTaskSubmitDeclaredServerWritesLocally` | role `server` → local insert, **zero** peer dials |
| `TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently` | seam-free, uses only the public API + env: the pre-fix failure above |

The two-ledger scene is the real topology in-process: `openTasksDB` on two
files, `GOPI_MEMORY_DB` on the mirror, `role=client` + `sync_to=peerbox`, and the
transport seam runs the peer's REAL `SyncRemoteServe` against the central
ledger (never a mock of the merge).

## Evidence (self-filed)

* Build: `go build ./...` → `BUILD_OK` (t+813s, t+979s).
* `gofmt -l` on the four changed/added files → no output; `go vet ./internal/tools` → no output (t+979s).
* Red: the failure line quoted above, observed at t+895s in the control
  worktree (exit 1, `FAIL github.com/nick/gopi/internal/tools`).
* Green: `ok github.com/nick/gopi/internal/tools 10.072s` for the five named
  tests (t+979s).
* **Black-box, the real binary** (`CGO_ENABLED=0 go build -o <sb>/gopi ./cmd/agent/`,
  empty `settings.json`, throwaway ledger paths) — the three address arms:

  | env | observed | ledger created? |
  |---|---|---|
  | `GOPI_NODE_ROLE=client`, no `sync_to` | `task_submit: REFUSED — this row was NOT written to any local ledger. Authoritative ledger: NONE DECLARED (sync_to). Local ledger this process would have written (the node MIRROR): <sb>/memory.db. Reason: this node declares role=client (a mirror leg) but no authoritative peer is configured…` exit 1 | **no** |
  | `GOPI_NODE_ROLE=client`, `GOPI_SYNC_TO=no-such-host-smoke` | same refusal wording, `Reason: ssh no-such-host-smoke: exit status 255 (stderr: ssh: Could not resolve hostname …)` exit 1, 0.076s | **no** |
  | `GOPI_NODE_ROLE=server` | `Task queued: 01a0b4c3-27ff-7f2d-8b04-97f3ce2764d9 (status=pending, source=human, tags=—)` | yes (the local/authoritative arm) |

* Re-measured at close-out (same throwaway sandbox
  `/home/user/workspace/gopi-sbfwd-PRK7`, empty `settings.json`): arm 1 exit
  **1**, arm 2 exit **1**, arm 3 exit **0**; `ls $SB/memory.db` → `NO`,
  `ls $SB/memory2.db` → `NO`, `ls $SB/memory3.db` → the ledger (created). The
  two refusal arms name the local mirror path (`$SB/memory.db` /
  `$SB/memory2.db`) explicitly.
* Commits on `fix/remote-submit-ledger`: `85859202` (the fix), `d2d1ae8e`
  (repro pin + this devdoc), `8d98cb82` (black-box evidence), `227fc3ba`
  (the claim-verb wording correction), `5533ced2` (the confirmation composed
  from `syncRemoteGopi` + `taskverbs.Show`), `32478fe2` (this report's
  close-out evidence), `5cdbaad1` (Rule 17 env restore in the new test file).

**Repository gates (the branch's own standing gates, not just its tests):**

```
$ go test ./internal/standards -count=1     → ok  github.com/nick/gopi/internal/standards  45.174s
    (before 5cdbaad1 it was RED: Rule 17 reported 6 NEW os.Setenv sites in
     internal/tools/task_submit_forward_test.go — the two per-call ledger
     flips plus the scene's flip. Fixed to t.Setenv for the test-scoped flip
     and testutil.SnapshotEnv for the two per-call ones, with the two hook
     flips carrying the reviewed inline escape. The per-call pair is
     deliberately NOT t.Setenv: its undo scope is the test, so a per-call flip
     would leak the last value into every later assertion.)

$ go test ./internal/stylegate -count=1     → FAIL, but byte-identical to
    master's own failure: the reported site lists diff EMPTY (230 lines each
    side) and the only difference is "undecidable sites: 157 (master) vs 153
    (branch)" — i.e. the branch ADDS no finding and resolves four master
    could-not-decide sites. The gate is red on master (`5cdae25e`) for the
    same pre-existing reasons; this branch is not the cause and does not
    widen it.
```

**Who else goes through the new gate.** The gate sits INSIDE `TaskSubmit`, so
every internal auto-submit caller inherits it: `task_adoption`,
`verify_gate`'s rework/retry rows, `task_merge_backlog`, `agent/reframe`,
the LLM's `task_submit` tool. On a `server`/undeclared node this is a
no-op (the server arm is unchanged). On a `client` leg those rows now either
forward (they are payload-carrying rows, so nothing about them is
unforwardable) or refuse and are reported by their caller — `task_merge_backlog`
already collects per-branch submit failures into `counts.Failed`, and
`verify_gate` returns the error. No caller was changed, so no caller can
silently swallow the new refusal.

