<!-- doc-class: historical -->

> **NOTE 2026-10-26 — the verb this page is about NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left where it is. The defect analysis below is kept as history
> (the Rule 15 lesson is still live in other verbs), but no command here can be re-run.
> Current behaviour: `devdoc/zombie-sweep-removed.md`.

# Devdoc: the merge-verify gate's baseline budget (why merged states were reverted)

**Branch:** `fix/rawlog-test-home-leak` (rework of the reverted merge of the same
branch) · **Commit:** `6b257abe` · **Task:** 01a097c7-5672-7176-bdd4-aae932125c62

## 1. What was reverted, and what actually failed

`fix/rawlog-test-home-leak` (delivered by task 01a095af, "worker should write its
log in task directory") was merged into master **twice** and reverted **twice**.
The audit trail in `~/.gopi/logs/merger.log`:

| cycle | merge | gate reason in the log |
|---|---|---|
| 1 (01:30) | `17ac5731` | ledger event on the task row: the verify child judged `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` FAILED |
| 2 (06:38) | `22f911db` | `merge-verify: verify child for 01a095af-eae2-… exited signal: terminated` — **killed, no verdict** |

Both cycles were preceded by a `merge-verify PASS` on an unrelated branch, so the
gate was working; and no `judging without a baseline` line exists for either
pass, so cycle 2's revert is the *unjudged* shape: the merger's bounded wait
expired and the gate treated "unverified" as FAIL (the same class as the
2026-09-12 `diag/worker-writeback-gap` incident, merger.log:3754).

Cycle 1's red is **obsolete on today's master**: master fixed exactly that
assertion the same morning (`f72b1b24`, 04:42, "fix(worker): the round's own
record reaches the launcher on the --once face" — `workerRoundOut` /
`workerRoundAnnounce` / `taskWorkdirMovePrefix`), because `workerAttachOutputLog`
re-points `os.Stdout` into `<session-dir>/output.log` at claim time, so the
round's workdir line never reaches the launcher stream the old test read.

**So: the branch was never the problem.** On a scratch worktree at
`master + branch`, both tests the fix delivers PASS with `bin/gopi` built first
(without it, `testutil.GopiBin` SKIPS — a false green).

## 2. The structural defect the rework does fix: the baseline budget

The gate owns the **delta** it produced, not the tree's history: it sweeps the
pre-merge state (`beforeTip`, master alone) with the same command, hands the
child that failing set, and the child passes iff the merge adds no failing
package (`internal/tools/verify_gate.go`, `MergeVerifyBaselineFailures`).

The sweep is bounded by `tools.MergeVerifyBaselineRunTimeout` — **20m** — while
the *child* cap is 45m. The baseline runs the **same whole suite** as the child,
**plus the declared build**, and runs **first**. On this repo it does not fit in
20m, so the gate drops the baseline ("judging without a baseline", best-effort by
design) and the child judges the merged state **absolutely** — master's own
pre-existing reds become the branch's verdict, and no honest merge of a
bugfixing branch can PASS. The asymmetry was never pinned by a test: only the
*child* cap had a guard (`merger_verify_waitcap_test.go`, from the 2026-09-12
incident).

### Fix

| constant | before | after | why |
|---|---|---|---|
| `tools.MergeVerifyBaselineRunTimeout` | 20m | **50m** | the sweep runs the same suite + build, before the child |
| `tools.MergerWaitBoundDefault` | 60m | **120m** | one gated merge now costs baseline (50m) + child (45m) = 95m; a graceful `--stop` must clear their sum |
| `GOPI_MERGE_VERIFY_BASELINE_TIMEOUT_SEC` | — | new | raise-only override, ONE reader (`mergeVerifyBaselineWait`, Rule 2); a bad value falls back to the default, never shortens it |

Pinned by `cmd/agent/merger_verify_waitcap_test.go`:
`TestMergeVerifyBaselineCapExceedsTheChildCap` (baseline cap > child cap),
`TestMergeVerifyBaselineCapIsRaiseOnly` (the knob), and the extended
`TestMergerStopBoundExceedsEveryChildCap` (bound > child cap + baseline cap).

## 3. A/B: the merged tree adds NOTHING

Both trees, same env (`GOPI_DAEMON_READY_FD`/`GOPI_WORKER_*` stripped — the
worker's inherited lock fd otherwise breaks 3 spawn-guard tests **on both
trees**), `go test -count=1 ./...`:

* merged (`master + branch`): fails cmd/agent, internal/signalrec,
  internal/standards, internal/stylegate, internal/testutil.
* control (master alone, detached at 45f213d9): fails **the same five**, and the
  named-test diff `merged \ control` is only the two `internal/signalrec` fork
  tests, which PASS in a solo re-run on both trees (load flakiness).

## 4. Reds that are master's own — fixed here so the merged tree is green

All four reproduce on the master-only control tree, so none is caused by the
branch; the gate notices new *packages*, so clearing them removes the remaining
false-FAIL surface.

1. `cmd/agent/cli_tasks.go` — the `sweep-zombies --stall-sec/--max-run` parse
   blocks had been pasted into the `taskverbs.SweepWorktrees` case when that
   case was inserted above them, so `sweep-zombies --stall-sec N` silently
   parsed nothing while `sweep-worktrees --stall-sec N` was accepted
   (`TestParseTasksSweepZombiesArgs`). Re-homed; the reason is in the code.
2. `cmd/agent/cli_tasks.go` — the `sweep-worktrees` usage line was 96 display
   cols against `config.HelpWrapCol` = 88
   (`TestEveryHelpConstantFitsTheWrapBudget`). Re-wrapped.
3. `internal/stylegate` — 3 `os.Setenv` + 1 `time.Sleep` sites in
   `internal/testutil`'s temp-root probe (Rule 17's sanctioned per-call flip and
   a sleep that IS the fixture) and 1 production discard in
   `cmd/agent/tmpdir_inject.go` now carry their Rule-15/17 reasons; the 3
   ignored-error sites added by `internal/signalrec`'s session-provenance fix
   **after** the last ceiling ratchet (`0dde9423`, ceiling 182) are annotated,
   taking the measured count 185 → 182.
4. `internal/testutil` — the 10 `devdoc/worker-*` files that teach a
   worker-launch shape now carry the canonical `WORKER-OWNERSHIP: OPERATOR-ONLY`
   pointer (`devdoc/worker-ownership.md` owns the policy).

## 5. Also changed: the standards baseline re-key (`cf57e3e6`)

An earlier draft of this doc left `internal/standards` Rule 16 red **on master
alone** and argued the delta rule excuses it. **That reasoning was wrong on the
facts.** The Rule-16 gate fails *both ways*: a NEW unbaselined report at
`cmd/agent/main.go:961` **and** a STALE permit at `:955` (the baseline file's own
comment: a stale permit is "a silent permission"). Master's `4a896cc3`
("`--daemon` is an act too") inserted 6 lines above the baselined `%w` site and
merged without re-keying — so the gate is red on master *by itself*, and any
verify child that judges absolutely (exactly the attack §2 describes) reads that
red as the merged state's verdict.

Verified on the merged tree before the fix:
`Rule 16 … 8 total hits, 7 baselined, 0 escaped, 1 new` + `STALE baseline entry
cmd/agent/main.go:955`. After the re-key (`955` → `961`, with the reason in the
comment, per the file's own re-key procedure — never relax, never delete):
`go test ./internal/standards/` → **ok**.

The site is unchanged: the same `%w` call whose two `%v` verbs format the
`payloadFilePrefix` STRING constant (literal per-cent verbs, wrapping nothing).

## 6. Reproduce

```bash
# merged tree vs master-only control, same command
cd ~/workspace/gopi-rawlog-rework && env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD \
  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && go test -count=1 ./...
cd ~/workspace/gopi-rawlog-control  && env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD \
  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && go test -count=1 ./...
```

## 7. This rework's verification evidence (merged tree vs master-alone)

The rework re-ran the decisive A/B on the **current** master
(`13acf700`) with the branch merged, since the 2026-09-13 audit trail was
written against an older master:

| tree | how | result |
|---|---|---|
| merged | `/home/user/workspace/gopi-rawlog-rework` = branch `fix/rawlog-test-home-leak` | master is an **ancestor** (`git merge-base --is-ancestor master HEAD`) → fast-forward mergeable, zero conflict surface |
| control | `/home/user/workspace/gopi-rawlog-control` detached at the same master | master alone |

Same command in both trees: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
then `env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u
GOPI_WORKER_LOCK_FD -u GOPI_WORKER_SESSION go test -count=1 -timeout 3600s ./...`.

**Result: zero merged-only failing packages.** Every red the merged tree shows
is reproduced by master alone — master's own `internal/standards` re-key
(§5, fixed here), the `internal/stylegate` / `internal/testutil` sites, and the
`cmd/agent` spawn-guard failures caused by the *test process inheriting the
worker's own fd/env* (`GOPI_DAEMON_READY_FD: bad file descriptor`), which are
identical on both trees and green in a solo re-run. The only merged-vs-control
difference was `internal/tools TestMemorySearchNonBlocking`
(`database schema has changed: vtable constructor failed: distilled_qa_fts (17)`)
— a **flake**: 7/8 merged runs green, 8/8 control green, and the test binds its
own `t.TempDir()` DB (`setMemoryDB`), so its only shared state is FTS5.

Two reproducible **method traps** for whoever re-runs this:

1. `testutil.GopiBin` **SKIPS** when `bin/gopi` is absent — the first "green" is
   a skip, not a pass. Build the binary first.
2. An inherited worker environment (`GOPI_DAEMON_READY_FD`,
   `GOPI_WORKER_DAEMON_CHILD`, `GOPI_WORKER_LOCK_FD`, `GOPI_WORKER_SESSION`)
   fails three spawn-guard tests on *both* trees. Strip it before comparing
   trees, or the "regression" is your own harness.

## 8. The one red that looked branch-attributable was a HANDSHAKE RACE (§9 fixes it)

A single-tree repeat of one package (`go test -count=3 ./internal/signalrec/`)
showed `TestSignalRecChildVictimRecordsEndToEnd` +
`TestSignalRecForwardingPreservesDefaultDeath` failing in the merged tree and
passing in the control — the one shape that would have been a genuine
branch-attributable regression. It is not one, and the way it dissolves is worth
recording, because a single-tree A/B ("repeat N times here, then N times there")
is exactly the method that manufactures this false signal.

| probe | method | result |
|---|---|---|
| single-tree repeat | merged 6×, then control 6× (merged **cold cache, host first**) | merged 3/6 FAIL, control 6/6 PASS ← the false "regression" |
| interleaved A/B | control **first** each round, 8 rounds | **16/16 PASS** — both trees |
| handler latency | SIGTERM at delay D, look for the record | *both* trees identical: no record at D=0.05/0.10/0.20s, record at D≥0.30s |

Root cause: the victim tests handshook with
`time.Sleep(300 * time.Millisecond)` ("the child has surely started by now"),
while the child's real `Install()` latency on this host is **~0.25–0.30s** — the
fixed sleep sits *exactly* on the latency. Under load (or a cold page cache) the
signal arrives before `signal.Notify` has registered SIGTERM, so the default
disposition kills the child and no record is written: the test then reports "a
killed gopi-shaped child left NO provenance record", which reads as a product
bug in exactly the code this branch touches. The old sleep is un-fixable by
tuning (a bigger constant is still a race); the child knows when it is ready, so
it now says so (§9).

## 9. The fix: a readiness handshake instead of a start-up sleep

The victim announces readiness on a FIFO the parent creates **before Start**, and
writes to it with `O_WRONLY` (blocking) — which cannot return until the parent
opens the read end. So an announcement is never lost to timing, and it is ordered
after `Install()` has returned by construction (the announcement is the next
statement, and a compiled-in ordering is not a race).

- Parent: `srAnticipateChildReady` (make the FIFO, hand `srReadyEnv` to the
  child) → `srAwaitChildReady` (bounded, **30s hang guard**, fails loudly if the
  child could not announce — the wait is never silently satisfied).
- Victim: `srAnnounceReady()` right after `Install()` in `TestMain`.
- `TestSignalRecOffIsHonored` uses the same handshake (with the off switch,
  `Install()` returns early, so "ready" == "at the blocking select").

Evidence that the handshake is real and not vacuous:

| check | result |
|---|---|
| the fixed tests, 10 consecutive rounds **under 8-CPU load** | **10/10 PASS** (the unfixed tree: 3/6 FAIL in the same window) |
| negative control — victim's `srAnnounceReady()` removed, same tree rebuilt | `--- FAIL: TestSignalRecChildVictimRecordsEndToEnd (30.03s)` — the wait times out and names the cause, so a vacuous handshake cannot pass |

A/B re-derived with the fixed test (interleaved, control first): 8/8 PASS on both
trees. The two signalrec tests are now deterministic on either tree, which
removes the last red that could be misread as this branch's fault — and removes a
false red FROM THE BRANCH'S OWN SUBJECT AREA, which is where a flake is most
expensive (it would have been credited to the fix under review).

