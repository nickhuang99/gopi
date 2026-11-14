<!-- doc-class: historical -->
# Merger: ff-only, park-and-file, never wait

**Epic**: merger-ff-park-file (briefing `briefings/brief-merger-ff-park-file.md`,
operator judgement 2026-09-16) · **Branch**: `fix/merger-ff-park-file` ·
**Worktree**: `~/workspace/gopi-merger-simplify`

> A daemon must be simple. Every branch of logic it carries multiplies its
> failure surface. The merger's logic itself is the defect — not any one bug in it.

This devdoc is the deliverable's argument. It says what the tick IS, what was
**deleted** (with line counts), and how each acceptance item is evidenced.

---

## 1. The design as shipped

A tick is a pure git operation. Per configured repo, in order:

| step | what happens | why it cannot wedge |
|---|---|---|
| ⓪ compile gates | merge-time CLI-contract / branch-build / union-build checks run **before** anything touches master or the branch | they are read-only inspections; a refusal archives the branch and returns |
| ① spent work | `git merge-base --is-ancestor <branch> master` → `git branch -d` | git's own `-d` lock refuses an unmerged ref |
| ② landing | `git merge --ff-only <branch>` → `git branch -d` | `--ff-only` IS the merge: it advances master to the branch tip or fails without touching master. **No merge commit can be minted, no conflict can occur, no `MERGE_HEAD` can be left behind** |
| ③ park | create `refs/tags/archive/<branch>` (branch ref **preserved**) + file ONE ordinary ledger row | the branch leaves the candidate set; the tick moves to the next branch immediately |

Then `MergedCleanupSweep` tidies merged worktrees + already-merged branches, and
the tick-level rebuild invariant rebuilds each repo whose tick landed work.

**Never wait. Never spawn a resolver. Never run an LLM. Never keep in-flight
state across a tick.** There is no state file, no pass clock, no child registry
entry, no grace ladder, no `SIGTERM`/`SIGKILL` escalation.

### Why `--ff-only` is the whole merge, and not a weakening

The previous design ran a real `git merge` and, on conflict, aborted, spawned a
resolver child and waited. `--ff-only` is *stronger* than "abort on conflict":
it removes the possibility of a conflict, a merge commit and a dirty index in
the same statement. Everything a non-ff branch needs is, by definition, a
judgement — so it is handed off instead of performed here.

### Why not keep the clean-merge case in the daemon

The briefing asks for this argument explicitly. The reason the daemon stops at
`--ff-only` is that **"ff-able" is decidable by git alone**
(`merge-base --is-ancestor`), while "clean-but-diverged" is a *judgement*: git
says the three-way merge produced no conflict markers, but whether the result is
*semantically* right (both sides' intent preserved, tests green, build intact) is
exactly what the human/agent reviewing the merge is for. The moment the daemon
performs that merge it must also decide what to do when the *result* is wrong —
which is where the verify-gate child, the wait cap, the kill ladder and the
unreadable status file all came from. Keeping the line at the mechanically
decidable property is what buys the deletion; a merge commit is one step from a
conflict, and this codebase has the incident history to prove it
(`fix/dump-name-collision` merged with "zero conflicts" and did not compile;
`82b05c4f` landed ten undefined symbols with no git conflict).

Cost of the rule, stated honestly: a branch that diverged but would have merged
cleanly is now parked and resolved by a worker instead of landing mechanically.
If that turns out to hand off too much in practice, the fix is to **measure** it
(how many branches/day are clean-but-diverged) and change the rule with data —
not to re-add the code path.

### The park is a ref, never a delete

`refs/tags/archive/<branch>` is the mechanism the repo already has for "this
branch is out of the merge queue, but nothing is destroyed"
(`repoSweepArchiveBranch`, the same ref `scripts/safe-merge.sh` and GOPI.md
Rule 9 key on). The **branch ref itself is untouched**: the parked work is
forever revivable with `git tag -d archive/<branch>`, and `git log
archive/<branch>` still answers what it contained.

### The filed task is an ORDINARY task

`repoSweepFileParkTask` goes through `TaskSubmit` — the ordinary ledger path. A
worker claims it like anything else and it is visible in `gopi task list`. Its
payload (built from ONE format constant, `parkTaskPayloadFmt`) names the branch,
the repo, the park tag, and the acceptance (merge, build, tests), and tells the
executor to drop the tag once the branch has landed. **No merger-specific tags
beyond the `merge-conflict` priority tier + the ordinary auto-dispatch marker** —
so the row dispatches normally (which is *why* the merger-owned tag pair is
excluded: `resolveRowIsMergerOwned` still keeps historical merger rows out of the
worker queue).

---

## 2. What was deleted

Production lines: **+955 / −4897** across 14 files. Six files removed whole.

| file | + | − | what |
|---|---|---|---|
| `internal/tools/merger_graceful.go` | 71 | 993 | state file (writer+reader), interrupt/child records, busy predicate, stop/force request files + watcher, pass clock, pass stats, run-pass/child-registry arm, renderInFlight |
| `internal/tools/repo_resolve_row.go` | 98 | 700 | per-resolve tracking rows, failure counters, rework submit/concluded, resolve-row recovery scan; **kept**: the merger-owned marker pair + the rework lane's summary/tags/stamp/archive helper |
| `internal/tools/repo_sweep.go` | 335 | 421 | conflict arm, resolver seam, resolve-dispatch gate, `repoSweepBranchTip[Advanced]`, all resolve/pending/in-flight/landed/pending-fix counters; **added**: the ff/park tick |
| `internal/tools/merger_status.go` | 12 | 274 | `MergerInterrupt`/`MergerChild` types, `MergerChildState`, writer-live probe, child liveness split, in-flight rendering, three "why is it empty" explanations |
| `cmd/agent/merger_graceful.go` | 97 | 766 | pass state publisher, child exit/await ladder, drain budget, pass statistics, stop-watcher; **kept**: stop flag + signal listener + sliced wait |
| `cmd/agent/cli_merger_stop.go` | 164 | 329 | the durable stop-request file + its waiter, the force-request escalation (`SIGKILL`), the wait ladder, every `pass`/`busy` message |
| `cmd/agent/cli_tasks.go` | 77 | 188 | resolve child invocation, verify gate call, per-child rebuild, resolve recovery leg, pass stats plumbing |
| `cmd/agent/main.go` | 27 | 20 | usage text: tick semantics, the lock (not the pid stamp) as the single-instance authority, retired flags announced |
| `cmd/agent/merger_work_lock.go` | 16 | 11 | the child-exemption markers re-explained (spawns gone, inheritance exemption kept) |
| `cmd/agent/merge_resolve_child.go` | 0 | 225 | **deleted** — the resolve session's child protocol |
| `cmd/agent/merger_verify_child.go` | 0 | 971 | **deleted** — the post-merge verify child |
| `internal/tools/merger_interrupt_legacy.go` | 0 | 143 | **deleted** — the legacy state-file shape decoder (nothing reads a state file now) |

Test lines: **+1002 / −6547**, 24 files removed. The resolve/verify/busy/child-
exit suites tested machinery that no longer exists; the survivor suites were
narrowed to the behaviour that does.

**What SURVIVED on purpose** (and why, since the epic's shape is deletion):
`mergerWorkLock` / the flock guard (every git-mutating face needs it),
`MergerPidAlive` (the stop path's liveness probe), the lock-probe surface of
`merger_status.go`, the child registry (moved to **`cmd/agent/child_registry.go`**
— the WORKER wrapper's stop path still forwards a stop to the round it is
running), and `mergerMasterSafetySweep` (the successor's start-up repair — better
placed than ever: the process that can repair a mid-merge tree is the one that
comes next, alive and holding the lock).

---

## 3. Acceptance evidence

### 1 — The tick has no wait (mechanical gate + named deletions)

`cmd/agent/merger_tick_no_child_gate_test.go` reads the two files a tick is made
of (`internal/tools/repo_sweep.go`, `cmd/agent/merger_graceful.go`) and fails on
`ExtraFiles`, `.Wait()`, `awaitChildExit`, `passChildRegistry`,
`waitForChildren`, `MergeResolve*`, `mergerVerify*`, `RunAgent(` — with a read
floor and a presence control so the absence check cannot pass vacuously.

**Falsified green→red→green**: appending a line carrying `ExtraFiles` to
`repo_sweep.go` produced

```
merger_tick_no_child_gate_test.go:117: ../../internal/tools/repo_sweep.go:790 carries "ExtraFiles"
  — the fd-handoff shape a spawned child needs — a tick spawns no child to hand an fd to
FAIL
```

and removing the line restored `ok`. The deleted functions are named in §2;
the largest are `mergerRunPass`/`mergerStateFor`/`mergerPassOnce`'s child legs
(`cmd/agent`), the resolver seam `MergerRepoSweepOptions.Resolve`, and the whole
`merger_verify_child.go`.

### 2 + 3 — ff lands / non-ff parks and the tick continues

`internal/tools/repo_sweep_park_test.go`, driving real repos and the real ledger:

- `TestRepoSweepFastForwardLandsAndFilesNoTask` — master becomes the branch tip,
  the branch ref is deleted, `rev-list --count master == 3` (a merge commit would
  make 4), and **the ledger gained no row**.
- `TestRepoSweepParksNonFFBranchAndContinues` — the diverged branch is decided
  **first** (`feat/conflict` < `feat/ff`): parked (archive ref on the branch tip,
  branch ref preserved, exactly one row) and the **branch after it in the same
  tick is then merged**. The `res.Outcomes` ordering IS the "did not stop / did
  not wait" proof.
- `TestRepoSweepParkRearmsWhenTheTagIsDropped` — dropping the tag puts the branch
  back in the candidate set (the ticket is git state, not a one-way door).

Full `--once` transcript (evidence 3's log ordering, end to end):

```
===== TICK 1 =====
[merger] […/ffpark/repo] master safety: tip 40244c46b978, no unfinished merge, tree clean
[merger] […/ffpark/repo] branch feat/conflict (parked): not fast-forwardable into master
         — parked at archive/feat/conflict (branch ref kept, resolvable by an ordinary worker task)
[merger] […/ffpark/repo] branch feat/ff (merged): fast-forward into master, branch deleted
[merger] […/ffpark/repo] merged=1 parked=1
[merger] --once: single tick done

===== refs after tick 1 =====
refs/heads/feat/conflict          <- the parked branch ref is PRESERVED
refs/heads/master
refs/tags/archive/feat/conflict   <- the park
refs/tags/version/v20260916-1023-8bb9ee6

===== ledger park rows =====
#01a0a9be  [pending]  resolve merge conflict for feat/conflict in <repo>, parked at archive/feat/conflict
                      (model (unknown), source auto)

===== TICK 2 (dedup) =====
[merger] […/ffpark/repo] merged=0        <- a parked branch left the candidate set
===== park rows after tick 2 =====
1                                        <- still exactly one

===== master log (feat/ff landed, NO merge commit) =====
8bb9ee6 ff work
40244c4 master edit
e7ab0b9 seed
```

The two log lines in tick 1 are the epic in one screen: **the park is decided
first (alphabetically `feat/conflict` precedes `feat/ff`) and the tick then
carries on to land the next branch** — the thing the old bypass-on-conflict
design could not do. The master log's three commits are the other half: the
landing is a fast-forward, so there is no merge commit and no `MERGE_HEAD`.

**A caveat found while running this** (recorded honestly): the first draft of
`run.sh` omitted `GOPI_SETTINGS`, so `gopi merger --once` fell back to the
operator's live settings and swept `/home/user/work/GoPi`. It changed nothing
there (`merged=0`; verified: master tip, `git reflog`, and the ref set were
untouched — the only artifact was a lock-file pid stamp), and the script now
names `GOPI_SETTINGS` explicitly at every invocation. It is worth writing down
because it is the shape of the trap: `GOPI_HOME` sandboxes the *profile*, but
the **repos table comes from settings**, so any merger probe that does not set
`GOPI_SETTINGS` is aimed at the real repositories.

### 4 — Re-running files no second task (dedup across a restart)

Two levels, both in `repo_sweep_park_test.go`:

- `TestRepoSweepParkedBranchIsNotReParked` — at the tick level the dedup IS the
  archive ref: the branch is exempt from the candidate set, so the second tick
  reports no outcome at all and the ledger keeps exactly one row.
- `TestRepoSweepParkBranchRowIsIdempotent` — **the restart case**: the ref is
  gone (a merger that died between `git tag` and `TaskSubmit`; a hand-cleaned
  tag) while the row is live → the branch is parked again, `parkActionDuplicate`
  is reported, and the row count stays 1.

**Red-proof** (acceptance 6): `TestRepoSweepParkDedupIsLoadBearing` bypasses the
probe by calling the writer directly — the regression shape — and asserts the
duplicate DOES appear (2 rows) plus that the probe itself answers "live". Without
it, "exactly one row" could be satisfied by a park lane that files nothing.

### 5 — `--status` with no pass state

`--status` answers only what is still durable — liveness (the flock) and what the
next tick would do (per repo, read from git). Verbatim from the sandbox run
(`.evid/ffpark/out.txt`, last section):

```
merger:      not running
lock file:   …/.evid/ffpark/home/.gopi/serve.lock
lock holder: pid 1356494  (stale: the process is gone; the lock is FREE)

repo:        …/.evid/ffpark/repo
  protected: 0 branch(es) held by a live worktree
  pending:   none — nothing to merge
```

and against the real repo (read-only), where four branches would land next tick:

```
repo:        /home/user/work/GoPi
  protected: 10 branch(es) held by a live worktree
  pending:   4 branch(es) the next sweep would merge
    - fix/cwd-release-composition
    - fix/peak-guard-stale-instructions
    - fix/testutil-triple-20260916
    - fix/worker-ownership-pointer-backfill-3
```

**Why that is enough**: the deleted half of the answer ("what is it doing right
now") can only be answered by an in-flight record, and a tick has no in-flight
state to record — the honest answer to "what is it doing?" for a stateless
merger is *"it is either between ticks (idle) or inside one bounded tick"*, which
the lock plus the per-repo preview conveys. A state file whose every field would
be empty on every write is worse than none: it is a surface an operator believes
in (the epic's own unreadable-status incident). What replaces the old
"waiting on resolve child X" line is the *next tick's plan*, which is derived
from git and therefore cannot be stale.

### 7 — build / vet / suites

```
$ go build ./...            # clean
$ go vet ./...              # clean
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   # OK
```

Suite results for the touched packages are in the task report.

### 8 — this document

Sections 1–2 argue the design (including the clean-merge question) and list the
deletions with line counts.

---

## 4. Operator-facing consequences (deliberate, called out)

1. **`--force` is retired** (accepted, ignored, announced). It meant "stop now,
   interrupt the work"; there is no work to interrupt.
2. **`--wait DURATION` is retired** (still parsed and still validated — an
   existing service unit must not start failing — but it bounds nothing).
3. **No durable stop-request file.** It existed to reach a merger the signal
   missed, and reading it needed a watcher inside the loop. With no children a
   merger never blocks inside a tick, so the signal IS the delivery and the
   command reports the observed end state instead of a file it cannot guarantee.
4. **A conflict now costs a ledger row and a git tag**, not a model call — which
   is also why the merger's own E2E suites are hermetic without an API key.
