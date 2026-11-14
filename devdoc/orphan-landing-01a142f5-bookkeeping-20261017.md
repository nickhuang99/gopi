<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# ORPHAN LANDING `task/01a142670ace…` — bookkeeping verdict: the row EXISTS, the orphan was measured on the wrong ledger

**Task** `01a142f5-0268-744a-b393-33420ba829cf` · **run** 2026-10-17 ·
**measured on** cut point `942b3ac6ad27dd551080e726896d227875565e59`
(`refs/remotes/gitPushcheck/master` at run start), cross-read against
`9adad635b` (`master` on `hostb` at run start) and the peer host `peerbox`.

## The DECISION

**No ledger row needs to be back-filled for the producer of `e28706779`** — the
row already exists, on the producing host's ledger, and it is `completed` and
carries the branch and worktree that produced the commit. Neither is the branch
a stray push of unowned work.

The premise this task was dispatched on ("branch had no ledger row") is **not
false as an observation and not true as a conclusion**: it was measured against
the *merge host's* ledger, which does not carry rows produced on the client
host. The §3.11 orphan-landing report is a **cross-host false positive**, and
that — not a missing row — is the defect worth a row of its own.

### The criterion, stated so a later reader can re-apply it

A branch needs a back-filled row **iff both** hold:

1. **no ledger row on ANY host carrying this profile** names the branch (not
   just the ledger the reporter happens to be able to open), **and**
2. the work landed in the integration branch with **no producer session that can
   be named** — i.e. the commit cannot be traced to a session/row pairing.

If (1) fails → the row exists; nothing to back-fill (this case).
If (1) holds and (2) fails → back-fill: the producer is nameable, the row is
missing.
If both hold → stray push: no row, close the report as no-action.

The reason (1) must say "on any host" is the measurement below: one verb, one
branch name, two hosts, two opposite answers.

## Dossier — machine facts

### 1. Authorship of the branch tip

```
$ git log -1 --format='sha=%H%nauthor=%an <%ae> %ai%ncommitter=%cn <%ce> %ci%nsubject=%s' e287067798083a4e780f91244779bfeb7495b904
sha=e287067798083a4e780f91244779bfeb7495b904
author=qingzhe huang <user@gmail.com> 2026-10-16 11:58:29 +0800
committer=qingzhe huang <user@gmail.com> 2026-10-16 11:58:29 +0800
subject=fix(removalaudit): the sink creates its own logs/ directory (fresh-profile records were lost)
```

Author and committer are the operator identity, not `gopi <gopi@localhost>` —
consistent with a worker run committing as the profile user (the merge commit
`9902a5236`, by contrast, is `gopi <gopi@localhost>`).

### 2. The ref itself: deleted, and the deletion is audited

```
$ git rev-parse --verify --quiet refs/heads/task/01a142670ace7cddb138d7dfce3b24d0
rc=1                                  # ref does not exist any more
$ git reflog show task/01a142670ace7cddb138d7dfce3b24d0
fatal: ambiguous argument '…': unknown revision or path not in the working tree.
```

The removal-audit sink recorded **six** events for this branch, all four distinct
arms carrying the owning `task_id`, the `sha`, and the delivery point:

```
$ grep -h "01a142670ace" ~/.gopi/logs/removal-audit.log | wc -l
6
2026-10-16T04:18:41.525Z worktree-removal       task-self-reclaim
2026-10-16T04:18:47.774Z branch-delete          finish-cleanup-branch
2026-10-16T04:18:48.067Z branch-delete          finish-cleanup-branch
2026-10-16T04:18:54.075Z branch-delete          finish-cleanup-branch-force
2026-10-16T04:18:54.339Z branch-delete          finish-cleanup-branch-force
```

with `"task_id":"01a14267-0ace-7cdd-b138-d7dfce3b24d0"`,
`"sha":"e287067798083a4e780f91244779bfeb7495b904"`,
`"delivery_point":"peerbox:~/work/GoPi"`.

**This is the decisive trace**: the ref was deleted by *the producing run's own
finish-cleanup chain*, which stamps the row id into every arm. A stray push has
no such chain — nothing would delete it under a task id. (Timestamps are UTC:
04:18:41Z = 12:18:41 +0800, i.e. ~20 min after the 11:58:29 commit and ~5 min
before the 12:23:55 merge.)

### 3. The ledger lookup — ONE command, TWO hosts, TWO answers

This is the deciding measurement.

```
$ gopi task for-branch task/01a142670ace7cddb138d7dfce3b24d0 --repo /home/user/work/GoPi
1 owner(s) of branch task/01a142670ace7cddb138d7dfce3b24d0 (repo /home/user/work/GoPi)
  task 01a14267-0ace-7cdd-b138-d7dfce3b24d0  status=completed  (ended)
    session  90ae19f8-50fc-4960-8755-53822885a58c
    worktree /home/user/workspace/gopi-01a142670ace7cddb138d7dfce3b24d0
    branch   task/01a142670ace7cddb138d7dfce3b24d0
    repo     /home/user/work/GoPi
```

```
$ ssh peerbox 'cd ~/work/GoPi && gopi task for-branch task/01a142670ace7cddb138d7dfce3b24d0 --repo ~/work/GoPi'
no owner: no ledger row names branch task/01a142670ace7cddb138d7dfce3b24d0 (repo /home/user/work/GoPi)
  ledger: /home/user/.gopi/memory.db — this LEDGER only (a host carries one per profile; --db names another)
  (2254 rows carry a branch; none names this branch in repo /home/user/work/GoPi — drop --repo to look across repos)
  no repository read (/home/user/work/GoPi) registers the branch "…" — there is no ground to reconcile against
```

`hostname` on this run's host is `hostb`. The merge run
`01a142f2-cf19-777a-8f4d-20c218a2540d` is `run_method: auto`,
`run_cmd: gopi merge --every 2m` — a **server** row, and it is absent from this
host's ledger (`gopi task list --all | grep -c 01a142f2` → `0`) while present on
`peerbox`'s. The report was therefore emitted by a process reading **peerbox's**
ledger, judging a branch whose producing row lives on **HP's** ledger.

The divergence is *documented behaviour*, not a regression in propagation:
`~/.gopi/settings.json` on this host carries `"task_sync": false` (and
`"task_pull": true`), which suppresses client→peer row exchange — the same
mechanism recorded in this box's local `GOPI.md` §1.

### 4. The producing session exists, and so does its report

```
$ gopi task show 01a14267-0ace-7cdd-b138-d7dfce3b24d0
status:         completed
summary:        removal-audit 在新 profile 下静默丢弃：sinkPath 指向 <gopi dir>/logs/removal-audit.log 但无人创建 logs/ 目录…
session_log:    /home/user/.gopi/sessions/01a14267-0ace-7cdd-b138-d7dfce3b24d0/90ae19f8-50fc-4960-8755-53822885a58c.jsonl
runs:           1 run(s) — 1 completed … 90ae19f8… 2026-10-16 11:47:00 completed exit=0 155 loop passes
parent_task_id: —
tags:           bug,worker:hostb-Laptop
deliverable.branch: task/01a142670ace7cddb138d7dfce3b24d0
deliverable.worktree: /home/user/workspace/gopi-01a142670ace7cddb138d7dfce3b24d0
```

```
$ ls -la /home/user/.gopi/sessions/01a14267-0ace-7cdd-b138-d7dfce3b24d0/
90ae19f8-…jsonl  1066059   Oct 16 12:16
task-report.md     12161   Oct 16 12:16
artifacts/ evidence/ output.log metadata.json
$ grep -c "e28706779" …/90ae19f8-…jsonl
41
```

The session is on **this** host, `completed exit=0`, with a 12 KB task report and
41 in-session mentions of the landed sha. The recorded worktree directory is gone
and the ledger says so honestly: `the recorded worktree directory is not there —
nothing to remove … a stale reading`, and `ground (resolved now): … (NOT FOUND now
— a stale reading)`.

### 5. The landed content is genuinely on the integration branch

```
$ git rev-list --parents -n1 9902a5236af91ef4640029f0809d92582b2d9b91
9902a5236… 14861c100… e28706779…        # merge commit, 2nd parent = the branch tip
$ git merge-base --is-ancestor e287067798083a4e780f91244779bfeb7495b904 942b3ac6ad ; echo rc=$?
rc=0
$ git diff --stat e287067798083a4e780f91244779bfeb7495b904 942b3ac6ad -- internal/removalaudit/ devdoc/worktree-removal-forensics.md
                                        # EMPTY: byte-identical content at the cut point
$ git show 942b3ac6ad:internal/removalaudit/audit.go | grep -n "MkdirAll(dir"
569:		if err := os.MkdirAll(dir, sinkDirMode); err != nil {
$ git show 942b3ac6ad:internal/removalaudit/audit_test.go | grep -c "func Test"
13
$ ssh peerbox 'cd ~/work/GoPi && git merge-base --is-ancestor 9902a5236… master; echo rc=$?'
rc=0
```

So: the fix (`MkdirAll` before opening the sink), its 174-line test file
(13 test funcs) and the forensics doc are on `master` on **both** hosts. The
merge is real and must not be redone — as the briefing said.

## What is actually defective (and what is not)

**Not defective**: the row, the session, the landing, the cleanup. All four are
present and audited.

**Defective** (§3.11 orphan-landing detector): it renders a verdict of *"NO ROW
NAMED THIS BRANCH"* from **one** ledger, and neither the verdict line nor the
row it raises says *which* ledger was read or that other hosts hold ledgers. On
a two-host install with client→peer row sync off, every client-produced branch
reads as an orphan by construction. The detector's own sibling code is explicit
that a read ledger is not a universal one — `internal/tools/branch_attribution.go`
distinguishes `"no ledger at <path> (<channel>)"` from
`"no ledger row names this branch (the ledger was read)"` — but the §3.11 line
inherits only the second phrasing and drops the channel.

Cost of the gap, measured: this task. A completed, reported, correctly-cleaned
run's work was escalated as ownerless, and one agent session was spent proving
it was not.

## Action taken

- **No back-fill row opened** — the row exists (`01a14267-0ace-7cdd-b138-d7dfce3b24d0`).
- **No cross-host row sent.** Landing this row on `peerbox`'s ledger would be
  `gopi task send --task-id <id>`, which writes a file on **another host** —
  outside this run's declared write scope, and an operator configuration
  decision (`task_sync: true`), not a bookkeeping run's call. Recorded as a
  recommendation below, deliberately **not executed**.
- **One bug row opened** for the cross-host false-orphan verdict, with the two
  `for-branch` outputs as evidence (Rule 41) — task id in the closing message.

## Not Done (honest)

- I did **not** read the merge run's own session transcript
  (`peerbox:/home/user/.gopi/sessions/5f231fba-…`) beyond its `gopi task show`
  render — the row's briefing quote (§3.11 + the "no owner" line) already carries
  the deciding text, and a 1 GB-scale raw log is not needed to establish which
  ledger the verdict came from. Labelled: the *ledger identity* of that read is
  established by host + settings + the row's absence here, not by reading its
  tool calls.
- I did **not** re-run the merge, re-verify the merge build, or touch any ref
  (prohibited). Content-identity at the cut point is measured by
  `git diff --stat` = empty, not by re-running `go build` on master.
- The stale-worktree reading the producing row reports (`NOT FOUND now`) is
  **not** investigated further here: the removal-audit chain shows the worktree
  removed at 04:18:41Z by that run's own `task-self-reclaim` arm, i.e. the
  absence is the expected end state, not a leak.

## Verification

- Build: not run — this run changes no Go code (one devdoc file only).
- Tests: not run — same reason; no package was touched.
- Ran: `gopi task for-branch task/01a142670ace7cddb138d7dfce3b24d0 --repo /home/user/work/GoPi`
  and the same verb over `ssh peerbox`; `git merge-base --is-ancestor <tip> 942b3ac`;
  `git diff --stat <tip> 942b3ac -- internal/removalaudit/ devdoc/worktree-removal-forensics.md`;
  `gopi task show 01a14267-0ace-7cdd-b138-d7dfce3b24d0`.
- Result: HP → `1 owner(s) … status=completed`; peerbox → `no owner: no ledger row
  names branch …`; ancestry rc=0; diff empty; row `completed` with a 12 KB report.
- Commit: see this branch's tip.

## Evidence (self-filed)

- `hostname` → `hostb` (so "here" is the client, not the merge host).
- `gopi task for-branch … --repo /home/user/work/GoPi` (HP) → `1 owner(s) of branch task/01a142670ace7cddb138d7dfce3b24d0 … status=completed`.
- `ssh peerbox 'gopi task for-branch … --repo ~/work/GoPi'` → `no owner: no ledger row names branch …` (2254 rows carry a branch).
- `grep -n -E '"task_sync"|"task_pull"' ~/.gopi/settings.json` → `24: "task_sync": false`, `25: "task_pull": true`.
- `gopi task list --all | grep -c 01a142f2` (HP) → `0`; `ssh peerbox 'gopi task show 01a142f2-cf19-…'` → the merge row, `run_cmd: gopi merge --every 2m`.
- `git rev-parse --verify --quiet refs/heads/task/01a142670ace…` → rc=1 (ref gone).
- `grep -h "01a142670ace" ~/.gopi/logs/removal-audit.log | wc -l` → `6`, each carrying `task_id=01a14267-0ace-7cdd-b138-d7dfce3b24d0` and `sha=e28706779…`.
- `git merge-base --is-ancestor e28706779… 942b3ac6ad` → rc=0; `git diff --stat e28706779… 942b3ac6ad -- <3 paths>` → empty.
- `git show 942b3ac6ad:internal/removalaudit/audit.go | grep -n "MkdirAll(dir"` → `569: os.MkdirAll(dir, sinkDirMode)`.
- `ssh peerbox '… git merge-base --is-ancestor 9902a5236… master'` → rc=0.

Raw captures: `/home/user/.gopi/sessions/01a142f5-0268-744a-b393-33420ba829cf/evidence/`
(`ledger-divergence.txt`, `git-facts.txt`).

## Manifest (Rule 36) — and a pre-existing blocker found on the way

The doc was classified in the same change, through the corpus's ONE generator:

```
$ python3 tools/docclass/classify.py .
 "index-meta": 1, "normative": 717, "roadmap": 12 } total 2621 new 1 carried 2620 dropped 0
$ grep -c "orphan-landing-01a142f5" devdoc/DOC-INDEX.tsv
1
```

`gopi tool doccheck --fix` could **not** do it, and the reason is a defect on the
cut point rather than in this change: `classify.py --only <path>` refuses the
whole write when ANY path is duplicated —

```
$ gopi tool doccheck --fix
gopi tool doccheck: classify … --only devdoc/orphan-landing-01a142f5-bookkeeping-20261017.md:
  exit status 1: classify.py: refusing to write — duplicate row for
  devdoc/merge-compile-only-policy.md (a manifest with a duplicated path fails the coverage gate)
```

— and the cut point carries that path **three times** (lines 1290, 2630, 2631;
2630 is byte-identical to 1290, so it is a copy, and 2631 is a different class):

```
$ git show 942b3ac6ad:devdoc/DOC-INDEX.tsv | grep -c "devdoc/merge-compile-only-policy.md"
3
$ git show 9adad635b:devdoc/DOC-INDEX.tsv | grep -c "devdoc/merge-compile-only-policy.md"
0                       # master already carries the clean-up, and the FILE is gone there
```

So this run used the whole-corpus mode, which de-duplicates as a side effect; the
manifest diff is exactly that clean-up plus this doc's row:

```
$ git diff --stat devdoc/DOC-INDEX.tsv
 devdoc/DOC-INDEX.tsv | 5 ++---
$ git diff devdoc/DOC-INDEX.tsv | grep -E "^[-+]" | grep -v "^[-+][-+]"
-devdoc/merge-compile-only-policy.md  normative  …
+devdoc/merge-compile-only-policy.md  historical …
+devdoc/orphan-landing-01a142f5-bookkeeping-20261017.md  historical  …
-devdoc/merge-compile-only-policy.md  normative  …   (the byte-identical copy)
-devdoc/merge-compile-only-policy.md  historical  …   (the different-class third row)
```

Labelled: I did **not** delete `devdoc/merge-compile-only-policy.md` itself (it
still exists here and not on master — removing another writer's document is not
this run's call); only the duplicated MANIFEST rows collapsed. Reported, not
fixed.

Corpus consistency after the write (the gate Rule 36 protects, run as its own
read-only question):

```
$ python3 tools/docclass/classify.py . --check ; echo EXIT=$?
15 row(s) whose stored text is not what the classifier renders today (reported, NOT fatal: re-derive ONE with `--only <path>`)
EXIT=0
```

`EXIT=0` is the GAP count — no unclassified file, no duplicate row, no gone-file
row. The 15 STALE rows are this branch's pre-existing baseline (the cut point's
manifest was not regenerated on every commit, which the generator's own help text
calls "the expected state of a green tree"), and none of them is this doc.
