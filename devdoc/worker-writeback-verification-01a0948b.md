<!-- doc-class: historical -->
# Independent Verification Report — ledger row 01a0948b-a6dc-78b1-9973-854b1bffe254

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Epic:** two worker-writeback defects — (1) an `in_progress` row's `claimed_by`
stamp names a wrong/dead holder because `TaskStart` discarded its `by` argument
and the zombie liveness layer was a live veto (an unreaped Z pid read as alive),
so stale rows were silently suppressed; (2) a dead wrapper leaves its linked
worktree occupied, so `scripts/safe-merge.sh` gate [c] refuses the branch and the
merger's protected arm skips it silently forever.

**Verifier role:** verification only — no fix applied, no source file of the
delivered tree modified (the one probe file was removed after the runs; see
`git status` evidence below).

**Subjects verified**
| tree | commit / branch |
|---|---|
| delivered | `master` = `8aa1f9b5` (fix commits `28abe541`, `52a03ae0`, both ancestors of master) |
| pre-fix control | `b8c556ae` (detached worktree `/tmp/vth/prefix`), i.e. the tip the fix branched from |

Both trees were built with `CGO_ENABLED=0`; `scripts/safe-merge.sh` is
byte-identical between them (`git diff b8c556ae..master -- scripts/` → empty), so
the gate is a fixed measuring instrument, not part of the change.

---

## Completed

1. **Delivered fix exists and is in master** — `git merge-base --is-ancestor
   52a03ae0 master` → true. Diff vs `b8c556ae`: 9 files, +1653/−24
   (`internal/tools/tasks.go`, `taskzombie.go`, `task_reclaim.go` (new),
   `repo_sweep.go`, `merger_status.go`, `cmd/agent/cli_tasks.go`, 3 test files).
2. **Build green** — `gofmt -l ./internal ./cmd` empty; `CGO_ENABLED=0 go build ./...`
   exit 0; `go build -o bin/gopi ./cmd/agent/` produced the binary used by the
   black-box acceptance.
3. **Defect 1 verified by an INDEPENDENT probe** (author: this verifier, not the
   fix author). `/tmp/vth/probe/zz_verifier_probe_test.go` — public API + raw SQL
   on the real ledger file, new-column presence detected at RUNTIME so the same
   file runs on both trees. Pre-fix the three defect assertions FAIL, delivered
   they PASS; the negative twin passes on both:
   | probe | pre-fix `b8c556ae` | delivered `8aa1f9b5` |
   |---|---|---|
   | `TestVerify_TaskStartStampsTheExecutingProcess` | FAIL (`claimed_by = "vp-ladder:1"`, want `vp-executor:4242`; "no in_progress_at referent column") | PASS |
   | `TestVerify_UnreapedZombieIsNotLive` | FAIL (pid in `/proc` state **Z** reads ALIVE) | PASS |
   | `TestVerify_LiveLookingPidUnderAStaleStampIsNotVetoed` | FAIL (`DetectStaleTasks` reported **0**, want 1 — the live-looking pid vetoed the stale event) | PASS |
   | `TestVerify_LiveStampStillVetoes` (negative) | PASS | PASS — the fix narrows the pid layer, it does not abolish it |
4. **Defect 2 verified end-to-end, black box** — `/tmp/vth/shell-acceptance.sh`
   drives the REAL `bin/gopi worker --once` (one real lifecycle pass →
   `serveFinishCleanupSweep` → `ReclaimDeadHolderWorktrees`) against the REAL
   `scripts/safe-merge.sh`, with a sandboxed `HOME`/ledger/settings. Fixture: a
   `completed` row whose deliverable names a feat branch and a CLEAN linked
   worktree, `claimed_by` = a provably dead (real, reaped) pid of this host.
   | step | pre-fix `b8c556ae` | delivered `8aa1f9b5` |
   |---|---|---|
   | B `safe-merge.sh` before | exit **1** (gate [c]) | exit **1** (gate [c]) |
   | C one `gopi worker --once` | cannot see the row (`t-shell-dead … done=[worktree]` if reached; with the starvation shape: **nothing**) | `dead-holder worktree reclaimed: feat/dead-holder-shell (t-shell-dead) freed for merge; branch kept` |
   | D `safe-merge.sh` after | directory still present → still **1** | **0**, tip byte-identical, `branch:deliverable.txt` still present |
   | E negative (live process cwd inside) | — | directory survives, **1** still refused, log names the live pid |
5. **Red-proof of the coverage gap** — the author's own acceptance test
   (`cmd/agent/cli_tasks_dead_holder_accept_test.go`) run against the PRE-FIX tree
   fails with exactly the epic's symptom:
   `sweep lines: [t-accept-dead branch=feat/dead-holder-shell done=[worktree]]` →
   `the sweep must report the reclaim`.
6. **Delivered tests green** (env scrubbed per the epic:
   `env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID -u GOPI_TASK_RUN_ID`):
   - `internal/tools`, 36 targeted tests over the holder-stamp / zombie /
     reclaim / merger-status layers → PASS.
   - `cmd/agent` acceptance pair → PASS; the negative test logs the live-cwd
     refusal (`worktree in use by live process pid … (sh): cwd …/wt`).
   - `internal/standards` (Rules 16–22 gates) → ok 3.78s; `go vet` on the two
     touched packages → clean.
   - `go test ./... -count=1` (whole repo) → see the raw tail in the verdict.

## Not Done

- **No fix was written or reverted** (verifier role). The probe file was removed
  from the delivered worktree after its runs; the delivered tree is clean
  (`git status` shows only the pre-existing untracked `briefings/` and `devdoc/`
  working files of the main checkout, none of them mine).
- **Two observations were NOT treated as verdict-blocking defects** (honest
  disclosure, both structural to the row chain, not to this fix):
  1. `taskTerminalStatus` = `{completed, failed}`, so a `verified` or `cancelled`
     row's worktree is not collected. The reclaim's SQL names `verified` in its
     `status IN (…)` list but its post-scan filter `taskCleanupEligibleStatus`
     (`completed|failed|cancelled`) drops it, so the delivered `verified` case
     behaves exactly as before the fix (directories survive a sweep). The epic's
     acceptance fixes `safe-merge.sh` against the reclaim's LIVE-guards, not
     against `verified`/`cancelled`; the row chain's newest-10/oldest-10 windows
     (`serveFinishCleanupScanMax`, `serveFinishCleanupWorktreeScanMax` = 10) also
     exclude `verified`/`cancelled` rows from the row-chain arm entirely, so no
     regression is introduced and nothing that worked before is broken.
  2. The row chain already freed a *small* (`completed`) fixture, so the fix's
     measurable delta is the STARVATION shape (the incident's real ledger, rows
     thousands deep: "protected=7"): with 10 newer + 10 older decoy rows the
     row-chain windows cannot see the stranded branch, while the reclaim scans
     200 and reaches it. That is the shape recorded above and the one that fails
     pre-fix.
- The standing limit documented by the fix is real and verified as such: an
  `in_progress` row with a STALE stamp is *reported*, not repaired (a stale stamp
  proves neither life nor death) — the probe's negative twin pins that a fresh
  live stamp still vetoes, i.e. no new false-positive nagging.

## Verification (raw commands + tails)

```text
$ gofmt -l ./internal ./cmd                 → (empty)
$ CGO_ENABLED=0 go build ./...             → BUILD_EXIT=0
$ go vet ./internal/tools/ ./cmd/agent/    → VET_EXIT=0

$ go test ./internal/tools/ -count=1 -v -run 'TestTaskStartStamps…|TestClaimStampFreshness|…'
--- PASS: TestTaskStartStampsTheExecutingProcess / …FallsBackToThisProcess / …SelfClaimMovesTheReferentWithTheStamp
--- PASS: TestClaimStampFreshness, TestZombieVerdict_StaleStampIsNotALiveVeto, …_FreshStampStillVetoes, …_LegacyRowKeepsItsVote
--- PASS: TestPidIsZombieIsDead, TestPidIsZombieOnARunningProcess, TestDetectStaleTasks_ReportsAStaleStampRow, …_LivePidSuppressed
--- PASS: TestDeadHolderReclaim* (7), TestReclaimEnvKillSwitch, TestDeadHolderBranchIsNamedInTheSweepPreview, TestLiveHolderBranchIsNotReportedAsDead
ok  github.com/nick/gopi/internal/tools  0.795s

$ go test ./cmd/agent/ -count=1 -v -run 'TestAcceptance_…'
    cli_tasks_dead_holder_accept_test.go:176: sweep lines: [dead-holder worktree reclaimed: feat/dead-holder-accept (t-accept-dead) freed for merge; branch kept]
--- PASS: TestAcceptance_DeadHolderReclaimUnblocksSafeMerge (0.08s)
--- PASS: TestAcceptance_LiveHolderKeepsSafeMergeRefused (0.07s)

# INDEPENDENT probe — pre-fix control (b8c556ae)
    zz_verifier_probe_test.go:105: DEFECT 1: pid 2756000 is in /proc state Z (unreaped zombie) yet reads ALIVE
    zz_verifier_probe_test.go:143: DEFECT 1: claimed_by = "vp-ladder:1", want "vp-executor:4242"
    zz_verifier_probe_test.go:146: DEFECT 1: no in_progress_at referent column
    zz_verifier_probe_test.go:208: DEFECT 1: … must be REPORTED, not suppressed (reported=0, want 1)
--- FAIL: TestVerify_UnreapedZombieIsNotLive / _TaskStartStampsTheExecutingProcess / _LiveLookingPidUnderAStaleStampIsNotVetoed
--- PASS: TestVerify_LiveStampStillVetoes
# INDEPENDENT probe — delivered (8aa1f9b5): all 4 PASS

# BLACK-BOX SHELL ACCEPTANCE (real bin/gopi + real scripts/safe-merge.sh)
# pre-fix b8c556ae, starvation shape:  B exit=1 → C no reclaim reported → directory survives
# delivered 8aa1f9b5, starvation shape:
[worker] [finish-cleanup] dead-holder worktree reclaimed: feat/dead-holder-shell (t-shell-dead) freed for merge; branch kept
B: exit=1 (gate [c])   D: exit=0, tip byte-identical, deliverable intact
E: dead-holder reclaim feat/live-holder-shell skipped: worktree in use by live process pid … (sleep): cwd …/wt2  → exit=1, directory survives
VERDICT: PASS — gate [c] 1 -> 0 across one real sweep; tip kept; live holder untouched

$ go test ./internal/standards/ -count=1  → ok  github.com/nick/gopi/internal/standards  3.781s
$ go test ./... -count=1                  → <see verdict result string>
```

## Verdict

**VERIFIED** — both acceptance criteria hold on the delivered tree, each with a
red/green axis measured on the pre-fix tip: (1) `TaskStart` stamps the executing
process and a stamp older than the `in_progress` transition no longer acts as a
live veto (independent probe: 3 FAIL → PASS, negative twin PASS both sides);
(2) a dead holder's clean linked worktree goes from `safe-merge.sh` exit 1 to
exit 0 across exactly one real `gopi worker --once` pass with the branch tip
byte-identical and the deliverable intact, while a LIVE holder's worktree is
never touched (shell acceptance + negative case).
