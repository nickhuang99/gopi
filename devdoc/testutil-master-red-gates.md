<!-- doc-class: historical -->
# Task Report: the two master-red gates in `internal/testutil`

Branch: `fix/testutil-master-red-gates` (worktree `~/workspace/gopi-testutil-reds`)
Base: master tip `c0e4c809` (the epic brief quoted `f4f761c5` — STALE; measured
with `git log --oneline -1 master` before starting).

## Completed

### 1. Deliverable (a) — the temp-using package without a scratch-root pin: ALREADY FIXED ON MASTER

Measured, not assumed:

- `internal/cgroupscope/testmain_scratchroot_test.go` exists on master
  (`func TestMain(m *testing.M) { testutil.RunSuiteMain(m, "<pkg>") }`).
- `go test ./internal/testutil/ -run TestPinCoverageTable -count=1 -v` prints the row

  ```
  internal/cgroupscope                       yes        yes       PIN     PIN
  packages: 31 scanned | scratch-using: 23 by the ORIGINAL markers, 23 by the widened markers (+0 newly detected) | pinned: 23
  ```

  i.e. **23/23 scratch-using packages pinned** — coverage is exhaustive, so no
  sibling backfill is outstanding (a hand-sweep of siblings would be redundant).
- `go test ./internal/cgroupscope/ -count=1` → **ok** (the pin does not break the suite).

The epic's item (a) is therefore **not re-fixed** — it was landed by `7f799501`
("fix(cgroupscope): pin the suite scratch root via testutil.RunSuiteMain") after
the brief was written. Nothing to change; the gate row is the proof. Rule 22
partition criterion re-checked: `stat -c %d /` = **2051**, scratch home
(`~/workspace/gopi-test-scratch`) = **2052** — different devices, so the pinned
suite temp data is off the `/` partition.

### 2. Deliverable (b) — the ownership-pointer offenders: TWO, not one

`TestWorkerOwnershipPointerPresent` on live master reported **two** files, while
the epic brief named one (`closing-run-1806.txt`). Fixing only the named file
would have left master red. Each file was judged by the gate's own criterion —
*pointer where a launch is taught, dismiss where only a code path is named*:

- **`devdoc/worker-daemon-acceptance/closing-run-1806.txt` → POINTER (annotate).**
  A verbatim transcript that quotes the operator's own worker launch lines, so it
  genuinely teaches the shape. Added the canonical pointer in the file's own
  comment form (`#`, the `.txt` language) plus two lines naming the lines below as
  **CAPTURED OUTPUT, not a recipe** — the pointer line alone would say *who* may
  run it, the extra lines say *this is not an instruction*.
- **`devdoc/worktree-cwd-zombie.md` → DISMISS (reviewed reason).** Its ONLY match
  is a noun phrase naming the task-run *face* (`cli_tasks.go:1833`) inside a 5-why
  mechanism sentence about the cwd release — a code path, not a command. Measured
  evidence for the judgement: the file has **zero fenced code blocks**
  (`grep -c '^```'` = 0), so there is no recipe to warn a reader off of; an
  injected pointer would claim a launch the file never teaches. Dismissed with a
  reason, sibling to the existing entry `worktree-live-cwd-guard-20260910.md`
  (same evidence family: CWD forensics).

**This is a reviewed dismiss, not a widened permit.** `workerOwnershipDismissed`
is the gate's own policy table for files the audit did not name; the gate also
runs `TestWorkerOwnershipDismissTableIsHonest`, which fails a dismissed file that
carries the marker anyway — so the table cannot drift into a hiding place. The
code gate itself was **not** weakened: no needly list, anchor, scan root or
threshold was touched (one table row added; the gate's detection behaviour is
byte-identical).

## Not Done

- Nothing in scope. Two notices, recorded honestly:
  - Deliverable (a) required no change (already green — see above); the item is
    satisfied by the existing pin, and I re-proved it rather than re-landing it.
  - The brief's master tip was stale; I verified against the live tip `c0e4c809`.

## Verification (all commands run in the worktree)

- Base re-check: `git log --oneline -1` → `c0e4c809` (NOT the brief's `f4f761c5`).
- Repro (before): gate trio FAILed, naming
  `closing-run-1806.txt` + `worktree-cwd-zombie.md`.
- After:
  - `go test ./internal/testutil/ -run 'TestPinCoverageTable|TestEveryTempUsingSuiteIsPinned|TestWorkerOwnership' -count=1` → **ok**
  - `go test ./internal/testutil/ -run TestWorkerOwnership -count=1 -v` → **4/4 PASS**
    (PointerPresent, CanonicalBlockIsReal, GateScansTheTree, **DismissTableIsHonest**)
  - `go test ./internal/testutil/ -count=1` (whole package) → **ok 49.624s**
  - `CGO_ENABLED=0 go build ./...` → **OK (exit 0)**
  - `go test ./internal/cgroupscope/ -count=1` → **ok** (pin suite unaffected)
- No baseline widened: `internal/standards/baseline.go` untouched; the only
  permission added is one reasoned dismiss-table row (see above).
- Rule 6b self-check: `git -C /home/user/work/GoPi status --short` → **clean**
  (an earlier relative-path edit landed in the main repo; it was reverted with
  `git restore` on that single file and re-applied inside the worktree — the
  main repo carries none of this change-set).
- Rule 8: committed one file at a time (docs commit `66517e00`, gate commit
  `c6e0d4aa`).

## Commit

- `66517e00` fix(docs): restore the operator-only pointer on the 18:06 closing-run transcript
- `c6e0d4aa` fix(testutil): dismiss the worktree-cwd-zombie post-mortem from the ownership gate, with a measured reason
- Branch: `fix/testutil-master-red-gates` (base `c0e4c809`)

## Deliverable location (Rule 6d)

- Branch: **`fix/testutil-master-red-gates`** in the MAIN repo
  (`/home/user/work/GoPi`) — worktrees share the main repo's refs, so the
  commits are already there; no push needed.
- Recover: `git show fix/testutil-master-red-gates:devdoc/testutil-master-red-gates.md`
- Mergeable check: `bash scripts/safe-merge.sh fix/testutil-master-red-gates`
