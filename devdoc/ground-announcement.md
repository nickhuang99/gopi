<!-- doc-class: normative -->
# The Ground Is Announced — Or Every Run Builds a Spare Worktree

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** landed 2026-09-25 (`feat/ground-announcement`)
**Supersedes the diagnosis in:** `briefings/brief-ground-is-made-and-never-announced.md`
(kept: the correction in its own `CORRECTION 2026-09-24` section is what this
devdoc implements)

---

## The mechanism, in one paragraph

The worker creates a per-task linked worktree — **the ground** — *before* it
spawns the executor child, and sets `cmd.Dir` to it
(`tools.TaskRunGroundFor`, called at the ONE spawn seam,
`cmd/agent/cli_tasks.go`). **The child does work there.** But nothing *announced*
the ground, while PROJECT.md Rule 6a tells every run to create "a worktree for YOUR
BRANCH" (`~/workspace/gopi-<descriptive>`, branch `fix/<descriptive>`). The
ground's branch is `task/<uuid>`, which matches **nothing** in Rule 6a's literal
test — "does a worktree for MY branch exist?" — so the answer was *no*, and the
run created one. The result, measured live: **one task → two refs, two
worktrees**; the child working in the ground, and an orphaned **spare** left
behind that no row names and no reclaim sweep collects. The spare is not a
mistake by the child; it is Rule 6a obeyed exactly.

**The name of the loop, for the search that finds this doc later:**
announce-or-the-child-builds-a-spare.

## Why the fix is an announcement and not a mechanism

- The ground is where the work actually happens (measured: the child's
  `/proc/<pid>/cwd` was in the ground for the whole run). **Deleting the ground
  would re-open the 2026-09-23 defect** — a child starting in the repo's MAIN
  checkout. Resolution B was rejected for that reason.
- The runner cannot pass a *descriptive* branch name: the name depends on work
  the child has not done yet. So the ground can only ever be a placeholder
  **unless the run may rename it** — which is why the fix blesses `git branch -m`
  instead of forbidding a descriptive name. Forbidding it would send the run
  straight back to Rule 6a's creation step.
- Nothing may *enforce* "no second worktree" in Go without breaking the
  no-ground case: Rule 6a's creation path must keep working for every run the
  seam did not spawn (an operator's shell, a nested gopi, a repo-less row). The
  only agent that can decline to build the spare is the one holding Rule 6a —
  i.e. the model. **The prose is the mechanism.**

## What was changed

| surface | change |
|---|---|
| `internal/tools/task_run_ground.go` | `TaskGroundAnnouncementClause(dir, branch)` + `TaskGroundAnnouncementLabel` (the ONE spelling of the briefing block); the env-name pair `TaskGroundDirEnv` / `TaskGroundBranchEnv`; the reader `TaskGroundAnnounced()`; `GroundBranchLabel` |
| `cmd/agent/cli_tasks.go` | at the ground-resolution seam: bind the clause into the briefing (before the spawn), append the env pair, and print the announcement record |
| `cmd/agent/task_child_env.go` | `taskGroundAnnounceLineFmt` — the round-log record that the child was TOLD (Rule 15: the fix is not silent) |
| `GOPI.md` Rule 6a | one deferral clause ABOVE the existing steps: if the briefing carries the ground block, that IS your worktree — `git branch -m` for a descriptive name, do NOT create a second one. The no-ground steps, the anti-force-spellings passage and `TestBranchForceGateRedProof` are untouched |

Two channels, deliberately: **the briefing** is the only channel that reaches
the *model* (and the model is who reads Rule 6a), and **the env pair** is the
same fact readable by the child's own code and any gopi it spawns. Neither is a
fallback for the other.

## What a future agent must NOT "fix"

- **Do not delete `TaskRunGroundFor`, and do not lose `ChildInMainCheckout`**
  (`internal/tools/task_run_ground.go`). That assertion is what stops a child
  starting in the MAIN checkout.
- **Do not weaken `finishCleanupWorktreeInUse`.** The guard is correct; the
  producer was confused. A guard that "tolerates" the spare is a machine that
  learns to live with litter.
- **Do not add a third worktree-creating path.** Two was the bug.
- **Do not try to enforce the ban in Go.** See "Why the fix is an announcement"
  above; an enforcement would need to distinguish "the model is building a
  spare" from "this run has no ground", and the second case is legal by Rule 6a.

## Verification recipe (reuse it, do not re-derive it)

The end-to-end walk is
`cmd/agent/task_ground_announcement_e2e_test.go:TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree`.
Its shape is reusable for **any** "was the child told X before its first tool
round?" question:

1. `GOPI_BIN=<pinned binary>` — pin it, or `testutil.GopiBin` silently picks
   whichever `bin/gopi` is newest in the module root.
2. The runner is `gopi worker <row>` (the retired `task run` face is gone).
3. The child is found by `/proc` **environment**
   (`GOPI_TASK_WRAPPER_ID=<row>`), never by `pgrep -f` (Rule 24 self-match).
4. **The child's cwd is readable long before its first model request.** Read
   `/proc/<pid>/cwd` for the placement assertions, then WAIT (bounded) on
   `mock.RequestCount() >= 1` before asserting on `mock.RequestBody(1)` —
   measured: cwd was available ~7s before the first request arrived, and
   asserting the body immediately fails on a run that is merely young.
5. Assert the run's worktree accounting (`git worktree list --porcelain`) from
   the repo while the run is IN FLIGHT, and assert the row is `in_progress` —
   that is what makes the reading evidence about the spawn rather than about
   some later state.

## Honest limits of the verification

- The **spare itself is not reproduced by a scripted child.** The spare is a
  *model* obeying Rule 6a; a mock LLM's script is identical on the pre-fix and
  post-fix trees, so no mock can be the red proof of "the spare stopped". The
  red proof is on the **announcement** (impossible pre-fix — the string did not
  exist) plus the exactly-one-worktree invariant.
- The elimination of the spare therefore rests on the model reading the amended
  Rule 6a. It is a prose fix, verified as far as prose can be verified.
