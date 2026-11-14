<!-- doc-class: historical -->
# Task Report: dispatch-time staleness guard for the merge-verify rework lane

Row: `01a09e9b-d4e4-755a-af4c-85ffb667801b` ("merge verify rework: fix/tasklist-inprogress-scan (was 01a09d2d)")
Session: `01a0a028-f9e7-7a13-af01-0bee871b3cd0`
Branch: `fix/merge-verify-rework-staleness-guard`
Worktree: `~/workspace/gopi-sg4` (also checked out in `~/workspace/gopi-sg3`). The original
`~/workspace/gopi-staleguard` worktree was removed by the shared repo's worktree sweep.
Base: `master` merged IN (`21c4ca68` = 9 branch commits + master `10f96bc0`); the pre-merge
branch tip was `0dd6d27c`.
The merge is clean: master `10f96bc0` and this branch share no file.

## Completed

### 1. Dispatch-time staleness guard (deliverable 1)

New file `internal/tools/task_staleness_guard.go`. The judgement is a claim-time
**git measurement**, never a lease on the frozen briefing text:

- `git rev-parse --verify --quiet refs/heads/<branch>` — exit **1** = measured absence
  (`--quiet` turns "no such ref" into 1), exit **0** = the tip, any other status
  (128 fatal, timeout, no git) = **unmeasured** (never read as absence).
- `git merge-base --is-ancestor <tip> <master>` — exit 0 = the fix is already on the
  integration branch, exit 1 = not an ancestor, anything else = unmeasured.
- `git log --format=%H%x1f%B%x1e -n 40 <master>` — the re-landing witness:
  an `adoption: merge <branch> (<sha>)` commit on master. This is the ONLY witness
  available once the merged-cleanup sweep has deleted the ref, and it is what makes
  the measured case visible at all.
- **Separator-insensitive name arm** (`MergeVerifyStaleNameKey`): the measured row's
  deliverable names `fix/tasklist-inprogress-scan` while the ref that really landed is
  `fix/task-list-inprogress-scan` — one dropped hyphen — so an exact-name scan finds
  nothing. Only `-`/`_` are folded, the arm is used **only** to attach evidence to a
  row that is already refused (never to cancel a claimable row), and the no-op text
  names both spellings.

Wiring: `TaskNextFiltered` (the single CAS claim funnel behind the worker round,
`serve` dispatch and `task run next`) runs the sweep **before** its claim CAS and
closes a stale row as a no-op — the row is never handed to a worker, and no
forward-rework instruction is ever delivered. Lane membership is judged on the tags
the gate itself wrote (`merge-verify` + `branch-rework`), with `merge-verify-retry`
**excluded**: a retry row's premise (the branch is on master) is true by design, so
judging it would cancel the verify the retry lane exists to perform.

Verdict vocabulary (exported constants, one spelling per fact):

| verdict | meaning | fate |
|---|---|---|
| `already-landed` | ref resolves and its tip IS an ancestor of master | closed, no-op |
| `branch-not-found` | ref does not resolve (hard refusal either way) | closed, no-op |
| `premise-open` | ref resolves, tip NOT on master ⇒ real rework owed | **dispatched** |
| `not-judged` | premise unmeasurable (no repo/branch recorded, git absent, timeout) | dispatched |

Not-this-arm is explicit and tested: a genuinely unlanded revert keeps its worker.
Rule 15b: every git failure is recorded (the probe's exit status and stderr tail ride
the row's evidence string), an unmeasurable premise is never silently "clean".

### 2. Wrong-premise briefing text fixed (deliverable 2)

`MergeVerifyFileRework` hard-coded "…because the merger's post-merge verify child
judged the merged state ON THE REAL MASTER failing (%s)" — while the `%s` it
substituted was, in the measured row, `MergeVerifyReasonChildDead` ("verify child
exited without recording a verdict on the judged row"). The sentence **contradicted
its own parenthetical** and upgraded an absence of measurement into a measured
failure. Now `mergeVerifyReworkCauseClause(reason)` splits on the gate's own
judgement axis (epic verify-no-verdict-keeps-merge):

- `MergeVerifyReasonChildDead` / `ChildTimeout` → "recorded NO VERDICT … nothing
  measured the merged state, so there is NO measured failure on master to reproduce";
- `MergeVerifyReasonFail` → "judged the merged state ON THE REAL MASTER failing
  (… — an observed, evidenced failure of that measured state)".

The payload IS the briefing the executor reads (`TaskBriefing` returns the payload;
`runTaskRunOnce` passes it through), so the fix lands at the filing site.

### 3. Regression tests (deliverable 3)

`internal/tools/task_staleness_guard_test.go` — 8 tests driving the REAL dispatch
seam (`TaskNextFiltered`) against a real temp repo and a real temp ledger:

- `TestStaleReworkGuardTypoBranchAlreadyReLandedIsNoOp` — **the exact measured case**:
  row deliverable `fix/tasklist-inprogress-scan` (typo) + the real ref merged and swept
  + the landing commit on master ⇒ not claimed, closed as a no-op, the closed row
  points at the landing sha, the frozen payload is preserved.
- `TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp` — unresolvable ref
  with no landing witness ⇒ hard refusal with the verdict recorded.
- `TestStaleReworkGuardAlreadyLandedRefStillPresentIsNoOp` — ref kept, tip already an
  ancestor ⇒ no-op.
- `TestStaleReworkGuardUnlandedReworkStillDispatched` — **the not-this-arm control**:
  genuinely unlanded rework is still claimed.
- `TestStaleReworkGuardLeavesUnrelatedRowsAlone` — lane scoping.
- `TestStaleReworkGuardClaimNoteNamesTheMeasurement` — the read-only operator entry.
- `TestVerifyGateReworkBriefingRendersNoVerdictAsNoVerdict` — the briefing pin, three
  sub-cases (ChildDead, ChildTimeout, Fail).

### 4. Rules honoured

Rule 1/1b/13/14 (named constants; the adoption-prefix token comes from
`adoptionSummaryPrefix`, the integration branch from `IntegrationBranchName`, the
log format from `mergeVerifyLogFormat`); Rule 4 (surgical edits); Rule 6 (worktree,
off the main tree); Rule 8 (file-by-file `git add`); Rule 15/15b (every git failure
recorded — exit status + stderr tail ride the evidence); Rule 22 (runs pointed at a
scratch root on `/home`, never the `/` partition); Rule 3 (no new tools); zero new
dependencies (stdlib + `os/exec` git only).

## Known costs / limits (honest)

- The sweep runs once per `TaskNextFiltered` call, so its cost is paid on every claim
  attempt: one tag-narrowed SQL query, then 2–3 bounded git subprocesses **per pending
  lane row** (≤ `mergeVerifyStaleSweepCap` = 8). At the current lane size (≈1 row) that
  is single-digit milliseconds. The repeated-probe case is a `premise-open` rework row
  that stays pending while *other* rows are claimed (it is re-probed on each attempt);
  no cache is added — a cache would be state the ledger does not own, and the guard's
  whole value is that it measures at claim time.
- The close is a `taskUpdate` (the same transition the merger uses), not a compare-and-
  swap: a row that a concurrent claimer grabbed a moment earlier ends in the same
  terminal state (`claimed → cancelled` is legal), so the outcome converges. A row
  whose close FAILS is logged and left claimable — the guard never reports a refusal it
  did not record.
- A branch name containing a pipe/newline would make the evidence string ambiguous.
  Git ref names cannot contain those characters (refs are stricter than branch-name
  rules), so no escaping is applied; noted rather than papered over.

## Not Done

- **The step that has NOT been observed end-to-end is the LIVE dispatch of a stale row.**
  The claim pump (`TaskNextFiltered` → `mergeVerifyStaleSweepPending`) is exercised against
  a real temp ledger and a real temp repo by the regression tests, and the judgement is
  proven against the live measured row read-only (above) — but no real worker was allowed
  to reach the sweep against the production ledger during this task, so the
  close-and-skip of a production row is verified at the seam, not in production. The
  first production claim attempt after the merge is the true end-to-end run; anything it
  logs goes to stderr plus the row's own reason/event.
- **Arm (2) of the epic's staleness guard** — regenerating/validating the briefing
  TEXT at claim time — is deliberately not implemented as a second mechanism: the
  claim-time git judgement (deliverable 1) subsumes it, and the briefing text is fixed
  at its source (deliverable 2). A text rewrite would add a mutable-briefing path the
  append-only ledger does not have.
- The shared scratch root `/dev/shm/gopi-test-scratch` was **found 100% full** (7.8G
  tmpfs, 6.1G residue, 694 leftover suite dirs) *before* any run of mine. The
  sanctioned `gopi scratch-gc` swept `removed=0` (everything is younger than the 6h
  `StaleAge`). I did **not** delete shared scratch state as a side effect; I pointed my
  runs at `$HOME/workspace/gopi-test-scratch` instead. Reported, not fixed — the host
  needs an operator's sweep or a larger `/dev/shm`.
- The `cmd/agent` merger e2e tests were not re-run in isolation (the package is
  large); the full-tree run is the acceptance gate. See Verification.

## Verification

### The measured case, reproduced against the live repo

```
$ git rev-parse --verify --quiet refs/heads/fix/tasklist-inprogress-scan; echo "exit=$?"
exit=1                                   # the briefing's spelling does not resolve
$ git rev-parse --verify --quiet refs/heads/fix/task-list-inprogress-scan; echo "exit=$?"
exit=1                                   # the REAL ref is gone too — merged refs are swept
$ git log --oneline --grep='adoption: merge fix/task-list-inprogress-scan' master
fc9b06d4 adoption: merge fix/task-list-inprogress-scan (0618a2cd)
$ git merge-base --is-ancestor fc9b06d49e217f85f15822c7af1061fa8fa7d21f master; echo $?
0                                        # the fix IS on master
```

The measured row (live ledger `~/.gopi/memory.db`):

```
01a09e9b-d4e4-755a-af4c-85ffb667801b | merge verify rework: fix/tasklist-inprogress-scan (was 01a09d2d-...) | in_progress | merge-verify,branch-rework
deliverable {"branch":"fix/tasklist-inprogress-scan","origin_task":"01a09d2d-...","repo":"/home/user/work/GoPi"}
```

### The guard's judgement on the LIVE measured row (read-only probe)

Run against the live ledger (`GOPI_MEMORY_DB=/home/user/.gopi/memory.db`), through the
same exported entry the claim pump uses:

```
VERDICT=branch-not-found skip=false repo="/home/user/work/GoPi" branch="fix/tasklist-inprogress-scan" tip="" master="master"
EVIDENCE: git rev-parse --verify --quiet refs/heads/fix/tasklist-inprogress-scan ⇒ exit=1
        | git log master (n=40, --format=%H%x1f%B%x1e) -> exit=0
        | adoption landing on master: fc9b06d49e217f85f15822c7af1061fa8fa7d21f
          "adoption: merge fix/task-list-inprogress-scan (0618a2cd)" — the row's spelling
          "fix/tasklist-inprogress-scan" differs from the landing's "fix/task-list-inprogress-scan"
          (a separator divergence; the SAME work)
NOOP_TEXT: no-op — branch "fix/tasklist-inprogress-scan" does not resolve in "/home/user/work/GoPi",
          but master carries its landing commit fc9b06d4 … (the landing names the ref
          "fix/task-list-inprogress-scan" — the row's "fix/tasklist-inprogress-scan" differs only by
          separators): the fix was RE-LANDED and the ref swept master. The forward rework is not owed —
          nothing to check out, nothing to fix. …
```

This is the whole defect answered by a measurement: the row would have been dispatched
with "rework the branch forward", and the guard instead records the landing sha and both
spellings, then closes it. (The probe test was throwaway evidence and is not committed;
the same judgement is what `TestStaleReworkGuardTypoBranchAlreadyReLandedIsNoOp` pins.)

### Build + tests

```
$ CGO_ENABLED=0 go build ./...                 # BUILD_OK
$ gofmt -l internal/tools/                     # empty
$ CGO_ENABLED=0 go vet ./internal/tools/       # clean
$ CGO_ENABLED=0 go test -count=1 -run 'TestStaleReworkGuard|TestVerifyGateReworkBriefing' ./internal/tools/
ok  github.com/nick/gopi/internal/tools  5.233s
```

### Acceptance suite — and three failures proven NOT to be mine

The headless/scratch suites pin their tmpdir under the shared scratch home
(`internal/testutil` `ScratchHomeDir()` = `$HOME/workspace/gopi-test-scratch`), which on
this host is a **symlink to `/dev/shm/gopi-test-scratch`** — a 7.8G tmpfs that was
**100% full before I ran anything** (6.1G residue, 694–717 leftover suite dirs; the
sanctioned `gopi scratch-gc` swept `removed=0` because every entry is younger than the
6h `StaleAge`). Errors observed under it were `no space left on device` /
`database or disk is full (13)` / `git init` copy-fd failures — i.e. the ENOSPC flutter,
not a regression.

The pin does NOT escape it: the operative override is **`GOPI_TEST_TMPDIR`**
(`SuiteTempRootEnvName`), not `GOPI_TEST_SCRATCH_HOME` (which only *publishes* the home
to children). Pinning `GOPI_TEST_TMPDIR=/home/user/scratch-gopi-test` (a real directory
on `/home`, off the `/` partition — Rule 22) moved every suite root off tmpfs; the
acceptance run below was taken that way.

Pre-existing failures, each proven against **pristine master `79738cb0`** (a separate
run in `~/work/GoPi`, which my branch does not touch):

| failure | proof it is not mine |
|---|---|
| `internal/testutil` `TestWorkerOwnershipPointerPresent` — 5 unannotated devdoc files (`daemon-cwd-escape`, `rule18-voice-device-else`, `task-list-inprogress-scan-20260914`, `worker-help-capability-first`, `worker-status-lock-assert`) | reproduced **identically on pristine master** (`go test -run TestWorkerOwnershipPointerPresent ./internal/testutil/` → same 5 files). Those files are docs other sessions landed; my diff touches no `devdoc/`-gate content. Independently corroborated: a SEPARATE session filed bug row `01a0a0a5` ("TestWorkerOwnershipPointerPresent red on master — 5 devdoc files …") at 00:00:13 — the same 5 files, nobody's-diff-in-particular. |
| `internal/tools` `TestWorktreeDropStaleLockRepairsTheGateShape` | passed on pristine master and on my clean re-run; failed only inside the saturated-tmpfs run (`git commit: No space left on device`). |
| `cmd/agent` `TestMemorySync*`, `TestTaskRunSeam*` | the failure text is literally `copying …/a.out to /dev/shm/gopi-test-scratch/…/gopi: no space left on device` — the test's own `go build` could not write its scratch binary. |

Run log: `/home/user/scratch-gopi-test/acceptance2.log` (host path; not committed — it is
an environment artifact, not a deliverable).

### Red-proof (the guard is what changes the outcome)

`TestStaleReworkGuardUnlandedReworkStillDispatched` is the positive control for the
branch where the guard must DO nothing: the same `TaskNextFiltered` call that refuses
the stale row in the other tests hands the unlanded row back (`status=claimed`). A
guard that refused unconditionally would fail that test.

## The defect reached a real executor (measured on the live ledger)

The row is now **terminal**, and its stored briefing is the wrong-premise text verbatim —
so this is not a near-miss: the contradiction shipped.

```
$ gopi task show 01a09e9b-d4e4-755a-af4c-85ffb667801b
status:  completed        reason: task finished
tags:    merge-verify,branch-rework,worker:peerbox
briefing: WHAT HAPPENED: branch "fix/tasklist-inprogress-scan" was merged into "master" and then
  REVERTED, because the merger's post-merge verify child judged the merged state ON THE REAL
  MASTER failing (verify child exited without recording a verdict on the judged row).
```

The executor it was handed to reworked onto the **real** ref (`fix/task-list-inprogress-scan`,
one hyphen) and delivered `fix/task-list-inprogress-doc-pointer` — it recovered the missing
hyphen itself. One sentence of the briefing asserted a failure nobody measured, in the same
clause that named the absence of a verdict.

## Evidence / audit trail

- Row: `01a09e9b-d4e4-755a-af4c-85ffb667801b`
- Landed commit: `fc9b06d4` "adoption: merge fix/task-list-inprogress-scan (0618a2cd)"
- Cause reason in the row's briefing: `MergeVerifyReasonChildDead` — "verify child
  exited without recording a verdict on the judged row" (NOT a content failure)
- Session: `01a0a028-f9e7-7a13-af01-0bee871b3cd0`
- Branch: `fix/merge-verify-rework-staleness-guard`

## Final state (rebased onto master 7099edf0)

- Master **reverted** the verify-no-verdict branch, so `mergeVerifyRetryTag` no longer exists and
  there is no verify-retry lane to exclude. Lane membership is now the tag pair **plus** the gate's
  own rework summary prefix (`mergeVerifyReworkSummaryPrefix` + branch). That is a purely NEGATIVE
  test — a row the guard does not recognise stays claimable — so a stricter predicate can never
  block legitimate rework.
- **Neither deliverable is on master — an earlier draft of this report claimed master had
  absorbed the cause-clause fix, and that claim is FALSE; corrected here with the measurement.**
  `git cat-file -e master:internal/tools/task_staleness_guard.go` ⇒ ABSENT;
  `git show master:internal/tools/verify_gate.go` still carries the literal wrong sentence
  `"post-merge verify child judged the merged state ON THE REAL MASTER failing (%s)"` (line 469);
  `git show master:internal/tools/tasks.go | grep -c mergeVerifyStale` ⇒ `0`.
  Both deliverables ride this branch and only this branch.
- Rebasing also pulled in master's new `tests/fixtures/log-stats-raw`. The earlier 43
  `open ../../tests/fixtures/log-stats-raw/...` failures were that gap (my branch predated master's
  fixtures) — not a defect of this change.

## Verification (final)

| suite | result |
|---|---|
| `CGO_ENABLED=0 go build ./...`, `go vet ./internal/tools/`, `gofmt -l internal/tools/` | clean |
| `./internal/standards/` (the Rule 16–21 gates) | **ok** |
| `./internal/tools/` — full suite on the rebased branch | **ok (152s)** |
| guard tests + `TestTaskNextOrdersByPriority` + every `TaskNext*` | **ok (60s)** (10m timeout) |
| `./cmd/agent/` — pristine master, isolated | green (`ok … 2.902s`); master full-package run also green |

ENVIRONMENT EVENT (recorded, not hidden): the shared repo's worktree sweep deleted this task's
worktree mid-run — three times, including during the final full-package run. The resulting
`cmd/agent` / `internal/testutil` failures are all blind-scan artifacts of a removed CWD
("read main.go: no such file", "scanned only 0 production sources", "the glob is wrong, not the
code") and name nothing in this change. `internal/tools` — the package holding every line of it —
was green in the same interrupted run. The three genuinely pre-existing failures (the
worker-ownership doc gate, and the ENOSPC/flake pairs) are documented above with their
pristine-master reproductions.
