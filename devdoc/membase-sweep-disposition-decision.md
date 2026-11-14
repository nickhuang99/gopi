<!-- doc-class: historical -->
# Task Report: master-red `TestMemoryBaseKeepArmsOverrideTheNewRoot` / `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` — closed by merge, and the disposition decision recorded

Task: `01a0a88f` (briefing named failed task `01a0a429`).
Date measured: 2026-09-16.

## Headline

**Nothing is red on master any more, and no code change to the sweep was needed
for the two named tests.** The briefing is a STALE snapshot: it pinned master tip
`b6479088` (2026-09-16 12:51), while the repair landed on master at **14:05 the
same day** as `dc9b20b5 Merge branch 'fix/worktree-sweep-finding-root-stamp'`
(plus its test-tightening commit `d2faeeaf`, 14:02).

The only thing still genuinely open from that epic was its own explicit demand:

> the current expected value is `report`, the produced one is `escalate` —
> **decide which is right and prove it, do not just flip the assertion to match
> the output.**

The assertion was flipped by `d2faeeaf` with a reasoned argument in commit/PR text
but **no landing document**. This task supplies the missing record (see
[The decision](#the-decision-escalate-is-correct)) and the independent proof.

## Completed

1. **Reproduced the briefing's failure on its own pin, and located the repair.**
   Fresh extracts, identical command to the briefing:

   ```
   pre-fix  master 64b11499 (parent of the merge)  → 2 FAIL, message byte-for-byte the briefing's
   current  master 0dac2ae7                        → PASS
   ```
   `b6479088` is an ancestor of master (`git merge-base --is-ancestor`), so the
   briefing was not wrong — it was read before `dc9b20b5` merged. No action.

2. **Proved the disposition decision** (`escalate` over `report`) from the code's
   own reachability argument, an independent live observation, and the test
   suite — not from the assertion. Recorded below.

3. **Verified the whole sweep suite**, not just the two named tests — 133 test
   runs across `TestMemoryBase|TestWorktree|TestSweep|TestOrphan|TestReclaim`:
   `ok github.com/nick/gopi/internal/tools 30.870s`.

4. **Verified the fix end-to-end on the REAL binary** (`bin/gopi`, built from master
   14:24) — `gopi task sweep-worktrees --dry-run` on the live host now prints a
   rule and a root for every finding, including the out-of-policy arm that used to
   say "the finding names no root":

   ```
   keep [live-holder] /dev/shm/…/gopi-verify-2954040037/wt
        judged against root: scratch /dev/shm/gopi-test-scratch [scratch home]
   report [declared-root-scope] /tmp/fix-prefix-loop
        (outside every declared root (consulted: worktree /home/user/workspace
         [default <home>/workspace], scratch /dev/shm/gopi-test-scratch
         [scratch home], memory-base /dev/shm [memory base measured from the
         scratch home], fixture …/tmp [system temp root], session
         /home/user/work/GoPi [session working directory]) — … to bring it into
         policy)
        judged against root: /dev/shm [memory base measured from the scratch home]
   ```

   This is the deliverable the failed epic was about: the previously
   ungovernable `/dev/shm` family is inside policy **and** the refusal that leaves
   something outside policy now names every root that was consulted.

## The decision: `escalate` is correct

**Question.** A worktree that is DIRTY (uncommitted work) and owned by NO ledger
row and past the age floor: `report` or `escalate`?

**Answer: `escalate`.** The two dispositions are not degrees of the same thing —
they make different claims, and only one of them can be true here.

| | what `report` claims | what `escalate` claims |
|---|---|---|
| **subject** | a dirty tree **whose owner row exists** | a dirty tree **nobody owns** |
| **who acts next** | the owner (or the reclaim arms when they commit) | **nobody automatic** — a human |
| **tree** | untouched | untouched |

The proof is a **reachability** argument, and it is a property of the ladder, not
of the output (this is why flipping the assertion to match `escalate` would have
been wrong if the argument had failed):

1. The producer pass `ReclaimOwnFinishedWorktrees` requires a row keyed on *this
   host's own claim*;
2. the dead-holder arm requires a **terminal** row;
3. the age arm's guard ② (`reclaimDisposition`) **refuses any dirty tree**.

With **no row at all**, none of the three can ever fire. So for this exact shape
`report` does not mean "somebody will deal with it" — it means **an infinite
park**: the tree pins the branch, the branch pins `scripts/safe-merge.sh` gate
[c], and nothing on the host reports it again. That is the `report` claim being
**false**, not merely weaker. Escalation is the disposition that tells the truth:
the only actor who can end it is a human, and the plan must say so.

Corroboration, independent of the ladder:

- **Live, on this host**: `gopi task sweep-worktrees --dry-run` (master binary)
  emits `report [dirty-worktree]` for `/home/user/workspace/gopi-firstopen` and
  `gopi-master-baseline` — trees that a live holder/row situation still covers —
  i.e. the ladder does not escalate dirt in general. The escalation arm is
  reserved for the unowned shape.
- **Safety is unchanged either way**: both dispositions leave the tree and its
  branch in place (no `--force`, ever). The devdoc of the landing commit states
  the invariant: "The never-destroy invariant it asserts (no reclaim) is
  unchanged — nothing is removed either way."

The escalation is not a silent one: the arm adds an operator notify
(`worktreeSweepNotifyOrphan`, event `worktree-orphan`, once per (repo,path) per
process) so the condition **leaves the log** — Rule 15 (no silent failure).

**Where the decision is recorded in code**: `worktreeEscalateOrphan`'s doc
comment (`internal/tools/worktree_sweep.go:218-228`) and the rung-③O comment
(line ~810) both state the reachability argument verbatim. This devdoc is the
standalone record; the earlier gap was that the *rationale existed only in a
commit message*.

## Not Done (honest)

- **No code change was made by this task**, because none was needed: master at
  `0dac2ae7` already fixes the two tests. The task's genuinely-missing artifact —
  the written decision record — is what this branch lands.
- The briefing's own premise ("master red") is therefore **false at its
  verification time**; the finding is that the briefing pinned a tip that was
  superseded 74 minutes later, not that anything is broken. If the operator wants
  the red re-checked, the pin must be re-resolved to the tip — no sweep code
  change will make a stale pin green.
- The failed task row `01a0a429` is **not retrievable** on this host
  (`gopi task show 01a0a429` → `no task with task_id`), so its report could not
  be read; the mechanism was reconstructed from branch/commit history instead
  (`1e553d7f wip(sweep) … 2 assertions unmet` → `a7ea026c` → `d2faeeaf` →
  `dc9b20b5`).

## Verification

```
# Repro on the briefing's own pin (pre-fix): FAIL, message identical to the briefing
$ cd /dev/shm/membase-redproof-64b11499   # git archive 64b11499
$ go test ./internal/tools/ -run 'TestMemoryBaseKeepArmsOverrideTheNewRoot|TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy' -count=1
--- FAIL: …/dirty_⇒_kept_(uncommitted_work_is_never_destroyed)
    disposition = escalate (ORPHAN WORKTREE: …/gopi-verify-guarded (branch fix/verify-guarded, age 72h) is DIRTY and NO ledger row owns it — the producer pass needs a row keyed on this host's own claim, the dead-holder arm needs a terminal row, and the age arm refuses dirt, … want report
--- FAIL: TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy
    worktree_sweep_memory_base_test.go:350: the finding names no root — the operator cannot see what was consulted
FAIL github.com/nick/gopi/internal/tools 2.584s

# Current master, same command: PASS
$ cd /dev/shm/membase-verify-Mzuc       # git archive master (0dac2ae7)
$ go test ./internal/tools/ -run 'TestMemoryBaseKeepArmsOverrideTheNewRoot|TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy' -count=1
ok  github.com/nick/gopi/internal/tools 0.668s

# Whole sweep family on current master
$ go test ./internal/tools/ -count=1 -run 'TestMemoryBase|TestWorktree|TestSweep|TestOrphan|TestReclaim'
ok  github.com/nick/gopi/internal/tools 30.870s      (133 test runs)

# Live end-to-end, real binary built from master
$ ./bin/gopi task sweep-worktrees --dry-run
sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  … every finding prints "[rule]" and "judged against root: <path> [<source>]"

# Build
$ CGO_ENABLED=0 go build ./...          # OK (no source change on this branch)
```

Commit: *(this branch — see `git log -1 --format=%h` in `docs/membase-sweep-disposition-decision`)*.

## Deliverable declaration

- Branch: `docs/membase-sweep-disposition-decision` (worktree
  `/home/user/workspace/gopi-membase-decision`, based on master `0dac2ae7`)
- Recover: `git show docs/membase-sweep-disposition-decision:devdoc/membase-sweep-disposition-decision.md`
- The commit lives in the shared main repo object store (worktree, no push
  needed).
