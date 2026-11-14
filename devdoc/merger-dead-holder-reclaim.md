<!-- doc-class: historical -->
# Merger dead-holder reclaim — can the merger tell a branch is dead?

Epic: 01a09848-b3ff-78ef-95c4-fb80bc56aae5 (attachment `/tmp/dead_branch.txt`).
Branch `fix/merger-dead-holder-reclaim`, worktree `~/workspace/gopi-dead-branch`.

## 1. What the operator saw

```
repo:        /home/user/work/GoPi
  protected: 20 branch(es) held by a live worktree (dead holder: 7 [docs/glm-night-watch-verify-01a092a6,
             feat/peak-hour-guard, feat/version-meaningful-tag, feat/voice-e2e-hp, fix/merger-rebuild-selfrestart,
             fix/merger-status-doc-verb, fix/task-show-run-cmd-verbatim] — reclaimable by the finish-cleanup sweep)
  pending:   2 branch(es) the next sweep would merge
```

Two open questions in the briefing: (a) can the merger detect a branch is
dead, and (b) may it drop worktree protection and merge it?

## 2. What "dead" actually means in the code (two DIFFERENT claims)

| claim | source | evidence |
|---|---|---|
| **protected** | `repoSweepProtectedBranches` (`internal/tools/repo_sweep.go:241`) | the branch is checked out in a **linked worktree of the repo** (`git worktree list`). Pure git occupancy — the ledger is not consulted. |
| **dead holder** | `DeadHolderBranches` (`internal/tools/task_reclaim.go:703`) | the ledger attributes the branch to a row whose **claim pid is provably gone** (`reclaimDeadHolderCandidate` → `claimHolder` + `pidAlive`; `internal/tools/task_reclaim.go:611`) **and** git confirms the row's own directory holds the branch. A foreign/blank host stamp is "unknown", never "dead". |

So yes — the merger **does** detect death, and it detects it conservatively (pid
layer only; a stale heartbeat is never treated as death). But “dead holder” is a
claim about the **holder**, not about a **release**. The release needs the
reclaim arm's three guards (`reclaimDisposition`, `internal/tools/task_reclaim.go:254`):

1. ① the ledger names the branch on a row whose claim pid is dead;
2. ② the directory is **CLEAN** (`git status --porcelain` empty) — a dead
   holder's uncommitted work is still work, "left for a human";
3. ③ no live process has its cwd inside the directory.

`ReclaimDeadHolderWorktrees` removes the **DIRECTORY** (never the branch — the
branch is the deliverable). With the directory gone, `git worktree list` no
longer names the branch, so `scripts/safe-merge.sh` gate [c] (`scripts/safe-merge.sh:62`)
passes and the very next sweep picks the branch up as ordinary merge input.

## 3. Root cause of the 7 stuck branches (measured, not inferred)

The rerun of the arm's own disposition over the **live** ledger (read-only probe,
`ReclaimDeadHolderWorktrees(false)` on the host, 2026-09-13) says:

```
LINE: dead-holder worktree reclaimable: fix/merger-status-doc-verb (01a09264-…, peerbox:2822463)
skipped: docs/glm-night-watch-verify-01a092a6 — worktree is DIRTY (uncommitted work) — left for a human
skipped: fix/merger-rebuild-selfrestart      — DIRTY
skipped: feat/version-meaningful-tag          — DIRTY
skipped: feat/voice-e2e-hp                    — DIRTY
skipped: fix/task-show-run-cmd-verbatim       — DIRTY
skipped: feat/peak-hour-guard                 — DIRTY
```

**6 of the 7 are DIRTY** → guard ② refuses them **by design**, so they were
never going to be reclaimed no matter how often the arm ran. The 7th
(`fix/merger-status-doc-verb`, worktree `~/workspace/gopi-merger-status`, clean)
was freeable all along — but the arm was only reachable from
`serveFinishCleanupSweep`, a **WORKER** lifecycle pass
(`cmd/agent/cli_tasks_worker.go:394`, 10-min throttle), and the host ran the
**merger** and no worker: 1h26m of 5-minute merger ticks, `protected=20
(dead holder: 7)` repeated in `~/.gopi/logs/merger.log` (lines 7560–7600),
**zero reclaim passes**.

Contributing facts measured at the same time:

- Branch geography: of the 7, **3 are already in master** (`feat/peak-hour-guard`,
  `feat/version-meaningful-tag`, `feat/voice-e2e-hp` — `master..branch` = 0), so
  their protection blocks only the tidy/delete path; the other 4 carry
  unmerged work (1–3 commits ahead) that their dirty worktrees block.
- The "reclaimable…" phrasing was therefore **doubly false**: for 6 branches the
  guard refuses them (permanent, until a human), and for the 7th the arm that
  was advertised is not the arm that runs on this host.

## 4. Other situations that strand a branch (all found in code, several measured)

| situation | who withholds the branch | recoverable? |
|---|---|---|
| holder died, directory **clean** | gate [c] + protected arm | yes — the reclaim arm frees the directory |
| holder died, directory **dirty** (the live majority) | gate [c] + protected arm | **human only** — guard ② is deliberate: uncommitted work is recoverable work |
| directory **vanished**, registration left | gate [c] (grep only reads the registration) | yes — `git worktree prune` repair (`reclaimVanishedProven`) |
| live-status row (`in_progress`) whose local holder is dead (orphan) | `taskLiveHolderStatus` made the row a holder by design | directory freed by the live-status arm; the **row stays open** until `task sweep-zombies --kill` or a human close-out |
| foreign host stamp (another machine claimed it) | "unknown" ≠ "dead" | no — never called dead by design |
| worktree re-attached a DIFFERENT branch | guard ① | no — refuses to free a directory the row does not own |
| path exists but is not a linked worktree of the row's repo | registration probe | no — refuses (somebody else's ground) |

Crash vs. dirty: a crashed/killed gopi leaves the claim stamp and the worktree
behind; the pid layer is what proves the crash (a killed wrapper's pid is gone).
The **work** is not resumed automatically — gopi's model is *the branch is the
deliverable*: if the branch carries commits, the merger merges it after the
directory is freed; if the work never reached a commit, it is only in the dirty
directory, and `gopi task reopen <id> --reason …` re-issues a NEW pending row
(status `reopened` is retired, `cmd/agent/cli_tasks.go:332`) — the row is a
pointer, the commits are the work. So: gopi resumes the *task*, never the
*tree*; an uncommitted tree is deliberately a human's call.

## 5. Fixes delivered on this branch

1. **`fix(merger): run the dead-holder worktree reclaim from the merger pass`**
   (`08bf5fd6`) — the arm is now reachable from `mergerPassOnce`, before the
   repo sweep. Regression test `TestAcceptance_MergerPassReclaimsDeadHolder`
   (red pre-fix: `merger reclaim lines: []`).
2. **Honest verdict** (this commit) — the protected arm no longer promises a
   release it cannot deliver:
   - `protectedDeadHolderFmt` says *"the holder is gone, the worktree still
     blocks the merge"* instead of *"reclaimable by the finish-cleanup sweep"*;
   - new `ReclaimHeldBranches(repo)` (`internal/tools/task_reclaim.go`) returns,
     per branch, **why** the reclaim will not free it — the arm's own
     `reclaimDisposition` reason (dirty above all), or *"its directory is gone
     and only the git registration is left"*;
   - both merger front doors render it (`protectedHeldNoteFmt`, via
     `describeHeldNote` → `DescribeProtected` for the merger log and the
     `gopi merger --status` repo block);
   - regression test `TestDeadHolderHonestVerdict_RefusesTheReclaimableClaim`
     asserts both directions (dirty ⇒ named as still-refused *with* the reason;
     clean ⇒ NOT reported as refused).
   Live render after the fix (same host, same 7 branches) becomes:
   `protected=20 (dead holder: 7 […] — the holder is gone, the worktree still
   blocks the merge) (still refused: N [branch — worktree is DIRTY …])`.

## 6. Answer to the briefing's policy question

> what is best approach when merger faces large number of branches waiting for
> merging, bug-protected by worktree?

1. **Never drop protection to make a merge possible.** Protection is the only
   signal separating "active work" from "abandoned work"; git itself will merge
   a checked-out branch, so this guard is load-bearing. The current design
   (withhold + **name the reason**) is the right one — the missing half was
   honesty, not permissiveness.
2. **Split the population by the arm's own verdict** (now possible from a single
   line): *clean dead holder* → the arm frees it, nothing for a human;
   *dirty dead holder* → a human must commit/stash/`reopen`; *live holder* →
   untouched.
3. **A dirty dead-holder directory is a work-loss risk**, not merely a stuck
   merge: 6 such directories sit on this host right now. The honest line turns
   that into an actionable operator list instead of an anonymous count.
4. **Run the reclaim wherever a gopi process is resident** (delivered): a host
   running only the merger must not depend on a worker pass for a repair its own
   log advertises.

## 7. Live render after the fix (same host, same 7 branches — the operator's answer)

`bin/gopi merger --status` built from this branch, run on the 2026-09-13 host:

```
repo:        /home/user/work/GoPi
  protected: 21 branch(es) held by a live worktree (dead holder: 7 [docs/glm-night-watch-verify-01a092a6,
             feat/peak-hour-guard, feat/version-meaningful-tag, feat/voice-e2e-hp, fix/merger-rebuild-selfrestart,
             fix/merger-status-doc-verb, fix/task-show-run-cmd-verbatim] — the holder is gone, the worktree
             still blocks the merge) (still refused: 6 [docs/glm-night-watch-verify-01a092a6 — worktree is
             DIRTY (uncommitted work) — left for a human; feat/peak-hour-guard — …; feat/version-meaningful-tag
             — …; feat/voice-e2e-hp — …; fix/merger-rebuild-selfrestart — …; fix/task-show-run-cmd-verbatim — …])
  pending:   2 branch(es) the next sweep would merge
```

Read it as an operator: 7 branches have a dead holder; **6 of them will not be
freed by anything gopi runs** (uncommitted work — a human must commit, stash or
discard); the 7th (`fix/merger-status-doc-verb`, clean) is correctly NOT in the
refused list — the merger pass now frees it (fix 1). The old line's
"reclaimable by the finish-cleanup sweep" is gone from both front doors.

## 8. Verification

- `CGO_ENABLED=0 go build ./...` → exit 0; `gofmt -l internal cmd` → empty.
- `CGO_ENABLED=0 go test -count=1 ./internal/tools/ ./cmd/agent/` →
  `ok github.com/nick/gopi/internal/tools 98.082s` / `ok github.com/nick/gopi/cmd/agent 205.204s` (exit 0).
- **Red proof, A/B, real artifact** (`internal/tools/zz_redproof_reclaim_promise_test.go`,
  written against the PRE-FIX API only so one file compiles on both trees):

  ```
  # pre-fix tree
  git worktree add --detach /tmp/gopi-redproof-01a09848 15ffde2f
  cp internal/tools/zz_redproof_reclaim_promise_test.go /tmp/gopi-redproof-01a09848/internal/tools/
  cd /tmp/gopi-redproof-01a09848 && go test -count=1 -run TestZZRedProofFalseReclaimPromise ./internal/tools/
  --- FAIL: TestZZRedProofFalseReclaimPromise
      FALSE PROMISE: a DIRTY dead-holder worktree is refused by the reclaim arm's guard ②,
      yet the operator line promises a release:
      "protected=1 (dead holder: 1 [feat/dead-holder] — reclaimable by the finish-cleanup sweep)"

  # this branch
  cd ~/workspace/gopi-dead-branch && go test -count=1 -run TestZZRedProofFalseReclaimPromise ./internal/tools/
  --- PASS
  ```
  The dirty-worktree precondition is the same in both runs (a file written into
  the linked worktree), so the two runs differ only by the fix.
- Live evidence: the `merger --status` render above; merger.log lines 7560–7600
  (1h26m of the old promise); read-only reclaim probe (`remove=false`)
  dispositions listed in §3.
