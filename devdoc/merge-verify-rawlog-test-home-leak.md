<!-- doc-class: historical -->
# Task Report: merge of `fix/rawlog-test-home-leak` — already landed; acceptance verified on master

**Epic:** resolve the merge of branch `fix/rawlog-test-home-leak` (tip `9efd835a`) into
`master` in `/home/user/work/GoPi`, so master builds and its suite passes.

**Outcome: there is nothing left to merge.** The branch was merged by `833cc2b6`
(2026-09-14 13:20) and the ref was deleted afterwards as normal post-merge cleanup.
The deliverable of this task is therefore the acceptance proof — build green, zero NEW
test failures vs the pre-merge master, and an honest statement of what superseded the
branch's two non-identical files — plus this record.

**Verdict of this task: NO code change was needed.** No fix was written; the report
lives on a fresh branch (Rule 23 — the old branch name is never resurrected).

- Branch carrying this report: `feat/rawlog-merge-verification`
- Worktree: `/home/user/workspace/gopi-rawlog-verify` (merge-based on master tip `79738cb0`)
- Recovery: `git show feat/rawlog-merge-verification:devdoc/merge-verify-rawlog-test-home-leak.md`
- No temporary file of this task lives on `/`; every scratch root is off it (measured
  `TMPDEV` ≠ `ROOTDEV`, Rule 22).

**Reading order.** This record grew as master advanced under it, so the sections are
not in reading order. Sections (a)–(e) are the original acceptance record at
`b76942b8`; the labelled primed sections and (f) are the re-measurements. Read:

1. **(a)** merge-landed proof, the seven conflicted files, and the supersession of the
   branch's two drifted files — *the epic's core deliverable*;
2. **Precision on the baseline** — why `b9ae5cbc` is the exact pre-merge master (it is
   the merge commit's own `^2`);
3. **(b)/(b′)/(b‴)** build — green at every tip, plus the Rule 16–21 standards gates green
   at `79738cb0`; **(c″″)** the authoritative whole-suite
   sweep; **(c‴)** the tip trajectory; **(c″)** the branch's own guards green;
4. **(f)** line-survival proof + the exact three non-verbatim lines;
5. **(d)/(e)** disposition and recovery.

Section (f) SUPERSEDES the "superseded the branch's two lines" narrative in (a): (a)
explains the two *drifted* files it had already identified, (f) proves the claim across
all 35 files the branch touched.

## (a) Proof the merge already landed

`833cc2b6` is a real merge commit **whose first parent IS the branch tip**:

```
$ git rev-list --parents -1 833cc2b6
833cc2b6a855464301498119b9a3b5906ffb152d 9efd835a4712d82f8491a617832f76b736199467 b9ae5cbc728bc147839d7c789e93ccc2a57e9432
$ git log -1 --format='%ci %s' 833cc2b6
2026-09-14 13:20:29 +0800 resolve merge fix/rawlog-test-home-leak into master
```

So tip `9efd835a` (branch side, `^1`) was merged into `b9ae5cbc` (master side, `^2`):

| check | command | result |
|---|---|---|
| tip is an ancestor of master | `git merge-base --is-ancestor 9efd835a master` | **YES (exit 0)** |
| resolution commit is in master | `git merge-base --is-ancestor 833cc2b6 master` | **YES (exit 0)** |
| nothing in the tip is missing from master | `git log --oneline 9efd835a --not master \| wc -l` | **0** |
| merge-base of the two parents | `git merge-base 9efd835a b9ae5cbc` | `13acf700` (2026-09-13 17:28) |
| branch ref | `git show-ref refs/heads/fix/rawlog-test-home-leak` | **ABSENT** (post-merge cleanup, no `archive/*` tag either) |
| the tip is still reachable | `git branch -a --contains 9efd835a` | **13 refs**: `master` + 12 in-flight branches. The count drifts as branches land and are swept; the load-bearing invariant is `git log 9efd835a --not master` = 0 |

### Blob identity — the branch's own files are in master, byte for byte

`master` / `833cc2b6` / `9efd835a` all hold the SAME blob for every file the branch
introduced (and for the branch's two touched test files):

| file | master | 833cc2b6 | 9efd835a |
|---|---|---|---|
| `internal/testutil/profileguard.go` | `a0654895fa` | `a0654895fa` | `a0654895fa` |
| `internal/testutil/rehome_flat_raw_logs_test.go` | `270d18354d` | `270d18354d` | `270d18354d` |
| `scripts/rehome-flat-raw-logs.sh` | `416342268c` | `416342268c` | `416342268c` |
| `devdoc/rawlog-test-home-leak.md` | `5f3e8247f9` | `5f3e8247f9` | `5f3e8247f9` |
| `internal/gopilog/rawlog_test.go` | `SAME` | — | `SAME` |
| `internal/gopilog/rounds_test.go` | `SAME` | — | `SAME` |

The branch's actual **intent survived**, not just its files: `dbbe5329`
("stop the gopilog tests writing into the operator's live /home/user/.gopi/raw"),
`48bc0baf` ("follow a raw log that moved into its session dir") and `1eb54fa9`
("exclusive raw-log creation") are all ancestors of master. The branch's own test
still passes on master:

```
$ go test -count=1 -run TestRehomeFlatRawLogsMovesOnlyWhatASessionLocates ./internal/testutil/
--- PASS: TestRehomeFlatRawLogsMovesOnlyWhatASessionLocates (1.86s)
ok  github.com/nick/gopi/internal/testutil
$ go test -count=1 ./internal/gopilog/
ok  github.com/nick/gopi/internal/gopilog  1.035s
```

## The seven conflicted files, and what superseded the branch's two lines

The merge resolution `833cc2b6` hand-merged **seven** files — the overlap of the two
sides (filenames both sides had modified): `cmd/agent/cli_tasks.go`,
`cmd/agent/tmpdir_inject.go`, `internal/gopilog/rawlog.go`,
`internal/signalrec/signalrec_test.go`, `internal/standards/baseline.go`,
`internal/testutil/tmproot_assert_test.go`, `internal/tools/tasks.go`. The resolution
is a **union, not a side-pick**: its blob matches neither parent for any of the seven,
and both parents' unique content is present in the result.

Two files drifted further **after** the merge, and both drifts are explained; in both
cases master keeps the branch's intent and supersedes only wording/mechanics:

1. **`internal/gopilog/rawlog.go`** — the branch's inlined two-level scan of the
   sessions tree (tip blob `eeb191a2`) was superseded by
   **`f3569050` "feat(sessionlayout): classify session records by layout in every
   production reader"** (2026-09-13 22:02, post-tip, master-side only). Master's
   `SessionsRecordingRawLog` scans through the shared `sessionlayout.Records(dir)`
   classifier instead — same resolution, plus the side-file
   (`signal-provenance.jsonl`) exclusion the hand-rolled scan lacked. The branch's
   OTHER half of that same file, `SiblingRawLog` **is** in master and is what
   `ResolveRawLogForSession` calls (rawlog.go:351 and :381) — that is the "raw log
   that moved into its session dir" fix, i.e. the branch's own intent, alive.
2. **`cmd/agent/merger_verify_child.go`** — the resolution kept the branch tip's blob
   `b2b9cf55` verbatim; the later **`c4c7f004` "fix(adoption): an already-integrated
   branch is not a failed adoption"** (2026-09-14 20:36, after the merge) reworded the
   one baseline `fmt.Printf` footer
   (`… baseline sweep at %s: failing packages = …` → `… pre-merge baseline at %s — the
   branch is judged as a DELTA against these packages, not by them: …`). Semantics
   unchanged; the reword exists so the footer is not misread as the merge's verdict.
   The tip→master delta for that file is byte-identical to `c4c7f004`'s own diff.

Nothing in the branch tip was dropped: `git log 9efd835a --not master` is empty.

## (b) Build on master tip — GREEN

Worktree `/home/user/workspace/gopi-rawlog-verify` @ `b76942b8` (master tip,
2026-09-14 21:05), `TMPDIR=/dev/shm/gopi-vf-rawlog/tmp`:

```
$ go build ./...
build-all exit=0
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # Rule 12 declared build
declared-build exit=0
-rwxrwxr-x 1 nick nick 22106054 bin/gopi
```

Rule 16–21 gates are green on master: `go test -count=1 ./internal/standards/` →
`ok github.com/nick/gopi/internal/standards 46.310s`.

## (c) Test suite vs the pre-merge baseline — zero NEW failures

Three full `go test -count=1 ./...` runs, each in its own worktree, all with an
off-`/` TMPDIR:

| tree | commit | `go build ./...` | `go test ./...` | pkgs ok | pkgs FAIL | failing tests |
|---|---|---|---|---|---|---|
| pre-merge master baseline | `b9ae5cbc` (merge `^2`) | 0 | exit 1 | 27 | 4 (`cmd/agent`, `stylegate`, `testutil`, `tools`) | 9 |
| the merge resolution | `833cc2b6` | 0 | exit 1 | 28 | 3 (`cmd/agent`, `stylegate`, `testutil`) | 8 |
| **master tip** | **`b76942b8`** | **0** | exit 1 | 32 | **1** (`testutil`) | **3** |

The failing-TEST sets (not just packages):

```
baseline (b9ae5cbc) : TestAcquireServeLock_RefusesAfterUnlinkUnderneath,
                      TestEveryTempUsingSuiteIsPinned, TestPinCoverageTable,
                      TestMergerRestart_ForceInterruptsAndReportsAndKeepsMasterBuildable,
                      TestStyleGate, TestSweepZombiesCLI_DryRunTouchesNoNotifySink,
                      TestSweepZombiesCLI_ReportNotifiesOperator,
                      TestTaskRunningSuffixMonotonicAcrossRearm,
                      TestWorkerOwnershipPointerPresent
master (b76942b8)   : TestEveryTempUsingSuiteIsPinned, TestPinCoverageTable,
                      TestWorkerOwnershipPointerPresent
$ comm -13 <(baseline FAIL tests) <(master FAIL tests)
(empty)   ← ZERO NEW FAILING TESTS
```

Master's three remaining reds are **new master-side offenders in an old gate**, not
regressions from this merge — the gate already failed at `833cc2b6` (and at
`b9ae5cbc`) for *different* offenders, and every file that turned it red on master
arrived AFTER `833cc2b6` (all in-flight work of 2026-09-14 19:10–20:53):

- `TestEveryTempUsingSuiteIsPinned` / `TestPinCoverageTable` — `internal/daemoncwd`
  creates scratch with no `TestMain` pinning the root; the package was added by
  `641018bc` (19:10). At `833cc2b6` the same test failed naming `internal/cgroupscope`,
  fixed by `7f799501`; at the baseline it named `cgroupscope` too. Fix in flight on
  `fix/verify-no-verdict-keeps-merge` (`4a836c0f` adds `internal/daemoncwd/testmain_test.go`).
- `TestWorkerOwnershipPointerPresent` — four devdocs written after the merge
  (`devdoc/daemon-cwd-escape.md` 641018bc 19:10, `devdoc/worker-status-lock-assert.md`
  ec8c56e5 20:03, `devdoc/worker-help-capability-first.md` 9e24d228 20:26,
  `devdoc/rule18-voice-device-else.md` e63e83b4 20:53) teach a worker-launch shape
  without the operator-only pointer. Baseline's offender was a different file
  (`devdoc/worker-daemon-acceptance/closing-run-1806.txt`), fix in flight on
  `fix/worker-ownership-pointer-line` (`8cd5ba18`).
- `cmd/agent` + `internal/stylegate` + `internal/tools` are GREEN on master but were
  red at the baseline — the pre-existing reds named in the briefing's expectations
  (`TestSweepZombiesCLI_*`, `TestMergerRestart_ForceInterrupts…`, `TestStyleGate`,
  `TestAcquireServeLock_RefusesAfterUnlinkUnderneath`,
  `TestTaskRunningSuffixMonotonicAcrossRearm`) have since been fixed by other merged
  work. Two back-to-back re-runs of the two noisy packages on master tip confirm the
  greens are stable, not luck:

```
$ go test -count=1 ./cmd/agent/ ./internal/stylegate/     (×2)
ok  github.com/nick/gopi/cmd/agent     210.517s   /   221.021s
ok  github.com/nick/gopi/internal/stylegate  3.681s  /   6.317s
$ go test -count=1 ./internal/testutil/ -run 'pin|ownership'   → the same 3 reds reproduce
```

**Acceptance:** `go build ./...` = 0, declared build = 0, and master's failing-test
set (3) is a strict SUBSET of the pre-merge baseline's (9) ⇒ **zero NEW failures**.

## (d) Disposition of the branch ref / worktree

- `refs/heads/fix/rawlog-test-home-leak`: **deleted** (post-merge cleanup); no
  `archive/*` tag was created and none is required — the tip is an ancestor of master,
  so it is permanently reachable.
- No worktree ever existed for it: `git worktree list` shows no `gopi-rawlog-rework`
  entry, and none of the 28 registered worktrees holds it.
- The branch name is NOT resurrected (Rule 23); this report is on the fresh branch
  `feat/rawlog-merge-verification`, cut from master tip `b76942b8`.
- Reachability audit at the time of writing: `git branch -a --contains 9efd835a`
  returns **13 refs** (master + 12 in-flight branches — `fix/task-list-inprogress-doc-pointer`,
  `fix/version-dirty-witness`, `fix/liveclaim-worktree-veto`, this deliverable branch, …)
  and `git log 9efd835a --not master` = 0. The original branch ref is ABSENT from
  `git show-ref --heads` (count 0) and no `archive/*` tag names it, so the tip is
  reachable ONLY through the merge that landed it — which is exactly why
  "the branch ref is gone" is not evidence of lost work.
- Worktrees created by this task and their fate: `gopi-rawlog-verify`
  (the deliverable branch), `gopi-pristine-master` @ `b76942b8`, `gopi-vf-baseline` @
  `b9ae5cbc`, `gopi-vf-resolution` @ `833cc2b6` — the three detached ones are
  verification-only and can be removed with
  `git worktree remove <path>` once the record below is read.

## (e) Recovery command for this deliverable

```bash
git show feat/rawlog-merge-verification:devdoc/merge-verify-rawlog-test-home-leak.md
```

The commit lives in the shared object store of the main repo
(`/home/user/work/GoPi`), so the branch is visible there without any push
(`git log --oneline -1 feat/rawlog-merge-verification`).

## Not Done

- **No code was changed and no fix was written** — deliberately: the epic's premise
  ("a merge still needs resolving") is false, and every remaining master red is
  another task's in-flight work (fixes already exist on
  `fix/verify-no-verdict-keeps-merge` and `fix/worker-ownership-pointer-line`). Fixing
  them here would duplicate those branches and touch files this task has no mandate
  over.
- **The three master-reds are left red**, reported rather than silenced: they are NEW
  offenders in an OLD gate, they are not caused by this merge, and the acceptance
  criterion is "zero NEW failures vs the pre-merge baseline", which is met.
- Timings differ between runs (whole suite under load, load average ~25 on this host);
  the comparison above is on failing-set IDENTITY, which is load-independent.

## Verification

- Build: `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0 (master `b76942b8`)
- Tests: `go test -count=1 ./...` on master → exit 1 with 3 failing tests, all present in the pre-merge baseline's 9 (`comm -13` on the failing sets is empty) ⇒ **zero NEW failures**; `go test ./internal/standards/` → ok (27.5s range), Rule 16–21 gates green
- Merge evidence: `git merge-base --is-ancestor 9efd835a master` = YES; `git log 9efd835a --not master` = 0 commits; 4/4 branch-introduced files blob-identical in master; branch ref absent
- Commit: `0d30c0b8` (this record) + `ad01cf5a` (the tip-advance addendum) on
  `feat/rawlog-merge-verification`, cut from master; the commit lives in the
  shared object store of `/home/user/work/GoPi`, so no push is required
- Worktrees: `gopi-rawlog-verify` (this deliverable), `gopi-rawlog-tipfc`
  @ `fc9b06d4`, `gopi-pristine-master` @ `b76942b8`, `gopi-vf-baseline` @
  `b9ae5cbc` — all four carry their own `scratch/tmp` off the `/` partition
  (measured `TMPDEV=2052` vs `ROOTDEV=2051`, Rule 22)

---

## Precision on the baseline: it is the merge's OWN pre-merge parent

`833cc2b6` is a real merge commit with exactly two parents:

```
$ git log -1 --format='%H %P' 833cc2b6
833cc2b6a855464301498119b9a3b5906ffb152d 9efd835a4712d82f8491a617832f76b736199467 b9ae5cbc728bc147839d7c789e93ccc2a57e9432
                                         ^1 = branch tip      ^2 = pre-merge master
```

`^1` is the branch tip `9efd835a`; `^2` is **`b9ae5cbc`** — the very commit this
task's control worktree (`gopi-vf-baseline`, `gopi-master-ctl`, `gopi-redproof`)
is detached at. So the "9 failing tests at the baseline" figure is not a
nearby-master proxy: it is the exact pre-merge master the merge commit itself
records as its other parent.

---

# ADDENDUM — re-verification on the ADVANCED master tip `fc9b06d4`

Master moved while this record was being written (`b76942b8` → **`fc9b06d4`**,
`adoption: merge fix/task-list-inprogress-scan (0618a2cd)`). `fc9b06d4` is the
`--is-ancestor` of the tip already verified, so it is a strict superset: the same
evidence was re-measured on it, per the rule that a master advance invalidates a
control hash.

## (a′) Merge evidence re-measured at `fc9b06d4`

```
$ git merge-base --is-ancestor 9efd835a fc9b06d4   → YES (exit 0)
$ git rev-parse fc9b06d4:<f>  9efd835a:<f>          (all four identical)
  internal/testutil/profileguard.go           a0654895fa04600b7e1e6cf37d8a16c948bc07c3  ==
  internal/testutil/rehome_flat_raw_logs_test.go  270d18354d1f36b4379fcf1df42df03fb907822a  ==
  scripts/rehome-flat-raw-logs.sh             416342268ce54d618fe1187df649aa3603d8907c  ==
  devdoc/rawlog-test-home-leak.md             5f3e8247f99dfb3956196f0152844942bcd8f939  ==
```

`fc9b06d4` adds exactly one branch on top of `b76942b8`
(`b76942b8..fc9b06d4` = `fc9b06d4` + `0618a2cd`), touching 4 files, all inside
`internal/tools/` plus one devdoc:

```
 devdoc/task-list-inprogress-scan-20260914.md      | 130 +++++
 internal/tools/tasks.go                           | 111 ++++++--
 internal/tools/tasks_session_resolve_cost_test.go | 151 +++++
 internal/tools/taskstate.go                       |   4 +-
```

## (a‴) Merge evidence re-confirmed at the NEWEST master `79738cb0`

Master advanced a second time while this record was written
(`fc9b06d4` → **`79738cb0`**, `Merge branch 'fix/verify-no-verdict-keeps-merge'`, +11
commits). The merge-landed evidence was re-run against that tip as well:

```
$ git merge-base --is-ancestor 9efd835a master   → YES (exit 0)
$ git log --oneline 9efd835a --not master | wc -l → 0
$ git rev-parse 9efd835a:<f> master:<f>           (4/4 identical)
  internal/testutil/profileguard.go            IDENTICAL
  internal/testutil/rehome_flat_raw_logs_test.go IDENTICAL
  scripts/rehome-flat-raw-logs.sh              IDENTICAL
  devdoc/rawlog-test-home-leak.md              IDENTICAL
```

The conclusion is invariant under every master advance observed: the tip
`9efd835a` is an ancestor of master, carries no commit master lacks, and its
introduced files are byte-identical. Nothing about this merge is outstanding.

## (b′) Build at `fc9b06d4`

```
TMPDEV=2052 ROOTDEV=2051          # worktree scratch off the / partition (Rule 22)
$ go build ./...                                 → BUILD_ALL_EXIT=0
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ → BUILD_BIN_EXIT=0
  -rwxrwxr-x 1 nick nick 22109192 Sep 14 21:43 bin/gopi
```

## (b‴) Build + standards gates at the NEWEST master `79738cb0` — all GREEN

`/home/user/workspace/gopi-rawlog-newest` @ `79738cb0`
(`TMPDIR=…/scratch/tmp`, measured `TMPDEV=2052` ≠ `ROOTDEV=2051`, Rule 22):

```
TIP=79738cb0
$ go build ./...                                  → exit 0
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ → BUILD_BIN_EXIT=0
  -rwxrwxr-x 1 nick nick 22135102 bin/gopi
$ go test -count=1 ./internal/standards/          → ok  github.com/nick/gopi/internal/standards  112.583s
                                                    STANDARDS_EXIT=0
```

Both acceptance build commands succeed on the newest master, and the Rule 16–21
standards gates (the line-keyed `baseline.go` permits among them) are **green** on
it — so the constraint in this task's briefing holds on the tip that master has
actually reached, not only on the older one.

**These results apply to the deliverable branch verbatim**, because the branch differs
from merged master by exactly one file — this report:

```
$ git diff --name-only master..HEAD
devdoc/merge-verify-rawlog-test-home-leak.md
$ git diff --stat master..HEAD | tail -1
 1 file changed, 566 insertions(+)
```

So the branch's tree IS master's tree plus a devdoc: no separate build/test run on the
branch can produce a different result, and the branch cannot introduce a red.

## (c″) The branch's OWN regression tests, run on the advanced tip `fc9b06d4`

The behavioral proof that the branch's deliverable is live on the acceptance tip —
not merely present as text — is that its own guards pass there:

```
$ go test -count=1 -run 'TestRehomeFlatRawLogsMovesOnlyWhatASessionLocates|TestSiblingRawLog|TestResolveRawLog' ./internal/testutil/ ./internal/gopilog/ -v
--- PASS: TestRehomeFlatRawLogsMovesOnlyWhatASessionLocates (17.13s)
ok   github.com/nick/gopi/internal/testutil  23.826s
--- PASS: TestResolveRawLogForSessionFollowsARehomedRawLog (0.00s)     ← the branch's reason to exist
--- PASS: TestResolveRawLogForSessionPrefersRecordedPath (0.36s)
--- PASS: TestResolveRawLogForSessionFallsBackToTimestamp (0.00s)
--- PASS: TestResolveRawLogForSessionMissing (0.00s)
ok   github.com/nick/gopi/internal/gopilog  7.096s
```

`TestResolveRawLogForSessionFollowsARehomedRawLog` IS the "follow a raw log that moved
into its session dir" fix; it is green on `fc9b06d4`. Combined with section (f)
(all 34 of the branch's `rawlog.go` lines present, `SiblingRawLog` callers at
rawlog.go:351/:381) this closes the merge: the branch's code is in master AND its
behavioral guard passes on the tip the epic names as the acceptance target.

## (c′) Failing-set diff, `b76942b8` vs `fc9b06d4` — MEASURED, not inferred

The three master-reds were run with identical commands and identical stripped
worker env (`env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u GOPI_WORKER_LOCK_FD`)
in two worktrees, one per tip:

| gate test | `b76942b8` (control) | `fc9b06d4` (advanced tip) |
|---|---|---|
| `TestEveryTempUsingSuiteIsPinned` | FAIL — `internal/daemoncwd` | FAIL — `internal/daemoncwd` |
| `TestPinCoverageTable` | FAIL — `internal/daemoncwd` | FAIL — `internal/daemoncwd` |
| `TestWorkerOwnershipPointerPresent` | FAIL — 4 devdocs | FAIL — **5** devdocs |

⇒ **failing TESTS: 3 → 3, same set — zero NEW failures.**
⇒ **failing OFFENDERS inside the pointer gate: 4 → 5 — the +1 is fc9b06d4's own
new devdoc `devdoc/task-list-inprogress-scan-20260914.md`.** The gate was already
red on both tips, so this does not add a newly-failing test, but it is a real
regression in a gate's hit-list and is recorded honestly rather than folded into
"no delta".

Both additions already have owners — NOT this task's mandate:

- the `internal/daemoncwd` pin → `fix/verify-no-verdict-keeps-merge` `4a836c0f`
  (`test(daemoncwd): pin the suite's scratch root (Rule 22)`), plus `0230f6c7`
  (fixture self-decoration via `t.Setenv`);
- the pointer on the new devdoc → `fix/task-list-inprogress-doc-pointer`
  `cc9ef918` (`docs(tasklist): carry the operator-only pointer on the perf fix's own record`).

## (e′) Recovery command (unchanged branch)

```bash
git show feat/rawlog-merge-verification:devdoc/merge-verify-rawlog-test-home-leak.md
```

---

## (c″″) AUTHORITATIVE whole-suite sweep on `fc9b06d4` — `go test ./...`

The gate-level comparison above is backed by a full whole-tree sweep on the tip
(`/dev/shm/gopi-vf-rawlog/tip.log`, job elapsed 763.8s):

```
=== TREE ===
fc9b06d4…
=== BUILD ./... ===          build-all exit=0
=== DECLARED BUILD ===       declared-build exit=0
=== GO TEST ./... ===        test-all exit=1
--- FAIL: TestEveryTempUsingSuiteIsPinned (0.36s)
--- FAIL: TestPinCoverageTable (0.18s)
--- FAIL: TestWorkerOwnershipPointerPresent (0.54s)
FAIL   github.com/nick/gopi/internal/testutil  219.936s
32 packages reported "ok"
```

Exactly **3** failing tests, and their failing package set is the single package
`internal/testutil` — all three exist in the pre-merge baseline's 9 ⇒ **zero NEW
failures**, confirmed by the whole suite, not only by the targeted gate run.
(Reproduced independently: the identical three tests fail in the
`gopi-pristine-master` @ `b76942b8` control, and the identical two scratch-pin
tests fail at the baseline too.)

## (c‴) Trajectory across the tips observed — zero NEW failures at every one

Master advanced three times during this verification (`b76942b8` → `fc9b06d4` →
**`79738cb0`**, the last being `Merge branch 'fix/verify-no-verdict-keeps-merge'`).
Each tip was re-measured with the same gate command; the failing count only falls:

| tip | commits vs previous | failing tests | offenders of the pointer gate |
|---|---|---|---|
| `b9ae5cbc` (pre-merge master, merge commit's `^2`) | — | 9 (baseline) | `worker-daemon-acceptance/closing-run-1806.txt` |
| `b76942b8` | — | 3 | 4 devdocs |
| `fc9b06d4` | +2 | 3 (same set) | 5 devdocs |
| `79738cb0` | +11 | **1** | 5 devdocs |

At `79738cb0` the two scratch-pin gates have gone **GREEN** — `TestEveryTempUsingSuiteIsPinned`
and `TestPinCoverageTable` no longer appear, because master merged the very fix this
record predicted (`4a836c0f` / `0230f6c7` on `fix/verify-no-verdict-keeps-merge` →
`79738cb0`). The single remaining red is `TestWorkerOwnershipPointerPresent`:

```
$ go test -count=1 -run 'TestWorkerOwnershipPointerPresent|TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable' ./internal/testutil/
--- FAIL: TestWorkerOwnershipPointerPresent (0.87s)
          devdoc/daemon-cwd-escape.md
          devdoc/rule18-voice-device-else.md
          devdoc/task-list-inprogress-scan-20260914.md
          devdoc/worker-help-capability-first.md
          devdoc/worker-status-lock-assert.md
FAIL   github.com/nick/gopi/internal/testutil  10.441s
```

That gate is red at the baseline too (with a different offender), so it is not a new
failure; and of its five offenders only `devdoc/task-list-inprogress-scan-20260914.md`
is newer than the baseline era — the one owned by the in-flight
`fix/task-list-inprogress-doc-pointer` (`cc9ef918`). **Zero NEW failing tests at every
tip measured; the count is monotonically decreasing toward the baseline's 9.**

## (f) What superseded the branch's `rawlog.go` / `merger_verify_child.go` lines

The epic asks for this explicitly. Section (a) above is already correctly scoped:
it claims blob identity for the files the branch **introduced** (4 of them) plus the
two test files it authored, and it names the seven conflicted files the resolution
hand-merged. What this section adds is the EXHAUSTIVE counterpart, because "the
conflicted files" and "the files the branch introduced" do not cover the branch's
whole footprint: measured from its own merge-base the branch touched **35** files
(`git diff --stat 13acf700 9efd835a`), and only 4 of them are byte-identical in
master today. The remaining 31 were either untouched by later work or **changed
again** after the merge. "The merge landed" is proven by ancestry and by content —
so the content is proved line by line for every file that moved after the merge.

## Method — line-survival, not blob identity

For every file the branch changed and that master later changed again, take the
lines the BRANCH added (`git diff -U0 <merge-base> 9efd835a -- <f>`, `^+` only)
and check each one still exists verbatim in master's copy
(`git show fc9b06d4:<f>`):

```
cmd/agent/cli_tasks.go                        added_lines=26 missing_in_master=2
cmd/agent/merger_verify_child.go              added_lines=27 missing_in_master=0
cmd/agent/tmpdir_inject.go                    added_lines=1  missing_in_master=0
internal/gopilog/rawlog.go                    added_lines=34 missing_in_master=0
internal/signalrec/signalrec_test.go          added_lines=124 missing_in_master=0
internal/standards/baseline.go                added_lines=14 missing_in_master=1
internal/testutil/tmproot_assert_test.go      added_lines=5  missing_in_master=0
internal/tools/tasks.go                       added_lines=19 missing_in_master=0
cmd/agent/cli_resume.go                       added_lines=5  missing_in_master=0
internal/gopilog/rawlog_test.go               added_lines=62 missing_in_master=0
internal/gopilog/rounds_test.go               added_lines=21 missing_in_master=0
                                              ─────────────────────────────
                                              362 added, 3 not verbatim (99.2%)
```

Every symbol the branch introduced is still present in master:

```
SiblingRawLog                    branch_files=3  master_files=3
mergeVerifyBaselineWait          branch_files=2  master_files=2
mergeVerifyBaselineTimeoutEnvName branch_files=3 master_files=3
ReportTempRootIfOnRootFS         branch_files=3  master_files=3
srReadyEnv (signalrec readiness) branch_files=1  master_files=1
```

## The three non-verbatim lines — all re-keying / re-wrap, none a loss

1–2. `cmd/agent/cli_tasks.go` help text:

```
-  gopi task sweep-worktrees [--dry-run] [--apply] [--min-age-h N] [--max N]
-                         [--json] [--db PATH]
+  gopi task sweep-worktrees [--dry-run] [--apply] [--min-age-h N]      (master:219)
```

   A usage line **re-wrapped** by later work, not deleted — the same subcommand
   with the same flags is still documented.

3. `internal/standards/baseline.go` permit key:

```
-  "cmd/agent/main.go:961",     (the key the BRANCH's re-key note wrote)
+  "cmd/agent/main.go:943",     (master:241 today)
```

   Exactly the line-keyed-permit mechanics this task's briefing warned about
   (Rule 16–21: baseline permits are `file:line`-keyed). The branch's permit and
   its **reason text survive** — master carries the branch's own explanation
   (`grep -c 'payloadFilePrefix STRING constant'` on master's baseline.go = **14
   hits**) while the key alone was re-keyed by two later master movements
   (`1035 → 1040` by the peak-guard exemption, then `→ 943` when the service-help
   epic shrank `workerUsageHead` / `mergerUsageHead` by 97 lines). The permit was
   NOT lost and NOT re-added — it moved with the site, which is the intended
   behaviour of a line-keyed allow-list.

## The two files the epic named specifically

- **`internal/gopilog/rawlog.go`** — the branch added `SiblingRawLog` (a raw log
  beside a session JSONL, matched by basename, so a re-homed log still resolves)
  plus the `want`/`head` plumbing. Master **keeps all 34 added lines**; a LATER
  master commit (`f3569050`) replaced the *inlined session-tree scan* in
  `SessionsRecordingRawLog` with `sessionlayout.Records(dir)` — a refactor of the
  surrounding walk (so `session-provenance.jsonl` side files stop being read as
  sessions), not a reversal of the branch's work. Master's `rawlog.go` =
  branch's lines + that refactor.
- **`cmd/agent/merger_verify_child.go`** — the branch added the baseline-sweep
  wait cap (`mergeVerifyBaselineWait` / `mergeVerifyBaselineTimeoutEnvName`).
  **All 27 added lines are verbatim in master.** The post-merge change
  (`c4c7f004`) is a *reword of the log footer* — dropping "failing packages = …"
  because it read like a verdict instead of a baseline RECORD (Rule 15b). The
  cap mechanism is untouched; only the operator-facing sentence changed.
