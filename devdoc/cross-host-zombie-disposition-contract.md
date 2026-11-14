<!-- doc-class: historical -->

> **RETIRED 2026-10-26 — this page describes machinery that NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left exactly where it is. Read this page as history, not as
> instructions: any command below that names `sweep-zombies` exits 1 with a pointer.
> The current behaviour is `devdoc/zombie-sweep-removed.md`.

# The cross-host zombie row: which arm disposes it, and to WHAT status

**Task**: `01a1317a-cd4e-7090-bf0d-54ee8a30fdfe` (bug-flow, `from-peerbox`) —
`TestTaskLifecycleTick_CancelsTheCrossHostZombieRow` fails at branch point
`4c3c9d047`: the lifecycle tick disposes the cross-host zombie row as `failed`,
the test asserts `cancelled`.

**Verdict of this run: the CODE is right and the FIXTURE was wrong.** The
literal claim stamp `"peerbox:3824874"` does not name the row's class: on this
host it makes the stamp **resolvable**, so a DIFFERENT arm owns the row and
writes `failed` — the arm the test is named for never sees it, because the
disposal leg is a chain of early-exiting arms and the first one wins. The
fixture now builds a **provably foreign** stamp at run time, and the disposition
it asserts (`cancelled`, from `UnownedZombieSweep`) is the one the arm under
test really produces.

## 1. The disposition contract (naming source: the code, not this document)

One leg — `autoDisposeConfirmedZombies` (`cmd/agent/cli_tasks_worker.go`) —
carries three arms, each with its OWN predicate, and each predicate measures
something DIFFERENT:

| disposition | deciding predicate (where) | what it measured | status | status-site (the write) |
|---|---|---|---|---|
| **confirmed** zombie | `zombieVerdict` → `claimHolder` → `pidAlive` (`internal/tools/taskzombie.go`) | the claim stamp is **resolvable here** (own host, fresh) AND that pid is **dead** | `failed` | `SweepZombies` → `TaskUpdateWithResult(id, taskStatusFailed, …)` — `internal/tools/taskzombie_sweep.go:263` |
| **suspected** zombie | same predicate, `zombieConfirmed` not reached | the stamp is **unknowable here** (a foreign host's pid is never called dead — `claimHolder`'s foreign arm) | *none* | never killed (the sweep skips it) |
| **unowned** zombie | `unownedZombieIsOrphan` (`internal/tools/taskzombie_unowned.go:132`) | the claim is **not attributable here** (`taskClaimAttributable==false`) AND no live holder (`claimPidLive`, `runnerLive`) AND no ground / run dir / session dir AND age ≥ `autoRunLitterMinAgeSec` | `cancelled` | `UnownedZombieSweep` → `taskUpdatePreservingUpdatedAt(id, taskStatusCancelled, …)` — `internal/tools/taskzombie_unowned.go:292` |

**Why the statuses differ, and why that is the contract and not an accident.**
`failed` says *the runner died mid-run and the queue may re-issue* — licensed by
a POSITIVE measurement (a pid this host can resolve, found dead). `cancelled`
says *abandoned* — licensed only by an ABSENCE set measured on one host, which
can never certify a delivery, so it may not claim `completed` either. Writing
`failed` from an absence argument would assert a death nobody measured;
writing `cancelled` from a resolved dead pid would throw away the re-issue
signal. The two names share the ROW CLASS ("a claim this host cannot verify");
they differ in EVIDENCE, and the evidence decides the status.

**The arms are disjoint by construction**: `unownedZombieIsOrphan` returns
`false` the moment `ClaimAttributable` is true (`taskzombie_unowned.go:146`),
i.e. any row whose stamp the pid layer CAN resolve — dead or alive — belongs to
`SweepZombies`. That is a required property, not a nicety: the leg runs
`SweepZombies` FIRST, so a row claimed by both arms would be disposed by the
earlier one on weaker evidence.

## 2. Root cause of the reported failure — measured, not inferred

**The fixture defect, not a code regression** — the root cause, measured:

The fixture's stamp was the hardcoded literal `"peerbox:3824874"`
(`lifecycleForeignClaim`) — the host and pid of one of the two rows the class was
originally measured on (`internal/tools/taskzombie_unowned.go:8`). Whether that
literal is foreign depends on the machine that runs the test, because
`claimHolder` decides with `os.Hostname()`.

Reproduced on this host (hosta, 2026-10-13) by making the process's own
hostname report `peerbox` — `unshare -u -r sh -c 'hostname peerbox; …'`, a one-off
experiment that changes nothing in the repo:

```
$ unshare -u -r sh -c 'hostname peerbox; cd <worktree> && go test ./cmd/agent/ -count=1 \
      -run TestTaskLifecycleTick_CancelsTheCrossHostZombieRow'
[task-notify] zombie sweep t-unowned-cross-host: KILLED (stalled, heartbeat stop + claimed pid 3824874 dead (peerbox) — confirmed zombie)
--- FAIL: TestTaskLifecycleTick_CancelsTheCrossHostZombieRow (0.18s)
    task_lifecycle_unowned_test.go:110: the cross-host zombie row is "failed" after the tick, want cancelled
        [worker] [zombie-dispose] t-unowned-cross-host disposed (confirmed, stalled) — heartbeat stop + claimed pid 3824874 dead (peerbox) — confirmed zombie
                    ↑ SweepZombies, NOT UnownedZombieSweep: the arm under test never saw the row
```

That is the verbatim failure string the bug report carries — mechanism
identified: with the hostname `peerbox`, `claimHolder("peerbox:3824874")` returns
`ok=true` (own host ✓, well-formed pid ✓, stamp fresh ✓), `pidAlive(3824874)` is
false, `zombieVerdict` → `zombieConfirmed`, and `SweepZombies` — the FIRST arm
of the leg — writes `failed`.

Without the hostname experiment the same literal is foreign, the row reaches
`UnownedZombieSweep`, and the test's own assertion (`cancelled`) holds — which
is why the failure is *host dependent*: on the machine named `peerbox` (the
server, the host the class was measured on) the fixture asserts the wrong arm;
on hosta it happens to assert the right one. A test whose subject depends on a
machine's nodename is the defect, and it is the fixture's, not the production
code's.

**The regression hypothesis is excluded.** At the branch point, with the
literal still foreign for the running process, the arm that owns the row is
`UnownedZombieSweep`, and it writes `cancelled`. The `failed` outcome requires
the OTHER arm to fire, and nothing in `4c3c9d047..HEAD` touches the producer's
`cmd/agent` disposition path (the diff carries the merger/standards work).

## 3. What changed (fixture only — no production behaviour)

`cmd/agent/task_lifecycle_unowned_test.go`:

- `lifecycleForeignClaim()` now **derives** the stamp at run time:
  `"not-" + os.Hostname() + ":3824874"` — provably foreign on EVERY host (a
  nodename would have to begin with `not-` to collide), and no hostname reads
  as `not-unnamed:<pid>`. The idiom already exists in this repo for the same
  reason: `foreignHostnameForZombieTest` (`internal/tools/taskzombie_unowned_test.go:314`).
- The **premise is asserted, not assumed**: after the tick, the test fails loud
  if the stamp resolves on this host, or if the tick's own line shows another
  arm's disposition (`disposed (confirmed`) instead of `cancelled`.
- The naming contract (the table in §1) is written into the file's comments with
  the status-site of each disposition, so the next reader sees WHICH arm owns a
  given shape without re-deriving it.

Production code is untouched; `internal/tools/taskzombie_unowned.go`'s
`taskClaimAttributable` gate (the arm-disjointness) is unchanged, and the
`test.name` is unchanged, so the acceptance's own command keeps working.

## 4. Verification (Rule 31 — the surface was executed)

| what | command (verbatim) | result |
|---|---|---|
| acceptance | `go test ./cmd/agent/ -count=1 -run TestTaskLifecycleTick_CancelsTheCrossHostZombieRow` | `ok github.com/nick/gopi/cmd/agent 1.935s` |
| root cause | `unshare -u -r sh -c 'hostname peerbox; … -run TestTaskLifecycleTick_CancelsTheCrossHostZombieRow'` | `FAIL … is "failed" … want cancelled` + `disposed (confirmed, stalled)` — SweepZombies owns the literal stamp |
| fix holds under the SAME hostname | same unshare command, after the fixture fix | `ok` — the derived stamp stays foreign on a host named `peerbox` |
| red proof (arm disabled) | `unownedZombieIsOrphan` forced to `return false`, then the acceptance command | `--- FAIL: …` (the row is never disposed) |
| restore (Rule 5 criterion) | `test "$(git hash-object internal/tools/taskzombie_unowned.go)" = "$(git rev-parse HEAD:internal/tools/taskzombie_unowned.go)"` | blob `dfd21bb6ce33079d759203ca2b22d5a7f4551279` on both sides — explicit-revision comparison, not an index-relative diff |

## 5. Not Done (honest)

- **The root cause was reproduced by an experiment on THIS host, not on the
  reporting host's own run.** `unshare -u -r` gives the test process the
  hostname `peerbox`, which is the mechanism; the reporting host's failure at
  `4c3c9d047` is taken from the bug payload's verbatim output, not re-run here
  (it needs a host literally named `peerbox` — `peerbox` is a separate machine and
  this run is confined to the hosta worktree).
- The `internal/tools` twin (`taskzombie_unowned_test.go`) was already
  host-independent (`foreignHostnameForZombieTest`); no change was needed there.
- The statuses themselves are NOT re-litigated here: `failed` for a confirmed
  local zombie and `cancelled` for an unowned row are the contract this run
  records, and nothing in this task's scope licenses changing either.

## 6. Rule 41 — a defect this run saw and did not touch

`make test-budget PKGS='./cmd/agent'` (the budget script widens the scope to
`./cmd/agent ./internal/docclass ./internal/standards`) reports the shared
`internal/standards` package **red on this worktree's own base**, from a change
this branch never made:

```
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource (0.47s)
    commands_doc_gate_test.go:189: COMMANDS.md's generated verb index disagrees with the source (44 verbs).
        first difference:
          got:  line 29: … `cmd/agent/cli_scratch_gc.go:614` …
          want: line 29: … `cmd/agent/cli_scratch_gc.go:625` …
--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock (0.47s)
    commands_doc_gate_test.go:691: precondition: the committed document's block is already stale
```

Both inputs were last written by OTHER branches' merges
(`git log --oneline -1 -- COMMANDS.md` → `2bede525e`; `-- cmd/agent/cli_scratch_gc.go`
→ `810b19f9c`), and `git diff 4c3c9d047 HEAD` touches neither file nor package —
so this is a defect of the integration branch, not of this task. Filed as a bug
row per Rule 41 with the raw stream and reproduction under
`~/.gopi/sessions/01a1317a-cd4e-7090-bf0d-54ee8a30fdfe/evidence/commands-doc-stale-at-master.md`:
task **`01a131af-8c32-76ac-a1fc-590bc5dd709a`** (tags `bug`, source `agent`).

## Verification

- Build: `go build ./...` → see the task report (`## Verification`).
- Tests: `make test-budget PKGS='./cmd/agent'` and `PKGS='./internal/tools'` →
  see the task report.
- Ran / Result: the table in §4.
