<!-- doc-class: historical -->
# Task Report: `internal/standards` gate red on master (3 findings re-verified at c4cc3f3d)

Branch: `fix/standards-gate-red` (worktree `/home/user/workspace/gopi-standards-gate-red`),
branched from master `c4cc3f3d`. The briefing named `ee1d0fd9` as the previous tip;
master had moved on, so **every finding was re-measured against the current tip
before anything was touched** — which changed the answer for two of the three.

## Summary of the three findings at the current tip

| # | Briefing claim | Measured at c4cc3f3d | Action |
|---|---|---|---|
| 1 | NEW Rule 18 else-after-return at `internal/tools/task_finish_deliverysync.go:~279` | **CONFIRMED** — the gate's only live red | Fixed in code (de-indented) |
| 2 | STALE Rule 16 baseline entry at `cmd/agent/main.go:1011` | **ALREADY RESOLVED** — the permit was retired by `3f7783c1`, and the surviving main.go permit is keyed `:1025` (a re-key of a *live* site, not a stale one) | Nothing to delete; documented |
| 3 | NEW Rule 16 `%v` at `internal/tools/task_merge.go:367` with an inline `// no-wrap:` escape | **PRESENT BUT A FALSE POSITIVE** — verdict below | Escape removed, `%w` restored |

At the tip, `go test ./internal/standards/` failed in exactly ONE place
(`TestStandardGates/TestGate3ElseAfterReturnRedProof`), not three. No STALE entry was
reported anywhere.

## Finding 1 — Rule 18 (`task_finish_deliverysync.go:279`) — fixed in code

```go
// before — both arms return, so `} else if` is the guard-clause shape Rule 18 wants
if refused, detail := finishCleanupLocalSyncChecks(repoDir, postTip); refused {
        return ...refused...
} else if detail != "" {
        return ...abstention...
}

// after — split into its own statement, refusal returns, abstention de-indented
refused, detail := finishCleanupLocalSyncChecks(repoDir, postTip)
if refused { return ...refused... }
if detail != "" { return ...abstention... }
```

Behaviour is byte-identical: the arms were already mutually exclusive returns, and
`detail` is now simply in scope for both. **No baseline entry was added** — the ratchet
only moves down.

## Finding 2 — the stale Rule 16 permit is gone already (re-verified, no action)

`cmd/agent/main.go:1011` no longer appears in `errorWrapBaseline` and the gate reports
**0 STALE**. The number in the briefing was the *pre-stale* key: `415afd6d` re-keyed it
1011, then `a5429a3b` moved the site again and the entry was re-keyed to `:1025` with a
recorded reason ("RE-KEY 1011 → 1025 … The call is byte-identical … `grep -n 'escape a
literal prompt' cmd/agent/main.go` prints exactly ONE line, 1025"). Independently
verified: `grep -n 'escape a literal prompt' cmd/agent/main.go` → `1025:` exactly once,
and on a clean-master A/B worktree the gate reports no stale entry. Deleting `:1025`
would license a genuinely new `%v` at that address, so nothing was deleted.

## Finding 3 — the `// no-wrap:` escape was a FALSE POSITIVE; the reason was wrong

**Verdict: false positive.** The escape claimed "the merge state this error came from is
gone — the abort above found no conflict to abort, so there is no chain left to carry".
Both halves of that claim fail against the code and the history:

1. **The claim is wrong about the code.** `mergeErr` is a real cause and the caller
   report is its only consumer: `fail(err)` → `TaskMergeBranchOutcome{Branch, Action:
   "error", Detail: err.Error()}`. `%v` discards the cause the caller renders; `%w`
   keeps it reachable for `errors.Is/As` and any unwrapping reader. Nothing about
   `mergeErr` is "deliberately outside the chain" — the sentence described the state of
   the **abort** call, not the error being wrapped.
2. **It was not a design decision at all — it was a merge loss.** `a2bf8ef4`
   ("fix(stylegate): document the drifted sites, ratchet the ceilings down") fixed
   **this exact site to `%w`** and pruned its `errorWrapBaseline` permit. That commit is
   an **ancestor of master**. The revert happened in the master-canonical merge
   resolution of `fix/standards-reds` (`86382ebb`, landed as `333fb3ac`), on a tree
   where the fix was already present — and the same commit then **added the `// no-wrap:`
   escape**, recording the revert as a deliberate exception instead of undoing it.

```
$ git show a2bf8ef4 -- internal/tools/task_merge.go
-  return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %v", branch, mergeErr))
+  return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %w", branch, mergeErr))
$ git merge-base --is-ancestor a2bf8ef4 master && echo ancestor   → ancestor
$ git show 333fb3ac^:internal/tools/task_merge.go | grep -n 'no conflict'
366:  return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %v", branch, mergeErr))
$ git show 86382ebb^1:...task_merge.go | grep 'no conflict'   → :367 …%v   (master side)
$ git show 86382ebb^2:...task_merge.go | grep 'no conflict'   → :366 …%w   (branch side, the fix)
```

**Fallout the escape hid:** `internal/stylegate`'s `errorf-verb` ceiling was **RED on
master over this same line** (8 measured vs 7 declared, `internal/tools/task_merge.go:367
(%v wraps error (expr mergeErr))` named in its hit list) — because stylegate's
`errorf-verb` **honours no inline escape**. So the code carried a claim ("deliberately
outside the chain") that one gate was told to believe and another gate silently rejected.

**Honest remedy applied:** the escape is **removed** and the site is `%w` again. The
verdict is recorded in the code (a comment at the line explaining the history, so a
future reader sees why it is `%w` and not an escape), in `baseline.go`'s RETIRED permit
note, in stylegate's ceiling comment, and here. No baseline entry was re-added; the
permit stays deleted.

## Regression guard (Rule 10.5)

`internal/tools/task_merge_wrapchain_test.go` — `TestTaskMerge_NoMergeToAbortKeepsTheChain`
asserts the source shape of the one call: the `"no conflict state to abort"` `fmt.Errorf`
must render its error with `%w`, and the line must **not** carry a `// no-wrap:` escape
(the escape is exactly what silenced the revert). A source-level guard is used
deliberately: the value-level chain is **not observable from outside** `taskMergeOne`,
because its local `fail()` flattens the error into `TaskMergeBranchOutcome.Detail` via
`err.Error()`.

**Red-proofed:** with the line rewritten back to `%v … // no-wrap: test probe` the test
fails with both messages; restored, it passes.

## Completed

- Finding 1: Rule 18 site de-indented in code (no permit added).
- Finding 2: re-verified as already fixed (`0 STALE`), documented; the live `:1025`
  permit is a re-key of a live site and was deliberately NOT deleted.
- Finding 3: escape declared a FALSE POSITIVE, removed; `%w` restored; verdict recorded
  in code, baseline note, stylegate comment and this report.
- Count-bearing prose in `baseline.go` re-measured and marked SUPERSEDED (9→8 hits).
- Stylegate's ceiling comment corrected: the half of the 8→7 ratchet that had been
  silently undone is named, and the measurement is 7 == ceiling again.
- Minimal repro test added and red-proofed.

## Not Done (honest)

- **The other three stylegate reds are NOT mine and are NOT fixed** — `ignored-error`
  5 production (ceiling 0) / 170 test (ceiling 163), `unchecked-close` 1 test (ceiling 0).
  A/B against a clean-master detached worktree at `c4cc3f3d` shows the **same** three
  lines, byte-identical; only `errorf-verb` (8 vs 7) was removed by this branch. They
  are pre-existing master reds in `internal/stylegate`, outside this task's scope (the
  epic scoped the standards gate).
- **`go test ./...` was not run repo-wide.** Only the touched packages were run
  (`internal/standards`, `internal/stylegate`, `internal/tools`), because the epic's
  acceptance is the standards gate and the repo has a known pre-existing suite hang.
- The Finding 3 revert is fixed but its **cause** — a "master-canonical on conflicted
  files" merge resolution that took the losing side of an already-landed fix — is a
  process defect that no single branch can close. Recorded here as evidence.

## Verification

- Build: `go build ./...` → **OK** (`gofmt -l` on the touched files: clean; the 4
  unformatted files it reports under `internal/`+`cmd/` are **identical on master** —
  verified in the clean-master A/B worktree — so they are not this branch's).
- Tests (branch, `fix/standards-gate-red` @ `01187d43`):
  - `go test ./internal/standards/` → **ok, exit 0** (was FAIL: 1 NEW Rule 18 violation).
  - `TestStandardGateDataReport` → Rule 16 `hits=8 baselined=7 escaped=1 NEW=0`
    (was `hits=9 baselined=7 escaped=2`) — the count moved by FIXING, not by permitting.
  - Red-proof tests still pass: `TestGate1ErrorWrapRedProof` PASS,
    `TestGate3ElseAfterReturnRedProof` PASS, `TestStandardGateDataReport` PASS.
  - `go test -run 'TestTaskMerge|TestFinishCleanup' ./internal/tools/` → **ok**.
  - `go test -run TestTaskMerge_NoMergeToAbortKeepsTheChain ./internal/tools/` → PASS
    (and FAILs on the regressed shape — red-proof).
  - `go test ./internal/stylegate/` → **errorf-verb hit is GONE** (was 8 vs ceiling 7);
    the 3 remaining failures are byte-identical to clean master (A/B worktree at
    `c4cc3f3d`).
- Commits (one file per commit, Rule 8):
  - `893bba1c` fix(standards): de-indent the else-after-return at task_finish_deliverysync.go:279
  - `b2690287` fix(merge): restore the %w wrap a merge resolution silently reverted
  - `152242dc` test(merge): guard the "no merge to abort" wrap against a silent revert
  - `eac82b94` docs(standards): re-measure the Rule 16 counts the task_merge.go fix moved
  - `01187d43` docs(stylegate): the task_merge.go half of the 8->7 ratchet had been undone
- Worktree: `git status --porcelain` → **empty** (two scratch worktrees used for the A/B
  were removed; the deliverable is committed, nothing left untracked).

## Branch and recovery

- **Branch:** `fix/standards-gate-red` (5 commits on top of master `c4cc3f3d`).
- **Where it lives:** a linked worktree of the main repo
  (`/home/user/workspace/gopi-standards-gate-red`), so the commits are already on the
  branch inside the main repo's object store — **no push needed**.
- **Recovery / inspection commands:**
  ```bash
  git log --oneline c4cc3f3d..fix/standards-gate-red      # the 5 commits
  git show fix/standards-gate-red --stat                  # files touched
  git show fix/standards-gate-red:devdoc/standards-gate-red-20260918.md   # this report
  git merge-base --is-ancestor c4cc3f3d fix/standards-gate-red && echo "branched from expected tip"
  ```
- No merge was performed and no branch other than `fix/standards-gate-red` was touched.
  The unrelated `feat/merger-pure-cronjob` branch was never accessed.
