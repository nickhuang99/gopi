<!-- doc-class: normative -->
# The clone channel publishes; the run does not — the prose half of the stray-ref defect

**Task:** `01a1604a-63d9-7269-9da7-1e6148b5ff74-r1`
**Machine half (already landed, task `01a16033`):** `internal/tools/git_publish_target_guard.go`,
wired at `internal/tools/bash.go` step 2e.

## What was measured, and what was missing

On 2026-10-21 the server held four refs the clone channel never creates
(`refs/heads/feat/reopen-carries-evidence` and three `refs/heads/task/<id>`), each
with exactly ONE reflog entry — `push` — on the same second the run's own bash
tool logged

```
cd /home/user/workspace/01a15b70-a6aa-… && git push origin HEAD:refs/heads/task/01a15b70-a6aa-…
```

and the run's reasoning is in the same log: *"Better: push to a named branch"*. The
agent applied **PROJECT.md Rule 6c** — written for a `/tmp` clone with **no channel
behind it** — to a ground whose delivery loop had **already** published its commits.

The machine check closes the enforcement half. It cannot close the decision half: a
guard fires **after** the model has decided to push, and the model decides from the
prose it was given. So the defect had two halves and only one of them was shipped.

## The two answers are opposite, and the ground's SHAPE decides which

| ground | `test -d .git` | who publishes | Rule 6c |
|---|---|---|---|
| linked worktree (worktree channel) | FILE | nobody — the commit is already on the branch in the shared repo | first half applies |
| clone workspace (clone channel, `dev_repo`) | DIRECTORY | **the channel's delivery loop**: clean → pull --rebase → declared build → push `HEAD:<ground branch>` | **does not apply** |

A `/tmp` clone **the run made itself** is a third shape: it has no channel behind it,
and there `git push origin feat/<name>` remains the correct route (Rule 6c's second
half, with its `receive.denyCurrentBranch` caveat). Rule 6c conflated shapes two and
three by calling both "a clone"; the amendment separates them.

## What changed

1. **`internal/tools/task_briefing_publish_clause.go`** (new). `TaskBriefingPublishClause(branch)`
   renders the clause and `TaskBriefingPublishLabel` is its one spelling. It states the
   route (the four steps, in order), the ban by the **measured spellings**
   (`HEAD:<some-name>`, `HEAD:refs/heads/<some-name>`, `feat/<name>`), the **second**
   harm the guard also refuses (naming the integration branch from another checkout
   **moves** it), the enforcement by the guard's own words, and the escape hatch
   **attributed to the operator** — a run told it may set `GOPI_ALLOW_DANGEROUS=1` is a
   run told the guard is advisory.
2. **`cmd/agent/cli_tasks.go:2709–2718`**. The clause is bound into the briefing at the
   ground-resolution block, `if cloneRun != nil`. Briefing scope and enforcement scope
   are therefore the **same condition** (`cloneRun != nil` ⇔ the guard can fire), so no
   run is told about a rule that cannot apply to it, and the worktree path's briefing is
   byte-identical to before.
3. **`PROJECT.md` Rule 6c** retitled *"the ground's own channel — two shapes, two
   answers"*: the shape test first, then the linked-worktree answer, then the
   channel-less `/tmp` clone answer, then the clone-channel ground (neither of the above
   applies; commit, leave the tree clean, report).
4. **Two gates.** `internal/tools/task_briefing_publish_clause_test.go` pins the prose
   (branch named, route, ban spellings, second harm, enforcement, hatch attribution;
   no branch ⇒ no clause; the defective sentence must not come back).
   `cmd/agent/task_briefing_publish_e2e_test.go` pins **delivery** on the real binary:
   the clause reaches the clone-channel child's FIRST model request, is **absent** on
   the worktree arm (whose briefing still carries the ground announcement, so the
   absence is not vacuous), and a scripted `git push origin HEAD:refs/heads/task/x`
   through the real bash tool is refused **with the guard's own sentence in the tool
   result the model received**, with the fixture server's ref list showing
   `refs/heads/task/x` was never written.

## Why one face, not both (Rule 28)

Rule 28 puts the **system layer** on peerbox: worktree lifecycle, branch/merge machinery,
the merger, the ledger, sync, scheduling. This change touches all of that — the spawn
seam's briefing composition, the ground model, the publish guard's family. It is
therefore **peerbox's** work, and it was done in this run's own ground on this host. The
feature-side alternative (a `internal/<feature>/` package with product-visible
behaviour) does not exist here: there is no user-visible surface to add.

**The other face that could have carried it was deliberately not touched.** The system
prompt kernel (`internal/agent/`, assembled by `assembleSystemPrompt`) says **nothing**
about pushing — measured: `grep -rn 'git push\|Rule 6c' --include=*.go internal/agent/*.go`
(non-test) is empty. Adding it there would have (a) spent the kernel's 4500-char density
budget (`systemPromptDensityBudget`, `internal/agent/prompt_density_budget_test.go`) on a
fact true of **one channel**, and (b) told a worktree run about a channel it is not in.
The briefing seam is the only place that knows **which channel the run is in**, so it is
the only place the statement can be true.

## Why no new dependency (Rule 35)

No cron line, no script, no state file, no config key, no resident process. The clause is
a string rendered by a function that already runs; the gates are ordinary tests in
packages that already have them. The guard it points at already existed.

## A finding recorded while doing this (not fixed here)

In the third arm's real run the delivery log reads
`pushed master to origin after 1 attempt(s)` — the clone ground's branch is `master` and
the loop pushed it — while the fixture server's heads afterwards are
`refs/heads/master refs/heads/task/01a1604a…b2`. The `task/…b2` ref belongs to the
**preceding arm's** worktree-channel run in the same fixture repo, whose push the
worktree path permits by design (that arm proved it by being allowed). It is an artefact
of test order — two channels sharing one fixture repository — not a stray ref this change
produced. Recorded because a reader of that log line will otherwise read it as one.
