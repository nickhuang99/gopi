<!-- doc-class: normative -->
# The take-over holds the single-instance lock (epic `merger-takeover-lock`)

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Record, not current
> truth: the take-over launcher this document is about was deleted with the
> resident merger (`f9ff1a89`, `55c4d6f3`), so there is no take-over to hold a
> lock any more. The single-instance WORK LOCK itself survives
> (`cmd/agent/merger_work_lock.go`); the one-tick merge is the bare `gopi merge`
> *(that wording dates from 2026-09-19; the standalone verb was later removed by
> the merge-removal epic — the one-tick merge is now `gopi task merge`, same
> engine and same lock).*
> See `devdoc/merger-retired.md` and the dated correction in
> `devdoc/merger-self-driving.md`.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Branch**: `fix/merger-takeover-lock` · **Base**: `16633bd1` (merger `--every` loop merged)
**Date**: 2026-09-17 · **Host**: peer1

---

## 1. The defect, measured

Under the post-`merger-pure-cronjob` architecture a merger tick takes
`~/.gopi/serve.lock`, **launches a detached take-over `gopi`** (the process that
actually writes git), and **exits**. The flock is therefore released seconds
after the tick returns, while the only process merging holds nothing.

Measured on this host before the change:

```
$ ps -eo pid,cmd | grep gopi-merger-takeover
3713454 gopi-merger-takeover          ← a merge round IS in flight

$ gopi merger --status
merger:      not running
lock holder: pid 3616234  (stale: the process is gone; the lock is FREE)

$ ls -l ~/.gopi/serve.lock
-rw-r--r-- 1 the operator the operator 7 Sep 17 22:51 ~/.gopi/serve.lock   ← the dead launcher's pid
```

Three consequences, all real:

1. **`--status` cannot see the writer.** It renders from the lock, and nobody
   holds the lock — so a live merge round reads as `not running`.
2. **Nothing refuses a second take-over.** The singleton ("exactly one merging
   PROCESS") covered the launcher — the process that decides nothing — not the
   writer.
3. **The lock holder label is a lie.** It names the launcher's pid, which is
   dead; the process doing the work is unnamed.

A fourth, latent one: the take-over stays in `session-<N>.scope`, so login-session
teardown kills it mid-merge (Rule 24 — the cgroup, not `setsid`, decides).

## 2. The fix: the writer inherits the guard

The launcher already holds the flock, so **the take-over inherits that exact
descriptor** and holds it for its whole life.

* `cmd/agent/merger_takeover.go` — the lock fd travels as `ExtraFiles[0]`
  (= fd 3 in the child), plus the staged prompt file as `ExtraFiles[1]` (fd 4),
  plus `mergerLockOwnerEnv` in the child's environment.
* `cmd/agent/merger_takeover_lock.go` — the child's side: adopt, fence, restamp,
  and the admission rule.
* No re-acquisition, no path re-resolution: **the fd IS the locked file**. A BSD
  `flock` lives on the **open file description**, so it survives `fork`+`exec`
  and dies only with the last copy.

### The retired contract (stated because a test pinned it)

The lock fd used to carry `O_CLOEXEC` so the child could **not** inherit it —
right for the old design (the child had to take the lock itself via
`gopi merger --once`) and exactly what made an unguarded writer possible. That
test is inverted by this change.

### The fence (the 2026-09-12 leak class)

An inherited descriptor has **no** `FD_CLOEXEC` — that is what lets it cross
`exec`. Without re-arming, **every** process the take-over starts (bash, git, a
build) would carry a copy of the lock; a flock dies with its **last** copy, so one
survivor would keep the singleton held by a dead pid and refuse every later
merger. `armInheritedMergeLock` re-arms the bit as the first act of `main()` and
**verifies it with `fcntl(F_GETFD)`** rather than trusting the call
(`TestMergerTakeoverLockAdoptArmsTheFence`).

## 3. Ruling: what holds what, and why nothing deadlocks

Three shapes had to be separated, and getting them wrong is a **deadlock**, not a
safety property:

| shape | inherits a lock fd? | admitted how |
|---|---|---|
| the take-over itself | **yes** (fd 3) | it IS the holder |
| faces the take-over runs (`gopi merger --once`, `gopi task merge`, the finish-cleanup chain) | no (the fence sees to it) | **descendant admission** — `mergerLockHeldByAncestor` |
| an independent gopi (operator, script, another tick) | no | **refused**, naming the holder |

**Why the middle row cannot simply be refused**: the take-over *waits* on those
faces (the close loop carries the rebuild contract of Rule 12). A refusal is a
self-deadlock.

**The chain that makes descendant admission sufficient** (not obvious, verified
by reading the call graph): the take-over runs `gopi task run <id>`, whose end
hook calls the finish-cleanup chain **in-process**, and `gopi task merge` is
spawned by the agent itself — so every face that needs the lock is a
**descendant** of the holder.

### The admission proof (two facts, neither sufficient alone)

* the **marker** (`TakeoverLockOwnerEnv`) — inherited through the environment,
  evidence about provenance only, **any caller can type it**;
* the **proof**, read from the kernel: a live **ancestor** of this process holds
  the flock **right now** (`tools.LockEvidences` + `tools.SelfAncestorPIDs`).

Plus a third fact that is not obvious: the ancestor's lock must be on the file the
path **currently names** (`sameFileAsPath`). `LockEvidences` joins on the **inode**
a descriptor locks, so a lock file replaced under an unlink would otherwise be
admissible — the exact state `LockProbe` refuses a *new* acquisition for.

The admitted face receives a **read-only, non-locking** handle
(`inheritedLockHandleForExemptFace`): it guards nothing, releases nothing (the
callers' `Close()` is a no-op on the guard), and cannot re-truncate the holder's
pid stamp.

### Why the daemon seam's model is not reused

`workerDaemonInheritLock` passes the descriptor **explicitly** to its children.
That needs Go code controlling the child's construction; the take-over's faces are
started through the ordinary tool surface, where no `ExtraFiles` slice exists.
Hence admission-by-proof instead of hand-off.

## 4. Ruling: `--status` semantics

`--status` now answers three questions instead of two, and adds a **fourth state**.

* New: **`running (take-over in flight; single-instance lock NOT held — the guard
  is down)`** — a live take-over with no holder. This is the state the old surface
  could not name, and it is the one that needs an operator.
* New `takeover:` line, **one per live take-over**, carrying the four facts an
  operator needs: pid, elapsed, **cgroup** (the "does it finish or does a session
  teardown kill it" verdict), and the log path (where the round's report is
  written).
* The `lock holder:` line gains the shape note when that holder is itself a
  take-over.
* **Schedule semantics when the holder IS the take-over**: `interval: not
  applicable` (a one-shot has no period — printing the compiled-in default would
  invent a schedule nobody bought) and `next tick: unknown to this surface — the
  next round is owned by the TRIGGER that starts it`. A **`--every` loop** holding
  the lock still reports its own measured interval (its schedule is readable).
* **Nothing is printed when there is nothing to say**: no take-over + readable
  `/proc` → no line (the ordinary output contract is unchanged). An **unreadable**
  `/proc` prints the honest degradation, because an empty answer reads as "no
  take-over is running" (Rule 15).

### 4b. ONE PROFILE, AND ONE PID NAMED ONCE (found by the acceptance, not by a test)

The scan is **host-wide**, the report is about **one profile**, so every observed
entry is partitioned by the log path — the one fact a take-over inherits from its
launcher (its stdio is pointed at the profile's `merger-takeover.log`). Two
defects in that wiring survived the whole test suite and were caught only by
running the **shipped binary** in the standalone acceptance (§7):

1. **The partition was discarded.** `MergerStatusReport` scanned `/proc`,
   partitioned the result, and then — 60 lines later, in the block that resolves
   the schedule and the log line — **scanned again** and overwrote
   `st.Takeovers` with the unpartitioned list. Both lists then held every entry,
   so each take-over was rendered **twice and contradictorily**: once as this
   profile's `takeover:` line and once under `belongs to ANOTHER gopi profile`.
   *Why no test saw it*: `partitionTakeoversByLog` was correct in isolation, and
   every unit test called **it**, never the entry point; the pipeline test that
   drives the real report passed its own `logPath` as *both* paths, which
   happened to make the overwrite harmless. The entry point now performs **one**
   scan and one partition.
2. **The wrong log path was passed.** The CLI handed the report the **daemon**
   log (`merger.log`) where the partition needs the **take-over** log
   (`merger-takeover.log`) — two different files written by two different
   processes. With nothing ever matching the correlation fact, this profile's own
   live lock holder fell through to the elsewhere note, so the operator read
   **one pid claimed twice**: as the `lock holder:` and as another profile's
   process. `MergerStatusReport` now takes both paths and `cmd/agent` resolves
   the take-over log through the same single definition it already had.
3. **The holder is claimed by definition.** It holds *this* profile's
   `serve.lock`, so it is this profile's whatever its stdio says — and a detached
   child's stdio commonly resolves to no file at all, so the log rule alone can
   never place it.

   **A claim I first wrote here was WRONG, and the correction is worth keeping**
   (Rule 26's corollary: a fix that overstates its coverage is worse than none).
   I wrote that `holderShapeNote` "had been dead code in production". It was
   not: the discarded partition had put *every* entry into `st.Takeovers`, so the
   note was printed — **by accident**, from a list that was not a partition at
   all, in the same render that listed the same holder as another profile's.
   (The overstatement is also in `bb9930e6`'s commit message, which is left as it
   was written rather than rewritten: the wrong claim is here, next to the
   measurement that refutes it.) The fix's real effect is narrower and it is the
   one that matters: the holder is now claimed **by rule** rather than by the side
   effect of a broken list, so the note survives the repair instead of depending
   on it.

Both fixes carry regression tests that are **red on the old wiring** (Rule 10.5):
`TestMergerStatusReport_ClaimsTheTakeoverItsOwnLauncherStarted` drives the real
entry point over a real process tree (a marker-carrying stand-in whose stdio is
this profile's take-over log; a foreign one whose stdio is not) and asserts no pid
is rendered twice, and `TestMergerStatusClaimsTheHolderItCannotAttribute` asserts
the holder is claimed and never also listed as elsewhere. One measured trap is
recorded in the fixture: a stand-in started through `flock` is **invisible**,
because `flock` *execs* its command and the marker in argv[0] is gone before the
process can be observed.

## 5. What was tried and REMOVED (the honest part)

The first design added a **runtime probe child** before the real spawn: build a
throwaway merger through the same construction and check it cannot win the lock.
It was written and **run**, and the suite killed it for the right reasons:

* the launcher's contract is **exactly one child, never waited on**, pinned by
  `merger_tick_no_child_gate_test.go` (which forbids the fd-handoff/wait shapes in
  that file) — the probe was a **second** spawned process and made the launcher
  **wait** (a `cmd.Run` with a deadline), i.e. it re-introduced by hand the
  blocking tick the cron redesign removed;
* the probe also used a **wrong instrument** first: a child re-flocking the fd it
  inherited succeeds trivially (same open file description), so it reported
  "broken" about a correctly guarded machine. The correct instrument is a **fresh
  open of the path**, which is what the probe was rewritten to use before removal.

**What replaced it**: `mergerLockHandoffCheck` (in-process: descriptor exists, is
the file the path names, and the FD_CLOEXEC invariant is stated), the child-side
adoption refusal, the verified fence, and the pure regression tests below. The
probe's remaining gap — end-to-end delivery through `systemd-run` — is covered by
the **host acceptance** in §7, which uses the real binary.

Also **removed from the plan**: a fix to `mergerWorkLock`'s second inheritance
model. With descendant admission at the one funnel (`acquireServeLock`), M1 covers
every real chain; M2 had **no live chain** and adding it would have been unproven
code (`merger_work_lock.go` is untouched).

## 6. Tests

| test | pins |
|---|---|
| `internal/tools` `TestMergerTakeoverArgv0MatchAcceptsTheKernelTruncation` | the 20-byte marker vs the kernel's 15-byte `comm`; path-qualified argv[0]; false positives |
| `internal/tools` `TestMergerTakeoverProbeScansTheRealProc` | the `/proc` read against the live table, self-consistency, no self-report |
| `internal/tools` `TestMergerTakeoverProbeReportsUnreadableProcAsUnknown` | unreadable `/proc` ≠ "no take-over" |
| `internal/tools` `TestMergerStatusNamesALiveLockHolderTakeover` | the render ruling: names the holder, `not applicable` interval, trigger-owned next tick, **and** that a `--every` loop keeps its own interval |
| `internal/tools` `TestMergerStatusReport_ClaimsTheTakeoverItsOwnLauncherStarted` | the **pipeline** half of §4b: this profile's own take-over is claimed, another profile's is not, and **no pid is rendered twice** (red on the old wiring) |
| `internal/tools` `TestMergerStatusClaimsTheHolderItCannotAttribute` | the holder is CLAIMED and never also reported as another profile's (§4b.3) |
| `cmd/agent` `TestMergerTakeoverExtraFilesPutsTheLockOnFd3` | the fd **numbering** contract (the defect's regression test) |
| `cmd/agent` `TestMergerTakeoverLockAdoptRequiresTheMarker` | no marker → no adoption (fd 3 is not reserved) |
| `cmd/agent` `TestMergerTakeoverLockAdoptRefusesANonRegularDescriptor` | adoption proves it is a regular file |
| `cmd/agent` `TestMergerTakeoverLockAdoptArmsTheFence` | the adopted fd carries `FD_CLOEXEC`, read back from the kernel |
| `cmd/agent` `TestMergerLockHeldByAncestorNeedsTheMarker` / `...RejectsAMarkerWithNoLiveHolder` | an env var alone never bypasses the singleton |

`merger_tick_no_child_gate_test.go` was **amended**: its `ExtraFiles` token was a
false positive after this change (the gate's subject is the *waited* child shape,
still caught by `.Wait()`/`cmd.Run()`), replaced by `cmd.Wait()` with the reason
recorded inline.

## 7. Verification

### Build / vet

```
$ go build ./...      → OK
$ go vet ./...        → OK (zero output)
```

### Host acceptance (the real binary, a sandboxed profile, §5's named gap covered)

**It is a checked-in script, not a scratch session**:
`scripts/accept-merger-takeover-lock.sh <gopi-binary> [scratch-root]` — shell
only, no Go test and no harness stub (the take-over's identity IS its argv[0] and
its guard IS a descriptor crossing exec; a Go test standing in for that process
would prove the stand-in). **7 groups, ALL PASSED** on the shipped build, and the
runs are reproducible: on the second run the §4b defects above were caught and
fixed, and the script's group ④ then printed the holder **once**.

A sandbox profile (`GOPI_HOME` under a scratch dir, one repo in the `repos`
table, no build command), with the take-over process stood in for by a stub that
performs **the production first acts** (re-exec with the marker `argv[0]`, adopt
fd 3 through `mergerTakeoverLockAdopt`) and then holds the guard — so no LLM
session is spent on a merge round. `GOPI_BINARY` points the launcher at the stub.

**① the tick hands the guard over and leaves** (deterministic, the shipped build):

```
$ gopi merger
gopi-merger-takeover: note: the single-instance lock fd carries FD_CLOEXEC; os/exec carries it across exec for ExtraFiles
gopi merger: take-over started pid=3893375 detach=systemd-scope log=…/merger-takeover.log — this process holds no
             child and will not wait; the single-instance lock went with it (fd 3), so the take-over is now the
             process 'gopi merger --status' names
```

**② `--status` names the WRITER** (the sentence that was missing):

```
merger:      alive
lock holder: pid 3893375 — the holder IS the take-over (a one-shot merge round, not a poll loop)
takeover:    pid 3787226 running for 16m39s; argv[0]=gopi-merger-takeover; cgroup=…/session-19555.scope — INSIDE the login session scope, …
             pid 3893375 running for 0s;   argv[0]=gopi-merger-takeover; cgroup=…/gopi-takeover-home-d08a2168fb59.scope — outside the login session scope, …
interval:    not applicable (the live lock holder IS the take-over — a one-shot merge round with no period; there is no interval to read)
next tick:   unknown (unknown to this surface — the next round is owned by the TRIGGER that starts it …)
```

Note the by-product: the scan reported **three real take-overs live on this host
while `--status` had been printing `not running`** — two of them *inside*
`session-19555.scope`, i.e. exactly the session-teardown exposure §1 named.

**③ a SECOND merger is refused, naming the holder** (deterministic):

```
$ gopi merger --once
gopi merger: serve lock held by a live daemon: another merge process is already running
             (lock …/home/.gopi/serve.lock held by pid 3893375) — refusing double-serve
```

**④ `/proc` evidence for the holder:**

```
$ cat /proc/3893375/cgroup
0::/user.slice/user-1000.slice/user@1000.service/app.slice/gopi-takeover-home-d08a2168fb59.scope
$ ls -l /proc/3893375/fd/3
lrwx------ 1 the operator the operator 64 … /proc/3893375/fd/3 -> …/home/.gopi/serve.lock
$ grep -i flock /proc/3893375/fdinfo/3
lock:	1: FLOCK  ADVISORY  WRITE 3892665 08:04:31347175 0 EOF
```

The child holds fd 3 **on the lock file itself**, outside the login-session scope
(the escape rung ① — `systemd-run --user --scope` — preserved the descriptor
through the wrapper, which is §5's named gap, now measured).

**⑤ the live holder is seen by the reader** (the detail that decides the
admission rule): the kernel's lock *record* still names the dead launcher
(3892665), but `tools.LockEvidences` reads `/proc/*/fdinfo` and reports the
**live** process, which is what the admission rule needs:

```
$ tools.LockEvidences(lock)  → ok=true entries=1: pid=3893375 stray=false
```

**⑥ the admission rule, and the deadlock it prevents** (deterministic — the same
command, twice, differing only in the two admission facts):

```
$ flock -x serve.lock -c "gopi merger --once"                      # no marker
… another merge process is already running (… held by pid …) — refusing double-serve

$ flock -x serve.lock -c "GOPI_MERGER_LOCK_OWNER=1 gopi merger --once"   # marker + live ancestor holding
[merger] [repo] branch feat/acc-clean (merged): fast-forward into master, branch deleted
[merger] [repo] merged=1
[merger] --once: single tick done
```

The admitted pass **really merged** (`master` moved to `54d3a72`, `merged=1`) —
so the holder's own face runs inside the guard instead of deadlocking on it,
while an identical command with no marker is refused. A marker **alone** is not
enough: with nobody holding the lock it is refused too
(`TestMergerLockHeldByAncestorRejectsAMarkerWithNoLiveHolder`).

**What the stub could NOT show, stated honestly** (Rule 26): the take-over's own
`gopi merger --once` grandchild was built but its `CombinedOutput` never landed in
the log, so ⑥'s admission half was measured with the flock held by a shell
**ancestor** instead of by the stub. The property proven is the same one (marker +
live ancestor → admitted, marker alone → refused), and the descendant chain itself
is a code-reading claim (§3) — it is the one thing a reviewer should re-measure
against a real take-over.

### Machine gates (Rules 16–21)

The standards suite was **RED on this branch when first run**, from two findings
this change itself introduced — neither visible from the merger tests, which is
exactly why the gates are run:

| gate | finding | repair |
|---|---|---|
| Rule 18 (no `else` after a terminating `if`) | **NEW** at `merger_takeover_lock.go` — the `sameFileAsPath` outcome was folded into `if ok, e := f(); e != nil { } else if !ok { }`; the body returns, so the `else` was dead weight *and* nested one independent failure (identity) inside another (read) | split into two top-level guards |
| Rule 16 (`%w` wrapping) | **STALE + NEW in the same run** — the adoption block added 20 lines to `main()` *above* the baselined @-payload refusal, moving it `1131 → 1151` | re-keyed, **not relaxed**: verified byte-identical to master's |

```
$ go test ./internal/standards/ -count=1   → ok   (11.204s)
$ go vet ./internal/standards/ ./cmd/agent/ ./internal/tools/   → clean
```

The re-key is the fourth recorded occurrence of that drift in `baseline.go`'s own
header, and it obeyed the rule the header states — **re-measure the key in the
final tree of a branch, not at the commit that moved the site** — with the
identity check (`diff` → no output) kept as the evidence, and no permit added or
relaxed. Committed as `4bbb83d3`.

### Test suite of record

**FINAL (the numbers that stand — the tree shipped as `66ae816b`)**:

```
$ go build ./...   → OK
$ go vet ./...     → OK (zero output)
$ gofmt -l <every file this branch touched>   → no output
$ go test -count=1 ./internal/standards/      → ok    (gates 16–21)
$ go test -count=1 ./internal/tools/          → ok    248.3s
$ go test -count=1 ./cmd/agent/               → ok    222.2s
$ scripts/accept-merger-takeover-lock.sh <shipped binary>   → ALL CHECKS PASSED (7 groups)
```

**The earlier red `cmd/agent` run was ENTIRELY the suite's own stale-binary
guard** — `bin/gopi` (00:11) older than the source (00:26) — so the e2e family
was exercising the *previous* binary. Rebuilt from the final tree, the full
package is **green (222.2s)**. That is the guard working, not a flake waved away;
the superseded run is kept below because the *distinction* is the lesson.

**Superseded run, kept for the record — why it was NOT a flake to tolerate**:

```
$ go test -count=1 ./cmd/agent/ ./internal/tools/     # BEFORE the rebuild
ok   github.com/the operator/gopi/internal/tools   310.054s
FAIL github.com/the operator/gopi/cmd/agent        228.353s   (8 failures, listed below)
```

The 8 failures then were the stale binary's, and they were also consistent with
host-load flakiness — every claim below was measured rather than assumed, which
is what kept me from *stopping* at "flaky" and shipping unrun code:

* they are all in the **real-binary e2e** family (`bare_session_one_dir_e2e`,
  `task_executor_repo_cwd_e2e`, `task_session_cwd_e2e`, the worker/console
  suites) — none of them touches the merger, the lock, or the take-over;
* the **membership differs between runs**: the full run failed 8 of them, while
  the earlier full run failed a different set of 4 (`TestTaskListReadPathE2E`,
  `TestSweepZombiesCLI_*`), and those 4 pass with the tree clean;
* all 8 pass **in isolation in one pass** (`-run` over exactly those 8: `ok
  12.365s`), which is the signature of contention on a shared machine, not of a
  deterministic defect;
* `internal/tools` — the package carrying the scan, the partition and the
  schedule ruling — is **green**, and every `-run TestMerger` test in BOTH
  packages is green.

The one `cmd/agent` failure that WAS mine is fixed and committed: the
read-only-faces test caught the host-wide/profile-scoped false positive
(`2a3cfe36`), and the seam e2e caught my dirty worktree (`9ee86cec` — Rule 30's
own check, working as designed).

## 5b. Deliverable declaration

* **Branch**: `fix/merger-takeover-lock` (in the shared repo — no push needed)
* **Recovery**: `git log --oneline fix/merger-takeover-lock` ·
  `git show fix/merger-takeover-lock:devdoc/merger-takeover-lock.md`
* The worktree is `/home/user/workspace/gopi-merger-takeover-lock`.
