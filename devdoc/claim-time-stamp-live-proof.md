<!-- doc-class: historical -->
# Task Report: the claim-time stamp, observed live end to end (epic 01a0b182 item 2/5)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `feat/claim-stamp-live-proof` (worktree `~/workspace/gopi-claim-stamp-live`).
Task row: `01a0b1ab-935f-7736-a943-9e36153e7eac`.

## Completed

### 1. The epic's acceptance was produced as a LIVE observation (writer + reader in one reading)

The predecessor (`01a0a9b1`, killed) landed the writer half on master (`6dc03ad7`:
`cmd/agent/task_claim_ground.go` + `tools.TaskWorktreeStampClaimerFor`) but produced
no live render. This task produced it, on a real claim, with the real wrapper:

- fixture: a row submitted into a **copy of the production ledger** (scratch, on
  `/home/user`, device 2052 — off the `/` partition, Rule 22), a settings file naming
  the real repo, and a **pre-existing linked worktree** of `/home/user/work/GoPi`;
- the real wrapper was launched with its cwd inside that worktree
  (`gopi worker <row>`, the manual single-row entry);
- during the run the **row in flight names its own ground** (observed at +2.4 s,
  status `in_progress`):

```
in_progress|peerbox:3625056|{"branch":"fix/claim-stamp-probe2","repo":"/home/user/work/GoPi",
                            "worktree":"/home/user/workspace/_claim-stamp-live/stamp-wt2"}
```

- the **reader** (`gopi merger --status`, same ledger, same moment) names the branch
  **with its task id** instead of the gap sentence. Full render lines are quoted in
  `## Evidence (self-filed)`; the attribution pair as the render prints it:

```
fix/claim-stamp-probe2 — 01a0b329-b97d-7dae-ae10-3cc8717679de [an unfinished run still owns it]
```

- **control, in the same render**: a sibling worktree of the same repo that no row
  claimed still prints the gap sentence — so the difference is the stamp, not the
  render.

### 2. Which writer fired — measured, not assumed

The predecessor's contract is that the CLAIM seam writes the row **before the child
is spawned**. The write order in the live wrapper's own log proves it: the stamp line
is followed by `executor cwd →`, which is the seam that runs *before* spawning
(`cmd/agent/cli_tasks.go` `taskClaimGroundStamp(target, childDir)` precedes
`cmd.Dir = childDir`/`cmd.Start()`). Nothing had been created at that instant, and the
row's `claimed_at == in_progress_at` while `updated_at` is one second later — the
claim, then the stamp.

A second live run (v3) captured the **ownership guard** operating as designed: a row
claiming a worktree another live row already names is refused

```
[task-notify] task 01a0b329-464e-795d-b99f-1907feff77ff: claim-time ground stamp:
  /home/user/workspace/_claim-stamp-live/stamp-wt is already named by row
  01a0b324-38cf-7bea-baa8-38fa623869db — not binding this row to another row's worktree
```

### 3. Repair: the claim-time write was reporting the `creation` moment

**Measured.** In the live claim run the row's ground was written by the claim seam but
the operator-facing line said *"at creation"*:

```
[task-notify] task 01a0b324-…: deliverable.worktree backfilled to "…/stamp-wt"
              (branch "fix/claim-stamp-live-probe") at creation
```

Nothing was created at that instant (the line precedes `executor cwd →`, i.e. the
child did not exist yet). The shared writer's outcome line was therefore a wrong
attribution of **when** the ground was learned — in an epic whose entire subject is
"which row owns this ground". The byte was right; the sentence was not.

**Fix (surgical, Rule 4).** The shared writer keeps its ONE blob parser/rebuild/key
(the anti-drift half of the original contract, Rules 13/14) and gains a `moment`
parameter with TWO named spellings: `taskWorktreeStampMomentClaim` / `_Creation`
(`internal/tools/task_deliverable_worktree.go`). The claim seam passes `claim`; the
creation detection seam passes `creation`; the line format is one const and is written
through one sink (`taskWorktreeStampLog`, a test seam — production rebinds nothing).
After the fix, the live claim reports:

```
[task-notify] task 01a0b329-b97d-…: deliverable.worktree backfilled to "…/stamp-wt2"
              (branch "fix/claim-stamp-probe2") at claim
```

### 4. Regression test: the writer closes the READER's gap (the missing acceptance)

The predecessor's tests pinned the blob; **nothing asserted the reader**. A writer that
wrote a key no reader loads — or the wrong spelling — would have passed every existing
test. `internal/tools/task_deliverable_claim_reader_test.go` drives the production
path end to end: claim + start → the ONE claim-time writer → `BranchAttributionReport`
(real git worktree listing + the real row loader) → `mergeCandidateLines`/`ownerClause`
(the status render's own sentence builders). It asserts:

- **before** the claim, the render states the gap for the registered-but-unclaimed
  worktree (so the after-state cannot pass vacuously);
- **after** the claim, the render does NOT contain `candidateNoRowNote`, DOES name
  `task <id>`, and DOES name the recorded checkout.

Red proof: with the writer call disabled, the test FAILS (observed); restored, it
passes. A second test pins the two moment spellings, so a regression to the single
"creation" wording reddens it.

## Not Done (honest)

- **The ordinary Rule-6a solo claim shape cannot be produced in-process.**
  `taskChildFailTestEnv=1` (the only hermetic in-process child escape) returns
  **before** the stamp, so the claim→stamp sequence is observable only structurally
  (the predecessor's AST call-site test, still green) or live. The live shape used here
  is the one where the wrapper's cwd is already the row's linked worktree.
- **Coverage statement (measured, not asserted).** The claim stamp records the ground
  when the directory the executor child starts in is **already inside the row's linked
  worktree**. That is not the majority of live in-flight rows: at measurement time the
  rows that DO carry the pair while in flight are `01a0b1ab` (this run),
  `01a0b17c` (fix/merger-stop-takeover-contract), `01a0b19d`
  (fix/zombie-state-assert) and `01a0b2d3` (feat/gopi-merge-command) — and of those,
  at least this run's own row was written by the **creation** seam (its wrapper's cwd
  is its session directory, measured in `/proc`, so its claim stamp had no worktree to
  name). A row claimed with the child starting in the MAIN checkout records
  **nothing at claim time**: the claim seam logs an INFO line and returns `false` by
  design (there is no worktree to name yet, and inventing one would bind the row to a
  directory it does not own — the 2026-09-12 hijack).
- **The standard shape is covered by the OTHER seam, measured live on this task's own
  row.** `01a0b1ab` (this run) was claimed with the child's cwd = the main checkout —
  the claim stamp logged "no linked worktree" — and the row nonetheless names
  `feat/claim-stamp-live-proof` + its worktree **in flight**, written by the executor's
  CREATION seam the moment this run's first bash round created the worktree. So for
  "wrapper claims from the main repo, the executor creates its worktree mid-run", the
  pair reaches the row from the creation seam, not the claim seam.
- **The window is not zero and was not measured.** Between the mid-run `git worktree
  add` and the next round's stamp there is a short window in which the checkout exists
  and no row names it. The claim seam cannot close it (it runs before the creation) and
  this task did not measure its duration; a reader that samples inside that window
  would still print the gap.
- **`internal/stylegate` is red on master too.** Measured on the pristine main working
  tree and on this branch: the same five rules (test-sleep 5, test-skip 1, errorf-verb
  8/ceiling 7, ignored-error 194/163 test + 11/0 prod, with identical sites). No new
  violation.
- The reader (`fix/merger-status-operator-text`) was not touched — it is the acceptance
  instrument. No render text was edited.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (worktree `~/workspace/gopi-claim-stamp-live`)
- Tests: `go test -count=1 -run 'TestClaimStamp|TestClaimTimeGroundStamp' ./internal/tools/` → ok (7.5 s);
  `go test -count=1 -run 'TestClaimGroundStamp|TestClaimGround' ./cmd/agent/` → ok (5.8 s)
- `go test ./internal/standards/` → ok on master baseline AND on this branch (19.8 s / 22.2 s)
- `internal/stylegate`: identical 5 failures on master baseline and on this branch (no new violation)
- `go test -race -count=1 -run 'TestClaimStamp' ./internal/tools/` → ok (10.2 s)
- `go vet ./internal/tools/ ./cmd/agent/` → clean; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (the repo's declared build, Rule 12) → OK
- Commit: `242019b7` on `feat/claim-stamp-live-proof`; `git status --porcelain` in the worktree → empty (Rule 30)

## Evidence (self-filed)

Live claim observation (v4, fixed binary), written to
`/home/user/workspace/_claim-stamp-live/wrapper4.log` and `render4.txt`:

```
IN-FLIGHT after ~2.4s: status=in_progress
  {"branch":"fix/claim-stamp-probe2","repo":"/home/user/work/GoPi","worktree":"/home/user/workspace/_claim-stamp-live/stamp-wt2"}
=== WRITER: the seam's own words, in the wrapper's measured order
8:running gopi with task 01a0b329-b97d-7dae-ae10-3cc8717679de as briefing...
11:[task-notify] task 01a0b329-b97d-7dae-ae10-3cc8717679de: deliverable.worktree backfilled to
   "/home/user/workspace/_claim-stamp-live/stamp-wt2" (branch "fix/claim-stamp-probe2") at claim
12:gopi worker: executor cwd → /home/user/workspace/_claim-stamp-live/stamp-wt2
   (task 01a0b329-b97d-7dae-ae10-3cc8717679de: launch workdir (already the task's own ground))
=== READER: 'gopi merger --status' on the same ledger, in flight
    - fix/claim-stamp-probe2 — NOT a candidate: a checkout holds it
      (/home/user/workspace/_claim-stamp-live/stamp-wt2) and a live process IS working in that
      checkout (pid 3624592 (cwd …/stamp-wt2); pid 3625745 …) — task 01a0b329-b97d-7dae-ae10-3cc8717679de
      — the task row's recorded runner is still alive; the checkout has stood for 8s
    - fix/claim-stamp-gap-control — NOT a candidate: a checkout holds it
      (/home/user/workspace/_claim-stamp-live/gap-wt2) and the scan found no process inside that
      checkout — no ledger row names this branch (a gap: the run holding this checkout should record
      it on its task); the checkout has stood for 35s          <-- the CONTROL, unclaimed
--- THE PAIR (branch → task id) as the render prints it:
fix/claim-stamp-probe2 — 01a0b329-b97d-7dae-ae10-3cc8717679de
```

Red proof of the new regression test (writer call disabled on purpose):

```
--- FAIL: TestClaimStampClosesTheStatusGapOnARealClaimedRow (2.59s)
FAIL	github.com/nick/gopi/internal/tools	3.114s
--- restored, re-run: ok  	github.com/nick/gopi/internal/tools	7.116s
```

Baseline-vs-branch gate measurement (commands run in both trees):

```
$ (main repo, pristine)  go test -count=1 ./internal/standards/  → ok   19.786s
$ (this branch)          go test -count=1 ./internal/standards/  → ok   22.191s
$ (main repo, pristine)  go test -count=1 ./internal/stylegate/  → FAIL: test-sleep 5, test-skip 1,
    errorf-verb 8/7, ignored-error 194/163 (test), ignored-error 11/0 (prod) — same sites
$ (this branch)          go test -count=1 ./internal/stylegate/  → FAIL: identical five rules, identical sites
```

## Files

- `internal/tools/task_deliverable_worktree.go` — the `moment` parameter + the two
  named spellings + the one line format and its sink.
- `internal/tools/task_deliverable_claim_reader_test.go` (new) — writer→reader
  acceptance + the moment-attribution regression.

## Recovery (Rule 6d)

- Branch: `feat/claim-stamp-live-proof` (commits live in the shared main repo — a
  worktree shares refs, nothing was pushed).
- Recovery: `git show feat/claim-stamp-live-proof:devdoc/claim-time-stamp-live-proof.md`
  and `git show feat/claim-stamp-live-proof:internal/tools/task_deliverable_claim_reader_test.go`.
- The live observation's raw evidence stays outside the worktree (Rule 30):
  `/home/user/workspace/_claim-stamp-live/{wrapper2.log,wrapper4.log,render4.txt}`.
