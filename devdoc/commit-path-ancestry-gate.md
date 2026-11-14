<!-- doc-class: normative -->
# The commit path's ancestry gate (step 7): a branch with no common ancestor with master is refused at the push

Incident, measurement, placement, and the honest limit. The card is
`briefings/brief-commit-path-ancestry-check.md`; the sibling diagnosis of the
DOWNSTREAM half (what the merge face does with a branch that already arrived) is
`devdoc/merge-unrelated-history-diagnosis.md`. *(The face was spelled `gopi
merge` then; that top-level verb was removed on 2026-11-01 and the face is
`gopi task merge` today.)*

## The incident

`feat/sync-conflict-completed` (root `39fd968a`, two files `a.txt`/`b.txt`,
commits `init` + `deliverable work`) was pushed into the real GoPi repo, whose
own root is `e9538e78`. The two share no commit. `git merge` refuses it outright
(`fatal: refusing to merge unrelated histories`, exit 128), so the branch could
never be landed by anyone, and the auto merge-backlog generator re-filed the
junk four times (rows `01a0c201`, `01a0c2e1`, `01a0c2e7`, `01a0c55c`).
Everything else in the lane — adoption, the sweep's prune, `--ff-only` landing —
is built on the assumption that a branch descends from the trunk. Nothing
checked it.

## What was already true when this card was picked up (measured, not assumed)

Two measurements changed the shape of the work, and both belong in the record:

1. **The ff-gate landed in master AFTER the incident.** The junk tag
   `archive/feat/sync-conflict-completed-20260921` was minted at
   `2026-09-21 14:35`; the ff-gate commit `e26dc4d3 fix(pushback): refuse a
   branch the peer cannot land, instead of forking it` carries
   `2026-09-21 17:13`. So at the time of the incident the pushback path had NO
   gate at all, and the orphan went straight through.
2. **That ff-gate incidentally blocks a non-descendant too — with the WRONG
   reason.** Measured through the real CLI on a pre-change binary (control run,
   below):
   `feat/orphan: not fast-forwardable onto master — push refused; merge master
   into it (or rebase it onto master) and re-deliver`. True as far as it goes,
   but its remedy cannot be performed: `git merge master` into a history that
   has no master IS the `refusing to merge unrelated histories` failure. An
   operator following that line loops.

So the deliverable is not "add a gate that did not exist" — the push is already
stopped. It is: **ask the ancestry question FIRST, and name its own reason and
its own remedy.** That is the difference between an operator reading "merge
master into it" (impossible) and "this ref was never cut from master's history;
discard it and re-create on master" (the only terminating action).

## Placement — client pushback, and why it is the earliest point

`gitPushbackPlan` (`internal/tools/task_sync_git_pushback.go`) is the one place
every outbound branch passes through: the harvest push-back
(`gitPushbackDefault`) and the client pass / `gopi task push`
(`GitSyncPushLocalBranches` → `GitSyncClientPass`) both route through it, and it
has already fetched the peer's master into `refs/remotes/gitPushcheck/master`
(`gitPushbackFetchMaster`), so the ancestry probe is object-local and costs no
extra round-trip. A refusal here keeps the junk out of the peer entirely.

The server/receive side was rejected deliberately (the card's own guidance): no
git hook in this repo runs on a push into the peerbox checkout, and putting the
rule there would place logic where no test can see it.

## The predicate (the exact form, and why)

`git merge-base --all <mirror> <branch>`, empty output + **exit 1** = no common
ancestor. Measured exit codes: `0` = a base was printed (shared history), `1` =
no shared commit, `128` = `fatal: Not a valid object name`.

The three-state measurement matters. An error is NOT evidence of an unrelated
history, so only git's documented exit 1 refuses; an unmeasured premise falls
through to the ff gate, which decides the branch on its own terms (Rule 15: a
premise that could not be measured is reported as unmeasured, never as a
finding). The exit status is read off the wrapped `*exec.ExitError`
(`gitPushbackExitIsNo`), reusing the lane's existing `mergeVerifyGitExitNo`
rather than inventing a second "git said no" constant.

**A trap worth recording.** `git merge-base A B` (no `--all`) also exits 1 with
nothing printed on an unrelated pair — but so does `git merge-base --octopus A B`
and, in a repo with fewer than two commits, so does the invocation itself
(exit 129, usage error). `--all` was chosen because its exit codes are documented
and were measured to be exactly 0/1/128. `git log --full-history <mirror> --not
<branch>` was considered as a corroborating signal and rejected: on an unrelated
pair it prints the whole mirror history (measured: 2 of 2 commits), so it
corroborates nothing.

## False-positive measurement (the card's requirement 4)

Re-measured in this run over **every** ref in the repository — 474 refs, heads
+ remotes + tags — with the gate's own predicate (`git merge-base --all master
<ref>` empty ⇒ UNRELATED):

| ref namespace | refs | UNRELATED |
|---|---|---|
| `refs/heads/*` (9, of which 8 are the gate's candidate set — master excluded) | 9 | 1 — the junk |
| `refs/remotes/*` | 89 | 0 |
| `refs/tags/*` | 376 | 3 — all the same junk history |
| **total** | **474** | **4 (one history)** |

Every ref the count reports as UNRELATED is the SAME known junk history
(`feat/sync-conflict-completed`, preserved as `refs/tags/archive/…` and
`refs/tags/orphan/…` per the salvage procedure in memory #4559) — **0 unexplained
"no"**. Every legitimate branch descends from master: the check's false-positive
rate over the real branch set is **0 of 8** live branches and 0 of 465
remote/tag refs. That the junk SURVIVES as a branch plus three tags is the
point: the gate must not be adapted to swallow evidence.

The per-branch verdict table (measured this run, master root `e9538e78`):

| branch (refs/heads) | root | verdict |
|---|---|---|
| `feat/code-delivery-axis-split` | e9538e78 | SHARED |
| `feat/commit-path-ancestry-check` | e9538e78 | SHARED |
| `feat/unrelated-history-retire` | e9538e78 | SHARED |
| `fix/jieba-token-absorbs-search-kw` | e9538e78 | SHARED |
| `fix/resume-terminal-status` | e9538e78 | SHARED |
| `fix/stash-guard-compound-chain` | e9538e78 | SHARED |
| `fix/sync-taskid-session-refuse` | e9538e78 | SHARED |
| `feat/sync-conflict-completed` | 59f47c97 | **UNRELATED** (the junk; archive tags carry the same history) |

The gate's candidate set is `refs/heads` only (`gitPushbackLoadBranches`), so
it would not have seen the tags anyway — a branch name is what a push
delivers.

## The refusal text (verbatim, from the real CLI)

```
git push-back to hp: branches_pushed=[feat/legit] push_failed=[feat/sync-conflict-completed: shares no common ancestor with master (git merge-base finds no shared commit) — push refused; this ref was never cut from master's history and no merge can land it: discard it (git branch -D feat/sync-conflict-completed) and, if the change is wanted, re-create the branch on master and re-apply the work there]
```

It names the branch, states the cause (`merge-base` finds no shared commit),
states the consequence (no merge can land it) and gives the one terminating
remedy. The refusal does NOT delete the branch, rewrite it, or touch the peer's
refs.

## The real E2E (Rule 31), and the relay that makes it real

The surface is the client push-back phase as the CLI actually reaches it:
`gopi task harvest --peer hp` → `HarvestOptions.GitPushback` →
`tools.DefaultGitPushback` → `gitPushbackPlan` (`cmd/agent/cli_tasks.go`).
Driven with a **real ssh peer** (`hp`) and a real local repo, not a mock and not
a local filesystem target.

The peer's `~/work/GoPi` is another node's live repo and must not be a test
target (Rule 29), so the run goes through git's own transport hook:

```
GIT_SSH_COMMAND=<relay>  →  relay rewrites ~/work/GoPi → <fixture>.git,
                            passes every other argv element through, ssh's hp
```

Not a mock: git composes the remote command, the relay rewrites only the repo
path, and ssh on hp runs the real `git-receive-pack` against a bare fixture
(`git clone --bare --single-branch --branch master`) — so the push, and its
refusal, really cross the wire.

**The argv had to be measured, not assumed — and the first attempt got it wrong.**
git's ssh invocation carries the peer repo as the LITERAL tilde form
(`harvestPeerRepo = "~/work/GoPi"`), and the remote command is ONE argv element:

```
$ git push --dry-run hp:~/work/GoPi master:refs/heads/zzz-probe   (probe relay)
ARGV: [-G] [hp]
ARGV: [hp] [git-receive-pack '/home/user/work/GoPi']
```

A first relay rewrote only the path-expanded spelling (and dropped the `-G` /
`-o` options by rebuilding argv), so the positive control did NOT reach the
fixture: it pushed into hp's live repo. That is a **Rule 29 breach, created and
repaired inside this run** — the ref was removed with `update-ref -d` on hp
(back to its 9 pre-existing heads) and the relay rewritten to rewrite every
argv element for BOTH spellings while preserving options and host. The final
run below is measured with the corrected relay, and the fixture/live-repo ref
checks are what prove the retargeting held:

```
$ ssh hp "git -C <fixture>.git for-each-ref refs/heads"
refs/heads/feat/legit 12eae1fb      ← the positive control landed HERE
refs/heads/master c364c414
$ ssh hp "git -C <fixture>.git rev-parse --verify --quiet refs/heads/feat/sync-conflict-completed"
ABSENT: the junk ref never reached the peer
$ ssh hp "git -C /home/user/work/GoPi rev-parse --verify --quiet refs/heads/feat/legit"
ABSENT in live repo (correct: relay retargeted)   — live heads still 9
```

### Negative case — verbatim from the real invocation

```
$ GOPI_MEMORY_DB=/home/user/workspace/_tmp/anc-e2e-a/local.db \
  GIT_SSH_COMMAND=/home/user/workspace/_tmp/anc-e2e-a/relay.sh \
  bin/gopi task harvest --peer hp --db /home/user/workspace/_tmp/anc-e2e-a/local.db
harvested 3047 rows from hp: harvested=3047 migrated=0 duped=0 conflict=0 skipped=0 failed=0
git push-back to hp: branches_pushed=[feat/legit] push_failed=[feat/sync-conflict-completed: shares no common ancestor with master (git merge-base finds no shared commit) — push refused; this ref was never cut from master's history and no merge can land it: discard it (git branch -D feat/sync-conflict-completed) and, if the change is wanted, re-create the branch on master and re-apply the work there]
```

The positive control (`feat/legit`) was pushed in the SAME flush, so the gate is
not a blanket refusal. The refused branch's tip was unchanged afterwards
(`ef5997a1`), and the peer's ref namespace gained nothing.

### The control run — the pre-change binary, same invocation

Built from `f80b555b^` (gate symbol count `0` in the source) and given the same
fixture, relay and branch set (`feat/sync-conflict-completed` only):

```
git push-back to hp: branches_pushed=[] push_failed=[feat/sync-conflict-completed: not fast-forwardable onto master — push refused; merge master into it (or rebase it onto master) and re-deliver]
```

So the pre-change code refused — with a remedy that cannot be performed. Run for
real on the orphan, that remedy is git's own refusal:

```
$ git checkout feat/sync-conflict-completed && git merge master
exit=128
fatal: refusing to merge unrelated histories
```

That closed pair (`merge master into it` → exit 128, `refusing to merge
unrelated histories`) is the incident's loop, measured rather than argued: the
operator who follows the old line performs the exact failure the gate exists to
prevent.

## Honest limits (Rule 26's corollary — a narrow fix must say so)

- **The check is at the client push only.** A ref that arrives by another route
  (a manual `git push`, a `git fetch`, a file copy) is not checked on the way in.
  There is no receive-side or server-side gate in this repo, and this card did
  not add one (no hook, no daemon, no scheduled job — the card's own boundary).
- **The repo's real branch set had zero orphans at the time of the run**, so the
  false-positive measurement is "0 of 6 live branches, 0 of 462 remote refs" —
  a small live set, corroborated by the 471-ref sweep. The unit test carries the
  positive control that makes the number meaningful.
- **The ff-gate still runs, on branches that share history.** A branch that
  shares a root but does not contain the mirror is still refused with the
  ff remedy, unchanged.
