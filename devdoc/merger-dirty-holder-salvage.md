<!-- doc-class: historical -->
# Merger "left for a human": what it is, who decided it, and the fix

Epic: `01a0a4f9-c760-7a8f-958c-7032d9c65110` ("merger cannot merge?").
Branch `fix/dirty-holder-salvage-dispatch`, worktree `/home/user/workspace/gopi-dirty-salvage`.

The operator's question, verbatim: *merger admits failure to merge and left for
a human — what is going on? if the worktree or branch cannot be handled, should
it be returned or marked failed and resubmitted for rework? what use is leaving
it to a human? who told him to do this? and does this block the whole fleet of
branches to merge? it can always archive the conflict branch and submit a task
to explain.*

Every answer below is measured on the live host (2026-09-15, 20:13–20:25), not
inferred. The attachment (`merger_conflict.txt`) is the `gopi merger --status`
render this all starts from.

---

## 1. It is not a merge conflict, and nothing "failed to merge"

The phrase comes from **one line of code**, not from a merge attempt:

`internal/tools/task_reclaim.go:85`

```go
reclaimSkipDirtyReason = "worktree is DIRTY (uncommitted work) — left for a human"
```

It is guard ② of `reclaimDisposition` (`task_reclaim.go:343`), the dead-holder
worktree **reclaim**. State it precisely:

| claim | evidence |
|---|---|
| the branch is **protected** | a linked worktree of the repo has it checked out (`repoSweepProtectedBranches`) |
| the holder is **provably dead** | the ledger row naming that worktree has a claim pid this host can see is gone (`claimHolder` + `pidAlive`) |
| the reclaim **refuses to free the directory** | `git status --porcelain` is non-empty → guard ② fires |

The branch is not a merge candidate at all: `scripts/safe-merge.sh` gate [c] and
the merger's protected arm BOTH refuse a branch a worktree has checked out —
git itself refuses it (`git branch -d` → "Cannot delete branch … checked out
at …", measured in the prior epic's scratch repo). Measured on this host today,
the three affected branches:

```
--- feat/web-lan-access   /home/user/workspace/gopi-web-ip    dirty=2   ahead-of-master=1
--- fix/brew-call-retry   /home/user/workspace/gopi-brew-fix  dirty=3   ahead-of-master=0
--- fix/gopilog-textguard /home/user/workspace/gopi-textguard dirty=15  ahead-of-master=0
```

**Two of the three have ZERO commits ahead of master.** Their task's entire
deliverable exists only as uncommitted files in that directory (the rows:
`01a0a4c7` web, `01a0a45c` brew, `01a0a416` gopilog — the brew one's untracked
file is a whole new test file, `internal/tools/memory_brew_call_budget_test.go`,
which a diff can never show).

## 2. Who decided it, and nobody was told anything

- The decision is a **guard in the code**, deliberate and documented
  (`devdoc/merger-dead-holder-reclaim.md` §2/§6: "uncommitted work is work — a
  human's call, not a sweep's"). No human was asked; the wording is the arm's
  own.
- The merger did **not** report a failure. `gopi merger` (pid 318278) has been
  alive 1h15m and never printed this line — it appears only in
  `gopi merger --status` (the operator-facing render) and in the process-local
  `taskNotifyLog` sink.
- The only human-facing escalation channel in gopi is `task_notify`
  (`internal/tools/tasknotify.go`, `GOPI_TASK_NOTIFY_EVENTS`). Nothing was
  paged for these three.

So "left for a human" is accurate about the *design* (a dirty tree is never
destroyed automatically) and **false about the hand-off**: gopi filed nothing,
notified nobody, and the only copy of the work sat in a dead holder's directory
until an operator happened to read a status line. That gap is the defect.

## 3. The fleet is NOT blocked — measured

- master moved **today**: `eb802d64` 19:12, merges landing all afternoon
  (16:25 → 19:12), the merger's own `[merger]` pass reports `merged=` counts.
- `gopi merger --status` reports `pending: 7 branch(es) the next sweep would
  merge`, and 6 of those 7 are genuinely ahead of master
  (`feat/scratch-base-residue` 4, `feat/stylegate-green` 2, `feat/tmpdir-settings-value` 2,
  `fix/binary-summary-guard` 2, `fix/scratch-gc-queue-namespace` 3,
  `fix/worktree-residue-closeout` 1; `fix/memory-base-worktree-policy` is
  ahead=0 = already in master, i.e. tidy-only).

**Answer: the three held branches are a separate population, not a lane
blocker.** They are excluded from the candidate set individually; everything
else merges normally on the next tick.

## 4. The proposed remedy would DESTROY work — measured, not argued

The briefing proposes: *archive the conflict branch and submit a task to
explain*. Measured with `git diff HEAD | git apply --check` against master:

| worktree | verdict |
|---|---|
| `/home/user/workspace/gopi-brew-fix` | dirty edits **apply cleanly to master** → 97 insertions incl. an untracked test file = **work not in master** |
| `/home/user/workspace/gopi-textguard` | dirty edits **apply cleanly to master** → 6 files, −278/+89 = **work not in master** |
| `/home/user/workspace/gopi-web-ip` | drifted (branch 1 ahead, patch overlaps) |

Archiving is the right disposal for a branch that **failed to merge**; it is
wrong here, where two of the branches are unmerged *because they are held*, and
their only copy of the work is a dirty directory. `git worktree remove` /
`branch -D` on them = irreversible data loss. The reclaim's guard exists
precisely to prevent that, and this fix does not weaken it.

## 5. What this fix delivers

`internal/tools/dirty_holder_salvage.go` (+ wiring):

- **detect + dispatch** — the merger sweep, after computing `Held`, calls
  `SalvageDirtyDeadHolderWorktrees(repo, res.Held)`. For each branch the arm
  refuses because it is DIRTY, ONE salvage row is filed
  (`salvage uncommitted work: <branch>`, tags `salvage,worktree`, source auto),
  deduped through the same live-summary probe the branch-rework lane uses.
- **capture (durable)** — before the row, the arm writes
  `tracked.patch` (`git diff HEAD --binary`, binary-safe, so an image/golden
  file applies again) and `untracked.tar.gz` (the untracked files — a diff can
  never show a NEW file; stdlib tar+gzip, zero dependency) into the scratch
  tree, and submits them as task **attachments** (TaskCopyAttachments archives
  them into the task's session dir). Each file's sha256 is in the briefing and
  the report line.
- **never** — no commit, no clean, no removal, and only read-only git commands
  run inside the worktree. The operator's promise holds literally.
- **honest by construction** — a failed capture files NO row (a row claiming a
  capture that did not happen is a lie, Rule 15); a `Held` reason that is not
  the DIRTY guard is not salvaged; the kill switch
  (`GOPI_DIRTY_HOLDER_SALVAGE`) reports the skipped dispatch instead of going
  silent; the sweep summary carries `dirty-salvage=<n>` and prints every line.
- The salvage row does NOT decide whether the work is wanted — that is the
  row's job, and it may close **superseded with a one-line reason** (the
  ordinary close), which is the cheap exit for the two branches whose work
  already looks re-applied in master.

### Without waiting for the merge

Because the merge of this branch cannot land while this session runs, the live
three were salvaged through the **public path** (no code from the fix needed to
do it) — evidence, not a promise:

```
01a0a504-bcd1-7d27-bb5f-f9232137167e  fix/gopilog-textguard  attachments: tracked.patch 24658 B, untracked.tar.gz 26675 B
01a0a504-d067-74dc-8e79-d84c52bda811  fix/brew-call-retry    attachments: tracked.patch  6788 B, untracked.tar.gz  2940 B
01a0a504-d7a3-78d0-a1c8-61627c976d75  feat/web-lan-access    attachments: tracked.patch  4163 B, untracked.tar.gz    45 B
```

(`deliverable` carries `repo` + `branch` + `worktree`; the capture was taken
read-only and the three worktrees were verified byte-identical afterwards:
2 / 3 / 15 porcelain entries and unchanged HEAD, before and after.)

The brew row's untracked archive was extracted and read back
(`package tools … "gopi brew --dry-run --limit 4" run surfaced (2026-09-14)`),
so the capture is verified content, not a byte count.

## 6. Verification

- `CGO_ENABLED=0 go build ./...` → exit 0.
- New tests (6) in `internal/tools/dirty_holder_salvage_test.go` — ALL PASS:
  `FilesARowWithBothHalvesCaptured` (row + patch content + untracked content read
  back out of the archive), `LeavesTheWorktreeUntouched` (porcelain + HEAD
  identical before/after), `DedupsToOneRow`, `IgnoresOtherRefusalReasons`,
  `KillSwitch_ReportsWithoutFiling`, `MergerSweepCarriesTheSalvageLines` (wiring).
- **Red proof, A/B, same file on both trees**
  (`internal/tools/zz_redproof_dirty_holder_salvage_test.go`, written against the
  pre-fix API only):

  ```
  # baseline eb802d64 (detached worktree /home/user/workspace/gopi-salvage-redproof)
  --- FAIL: TestZZRedProof_DirtyDeadHolderWorkIsOrphaned
      ORPHANED WORK: the merger refused the dirty dead-holder worktree of feat/dead-holder
      (holder provably dead, branch feat/dead-holder) and filed NO task for its uncommitted
      work — its only copy is that directory
  # this branch
  --- PASS (same test, same fixture)
  ```
  Both runs report the SAME precondition line
  (`dead-holder reclaim … skipped: worktree is DIRTY … left for a human`) — the
  two runs differ by the fix alone.
- Full-package regression: `go test -count=1 ./internal/tools/ ./internal/standards/
  ./internal/stylegate/ ./cmd/agent/` — see §8 for the run's result.

## 7. Not done (honest)

- **The live three are dispatched, not resolved.** The salvage rows are
  `pending`; whether each branch's work is wanted is a judgement the row's
  claimant makes (and for `fix/brew-call-retry` / `fix/gopilog-textguard`, whose
  branches are already in master, "superseded" is the likely verdict). No branch
  was committed, cleaned, archived or deleted by this task.
- **`feat/web-lan-access` (1 commit ahead) stays held** until its reworker
  commits or the operator decides; its patch drifted from master, so it is a
  genuine judgement call, not a mechanical re-apply.
- **No notification was added** for the salvage dispatch: the row plus the
  merger-log lines are the current surface. Wiring a `task-notify` event for it
  is a follow-up if the operator wants a push rather than a log.
- **The arm runs from the merger pass only** (the same host cover as the
  reclaim leg). A host running neither merger nor worker still renders the
  refusal and dispatches nothing — unchanged from before.

## 8. Build / Test / Commit

- **Base: rebased onto current master `72c543c7`** (branch head is now `ae3f0999`,
  same patch). The red proof in §6 was taken against `eb802d64`, which is still the
  honest "before" — and on that base the repo had *pre-existing* reds
  (`internal/standards` Rule-16 ledger, `internal/stylegate` errorf-verb ceiling 9/10)
  that master fixed while this task ran (`50413d89`, `72c543c7`). After the rebase both
  are green on this branch.
- Build: `CGO_ENABLED=0 go build ./...` → exit 0.
- Tests, each command run on **both** trees (branch and a clean master checkout),
  so every remaining red is attributable:
  | command | branch | master |
  |---|---|---|
  | `-run 'Salvage\|DirtyHolder\|RedProof' ./internal/tools/` | **ok** (6/6 PASS + red proof PASS) | (pre-fix: red proof FAIL — the red) |
  | `./internal/standards/` | **ok** | ok |
  | `./internal/stylegate/` | **ok** | ok |
  | `./internal/tools/` | FAIL: `TestMergeBuildGate_SharedModCacheResolvesWithProxyOff` | **identical FAIL** |
  | `./cmd/agent/` | FAIL: `TestWorkerDaemon_*` (11) | **identical FAIL** |
  The two remaining failures are **sandbox/environmental with no module proxy and no
  reachable detached-daemon start** — measured identical on the untouched baseline, so
  they are not this change's business (and are not silently claimed as green here).
- Commit: `git log -1` on `fix/dirty-holder-salvage-dispatch`.
