<!-- doc-class: historical -->
# The Inert Chat Ground Is Collected — the Chat Axis of the Worktree Sweep

Status: implemented on branch `task/01a0eacae75f7b22b2b3c866eb61d774`
(epic `chat-ground-reclaim`, 2026-09-29). This document is the design record;
the run's report is `tasks/01a0eaca-e75f-7b22-b2b3-c866eb61d774-report.md`.

## The defect, in the diagnosis document's own terms

`devdoc/chat-session-ground-lifecycle-diagnosis.md` measured it: every `gopi
chat` run **then** minted a ground — a linked worktree `<worktree root>/gopi-<stem>` on
branch `chat/<stem>` — and **stamped nothing in the ledger**. Six such grounds sat
on this host: clean, 0 commits ahead of `master`, owned by no row. None of them
had a collector, and the reason was structural rather than accidental: **every
arm of `worktreeSweepJudge` that could free a registration is LEDGER-KEYED.**

> CORRECTION, 2026-09-29, after this arm landed: the PRODUCER is gone. The
> sibling task (`01a0eaca-c2b3-…`) retired the creation path the same day —
> `internal/tools/chat_ground.go` + `cmd/agent/chat_ground.go`: a new chat
> creates no branch and no worktree, and reports in one stderr line which
> directory its writes land in. This arm now collects the residue of earlier
> gopi versions and nothing else. The measurements below stand unchanged (they
> are that run's record), and the classifier stays ADDITIVE: `chat/` is the
> marker an OLD ground carries, never one a new ground will.

The diagnosis named three keepers, of which one was real and two were
process-global:

| keeper | why it kept a chat ground |
|---|---|
| ⑤ the age floor (real) | measured on the REGISTRATION, so a ground minted today could never be reclaimed today, however conclusively its session had ended |
| ③ the session arm (process-global) | it asks "may a STRANGER free this?" and answers by checking whether ANY live row holds the ground — and a chat ground is owned by no row, so on a host whose ledger always carries a live row (the resident worker's own), the answer kept every chat ground forever |
| ② the self-checkout veto (process-global) | it asks "is this MINE?" and a chat runs itself inside its own ground, which is exactly the shape where the two meet |

Neither of the last two can ever answer the question that decides this
registration: **is the ground's OWN session over?**

## What was added, and what was deliberately not

```
internal/tools/worktree_sweep_chat.go   the chat session reader + the ONE inertness predicate
internal/tools/worktree_sweep.go        the ladder's ④C rung (judge) + the runner's two chat arms
internal/removalaudit/audit.go          ArmSweepChatGroundBranch (its own arm in the record)
```

**No new process, no cron, no settings key, no state file, no script** (Rule 35).
The arm rides the reap that already ticks (`cmd/agent/task_lifecycle_tick.go`),
and the only thing it reads that did not exist before is the `chats` table the
recorder already writes.

### The classifier is the BRANCH, and that is forced

A ground's DIRECTORY is `gopi-<stem>` for a chat **and** for a task
(`internal/tools/task_run_ground.go` reuses `TaskGroundDirPrefix` — deliberately,
so every existing reader of that prefix keeps recognising a ground wherever it
came from). So a directory name cannot classify. The branch namespace can:
`chat/` WAS minted only by the (now retired) creation path in `chat_ground.go`.
`worktreeSweepChatSessionStem`
therefore takes the branch half alone, and the test
`TestTheChatArmDoesNotClaimATaskGround` pins the blast radius — a task ground
must still reach the ordinary rungs.

### Inert means ALL of, in this order

1. **a chat ground's branch** — otherwise the rung does not apply at all;
2. **no live process in the ground** — a chat carrier stands in its own ground
   for the whole session (`cmd/agent/chat_ground.go` chdirs there before the
   first turn), so a live process's cwd inside it IS the session. This is the
   check that keeps a ground with no row to speak for it, and it survives the
   SIGKILL of the ledger writer alone;
3. **the session is over** — on POSITIVE evidence (the `chats` row records the
   close: only a clean exit runs one) or on QUIET evidence (nothing the session
   owns — the row it opens at its first turn, the session directory it writes its
   turns into — has been touched for longer than `worktreeSweepChatQuietGrace`,
   one hour). A zero timestamp everywhere is NOT endedness: an undatable ground
   is kept;
4. **0 ahead of the integration branch** — `git merge-base --is-ancestor
   <branch> master`, the merger's own spent-branch test, asked by the runner
   BEFORE anything is removed. A chat ground's commits live on its own ref and
   nowhere else (no merge path reads `chat/<id>`), so a ground holding commits
   master lacks is a DELIVERABLE, and freeing its directory would leave those
   commits reachable only by the ref the same arm is about to offer to
   `branch -d`.

Two spellings of the session id are asked, because the host writes both: the
branch's dash-free stem and the canonical dashed UUID. Measured on the live
profile — branch `chat/c5f1189ace3444f2a40c74ebcb148fc7`, index row
`c5f1189a-ce34-44f2-a40c-74ebcb148fc7` — **the dashes are the only difference**,
so the reader bridges rather than assumes, with `TestChatSessionIDSpellingsAgree`
pinned against that very pair.

### The branch axis: `git branch -d`, and nothing else

Until this arm, the ONLY branch rung belonged to a DEAD registration (a directory
already gone). A live registration's branch was offered nowhere, so an inert chat
ground's ref survived everything on the host. Now an inert ground's branch is
deleted after its registration is gone (git refuses `branch -d` while its
bookkeeping still associates the ref with a worktree), through `-d` and **never
`-D` / `--force`: an unmerged or elsewhere-checked-out ref is refused BY GIT, and
its refusal is reported on the finding.** The deletion is recorded under its own
arm, `ArmSweepChatGroundBranch`, so a reader grepping the audit log can tell this
decision from the dead-registration arm's.

### Where the rung sits, and the bug that finding its place exposed

The rung sits **after guard ④ (dirty) and before the session arm and the age
floor**, and both halves are load-bearing:

- **after ④**, because the dirty guard is about the ground's CONTENT and no
  reclaim arm may run before it. Measured while building: with the rung placed
  before ④, a dirty chat ground was handed to `git worktree remove` and only
  git's own refusal stopped it ("contains modified or untracked files"). Relying
  on a tool's refusal for a rule this cheap is what the ladder's order exists to
  avoid — and the escalation for a dirty ownerless ground (③O) has already run
  by then, which is why a dirty chat ground is REPORTED as an orphan escalation
  with the session named in it;
- **before the session arm and ⑤**, because both are about somebody else — a
  stranger's live row, or a clock — while this rung answers the registration's
  own question. That is the whole content of the change: the process-global
  keepers are replaced for THIS registration by evidence about THIS
  registration.

The self-checkout veto (①B) is **not** suppressed: it sits above, and a chat
running itself inside its own ground is exactly the shape where the two meet, so
the ground of the run reading this line is kept by that veto in its own words.

## What is NOT done (honest)

- **The creation path was retired by a sibling task**
  (`01a0eaca-c2b3-…`, landed 2026-09-29): this branch changes only what happens
  to a ground that already exists, and that task deleted the producer — a new
  chat mints nothing. The two halves compose: the sweep collects the residue
  that older versions left, and the producer that used to make it is gone.
- **A held ground is still skipped.** The measured target
  `chat/c5f1189ace3444f2a40c74ebcb148fc7` had a live `gopi chat` process standing
  in it (pid 1724010) when the diagnosis ran; a held ground is kept by the
  holder rung, correctly, and it becomes collectable the next time the sweep
  ticks after that process ends — no human action required, which is the point
  of riding the resident tick.
- **A ground whose index could not be read at all is kept.** Fail closed: "I
  could not ask" is not endedness. It falls through to the age floor.

## How to verify (the recipe)

```bash
go build ./... && go test ./internal/tools/ -run 'ChatGround|Sweep' -count=1
gopi sweep-worktrees --dry-run          # the plan names every chat ground and its evidence
```

The dry run is the operator-facing surface: each finding carries its `Rule`
(`chat-ground-inert` / `chat-session-active` / `chat-ground-carries-work`) and a
reason that quotes the fact which decided it, so a keep can be argued with.

## Evidence

The run's own observations (before/after `git worktree list` + `git branch`, the
dry-run plan, the counter-tests) are in
`~/.gopi/sessions/01a0eaca-e75f-7b22-b2b3-c866eb61d774/`, and the summary is in
the run's report (`tasks/01a0eaca-e75f-7b22-b2b3-c866eb61d774-report.md`).
