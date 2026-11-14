<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: worker-ownership pointer backfill #4 — 3 annotated, 4 dismissed (gate red on master `0dac2ae7`)

Branch: `fix/worker-ownership-pointer-backfill-3`
Worktree: `/home/user/workspace/gopi-worker-ownership3` (fresh linked worktree off master `0dac2ae7`, Rule 6)
Commit: `2c2f89dc` — "fix(docs): worker-ownership pointer backfill #4 — 3 annotated, 4 dismissed with reasons"
Recovery: `git show fix/worker-ownership-pointer-backfill-3:devdoc/worker-ownership-pointer-backfill4-20260916.md`
(or `git show fix/worker-ownership-pointer-backfill-3:internal/testutil/worker_ownership_gate_test.go` for the code half)

## Completed

- **Re-ran the gate on the current master tip first** (`0dac2ae7`), per the established
  method (`devdoc/worker-ownership-pointer-regrowth-20260915.md`): the offender list drifts
  as other branches land, and three sibling tasks (`01a09d73` / `01a09947` / `01a09d2d`) were
  already marked completed while the gate stayed red. Measured red: **7 uncovered files**.
- **Confirmed the list is not a truncation artifact** (bug `01a0a6f1`'s fix is on master):
  the disclosure line reads `7 uncovered file(s) … listing all 7` — nothing withheld — and
  the escape hatch `GOPI_WORKER_OWNERSHIP_FULL_LIST=1` lists the **same 7** paths
  (`grep -cE '^ +(devdoc|briefings|scripts)/'` → `7`). Total == uncovered count == listed count.
- **Root cause of the regrowth (class, not incident)**: all 7 files landed **2026-09-16**,
  i.e. after the 2026-09-15 backfill; each is a report/rig written *about* a worker or
  task-run failure, so the launch shape re-enters the tree with every post-mortem. Provenance
  measured per file (`git log -1 --format=%h` on the master tip): `12fb857a`, `8f0533d0`,
  `0d4c9d66`, `e6bc3474`, `afd0c40d`, `80f721f0`, `5bc975b4` — all dated 2026-09-16.
- **Per-file disposition** using the gate's own question — does the file TEACH the launch
  move (→ canonical pointer) or merely NAME/CITE it (→ reasoned dismissal)? A dismissal is a
  reviewed exception, never a way to quiet an actionable file; the 4 below are measurement
  or call-site records whose lines describe processes that already existed (or a step this
  session is explicitly forbidden to take), so a pointer there would read as an instruction
  to do the forbidden thing.

| file | disposition | why |
|---|---|---|
| `devdoc/daemon-scope-per-profile.md` | **pointer** | the daemon START shape is the document's subject (§"The defect"); its tables cite the very command whose naming was wrong |
| `devdoc/worker-idle-drained-stop.md` | **pointer** | a fix report whose subject is a live daemon's lifecycle (`start` with `GOPI_WORKER_IDLE_EXITS=0`, then the stop doors) — the start shape is taught |
| `scripts/accept-daemon-scope.sh` | **pointer** | a **runnable** acceptance rig: it STARTS two production daemons itself, so a reader can copy it — the strongest teach case here |
| `devdoc/nested-report-path-clobber.md` | **dismissed** | env-inheritance root cause; its ONE match names the wrapper as the process that SETS `GOPI_TASK_REPORT_PATH`, inside a mechanism sentence about its children — no launch step |
| `devdoc/why-worktree-residue.md` | **dismissed** | residue diagnosis: every match is live-host forensics (`systemctl`/`ps` output, `/proc/<pid>` cwd, a ledger claim stamp, the Rule 24 session-scope measurement) about workers **already running**; no launch recipe |
| `devdoc/worktree-refusal-arms.md` | **dismissed** | its ONE match is a noun phrase naming two spawn **call sites** where the new reclaim pass was wired — a code-attachment note, not an instruction |
| `devdoc/worktree-reap-reachable.md` | **dismissed** | live-evidence section of a sandboxed A/B/C probe through the production entry point; the doc states this session **cannot** restart the resident daemons (the agent-spawn guard exists for exactly that) and that the operator's next restart runs the reap — a pointer would instruct the forbidden restart |

- Each dismiss entry in `internal/testutil/worker_ownership_gate_test.go` carries a **real,
  file-specific reason** (Rule 27 / Rule 15 — a dismissed file is a skip, and a silent skip is
  a deleted test wearing a green); `TestWorkerOwnershipDismissTableIsHonest` passes, and none
  of the four carries the pointer (so no stale permit hides an annotation).
- Diff is **markdown + one shell comment + the dismiss table's entries** — no gate threshold,
  no scan root, no needle was touched (the check was not weakened; Rule 3/Rule 13).

## Not Done

- Nothing deferred on this branch: the gate is green and the tree carries the change.
- **Out of scope, filed elsewhere**: the unrelated master-red on Rule 17's 2 `os.Setenv` sites
  in `internal/agent/journalreport_test.go:101,104` is tracked by task `01a0a81c`.
- **Honest note on the class**: this was the fourth backfill of the same gate
  (2026-09-13, 2026-09-13 #2, 2026-09-15, now). Backfills treat the symptom; the structural
  question — should a *report* that quotes a refused/forbidden command have to carry a
  launch pointer at all — belongs to the gate's owner, not to this task, and is deliberately
  not decided here.

## Verification (transcript)

Branch worktree, `CGO_ENABLED=0` defaults:

```
$ go build ./...                      → exit 0
$ gofmt -l internal/testutil/worker_ownership_gate_test.go   → (no output)

# GREEN — the full worker-ownership suite, verbose:
$ go test ./internal/testutil/ -run 'TestWorkerOwnership' -count=1 -v
--- PASS: TestWorkerOwnershipPointerPresent (0.04s)
--- PASS: TestWorkerOwnershipCanonicalBlockIsReal (0.00s)
--- PASS: TestWorkerOwnershipGateScansTheTree (0.06s)
--- PASS: TestWorkerOwnershipDismissTableIsHonest (0.00s)
--- PASS: TestWorkerOwnershipReportDisclosesTheTotalWhenTruncated (0.00s)
--- PASS: TestWorkerOwnershipReportFullListEscapeHatchPrintsEveryFile (0.00s)
--- PASS: TestWorkerOwnershipReportAtTheCapWithholdsNothing (0.00s)
ok  	github.com/nick/gopi/internal/testutil	1.210s

# ACCEPTANCE:
$ go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1
ok  	github.com/nick/gopi/internal/testutil	1.660s

# RED PROOF (same worktree, change stashed → the gate must report exactly these 7):
$ git stash push -m redproof
$ go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1
    worker_ownership_gate_test.go:365: 7 uncovered file(s) teach a worker-launch / task-run
    command without the operator-only pointer; listing all 7:
          devdoc/daemon-scope-per-profile.md
          devdoc/nested-report-path-clobber.md
          devdoc/why-worktree-residue.md
          devdoc/worker-idle-drained-stop.md
          devdoc/worktree-reap-reachable.md
          devdoc/worktree-refusal-arms.md
          scripts/accept-daemon-scope.sh
FAIL
# TRUNCATION CROSS-CHECK (escape hatch lists EVERY hit):
$ GOPI_WORKER_OWNERSHIP_FULL_LIST=1 go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1 | grep -cE '^ +(devdoc|briefings|scripts)/'
7
$ git stash pop     → 4 files restored (M devdoc/…, scripts/…, internal/testutil/…)
$ go test ./internal/testutil/ -run TestWorkerOwnership -count=1  → ok
```

Self-check (Rule 6b): `git status` in the worktree lists exactly the 3 files this branch
touched (+ the dismiss table and this report); the main repo `~/work/GoPi` was never edited —
every read/edit/bash call ran in `/home/user/workspace/gopi-worker-ownership3`.

## Lesson for the next agent

The gate's red list is a **moving target**: re-measure it on your own base tip before editing,
believe the disclosure line (`N uncovered … listing all N`), and settle each file with the
gate's own question — teach vs cite — instead of blanket-inserting the pointer (a pointer on an
evidence file misreads the record as an instruction) or blanket-dismissing it (a reasonless
dismissal is a silent un-gate).
