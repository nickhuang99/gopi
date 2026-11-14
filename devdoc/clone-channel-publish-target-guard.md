<!-- doc-class: historical -->
# The clone channel's publish target is the channel's, not the run's

**Status:** standing rule — `internal/tools/git_publish_target_guard.go` (implemented 2026-10-21/22,
task `01a16033-c069-72fe-8b4e-d15d706f41ac`).

How the guard works, what it refuses, and why a hand-rolled `git push HEAD:<name>` from a
clone-channel ground must be refused — the enforcement that keeps a run from minting server
refs the channel never creates.

## The symptom, as measured

`peerbox:/home/user/work/gopi` carried four branch refs no merge path claimed, while the
ledger rows that produced them read `completed` with `deliverable.branch: master`:

```
$ ssh peerbox 'cd /home/user/work/gopi && git branch -vv'
  feat/reopen-carries-evidence              71ea4f7
  task/01a15b2c-7096-72c7-82b4-c64e1096914d efaa891
  task/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd 197c951
  task/01a15b70-c35f-7c7c-bd07-aad6c74cb70b 3e355d1
* master                                    c17e64d
```

## The reading that had to be discarded first

The briefing's working theory was "master never moved". It is false, and it is worth
recording that it is false because the whole diagnosis changes if you keep it:

```
$ ssh peerbox 'cd /home/user/work/gopi && for c in efaa891 197c951 3e355d1 71ea4f7; do
    printf "%s ancestor-of-master: " $c; git merge-base --is-ancestor $c master && echo YES || echo NO; done'
efaa891 ancestor-of-master: YES
197c951 ancestor-of-master: YES
3e355d1 ancestor-of-master: YES
71ea4f7 ancestor-of-master: YES
```

Every stray tip is **contained in master**. Master did advance — its reflog carries the same
commit ids as `push` entries (`efaa891 … @07:12:52: push`, `197c951 … @08:23:22: push`).
So the channel's delivery loop did its job, `deliverable.branch: master` is an accurate
record, and the stray refs are **pure residue**: a second, redundant publish of commits
that had already landed.

## Who wrote them

Each stray ref's reflog has exactly ONE entry, `push`, and its timestamp is the landing
second:

```
$ ssh peerbox 'cd /home/user/work/gopi && git reflog show --date=iso refs/heads/task/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd'
197c951 refs/heads/task/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd@{2026-10-21 08:23:03 +0800}: push
```

`08:23:03` is inside that row's own run. The run's session log
(`~/.gopi/sessions/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd/478f60e7-….jsonl`) contains, in the
same minute, one `bash` tool call:

```json
{"command": "cd /home/user/workspace/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd && timeout 60 git push origin HEAD:refs/heads/task/01a15b70-a6aa-7ad2-b7d4-9171a4bc8abd 2>&1 | tail -8; echo \"PUSH_EXIT=$?\"",
 "name": "bash", "type": "tool_use"}
```

and its reasoning, in the same log:

> "The commit exists only in that local clone. Rule 6c: worked in a /tmp clone → push. …
> Better: push to a named branch, e.g. `git push origin HEAD:refs/heads/task/01a15b70-…`"

**The writer was the agent's own `bash` call**, applying PROJECT.md Rule 6c — a rule written
for a `/tmp` clone with no channel — to a clone-channel ground that already had one.

## Why it is not the Go code

The candidate in the briefing (`cmd/agent/cli_tasks.go:3919`, the report-back push) is a
different leg, and the destination there is **not** a task branch name:

- it derives `branchName` from `git rev-parse --abbrev-ref HEAD` in the run's repo — which,
  in a clone-channel ground, answers `master` (`git -C … branch --show-current` = `master`
  for every `/home/user/workspace/01a1*` ground on this host);
- the channel's own delivery push is `git push origin HEAD:<the branch the ground stands on>`
  (`internal/devworkspace/devworkspace.go:400`), and `cfg.Branch` is set from
  `opened.Branch` (`cmd/agent/devworkspace_channel.go:128`) = `currentBranch(dir)` — again
  `master`.

So no Go path in this repo spells a `task/<id>` or `feat/<name>` destination. The one place
that does is a model choosing it: `internal/tools/task_run_ground.go:67` is the only source
of the `task/` prefix (`TaskGroundBranchPrefix`), and it names the **worktree** channel's
minted branch, not a push target of the clone channel.

## The fix: a guard on the publish target, in the bash tool

`internal/tools/git_publish_target_guard.go`, wired as step **2e** in `internal/tools/bash.go`:

- **Scope**: only a CLONE ground (`.git` is a **directory**; a linked worktree's is a file),
  read from the runner's own announcement pair `GOPI_RUN_GROUND` / `GOPI_RUN_GROUND_BRANCH`.
  No announcement → the guard says nothing. A worktree ground → Rule 6a/6c's
  `git push origin feat/<name>` stays exactly as it was.
- **Judged**: `git push <remote> <refspec>` whose normalized destination is not the ground's
  own branch. It refuses **before** the command runs, so nothing is written.
- **Not judged**: no refspec (the caller's own upstream), `HEAD:<ground branch>` in either
  spelling, `--tags` / `--all` (a family nobody picked), a glob, and `:<ref>` (a deletion
  writes no ref — and deleting a server ref is operator-only, Rule 9).
- **Escape**: the shared `GOPI_ALLOW_DANGEROUS=1` hatch the rest of the guard chain uses.

The refusal is a sentence, not a silence: it names the destination, the ground's branch, why
the destination is harmful (it CREATES a ref nothing merges, or — from a checkout that is not
the integration branch — MOVES the server's integration branch under
`receive.denyCurrentBranch=updateInstead`), and that the correct route is the channel's own
delivery loop.

## What was executed (Rule 31)

Both halves are in `internal/tools/git_publish_target_guard_test.go`, driven through
`BashInDir` — the real bash-tool entry point — against a bare fixture server plus a clone of
it:

- **red half** (`…_ReproducesTheIncidentWithoutTheGuard`): the incident's own command through
  **raw git** really does create `refs/heads/task/01a15b70-fixture` on the server. Without
  that, the green half would prove nothing.
- **green half** (`…_AcceptanceSet`): 15 cases — 6 refused (task/feat/fully-qualified/dwim/
  force-prefixed/`-C`/chained), 9 allowed (ground branch ×2, bare push, `-u origin <branch>`,
  delete refspec, `--tags`, worktree ground, no announcement, quoted words). Every refused
  case also asserts the server holds **no** such ref — a refusal that ran after the write
  would fail.

Plus a live run of the changed surface: `gopi` itself, asked to run the incident's command in
a clone ground with the announcement pair set, refused it and left the server at
`refs/heads/master` alone; the same push through plain `git` with the hatch open did create
the ref (the positive control proving the fixture and the guard are both live).

## Deferred / honest gaps

- **The four refs on peerbox are still there.** Deleting a server ref is the operator's call
  (`git push peerbox :refs/heads/<name>`), and this file records the command rather than
  running it.
- **The agent-side prompt half is not in this change.** The guard is the enforcement; a
  worker prompt that says "in the clone channel the delivery loop owns the push; never push
  to a named ref yourself" would stop the mistake before it is attempted. That is a prompt
  surface on the system layer (Rule 28) and is filed, not edited, here.
- **`cli_tasks.go:3919` was left alone, deliberately.** Its `HEAD:<abbrev-ref HEAD>` is
  correct for what it records (it names the run's own branch), and the evidence above does
  not implicate it. Changing it would have been an unproven edit.

## Cost and scope notes

- The guard reads `GOPI_RUN_GROUND` with one `stat` per bash call; the branch comes from
  `GOPI_RUN_GROUND_BRANCH` with no subprocess. Only when the branch half is absent does it fall
  back to one `git rev-parse --abbrev-ref HEAD` in the ground — and a branch it cannot read
  REFUSES nothing rather than guessing one (an unreadable announcement must not become a
  fabricated rule). The spawn seam exports the pair, so the ordinary path forks nothing.
- The guard is in `internal/tools` (the bash tool), not in `internal/devworkspace`: the
  delivery loop was never the wrong writer, so no line of it changed. The refusal therefore
  also covers a run that reaches for the shell later in its life, which the delivery loop
  could not have seen.
