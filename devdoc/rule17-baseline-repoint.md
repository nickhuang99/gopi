<!-- doc-class: historical -->
# Task Report: master's Rule 17 gate red — root cause CONFIRMED, fix ALREADY LANDED (duplicate delivered)

## Outcome in one line

The bug was real on pristine master (`805b313`/`d78b83e`) and I independently
reproduced it and produced the exact 4-line re-point — but **master was fixed
while this task ran**: `dca15ff` (an identical copy of my edit) landed via the
`feat/style-sweep` merge, and `origin` master `fe2b83b` is now **GREEN**. My
branch is a confirmed duplicate, not needed for merge; it is left for the
auditor to compare/discard.

## Root cause (5-why) — independently reproduced, not taken on faith

1. **Symptom** — `go test ./internal/standards/ -count=1` FAILS on pristine
   master: Rule 17 reports 4 NEW violations at
   `internal/agent/testmain_isolation_test.go:76/77/78/79` **and** 4 STALE
   baseline entries at `:62/63/64/65`; `hits=120 baselined=116 escaped=0 NEW=4`.
2. **Mechanism** — the Rule 17 allow-list is a list of literal `file:LINE`
   strings (`var testEnvBaseline`, `internal/standards/baseline.go`); `enforce`
   fails on a hit that is neither baselined nor escaped, and *also* on an entry
   that matches no violation.
3. **Why the lines moved** — commit `6093c76` rewrote
   `internal/agent/testmain_isolation_test.go` (+24 lines, `pinSuiteLedgerEnv`),
   pushing the 4 test-env hand-restore sites down 14 lines.
4. **Why it stayed red** — `6093c76` touched only
   `devdoc/ambient-tmp-scratch-gate.md`, the test file,
   `internal/testutil/pin_coverage_test.go`, `internal/testutil/scripts_gate_test.go`
   — **not** `baseline.go`. Nothing in the commit flow re-derives the list.
5. **Root cause** — a `file:LINE`-keyed ratchet is invalidated by *any* edit
   above a listed line, and the same commit that moves a site turns its own gate
   red with no signal to the author. (This line-keying is **intentional** per the
   `baseline.go` header — "it forces a human to look at each site while
   re-baselining" — so the bug is the missing process step, not the data shape.)

Reproduced by me on a detached probe worktree at pristine `805b313`
(`Rule 17 ...: hits=120 baselined=116 escaped=0 NEW=4`, exit 1) — same
violation set and counts as the briefing, matching memory record #1797.

## Completed

- Root cause confirmed independently on pristine master (`805b313`), including
  the full per-rule arithmetic from the gate's own `TestStandardGateDataReport`.
- Fix written in a linked worktree (`~/workspace/gopi-fix-rule17-baseline`,
  branch `fix/rule17-baseline-from-master`): 4 entries re-pointed
  `internal/agent/testmain_isolation_test.go:62/63/64/65` → `:76/77/78/79`
  (1 file, 4 insertions / 4 deletions — surgical, no other entry touched).
- Re-point rather than delete, deliberately: the 4 sites were *moved*, not
  fixed, and `testEnvBaseline`'s allow-list reason covers exactly this shape —
  deleting their permits would re-open the gate for real violations.
- Proven green on the fixed tree with the gate's own authoritative metric:
  `Rule 17 ...: hits=120 baselined=120 escaped=0 NEW=0`, `TestStandardGates`
  PASS, no STALE message, `TestGate2TestEnvRedProof` PASS (predicate not
  weakened), `go build ./...` exit 0.
- Verified the main repo's `internal/standards/baseline.go` was never edited by
  me (Rule 6b self-check: `git status --short` empty for it).
- Checked the handoff hazard against the LIVE branch set: no other branch
  (`feat/cov-agent`, `fix/web-signal-session-end`) carries a `baseline.go`
  change on top of master, so no two-sided drift remains to resolve.

## Not Done

- **Not merged, not pushed** (Rule 6e) — and now **not needed**: master already
  carries the identical fix.
- **No gate redesign.** Making the baseline robust to line drift (hash/anchored
  keys, or auto-regenerating during the commit/safe-merge hook) is deliberately
  out of scope: the line-keying is documented as intentional, and a structural
  redesign is its own task, not something to bolt onto a red-gate repair.
- **No durable guard for the drift class was added.** Today's green is only as
  durable as the next commit above a baselined line; a standing mechanism is
  still absent (recommendation below).

## Verification (final, on the current tree)

- **Build**: `go build ./...` → exit 0, no output (OK).
- **Gate (the failing check)**, run in `/home/user/work/GoPi` at master
  `fe2b83b`: `go test ./internal/standards/ -count=1` → `ok
  github.com/nick/gopi/internal/standards 4.417s` (was FAIL on `805b313`).
- **Gate arithmetic on master HEAD** (`TestStandardGateDataReport -v`): every
  rule reports `NEW=0` — Rule 16 `10/10/0`, Rule 17 `112 baselined=105 escaped=7
  NEW=0`, Rule 18 `7/7/0`, Rule 19 `1/0 escaped=1`, Rule 20 `1/0 escaped=1`,
  Rule 21 `0/0/0`.
- **Focused tests**: `-run 'TestStandardGates|TestGate2TestEnvRedProof'` →
  both `--- PASS`.
- **Fix equivalence**: my worktree's baseline.go == master HEAD's (same 4
  re-points, both from `805b313`); `git diff` between them is the style-sweep
  prune already in master, nothing of mine is unique.
- **Whole tree**: `go test ./... -count=1` on master `fe2b83b` — see the
  session's job output.
- **Worktree hygiene**: probe worktree `gopi-redcheck` removed; the deliverable
  worktree is left in place for audit (nothing was force-deleted).

## Commit

- Mine: `6d12ffd` `fix(standards): re-point Rule 17 baseline after 6093c76 moved
  the test sites` on `fix/rule17-baseline-from-master` (base `805b313`;
  2 files — `baseline.go` + this report).
- Master's (landed first, identical content): `dca15ff` `fix(standards):
  re-point Rule 17 baseline to the sites master 6093c76 moved`, merged into
  master by `fe2b83b Merge branch 'feat/style-sweep'`.

## Recommendation (the durable guard — NOT implemented here)

Nothing currently catches this drift at commit time; the gate only reports it
after the fact, and only if someone runs it. The cheap standing fix is to run
`go test ./internal/standards/` inside `scripts/safe-merge.sh` (or the merge
pre-check) before any merge is allowed — a red gate then blocks the merge that
would carry the stale baseline forward. That is a separate, reviewable change
and is deliberately left to its own task rather than smuggled into this repair.

## Hand-off status (re-verified after the run)

- Gate re-run on master HEAD `8447c58` (master advanced again after `fe2b83b`):
  `go test ./internal/standards/ -count=1` → **ok**; `TestStandardGateDataReport`
  → every rule `NEW=0`; `TestStandardGates` + `TestGate2TestEnvRedProof` PASS.
- `internal/standards/baseline.go` on master still pins
  `testmain_isolation_test.go:76/77/78/79` (4 entries, grep-confirmed) — i.e. the
  live master copy is equivalent to mine on exactly the 4 lines this bug is about.
  The `master ↔ my-branch` diff in that file is only master's later style-sweep
  prune (my branch is 40 commits behind), not a disagreement on the Rule 17 key.
- `feat/style-sweep` is gone (merged + cleaned) and `dca15ff` is an ancestor of
  master — the duplicate is fully integrated; no drift copy is left behind.
- Live branches re-checked for a competing `baseline.go` edit at the time of the
  previous hand-off: none (`feat/cov-agent`, `fix/web-signal-session-end` clean
  vs master). Note `feat/cov-agent` is 124 commits behind master and deleting
  `baseline.go` relative to the merge base — it must re-derive `baseline.go` from
  master at merge time, or it will re-break this gate (Rule 6c/9 hazard).
- `scripts/safe-merge.sh fix/rule17-baseline-from-master` correctly REFUSES my
  branch (worktree occupied + not merged) — the guard behaves as designed; this
  branch is not to be merged.

## Whole-tree note (flaky internal/tools under a concurrent run)

Two `go test ./...` runs overlapped by accident during this session (job-1 in the
fix worktree and job-3/job-5 in the main repo) plus a live `gopi task serve`
harness in the same machine state; one of them reported
`FAIL github.com/nick/gopi/internal/tools 420.809s`. An **isolated** re-run on
master `8447c58` settles it: `go test ./internal/tools/ -count=1` →
`ok github.com/nick/gopi/internal/tools 212.279s`, `TOOLS_EXIT=0`. That is
consistent with job-1's clean full-suite output (every package `ok`, including
`internal/tools 342.139s`). No evidence that this is related to the Rule 17
baseline change (that package does not touch `internal/standards`); flagged here
so the concurrency/flake is not mistaken for a regression.

## Deliverable declaration (Rule 6d)

- **Branch**: `fix/rule17-baseline-from-master` @ `13ed416` (HEAD) in
  `/home/user/work/GoPi` — shared object store, so the commit is already on the
  branch in the main repo; **no push needed** (Rule 6c).
- **Recovery**: `git -C /home/user/work/GoPi show 13ed416 --stat`
  (`git show 13ed416:devdoc/rule17-baseline-repoint.md` for this report;
  `git show 6d12ffd -- internal/standards/baseline.go` for the 4-line fix diff).
- **Master state**: green, verified at `8447c58` (master's own `dca15ff` is the
  landing copy and is an ancestor of master). My branch is a **duplicate** and
  must NOT be merged — merging it would conflict in the line-keyed
  `baseline.go`; `scripts/safe-merge.sh` already refuses it.
- **Cleanup**: the worktree `~/workspace/gopi-fix-rule17-baseline` is left in
  place deliberately (nothing force-deleted) so the auditor can diff my copy
  against master's; remove it after review with
  `git worktree remove ~/workspace/gopi-fix-rule17-baseline`.
