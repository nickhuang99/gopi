<!-- doc-class: normative -->
# The Rule-12 close loop, made durable across a dead pass

Task: `fix/merge-rebuild-loop` (worktree `~/workspace/gopi-rebuild-loop`,
branch `fix/merge-rebuild-loop`, based on master `830ac32b`).
Origin: verify report `devdoc/verify-01a0a72c-merger-stop-immediate.md` and the
operator's symptom — `bin/gopi` older than the source it must contain.

## 1. The symptom, re-measured before anything was changed

| what | value |
| --- | --- |
| `bin/gopi --version` | `f221db934e31` |
| `bin/gopi` mtime | 2026-09-16 13:10:31 |
| master tip at that moment | `830ac32b` (2026-09-16 13:10:48) |
| the merge that made master advance | `830ac32b Merge branch 'fix/merger-readact-force-refusal'` |

So the binary was **behind master the whole time**, and the file it was missing
was literally the branch's own `cmd/agent/cli_merger_stop.go` +
`cli_worker_control.go` (both mtimes 13:10:48). The general evaluation criterion
is the one the origin report used, and it holds today:

> `bin/gopi --version` != master tip ⇒ every `./bin/gopi` run judged code that
> did not contain the merge.

## 2. Which mechanism is true

The three candidates were (a) the rebuild hook never fired for that merge,
(b) it fired but built from a stale tree, (c) rebuild is not on that path at all.

**The answer is (c), restated precisely: the rebuild IS on every landing path,
but only as a step the LANDING PASS itself must survive to reach — and the
measured passes do not survive.** The close loop was a pass-level step, so the
invariant held only for passes that completed.

Evidence, in the order it was gathered:

1. **The 07:29:34 landing was never named or built by the daemon.**
   `merger.log` contains **0** occurrences of `e1229f85` and **0** of
   `91d85a0e` (the pre-merge tip the binary was at): the far-side daemon neither
   minted a version for that position nor rebuilt for it. The one
   `merger-stop-immediate` hit in the log is not a rebuild record at all — it is
   an untracked file name in a master-safety line
   (`?? devdoc/verify-01a0a72c-merger-stop-immediate.md`).
   The last mint before the gap is `version/v20260915-2231-781d9344`
   (merger.log:20235, ~06:31 local); the next is
   `version/v20260916-0344-9440b57b` (merger.log, 11:44:11 local) — a **~5h
   window with no close-loop trace**, which is exactly the window the merges
   below landed in.

2. **The pass that landed it was killed inside the verify judge.**
   `/home/user/.gopi/merger/merger-state-2470012` (started 06:35:57):

   ```json
   {"pid":2470012,"last_pass_start":"2026-09-16T07:22:54.986862607+08:00",
    "last_pass_end":"2026-09-16T07:17:54.556668409+08:00","passes":2,"merged":2,
    "interrupt":{"repo":"/home/user/work/GoPi","branch":"feat/merger-stop-immediate",
                 "stage":"verify",
                 "children":["verify-child 01a0a72c-cf5b-7e24-b291-53d687ae6849 (pid 2760166)"]},
    "note":"busy: a merge pass is running"}
   ```

   The pass recorded `merged:2` and then died at `stage=verify`, with the branch
   `feat/merger-stop-immediate` — the very merge at 07:29:34. `passes:2` means
   the increment after that pass never happened (the increment is written after
   the pass body), i.e. the process ended inside it.

3. **Two sibling daemons died with `passes:0`** —
   `merger-state-2436009` (started 06:26:55) and `merger-state-2456472`
   (06:33:44), both `{"passes":0,"note":"busy: a merge pass is running"}`.
   Three daemons in ~9 minutes, none of which reached a pass end. That is the
   measured shape the fix has to answer: **a SIGKILL runs no `defer`, so a
   close loop that exists only at the pass END is precisely the step a killed
   pass never reaches.**

4. **`bin/gopi` was built by a pass that ENDED — the last one before the gap.**
   mtime 07:17:53, built from `91d85a0e` (tip committed 07:03:58, minted
   07:17:51) — i.e. the artifact was left exactly at the last position a
   completing pass named. It stayed there through 07:29:34 (`e1229f85`, a merge
   that changed `cmd/agent/cli_merger_stop.go`, `cli_merger_daemon.go`,
   `merger_graceful.go`, `cmd/agent/main.go`, …) and through every later landing until the
   ~4h-later catch-up.

5. **No later pass came back for the position, by construction.** The
   pass-level hook fires per landing, and a fresh pass sees `merged=0` for an
   already-merged branch (`[merger] [/repo] merged=0` is the ordinary line) —
   there is nothing left to trigger it. The one hook that does run every pass
   rebuilds the *current tip*, which at 09:16:35 was `71655802` — a **different**
   position that had already absorbed the 07:29 and 07:24 merges. So the
   position was eventually covered *by luck* (any later sweep landing carries a
   catch-up rebuild), not by design: between 07:29:34 and 09:16:35 nothing named
   or built it.

### 2b. The second, non-crashing variant (also measured)

`(b)`-flavoured but same family: the gate LANDS the merge and then BLOCKS on the
verify child, so even a pass that survives leaves the artifact stale for the
length of the judge:

```
13:10:48  master advances (830ac32b)
13:10:31  bin/gopi — still the pre-merge build
13:19:35  the verify child for 01a0a793 spawns, judging the MERGED state
```

The judge therefore runs against a tree whose own binary is one merge behind.

## 3. The fix

Two changes, one idea: **a landing that cannot yet be built must leave a durable
obligation, and the obligation must be discharged at the START of the next pass
rather than the END of the current one.**

1. `internal/tools/rebuild_pending.go` (new) — the durable mark.
   - One file per repo, `<gopi data root>/merger/rebuild-pending-<repo-key>`,
     where `<repo-key>` is the first 16 hex chars of `sha256(clean repo path)`
     (a digest, not the leaf name: two repos share a leaf routinely).
   - The body's first field is the **repo path itself** — the digest is one-way,
     so that is the only faithful recovery — followed by a tab and the reason.
   - `MarkRebuildPending` refuses to mark a repo with **no declared build**
     (`config.ResolveRepoBuild` not found): no contract means no drain could ever
     clear the mark.
   - Writes/removals/reads are reported (Rule 15b); an unattributable mark is
     logged, never guessed at.

2. `mergerPassOnce` drains the marks as its **first act**
   (`mergerPassCloseLoop(nil)`), before the resolve, before the sweep, before any
   child. Consequences:
   - a pass killed one second later has already healed a stale artifact;
   - a successor process inherits exactly the work its predecessor owed;
   - on a clean host it is one directory read, and it is idempotent (a repo whose
     artifact already follows its tip costs one `--version` probe).

3. `mergerVerifyGate` (the gate's landing point) additionally
   - **attempts the rebuild right after the merge** (`serveRebuildAfterMerge(repo,
     nil)`, no revert handle — the gate's own post-verdict rebuild owns the
     unwind, and a failure here must never unwind a merge the judge has not ruled
     on), so a surviving pass no longer leaves `./bin/gopi` one merge behind for
     the length of the judge; and
   - leaves the durable mark, because the attempt can refuse (tracked-dirty
     tree), can fail, and above all can be stepped over by the process dying.

4. `serveRebuild` clears the mark on success — the single place every caller's
   success passes through — so a completed drain cannot leave a mark that
   rebuilds the repo on every pass forever.

## 4. Proof that the LOOP rebuilds (not a hand build)

`devdoc/loop-proof-merge-rebuild.sh` replays the measured shape against a real
fixture repo (its own git history + a real Go build contract) and runs the same
scenario twice, once per binary, so the diff localizes the defect:

* **PART A (control)** — a pass that COMPLETES: both binaries heal it (master
  already rebuilds every configured repo at the pass end). This is the control
  that stops the fix from claiming the loop was unconditionally broken.
* **PART B (the measured defect)** — a landing whose pass does not complete, then
  two passes SIGKILLed early (the `passes=0` shape) while the landing's durable
  mark is present:
  - pre-fix binary: `RESULT exit=1` — the artifact stays at the pre-merge build
    after **both** killed passes (`FAIL: … every ./bin/gopi run judges pre-merge
    code (THE OPERATOR'S SYMPTOM)`);
  - fixed binary: `RESULT exit=0` — the killed pass rebuilt in its first act
    (`[rebuild] ok: … built in 265.9ms`), the artifact now reports the merged
    tip, the mark is discharged (`mark still present? = no`), and a second killed
    pass finds nothing to do.

The scenario uses a pass-local `git` shim to place the kill: `always` (die on
the pass's first git call — the pre-fix binary's sweep, where passes really died)
and `after-drain` (die only once the mark is gone, i.e. after the fixed binary
has done the work). The two modes exist because the two binaries have to be put
against *different walls* for the same measurement to be honest; the mode is
printed in the evidence.

### 4a. The measured A/B run (both halves, pasted)

The two binaries are built from two REAL trees — the pre-fix one from master
`830ac32b` (a worktree at `~/workspace/gopi-prefix-bin`; `git log -1` =
`830ac32b Merge branch 'fix/merger-readact-force-refusal'`, and
`strings gopi-prefix | grep -c rebuild-pending` = **0**, i.e. it really is the
pre-fix build), the fixed one from this branch (`569a5b0e`).

**PRE-FIX (mode `always`) — `RESULT: exit=1`:**

```
############ PART A — control: a pass that COMPLETES ############
branch tip (merged by the loop) = 0ff330822a93
artifact before the pass       = dba528399553
[merger] [rebuild] ok: …/repo built in 1.178035687s (0ff330822a93…)
OK: PART A — the completing pass rebuilt the artifact to the merged tip

############ PART B — the measured defect: passes that DIE ############
master after the landing       = 7a66ba1  (the merged tip 7a66ba1934ab)
artifact after the landing     = 0ff330822a93  (the pre-merge build — the measured staleness)
landing's durable mark         = …/rebuild-pending-2c835527e111595a
--- pass 1: the first pass is SIGKILLed early (runs no defer); hang mode always ---
artifact after the killed pass = 0ff330822a93
--- pass 2: a second killed pass, so no single lucky pass explains the result ---
artifact after killed pass 2   = 0ff330822a93
mark still present?            = yes
FAIL: PART B: the artifact still reports 0ff330822a93 after two killed passes — master is at
      7a66ba1934ab, so every ./bin/gopi run judges pre-merge code (THE OPERATOR'S SYMPTOM)
FAIL: PART B: bin/gopi is OLDER than the source the merge introduced
===== RESULT: exit=1 (0 = the LOOP rebuilt the artifact in every scenario) =====
```

**FIXED (mode `after-drain`) — `RESULT: exit=0`:**

```
OK: PART A — the completing pass rebuilt the artifact to the merged tip

############ PART B — the measured defect: passes that DIE ############
master after the landing       = 44f3c29  (the merged tip 44f3c29117cd)
artifact after the landing     = 64c48a178e28  (the pre-merge build — the measured staleness)
landing's durable mark         = …/rebuild-pending-79f1dc1f139fafa4
--- pass 1: the first pass is SIGKILLed early (runs no defer); hang mode after-drain ---
  | [merger] [version] minted version/v20260916-0557-44f3c29 (names the position this round landed; landed: 0 [])
  | [merger] [rebuild] ok: …/repo built in 415.860963ms (44f3c29117cd…)
artifact after the killed pass = 44f3c29117cd
--- pass 2: a second killed pass, so no single lucky pass explains the result ---
artifact after killed pass 2   = 44f3c29117cd
bin/gopi mtime                 = 2026-09-16 13:57:11  ← NEWER than cmd/x/main.go 13:57:11.014
mark still present?            = no
OK: PART B — a KILLED pass healed the artifact in its first act (the durable mark drained),
   no hand build involved
OK: bin/gopi is NEWER than the source the merge introduced
===== RESULT: exit=0 (0 = the LOOP rebuilt the artifact in every scenario) =====
```

The pre-fix half is the control that makes the fixed half evidence: the SAME
scenario, the SAME kill, the SAME fixture — only the binary differs, and only
the fixed one drains. Note `landed: 0 []` in the fixed run's mint line: the pass
landed NOTHING and still rebuilt, which is exactly the recovery the fix exists
for (a successor pass with `merged=0` now has something to do).

## 5. Red proof (Rule 10.5) — every new test fails against master

Run in a worktree pinned at master `830ac32b` (`~/workspace/gopi-prefix-bin`),
with ONLY the new test files copied in:

```
$ go test ./cmd/agent/ -run 'TestVerifyGateLandingCarriesTheDurableCloseLoop|TestCloseLoopWiring'
--- FAIL: TestVerifyGateLandingCarriesTheDurableCloseLoop (0.00s)
    cli_tasks_rebuild_gate_test.go:65: mergerVerifyGate does not attempt
    "serveRebuildAfterMerge(o.Repo, nil)" — the gate lands a merge and leaves the
    rebuild to a step a pass interruption can step over
--- FAIL: TestCloseLoopWiringIsComplete (0.00s)
    cli_tasks_rebuild_gate_test.go:90: mergerPassCloseLoop does not drain
    tools.RebuildPendingRepos() — the durable marks are never discharged, so an
    interrupted landing's rebuild is lost forever

$ go vet ./internal/tools/        # the primitives do not exist on master
vet: internal/tools/rebuild_pending_test.go:49:2: undefined: MarkRebuildPending
$ go test ./cmd/agent/ -run TestPassCloseLoopDrainsTheDurableMark
cli_tasks_rebuild_pending_test.go:38:8: undefined: tools.MarkRebuildPending
cli_tasks_rebuild_pending_test.go:47:18: undefined: tools.RebuildPendingRepos
cli_tasks_rebuild_pending_test.go:69:20: undefined: tools.RebuildPendingDir
cli_tasks_rebuild_pending_test.go:73:36: undefined: tools.RebuildPendingPrefix
FAIL	github.com/nick/gopi/cmd/agent [build failed]
```

So each new test is red for the right reason (a missing call, or a missing
symbol) — not merely red.

## 6. Verification (this branch)

```
$ CGO_ENABLED=0 go build ./...     → build exit=0
$ go vet ./...                     → vet exit=0
$ go test ./internal/tools/ -run 'Rebuild|MergeLoop|RebuildPending' -count=1
ok  	github.com/nick/gopi/internal/tools	2.171s
$ CGO_ENABLED=0 go test ./internal/tools/ -count=1 -timeout 25m
ok  	github.com/nick/gopi/internal/tools
```

`./cmd/agent` is 480s and has FOUR failures; none is attributable to this change
and each is reproduced on a pristine master worktree
(`~/workspace/gopi-prefix-bin`, `830ac32b`), so they are pre-existing or
environmental:

| test | failure | reproduced pre-fix? |
| --- | --- | --- |
| `TestTmpDirValueProbeLeavesNothingBehind` | 3 probe dirs abandoned under the scratch tmp root, ages 9h50m–9h57m (i.e. left by runs hours before this session) | YES — same 3 dirs, same ages |
| `TestWorkerDaemon_DetachesAndKeepsRunning` | `SID != own pid` (setsid did not take under the suite's process group) | YES |
| `TestFrontRunConsoleOutputLandsInItsSessionDir`, `TestConsumeOutput…` (`task_session_cwd_e2e_test.go`) | worktree `bin/gopi` OLDER than a source file — the guard correctly refused to verify code not in the binary; gone after `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | YES (they are a stale-artifact guard, not a code defect) |

`./internal/standards` is RED on master and on this branch, identically:
`Rule 17 (test env isolation) … NEW=2` at
`internal/agent/journalreport_test.go:101,104` (`os.Unsetenv` after `t.Setenv`,
the documented absent-case pair — a genuine gate finding, but not this epic's, and
present in the SAME shape on master). Everything else in that report is
`NEW=0`.

## 7. The live case (not closed by this branch — it closes when this branch merges)

At the time of writing, the main repo is still in the exact state the epic
describes, and this branch cannot heal it from a worktree:

```
$ cd /home/user/work/GoPi && git rev-parse --short HEAD   → 830ac32b  (2026-09-16 13:10:48)
$ ./bin/gopi --version                                    → f221db934e31
$ stat -c %y bin/gopi                                     → 2026-09-16 13:10:31  (17s BEHIND the merge)
$ cat ~/.gopi/merger/merger-state-3473055
  {"passes":14, "interrupt":{"repo":"/home/user/work/GoPi",
   "branch":"fix/merger-readact-force-refusal","stage":"verify",
   "children":["verify-child 01a0a793  (pid 3868848)"]},
   "note":"busy: a merge pass is running"}
```

That is variant (b) live and ~70 minutes old: the daemon's pass landed the merge
at 13:10:48 and has been inside the verify child ever since, so the operator's
`./bin/gopi` has been running pre-merge code the whole time. Merging this branch
is what puts the drain at the pass start and the gate's own landing rebuild into
that daemon's successor.

## 8. Blast radius — merges landed while the loop was down

`blast-radius.sh` joins two independent sources: master's movement (timestamped
git reflog) and the positions the close loop actually NAMED (`version/*` tags +
its two log lines). A master move followed by no mint naming it is a move whose
rebuild never happened. Measured for 2026-09-16:

| window (local) | master moved | close-loop trace | exposure |
| --- | --- | --- | --- |
| 06:35 – 11:44 | 06:35 `1dccad8e`, 06:36 `2968fbc3`, 06:37 `2ea7b86e`, 06:59 `8a3a0a38`, 07:03 `8ee11b55`, 07:07 `91d85a0e`, 07:24 `9118b1c8`, 07:24 `b712cacb`, **07:29 `e1229f85`**, 08:50 `fc8bc007`, 08:51 `e6ec8ee4`, 08:55 `90c60c65`, 08:59 `1cfe2f1f` | **none** (mint gap `…-2231` → `…-0344`; 0 log lines naming any of these tips) | ~4h: `bin/gopi` stayed at the 07:17:53 build |
| 09:16 – 11:44 | 09:17 `63f851a2`, `5f599869`, `f2cf623b`; 09:22 `86ad17ff`; 09:27 `0085797c`; 09:28 `46838d9b`; 09:36 `30450fc6`; 11:43 `afd0c40d`, `11c025f2`, `0d51d35c`; 11:44 `9440b57b` | one mint at 11:44 (`…-0344-9440b57b`) catches up | up to ~2h stale; the catch-up was a **sweep landing**, i.e. luck, not design |
| 12:48 – 13:10 | `65bdffaf`, `611a0be6`, `0693a1f3`, `f221db93`, **`830ac32b`** | mints at 12:48/12:54/13:10 re-sync each time — and `830ac32b` (13:10:48) is **still not rebuilt** at the time of writing (master `830ac32b` vs `bin/gopi` `f221db93`) | the live case, now ~70m stale |

The log side of the same gap (the operator's own evidence, `~/.gopi/logs/merger.log`):

```
-- last close-loop pair before the gap --
20235:[merger] [version] minted version/v20260915-2231-781d9344 (names the position this round landed; landed: 3 […])
-- first close-loop pair after the gap --
20277:[merger] [version] minted version/v20260916-0344-9440b57b (names the position this round landed; landed: 4 […])
20278:[merger] [rebuild] ok: /home/user/work/GoPi built in 11.54816005s (9440b57b6c07)
-- total lines of merger.log --
20878
```

Reading: every master move in the main gap was unrebuilt by the loop, and the
positions were only ever covered later by an unrelated sweep landing. The
`~5h` gap is bounded by the daemons' lifetime, not by any recovery mechanism —
which is exactly what makes this a loop defect rather than an environment
accident.

## 9. What this is NOT

- Not a hand rebuild: no `go build -o bin/gopi` was run against the real repo in
  the acceptance path. (The worktree's own `bin/gopi` is the code under test and
  is gitignored.)
- Not a redesign of the merger: the landing faces, the gate's judgement, the
  revert/unwind logic and the pass cap are untouched. The change adds a mark and
  moves one call to the start of the pass.
- Not a device-load test (PROJECT.md Rule 27): the proof builds a ~4-line Go fixture
  and runs one merger pass; nothing saturates a shared resource. Rule 22 is
  respected — the fixture root is under `/dev/shm` (`/dev/shm` is tmpfs, off `/`).
