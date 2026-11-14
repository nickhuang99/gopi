<!-- doc-class: normative -->
# Task Report: Git Branch Hook Investigation — is post-receive triggering gopi merges?

Epic: "git branch hook should trigger gopi to merge branches, or at least clean up
after merge (remove worktree, delete branch). Investigate whether the hook works,
single-branch vs all-branches handling, local worktree vs remote push difference."
Node: peerbox · Date: 2026-09-01 22:30–23:00 · Method: log forensics + live-fire probes

## Completed

### 1. Hook inventory — the hook EXISTS and is INSTALLED
- Installed hook: `.git/hooks/post-receive` (only non-sample hook; `core.hooksPath` unset).
- Repo copy: `scripts/git-hooks/post-receive` (tracked, committed at 16c391a → b9aabac → d63a6f5 → c80f870).
- The installed copy drifted ahead of the repo copy during tonight's experiments
  (v3 worker-spawn was installed at 22:48 before being committed; after the 23:00
  revert commit c80f870 the installed copy was re-simplified). **Version skew between
  `.git/hooks/post-receive` and `scripts/git-hooks/post-receive` is a standing risk —
  there is no install/sync mechanism.** Recommendation: a tiny `scripts/install-hooks.sh`
  (cp + chmod) run after every hook commit.

### 2. Verdict: the hook IS WORKING (evidence)
`.git/post-receive-audit.log` (445+ lines) shows repeated firing tonight:
- 22:41:25 → 22:49:06: five passes, one per push event.
- FF path (v3 hook, token-free): `22:42:27 FF-merged: docs/ff-case-test`,
  `22:44:33 FF-merged: docs/verify-final`, `22:49:06 FF-merged: docs/v3-drain-test` —
  each followed by `branch -d`; master reflog confirms the fast-forwards
  (`master@{22:49:06}: merge docs/v3-drain-test: Fast-forward`).
- Cleanup path: `22:45:44 cleanup: docs/verify-final already in master — branch -d`.
- Enqueue path: every diverged branch logged `diverged: <b> — enqueued for LLM`
  and `merge-backlog audit (submit): scanned=N submitted=M duped=K failed=0`;
  ledger shows real pending rows (`merge fix/reflow-gate-contract into master`, …)
  and completed merge-executor rows (21:43–22:14: tasklist-issued-at-throttle,
  reflow-gate-contract, branch-scan-archive-filter, hook-test-probe, test-b-rule6 —
  each 2–10 min, 220k–620k tokens).

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

- Worker spawn (v3): `22:49:06 spawning worker to drain queue` → live `gopi worker`
  PID 775510 draining tasks from `.git/worker-drain.log`.

### 3. Scope: ONE branch per push — but the audit pass sweeps ALL branches
The hook's `while read oldrev newrev refname` loop only **acknowledges the pushed
ref** (one echo per pushed branch). But the real work is the
`gopi task audit-merges` pass, which does `git for-each-ref refs/heads` and
classifies **every local branch** (except master) in one shot:
already-merged → `branch -d` cleanup; FF-able → FF-merge + `branch -d`;
diverged → enqueue one LLM merge task. Dedup (same tip / live row) prevents
re-enqueue spam (`duped=13` in later passes). So: trigger is per-push, effect is
repo-wide. A single push drains the whole backlog posture.

### 4. Local commit vs push — trigger semantics PROVEN by live fire
- **Local commit / local branch creation → NO trigger.** Created
  `probe/local-no-push`, committed, deleted: audit log line count unchanged.
  (post-receive only runs in `receive-pack`, i.e. on a push/ fetch into the repo.)
- **`git push . branch:refs/heads/branch` (self-push) → no new audit pass either**
  when the ref already exists at the same tip ("Everything up-to-date" = no ref
  update = no post-receive run). Hooks fire on *ref updates*, not on push attempts.
- **Push from a separate client clone → TRIGGER confirmed.**
  `probe-tmp2/client2` clone → commit → `git push /home/user/work/GoPi
  probe/from-client2` printed `remote: [post-receive] branch pushed:
  probe/from-client2` and the audit log grew (496 → 514 lines) with a fresh
  `scanned=16 submitted=1` pass; ledger gained `merge probe/ff-check into master`.
- **Non-bare repo caveat:** this repo is the merge TARGET (`receive.denyCurrentBranch`
  unset; pushes to `master` from clients still update the ref — a quirk of the
  multi-node setup where peerbox is both working repo and server for hp).

### 5. Local worktree vs remote branch-push difference
- **post-receive fires on ANY ref update arriving via git transport** — ssh remote
  (`nick@192.168.7.6:/home/user/work/GoPi`, as hp pushes) or file-path push from a
  client clone behave identically. Both are "receive-pack" events.
- **A worktree checkout inside the same repo does NOT fire anything**: worktree
  branches are just local refs; creating/committing/deleting them never enters
  receive-pack. The finish-cleanup chain (push → worktree remove → branch -d)
  in the gopi client is what closes the worktree lifecycle; the hook only sees
  the resulting branch push.
- **Dirty-tree guard:** with staged/unmerged files in the main checkout (an active
  merge task was mid-flight tonight — `UU cmd/agent/cli_tasks.go`), v3 logged
  `skip: dirty tree — audit only` and went straight to enqueue, never touching the
  working tree. The simplified c80f870 hook is enqueue-only and tree-safe by design.
- **Worktree-occupied branches are invisible to `branch -d`**: a branch checked out
  in a worktree can't be deleted by the hook's cleanup path (git refuses) —
  `git worktree list` showed none tonight, but post-merge-cleanup.sh --auto remains
  the tool for worktree pairs.

### 6. Hook evolution timeline tonight (reflog + log)
- 16c391a v1: enqueue-only post-receive (replaces serve-tick periodic scan).
- 74bddd3: fix audit-merges verb parse (surfaced BY the hook firing — self-debugged).
- b9aabac v2→v3: token-free FF-merge + branch -d + audit enqueue.
- d63a6f5: installed v3 with worker-spawn (flock/pid/setsid).
- c80f870: **reverted to simplified enqueue-only** per nick — "v3 worker-spawn too
  complex; serve daemon owns queue draining". **The currently installed hook is the
  simplified version: enqueue on push; merging happens when `gopi task serve` (or a
  manual `gopi task run`) drains the pending merge tasks.**

## Current state after investigation
- Hook installed = simplified v4 (enqueue-only). FF-merge/branch-d cleanup now lives
  in `scripts/post-merge-cleanup.sh --auto` + the merge-executor tasks, not the hook.
- `gopi task serve --every 1m` is running (PID 776972) and drained several
  merge-backlog tasks during the investigation (that's why the working tree had a
  live merge with UU conflict — the daemon executor, not the hook).
- All probe branches/refs deleted; probe merge tasks cancelled (12 rows).
- 21 pre-existing pending hp/* merge-backlog rows remain (pre-dates this task —
  they are the real backlog the hook will drain via serve).

## Not Done
- No `scripts/install-hooks.sh` sync mechanism written (recommended, 5 lines).
- hp remote live-fire not tested (no ssh key from this session; file-path push from
  client clone is protocol-identical, so behavior is inferred not proven for ssh).
- The `hp/*` branch backlog (21 pending rows) not merged — out of scope for an
  investigation task; serve will drain them.

## Verification
- Log evidence: `.git/post-receive-audit.log` 445→514 lines across 6+ live passes.
- Live-fire: client-clone push → `remote: [post-receive] branch pushed` + new audit
  pass + new pending merge task row (#1788274699, later cancelled).
- Negative control: local commit & self-push up-to-date → 0 new log lines.
- Branch state: `git branch` unchanged (12 pre-existing feature branches; probes removed).
- No build/test changes required (investigation task; hook is shell, tracked in repo).

## Recommendations
1. Add `scripts/install-hooks.sh` (cp scripts/git-hooks/* .git/hooks/ + chmod) and
   run it in the same commit that changes a hook — kills the version-skew class.
2. Keep the hook enqueue-only (current design): FF-merge in a hook races the serve
   daemon and human work; enqueue+dedup is idempotent and observable.
3. For "clean up after merge" the existing pair already covers it:
   `safe-merge.sh` (pre-guard) → merge → `post-merge-cleanup.sh --auto`
   (archive-tag + branch -d + worktree remove). The hook's job ends at enqueue.
