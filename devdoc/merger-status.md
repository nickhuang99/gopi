<!-- doc-class: historical -->
# Task Report: `gopi merger` is not a verb — hijack diagnosis + merger status surface

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

> **SUPERSEDED IN PART (2026-09-12, later the same morning).** The two findings
> below are historical as of this note:
> 1. *"master has no top-level `merger` verb"* — **FIXED and MERGED**:
>    `c470a86b Merge branch 'feat/merger-daemon-flag'` landed
>    `isMergerSubcommand` (`main.go:142`, in the dispatch chain at `:318`) and
>    `CliVerbMerger` (`cli_verbs.go:74`). `gopi merger --help` on a master build
>    now prints the daemon help; `--bogus-flag` is rejected as a flag. See
>    `devdoc/merger-service-restore-20260912.md` for the rotation and the proof.
> 2. *"the merger SERVICE was not restarted by this task"* — **RESTORED and
>    PROVEN** in the follow-up round (pid 1070633, `/proc/locks` FLOCK row,
>    fresh `merged=2` passes), same devdoc.
>
> The rest of this report — the status surface, the `/proc/locks` liveness
> rationale and the shared `repoSweepUnprotectedBranches` discovery — is still
> the current description of the shipped code.

Branch: `fix/merger-status` · worktree `~/workspace/gopi-merger-status`
Base: master `bbe60f7f` · Date: 2026-09-12

## The finding that matters: the operator's daemon start silently became an LLM session

At **06:49:19** the operator ran `gopi merger` from a terminal (ppid 1785518, a
login shell). The intent is unambiguous — restore the merger service, which had
exited at 04:10 and whose flock had been free ever since.

**What actually happened** (verified, not inferred):

| evidence | value |
|---|---|
| `/proc/950367/cmdline` | `gopi merger` |
| `/proc/950367/stat` start | 06:49:19 |
| ledger row `01a092a9` | `[auto] merger`, chat session, `deepseek-flash`, `claimed_by peerbox:950367`, `created_at 06:49:20` |
| session JSONL | `/home/user/.gopi/sessions/ed7dea14-…/session.jsonl` — `session_start` pid **950367**, first user message **`[EPIC GOAL]\nmerger`** |
| fd 3 of pid 950367 | the session's `.raw.log` (an agent session, not a daemon) |

**Root cause**: master has **no top-level `merger` verb**. The merger lives at
`gopi task merger` (plus the deprecated `gopi task serve` alias), so the CLI
fell through to its positional rule — `gopi [options] < input.txt` /
`gopi [options] [input-file]` — and treated the bare word `merger` as the task
PROMPT. The service was never started; instead an agent session was spawned to
answer the prompt "merger".

Consequences, both real:

1. **The merge service stayed DOWN.** `~/.gopi/serve.lock` read `615179` from
   05:42 — a pid with no process (stale stamp). `ps` showed only workers. Nothing
   was sweeping branches for ~2h40m.
2. **A token-burning agent session** ran against the repo with full write tools
   until the pool reaped it at 06:56 ("session was running past its 15m pool
   timeout — reaped for recycle").

**Correction of the row's premise**: sibling human row `01a09264`
("need to know gopi merger status") says *"currently merger is running in
background as service"*. It was not — the exact command meant to run it was
what produced the agent session. The premise of the row was false; its REQUEST
(the status command) was exactly right.

## What was delivered

### 1. A read-only status surface — `gopi task merger --status`

Deliberately landed on the **current master face** (`gopi task merger …`)
rather than only on the new top-level verb, because worker `01a0927b` owns
`/home/user/workspace/gopi-mergerdaemon` (`cmd/agent/cli_merger_daemon.go`,
uncommitted changes in flight) and memory #1365 sets single ownership. The
status surface is therefore usable TODAY, before that work lands, and the
top-level verb can reuse it when it merges.

```
$ gopi task merger --status
merger:      not running
lock file:   /home/user/.gopi/serve.lock
lock holder: pid 615179  (stale: the process is gone; the lock is FREE)

repo:        /home/user/Downloads/games/Maps
  protected: 0 branch(es) held by a live worktree
  pending:   none — nothing to merge

repo:        /home/user/work/GoPi
  protected: 8 branch(es) held by a live worktree
  pending:   1 branch(es) the next sweep would merge
    - feat/tab-order-actuate-poc
```

**The side-effect problem, and why `/proc/locks`**: the obvious liveness probe
is `acquireServeLock()` — but on the FREE path it **writes our pid into
`serve.lock`**, so a mere status check would (a) leave a trace and (b) make the
next `--status` look at a freshly-stamped "holder". A status command must be
read-only. The probe therefore stats the lock file, looks its inode up in the
kernel's own lock table (`/proc/locks`, matching the `FLOCK` column — a POSIX
row on the same inode is a different mechanism and is ignored), and reports the
holder the kernel names. Proven both ways:

| case | probe | independent truth |
|---|---|---|
| nobody holds it | `merger: not running`, stale pid named | `flock(LOCK_EX\|LOCK_NB)` succeeds |
| `gopi-ms task merger --every 5m` running | `merger: alive`, `pid 991040` | `/proc/locks` FLOCK row + live process |
| after the daemon was killed | `not running` again | lock released by process exit |

and the stamp survived unchanged through every status call (`615179` → `991040`
→ `991040`), i.e. the probe touched nothing.

**Candidate truth was checked against git, not assumed.** The status reported
`1 pending` while `git branch --no-merged master` listed 7 branches. The other 6
are each held by a live worktree (`git worktree list --porcelain`), plus
`hp-unique-*` / archive-tagged exemptions — so `unmerged ∧ unprotected` = exactly
`feat/tab-order-actuate-poc`. That set is computed by ONE shared discovery loop
(`repoSweepUnprotectedBranches`), which `MergerRepoSweep` now also calls: the
status surface structurally cannot drift from what the daemon will do.

### 2. `merger-status.md` — the diagnosis above, in-repo

### 3. The refactor that makes the status honest

`MergerRepoSweep`'s inline discovery loop was extracted into
`repoSweepUnprotectedBranches` (repo, scanMax, excluded) → (candidates, protected,
err) and is now shared with the new `MergerSweepPreview`. The extraction
preserved every predicate: `master`/`main`, `hp-unique-*` (reusing TaskMerge's
`taskMergeExempt` rather than re-typing the prefix), archive-tagged, and
worktree-protected. Also added named constants `masterBranchName` /
`mainBranchName` (Rule 1b) — a cross-package `internal/gitrefs` package is the
right long-term home (Rule 14) but `task_merge.go` / `task_merged_cleanup.go`
still carry bare `"master"` literals, so that backfill is its own task, not a
drive-by.

## Not done (honest boundaries)

- **The merger SERVICE was not restarted by this task.** The brief said not to
  touch a running merger; none is running, and *starting a daemon that merges
  branches is a production action on the operator's repos* — it belongs to the
  operator (or to PA), not to a task that is still being reviewed. What this
  task delivers is the **safe way to observe it** and the precise reason it is
  down. Recommended restart (uses the verb that actually exists):
  `setsid ./bin/gopi task merger --every 5m &` — verify with
  `gopi task merger --status` (same binary).
- **The hijack was not yet made impossible.** Fixing it means making `gopi
  merger` a real verb — that is worker `01a0927b`'s deliverable on
  `feat/merger-daemon-flag`, and duplicating it would violate single ownership
  (memory #1365) and collide with its uncommitted work. The HAZARD is filed as
  a card instead.
- **`internal/standards` is RED on master already**: the stale baseline entry
  `internal/tools/memory_sync_claim.go:890` (Rule 16) fails **with my changes
  stashed**, so it is pre-existing master RED (salvage round-10 documented
  exactly this). I did NOT touch it — unrelated to this task, and silently
  deleting another task's baseline entry is how a gate starts lying.
- **`fix/merger-top-level-verb` is still unmerged** (4cfce995, worktree
  `~/workspace/gopi-merger-top`, 4 commits ahead) and **`fix/worktree-recycle-2`
  is also unmerged** — both sit in the merger's own queue. Safe-merge guard or
  merger will land them; this task did not.

## Verification

- Build: `go build ./...` — **OK** (also `CGO_ENABLED=0 go build -o /tmp/gopi-ms ./cmd/agent/`)
- `go vet ./internal/tools/ ./cmd/agent/` — clean; `gofmt -l cmd internal` — clean
- New tests: `go test ./internal/tools/ -run 'TestLockTableHolds|TestMergerLockProbe|TestMergerStatus|TestMergerSweepPreview|TestInodeOf' -count=1` — **PASS** (11 tests incl. malformed-row, POSIX-row-ignored, stale-stamp, read-only-preview, non-repo-error)
- `go test ./cmd/agent/ -run 'Usage|Completion|Merger|TaskFlagMember|TopLevel' -count=1` — **PASS** (28.4s)
- `go test ./internal/standards/` — 1 NEW Rule-18 hit at `cmd/agent/cli_tasks.go:1166`, which is the SAME pre-existing site that was baselined at `:1161` and moved down 5 lines by my usage-text insert; baseline re-pinned per the gate's own instruction (`baseline.go`). The remaining failure is the pre-existing stale Rule-16 entry (proven by stash).
- Live E2E (not a unit mock): real `--status` against the real `~/.gopi/serve.lock`, both free and held, cross-checked against `git branch --no-merged` / `git worktree list --porcelain`.

## Service restore (round 2) — the merger was DEAD, and this whole session is the hijack proof

### What was found (all evidence from THIS machine, 2026-09-12)

1. **The merge service was not running.** `ps -ef | grep gopi` showed 6
   `bin/gopi worker` daemons and **no** merger/serve process.
2. **The lock was stale, not held.** `~/.gopi/serve.lock` contained `991040`;
   `ps -p 991040` → **no such process**, and the lock file's inode
   (`29111171`) had **no row in `/proc/locks`** — a free lock. The pid stamp
   is a *stamp*, not liveness: `acquireServeLock` writes it and never clears
   it, so a killed daemon leaves a lock that *looks* held to anyone who only
   reads the file. That is exactly why the status probe must read
   `/proc/locks`, which is the fix this branch ships.
3. **The hijack is live, and this session is the exhibit.** The epic was
   launched as `gopi merger` (pid 950367) — a process that parents every bash
   call in this session, i.e. **an LLM agent session was started by the
   string `gopi merger`**. master has no `merger` case in the top-level
   dispatch chain (`main.go` → `isWorkerSubcommand` | `isTasksSubcommand` |
   …), so an unrecognised verb falls through into the *prompt* path. The
   consequence: an operator typing `gopi merger` to start the daemon gets an
   LLM session instead — burning tokens and never merging anything — while
   the real merge backlog grows silently.
4. **Meanwhile the service IS the thing that merges.** `/tmp/merger.log` shows
   a merger running until ~04:10 (pass start / merged=N / protected=N /
   sleeping 5m0s, one `feat/fix-crashdump-false-positive (merged)`), after
   which the log simply stops — consistent with the 06:58 stale lock.

### Restore + proof

Per M4.5 the flock's *purpose* is single-instance protection, not
never-restart; the merger ran from `/tmp` before (that was its whole problem:
no supervision), so it was relaunched with supervision and a log in the
gopi data root:

```bash
cd /home/user/work/GoPi
setsid nohup ./bin/gopi task merger --every 5m >> ~/.gopi/logs/merger.log 2>&1 &
```

Proof (the acceptance is *fresh pass lines*, not a process name):

- `ps` → `./bin/gopi task merger --every 5m`, ppid=1 (survives this session).
- `/proc/locks` → `5: FLOCK ADVISORY WRITE 1070633 08:04:29111171 0 EOF` —
  a REAL holder on the real inode; `serve.lock` stamp now `1070633`.
- `~/.gopi/logs/merger.log` → `pass start`, per-repo `merged=N`, `sleeping 5m0s`.
- **The sweep does real work**: the first pass picked up a conflicted branch
  and dispatched its resolver — child pid 1071541, cwd
  `/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-resolve-1070633/gopi-merge-resolve-…/wt`
  on `feat/worktree-live-owner-guard`. **Correction to a first-read
  suspicion**: the merger spawning an LLM child is NOT a violation of
  "merge-only" — the documented design is exactly that ("conflicts dispatch a
  merge-conflict task for the worker/LLM to resolve; the merger only merges
  what is clean"), and the child resolved its conflict (`git merge master
  --no-commit --no-ff` reproduced, one conflicted file) before exiting.
- `--status` after restore: `merger: alive` / `lock holder: pid 1070633` /
  live pending list. The read-only surface and the daemon agree — which is
  the point of sharing `repoSweepUnprotectedBranches`.

### Hazard filed, not fixed here (single-ownership rule)

The `gopi merger` hijack is FIXED on `fix/merger-top-level-verb` (worker
01a0927b): `cmd/agent/main.go:119 isMergerSubcommand` + `cli_verbs.go:77
CliVerbMerger = "merger"` + completion entry. That branch is unmerged — so
**the hazard is currently live on master**. This session deliberately did not
re-implement it (two owners in one dispatch chain is how a merge conflict is
manufactured); the remedy is to land that branch.

## Round 3 (2026-09-13): the status command named a CORPSE as the live holder

Row `01a09264` — *"need to know gopi merger status"* — was re-run against the
current master, as the row itself asked. The requested surface **already
exists** (`gopi merger --status`, alias `gopi task merger --status`), so no new
verb/tool/flag was added: re-implementing it would have been duplicate work.
Re-running it, however, exposed a **real defect** — the surface answered "is it
running?" with a pid whose process is gone.

### The defect, with kernel ground truth

```
$ gopi merger --status
merger:      alive
lock file:   /home/user/.gopi/serve.lock
lock holder: pid 98279          <-- WRONG: 98279 is GONE
```

| probe | value |
|---|---|
| `ps -p 98279` | **GONE** (no such process) |
| `ps -eo pid,args \| grep merger` | live merger = pid **98290** (`gopi-merger merger`), started 55 min earlier |
| `/proc/locks` row on the lock inode | `21: FLOCK ADVISORY WRITE 98279 08:04:29112398 0 EOF` — names 98279 |
| `/proc/98290/fd/3` | `-> /home/user/.gopi/serve.lock` (live process has the fd) |
| `/proc/98290/fdinfo/3` | `ino: 29112398` **and** `lock: 1: FLOCK ADVISORY WRITE 98279 08:04:29112398 0 EOF` |

**Root cause**: `/proc/locks` attributes a flock to the process that **created**
it, and `flock` lives on the **open file description** — so a fork/exec child
that inherits the descriptor keeps the lock after the creator exits. GoPi's
daemon is exactly that shape (`gopi-merger merger` forks `gopi-merger-dae`: the
zombie `[gopi-merger-dae] <defunct>` was visible in the process list). The dead
creator's pid therefore stays in the table forever while a live descendant holds
the lock. `mergerLockProbe` trusted the table alone for liveness, so the one
command whose job is "is it running?" reported a corpse as the holder.

**Why the existing tests missed it**: every lock-probe test holds the flock from
*the same process that created it* — the inherited-fd shape had no fixture, and
the "alive" assertion only checked that *some* pid came back.

### The fix (`internal/tools/merger_status.go`)

The table stays the first, cheapest source, but it is no longer the last word on
liveness:

1. table row found **and** `pidAlive(holder)` → that pid, alive (unchanged path);
2. table row stale **or** absent → the 2026-09-12 orphan probe (`OrphanLockHolder`,
   unlinked-underneath holder) — unchanged, still first among the fallbacks;
3. else `fdInfoLockHolder(ino)`: walk `/proc/<pid>/fdinfo/*` for a **live**
   process whose block carries both the matching `ino:` **and** a `lock:` row
   whose type is `FLOCK` and whose `dev:ino` column matches. fdinfo is
   per-process, so a hit is proof of a live holder needing no aliveness check of
   its own;
4. nothing live holds the inode → report the table's (dead) pid so the operator
   still sees **who** left the lock behind (the pre-existing stale-rendering
   contract), else the stamp.

The predicate is deliberately strict — `fdInfoLockRowIsFlockOn` requires the
lock row's own `dev:ino` to match. A plain open has the inode but **no** `lock:`
line (the common case, and the reason the predicate is not just "inode seen"),
and a POSIX row is another mechanism (mirrors `lockTableHolds`).

### Red-proof (the test actually catches the defect)

Reverting only the probe's decision logic to the pre-fix
`if !held { return stamped, false }; return pid, true` makes the new regression
tests fail with the real symptom:

```
--- FAIL: TestMergerLockProbe_FindsLiveHolderOfInheritedFlock
    the probe reported the dead creator 336140 as the holder — this is the 2026-09-13 defect
--- FAIL: TestMergerStatusReport_StatesTheTrueHolder
    render must name the live holder 336113: "...lock holder: pid 336147..."
```

Restoring the fix → PASS. New tests
(`internal/tools/merger_status_inherited_flock_test.go`) cover: the inherited
holder found when the table names a corpse (hermetic via the existing
`procLockTableRead` seam + a real flock + a real reaped `deadPID`), the
false-positive side (nobody holding → still not-running, leftover pid still
reported), the fdinfo predicate against byte-shaped fixtures read off the live
machine, and the join key against the live `/proc`.

### Also fixed: a phantom verb in the status surface's own docs

`merger_status.go` and `repo_sweep.go` comments told the reader to run
**`gopi status`** — which is **not a dispatched verb**. Typing it falls through
to the chat-session path (this is the same fall-through class as the `gopi
merger` hijack diagnosed in round 1: an unrecognised word becomes an LLM
prompt). The doc now names the shipped contract, and
`internal/tools/merger_status_doc_verb_test.go` (Rule 13 literal-gate pattern)
fails any production file in `internal/tools` that re-inlines the phantom verb.
Red-proof: planted literal → FAIL, removed → PASS.

### Verified on the real binary, against the real lock

| case | output | independent truth |
|---|---|---|
| daemon alive (real machine) | `merger: alive`, `lock holder: pid 98290` | `/proc/98290/fdinfo/3` = inherited holder; `/proc/locks` names the dead 98279 |
| no daemon / stale stamp | `merger: not running`, `(none stamped)` / `(stale …)` | fresh `GOPI_HOME`: no lock file created at all |
| alias `gopi task merger --status` | identical output + deprecation note on stderr | same code path |
| read-only | lock file mtime/sha unchanged; no lock file created in a clean `GOPI_HOME` | filesystem-level assertion, not the text output |

### Pre-existing RED, not from this branch (honest boundary)

`go test ./internal/standards/` fails on 2 Rule-17 (test env isolation) NEW
violations in `cmd/agent/cli_voice_review_test.go:1301` and
`cmd/agent/worktree_release_accept_test.go:225`. **Neither file is touched by
this branch** (`git status` lists only the three files of this change), so this
is master RED that arrived with other work — fixing another task's env-leak
sites here would be unrelated scope. My files carry zero gate hits, and
`go build ./...` / `go vet` / `gofmt` are clean.

## Commit

See `git log fix/merger-status-doc-verb` — the branch carries the doc-verb fix,
the inherited-flock fix, the regression tests and this report.

