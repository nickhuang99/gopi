<!-- doc-class: normative -->
# Bare `master` Is Not a Revision — a Host Carries Several, and Only One Is the Integration Truth

**Status**: normative (this file states a standing measurement rule).
**Measured**: 2026-10-13 on `hosta` (client host), by task
`01a13394-a0d9-7626-80c9-7b897cac2c70`.

## The defect this file closes

A briefing, a bug report, or a gate verdict that says *"gate X is red on master"*
**without naming a revision cannot be checked by anyone else** — because "master"
is not one thing on a host. Measured on this host at 2026-10-13T15:50:29+08:00,
one command, three different commits:

```console
$ git ls-remote origin refs/heads/master          # the ONLY authority
e7f3bf2d19673cc6efa03425cf708d9032c164f2	refs/heads/master

$ git rev-parse refs/heads/master                 # this host's own main checkout
64b8a316af16c3ab6333ec06d65e868000815652

$ git rev-parse refs/remotes/origin/master        # the fetch CACHE
e7f3bf2d19673cc6efa03425cf708d9032c164f2
```

`refs/heads/master` (64b8a316a, 2026-10-13 10:22:49) is **141 commits behind**
the peer's master (e7f3bf2d1, 2026-10-13 15:35:06) and is its **pure ancestor** —
not diverged:

```console
$ git rev-list --count refs/heads/master..refs/remotes/origin/master
141
$ git merge-base --is-ancestor refs/heads/master refs/remotes/origin/master && echo PURELY-BEHIND
PURELY-BEHIND
```

The consequence is measured, not hypothetical: a real fix (`a4a2b64e0`,
`fix(config): re-point CONFIG.md's prompt read-at anchor …`) is **not an ancestor
of `refs/heads/master`** while being an ancestor of the integration tips — so
running a gate against the bare word "master" on this host reports a
**fixed** defect as still red. That is the failure mode this file exists to stop.
(Historical instance of the same trap: memory record #10297 / #10289; task
report `tasks/01a1331864757265984080e74d62ca45-report.md`.)

## The rule

1. **A statement about "master" must name a revision.** Write
   *"red at `e7f3bf2d1`"*, not *"red on master"*. A revision-less claim is
   unfalsifiable and must not enter a briefing, a report, or a verdict.
2. **The revision is obtained from the peer, never from local state**:
   `git ls-remote <peer> refs/heads/master`. This reads no local cache and is
   the only answer that cannot be stale by construction.
3. **The local checkout's `refs/heads/master` is the weakest evidence of the
   four** — it is a working area, advanced by whoever happens to be standing in
   it, not a mirror of the integration branch. Reading a gate off it reads
   whatever that checkout last did.
4. **Verify a revision locally without checking anything out** (Rule 29 —
   a foreign worktree is never a scratch target):
   `git archive <rev> | tar -x -C /dev/shm/redproof-<x>` then run the gate there
   (scratch under `/dev/shm`, off the `/` partition — Rule 22).

## Why `refs/heads/master` stops advancing on this host — the mechanism

The measurement, all from `git reflog show --date=iso refs/heads/master`:

| verb in the reflog | count |
|---|---|
| `pull: Fast-forward` | **84** |
| `merge refs/remotes/gitPushcheck/master: Fast-forward` | 3 (last: 2026-10-12 13:37:54) |
| `commit:` | 2 |
| `reset:` / `clone:` | 1 / 1 |

and the dated tail of the reflog:

```
64b8a316a refs/heads/master@{2026-10-13 10:22:49 +0800}: commit: docs(devdoc): the PA's notify-to-a-live-worker …
003304024 refs/heads/master@{2026-10-13 10:09:12 +0800}: pull: Fast-forward
60cf0a4d5 refs/heads/master@{2026-10-13 09:49:29 +0800}: pull: Fast-forward
…
```

**`refs/heads/master` moves only when somebody is *standing in the main
checkout* (`/home/user/work/GoPi`, the only worktree holding that branch) and
issues a git command that fast-forwards it. Nobody is.** That tree is dirty-free
but idle: the resident processes are a worker and a merger
(`/home/user/work/GoPi/bin/gopi`, pids 3737665 and 4052615), and **neither is
`serve`**.

Who *could* advance it, and why neither leg does today:

- **`serveGitSyncSweep(role string)`** — declared at
  `cmd/agent/cli_tasks_sync_git.go:120` as `func serveGitSyncSweep(role string) {`
  (the role names the face that fired the pass) — is the one
  resident door to `GitSyncClientPass` (`internal/tools/task_sync_git.go:376`).
  Its callers are the **serve** tick (`cmd/agent/cli_tasks_sync_pass.go:141`),
  the **merger pass** (`cmd/agent/cli_tasks.go:5874`, `mergerSerialFaceLegs`) and
  — since 2026-10-13, the repair this doc's own diagnosis called for — the
  **resident worker round** (`cmd/agent/cli_tasks_worker.go:638`), which is the
  only one of the three a client host without serve and without a merger runs.
  It is a *serve/merger/worker-round* step, so it ticks only while such a pass is
  actually running here — start time 15:16 and 15:47, no tick since.
- **`runTaskPushGit()`** (`gopi task push`, `cmd/agent/cli_tasks_sync_git.go:123`)
  is the same atom by hand — a human action, not a cadence.
- The **close-out chain** does not carry it: the reflog's only three
  `merge refs/remotes/gitPushcheck/master` entries are 2026-10-12 13:23–13:37,
  while **111 of the 112 commits** the peer landed since sit at
  `2026-10-13 10:40–15:47`. A run's close-out therefore cannot be what has been
  keeping this ref current.

The fetch lane, by contrast, **is** running and is current: the push-back check
mirror `refs/remotes/gitPushcheck/master` was force-fetched at 15:47:51 and is
**exactly** the peer's tip (`e7f3bf2d1`) — and `refs/heads/master` is its pure
ancestor (`merge-base --is-ancestor` = yes). So the evidence says the peer's
commits **arrive on this host**; what is missing is the step that **moves the
local branch ref**, and that step is reached only from a face that is not running.

**Conclusion (the honest form): the client host has no *fetch cadence* that
advances `refs/heads/master` — the ref is advanced by an intermittent local
action (a serve/merger pass, or a human `git pull`), and such actions stopped
long enough for 141 commits to accumulate.** The mechanism that must keep it
current (the refspec-less-`git pull` flaw and its replacement,
`gitSyncFFIntegrationBranch`, `internal/tools/task_sync_git.go:530`, and the
replacement's own commit `711f9c3aa`) is **already an ancestor of every tip
measured here** — so this is **not** "an old binary needs updating": the current
code, run from the right face, does the right thing. What is missing is a face
that runs it on a cadence on this host.

### Partial coverage (set by the instruction, not by measurement)

The instruction for this task set the framing as "the root cause of why the ref
does not advance". Accordingly this file gives the *contributing* measurement
above and **explicitly does not** go on to select, build or decommission a
cadence owner: which face on a client host owns advancing `refs/heads/master`
(a dedicated fetch leg, or an explicit declaration) is a **system-layer**
decision under **Rule 28** — `sync`/`git`/ref machinery is `peerbox`'s. It is
filed as a local bug row rather than fixed here.

## What a consumer must do instead

```bash
# 1. get the truth (peer, no cache)
REV=$(git ls-remote origin refs/heads/master | awk '{print $1}')
# 2. measure AT that revision, without a checkout
mkdir -p /dev/shm/redproof-$$ && (cd /home/user/work/GoPi && git -C /home/user/work/GoPi archive "$REV") | tar -x -C /dev/shm/redproof-$$
cd /dev/shm/redproof-$$ && go test ./internal/standards/ -run '<the gate>' -count=1
```

A gate result quoted in a report carries the **revision it was measured at**, or
it is not quotable.

## Relation to the fetch cache

`refs/remotes/origin/master` is a *cache*, and on this host it happened to be
current (it equals `e7f3bf2d1`; the mirror ref was fetched at 15:47:51). That is
a coincidence of recency, not a guarantee: the reference-notes incident of
2026-10-05 left exactly this ref at `2e5b4b942` — 56 commits stale — and the
cached ref was mistaken for the truth then too (task
`01a13318-6475-7265-9840-80e74d62ca45`). **A cache is not an authority;
`ls-remote` is.**

**Check** (Rule 38: a census figure names its generator; here the generator is
`git` itself, and every number above is a command printed with its output):
`git ls-remote origin refs/heads/master` · `git rev-parse refs/heads/master
refs/remotes/origin/master` · `git rev-list --count
refs/heads/master..refs/remotes/origin/master` · `git merge-base --is-ancestor
refs/heads/master refs/remotes/origin/master` · `git reflog show --date=iso
refs/heads/master` · raw streams kept in the filing task's session directory
(`evidence/refs-root-cause.txt`, `evidence/acceptance-ground.txt`).
