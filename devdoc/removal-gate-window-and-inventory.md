<!-- doc-class: historical -->
# Task Report: the removal-ground gate read a GUARDED arm as unguarded — a 41-line window, and a census the floors could not pin

**Task:** `01a0e09a-f128-7306-97de-18ee48f0c89e`
**Branch:** `fix/removal-gate-arm-scope` (from master `885b0b8f`)

## What was red

```
--- FAIL: TestEveryWorktreeRemovalArmQuestionsTheGround (0.02s)
    worktree_removal_ground_gate_test.go:75: task_finish_cleanup.go:1716 frees a worktree without the ground checklist.
    worktree_removal_ground_gate_test.go:93: found 6 checklist-guarded arms, expected at least 7 — the guard is being bypassed wholesale
    worktree_removal_ground_gate_test.go:96: worktree-removal arms: 10 total, 6 checklist-guarded, 3 reviewed-exempt
```

The briefing read this as "the production arm is missing its guard". It is not: the arm
**is** guarded, and has been since it was routed through the checklist. The gate was
measuring **distance**, not the fact.

## Root cause (5-why)

1. Why did the gate flag `ArmFinishCleanupServer`? — No guard marker appeared in the 40
   lines directly above its `removalaudit.WorktreeRemove(` call.
2. Why not, when the arm calls `reclaimGroundChecklistVerdictForCloseout`? — The call sits
   **41 lines** above the removal (pinned: `:1675` vs `:1716` on master `885b0b8f`;
   `1716 − 1675 = 41`), one line outside the window.
3. Why 41 lines? — Three further guards were added between the checklist call and the
   removal (the run-identity guard, the completion fact, the audit record), each carrying
   its own comment block. None is a checklist rung, so none counted as a marker, and each
   pushed the real call further away.
4. Why was the guard ever placed that far up? — It was wired where the *delivery* question
   is answered ("the sync landed") rather than against the *removal* it authorises.
5. Why did the resulting red look like a production defect? — The read floor
   `guardReadFloor = 7` was **unreachable** by construction once the distance exceeded the
   window, so the gate reported a census (6 guarded) that no amount of correct production
   code could raise. A gate whose floor cannot be met is red for a reason unrelated to what
   it asserts.

## The fix (two surfaces, one commit)

**1. `internal/tools/task_finish_cleanup.go` — the rung moved to the removal.** The
checklist call now sits immediately before the audit record and the removal it authorises.
The change is **byte-identical code**: the same 7-line `if why := …` block, the same
argument list, the same refusal body. Verified by diff — the block is only removed from one
place and inserted, unchanged, in another. It is **behaviourally identical by construction**:
every guard it now sits *below* refuses by setting `rep.Skipped`, appending a failing step
and `return rep, nil` — so the checklist runs in exactly the cases it ran before, and no rung
is skipped. What the move changes is the *order* in which a refusal is reported when two
gates would both refuse (the completion fact now speaks first); that order is not frozen by
any test, and the completion fact is the one whose absence caused the 2026-09-21 removal.
The full reasoning is in the comment block above the call.

**2. `internal/tools/worktree_removal_ground_gate_test.go` — the window and the inventory.**
- `commentWindow` 40 → **80**: a window measures how *local* a justification must be, and
  40 was tight enough that three legitimate explanations between the guard and the removal
  flipped the verdict. It stays bounded on purpose (a marker must not be satisfiable from
  anywhere in the file).
- `armInventory = 10` (**new**): the floors above are *floors* — they catch a blind scan,
  not one site quietly entering or leaving while the counts still look healthy. The constant
  carries the **exact per-arm list** in its comment (7 guarded + 3 exempt, by file and arm
  constant), so a reader checks the number against the arms instead of trusting it.
- The census log now prints **every site** with its arm name and verdict, so the logged
  census is checkable against `armInventory` in one read.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK
- Tests: `CGO_ENABLED=0 go test ./internal/tools/ -run TestEveryWorktreeRemovalArmQuestionsTheGround -count=1` → **ok**
- Census: `10 total (inventory 10), 7 checklist-guarded, 3 reviewed-exempt`, and the logged
  per-site list is exactly the 10 arms the inventory comment names (see the report).
- Ran: the real verb, against a real repo, and the arm refused — quoted verbatim in the
  task report.

## Evidence (self-filed)

- Red-proof A (reproduce master's red): master's arm arrangement + `commentWindow = 40` →
  `task_finish_cleanup.go:1716 frees a worktree without the ground checklist` +
  `found 6 checklist-guarded arms, expected at least 7` + census `10/6/3`. Restored → `ok`.
- Red-proof B (the production guard is load-bearing): removing the checklist call from
  `ArmFinishCleanupServer` → `task_finish_cleanup.go:1713 frees a worktree without the ground
  checklist` + census `10/6/3`. Restored → `ok`.
- Both red-proofs were run with `cp` backups and restored; `git status --porcelain` after
  restore lists only the two intended files.
- A first attempt at red-proof A silently no-op'd (`commentWindow  = 40` — **two** spaces
  after gofmt's alignment), and printed `ok`; it was re-run with a regex. The green was the
  defect, not the fix.

## Not Done (honest)

- The window is a heuristic: an arm whose guard sits 81 lines up would fail exactly as this
  one did. The structural answer is that the gate would scan to the enclosing function, or
  that each arm would carry its own marker line; neither was in scope here, and the moved
  call plus the exact inventory make the current window's margin large (11 lines for this
  arm, versus 37 for the sibling). Recorded rather than papered over.
- `armReadFloor` is redundant with `armInventory` for the total (`10 >= 10` and `== 10`
  assert the same thing once they are equal). It was left in place rather than removed: the
  floor is what survives if the inventory constant is ever mis-typed upward, and Rule 13's
  discipline is to reference the existing constant, not to delete a working guard.
