<!-- doc-class: normative -->
# Task Report: archive/feat/worker-term-exit — per-unit SALVAGE / OBSOLETE, and the salvage

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Tag re-measured: `archive/feat/worker-term-exit`, tip `dd5dde45` (2026-09-17).
Branch: `verdict/worker-term-exit-remeasure` (fresh worktree off `master`
`fa12d97f`, Rule 23 — the tag was read only via `git show`/`git log`/`git diff`).
The tag is NOT deleted, the tag is NOT merged, and nothing was committed on it.

Recover the branch tip with `git rev-parse verdict/worker-term-exit-remeasure`.
(No tip sha is written into this file: committing the file moves the tip, so
only the PARENTS of the commits below are quoted.)

## The instruments, observed

```
$ git cherry -v master archive/feat/worker-term-exit
+ 227be195b47889a55033240bd662d97257dc76e1 fix(worker): a bare SIGTERM now reaches the in-flight run child (stop parks in futex_wait_queue_me)
+ 63f2d500e860be492f8a348ec412f30d7bf91c64 docs(worker): the stop deadline is conditional — the bare-signal bound, the drain wait with no deadline
+ 35c280bf44e4f37351d7d4a018a9062c141e839f docs(worm): sleep 600 via bash is killed by the 120s no-output net
+ dd5dde4526d7cc7cbcd8ac44e7dd7ca1507cef5a docs(worm): verbatim result of sleep 600 via bash — 120s no-output kill; detached survives

$ git rev-list --count master..archive/feat/worker-term-exit
4
$ git rev-list --count archive/feat/worker-term-exit..master
663
$ git merge-base master archive/feat/worker-term-exit
a2f519e45c809db81ce4d1b53c6891a2870c65e0
$ git merge-base --is-ancestor archive/feat/worker-term-exit master; echo $?
1
$ git tag -l 'archive/feat/worker-term*'
archive/feat/worker-term-exit
```

Readings: cherry `+4/-0` and rev-list 4 agree, so there is no internal merge and
no patch-id-equivalent copy on master. **No merged twin exists** — the sibling
sweep `git tag -l 'archive/feat/worker-term*'` returns this tag alone (contrast
`archive/feat/worker-run-entry*`, which has a twin). Base drift is 663 commits,
which is why the audit's absent-declaration list is not usable as evidence (see
"the audit's list is polluted", below).

## Per unit

The tag's kernel is commit `227be195` (its parent `a2f519e4` is the merge-base,
so that commit is the tag's own work); `63f2d500` is an amendment to it, and
`35c280bf`/`dd5dde45` are a separate documentation job.

| # | unit | verdict | evidence |
|---|---|---|---|
| U1 | the loop-lifetime forwarder — `startWorkerStopForwarder` / `workerStopNudge` / `notifyWorkerStopNudge` / `workerStopDrainSettle` / `forwardWorkerStopDuringRound` + the loop wiring. **The hang's primary mechanism**: the stop is only forwarded at the round boundary, and a round returns only when its child exits, so a bare SIGTERM mid-round reaches nothing | **SALVAGE** | all five symbols: `git grep -c -w <sym> master -- '*.go'` → 0 hits; `git log --oneline -S<sym> master -- '*.go'` → empty (never added ⇒ never removed by another path). The tag's own `TestWorkerBareSignalReachesChildDuringRound` is RED on clean master (probe U1 below) and GREEN on the salvage branch |
| U2 | the stop's target MEASURED, not assumed — `signalRunChild` (pgid via `Getpgid`; group signal only when `pgid == pid`), `workerStopUndeliveredNote` (a stop that reached nothing is REPORTED) | **SALVAGE** | `signalRunChild` / `workerStopUndeliveredNote`: 0 hits on master, `-S` empty. Master's `forwardWorkerStopToChild` still builds `kill(-c.PID)` from the registry's ASSUMED `Group`, and the production spawn sets no `Setpgid` (`git grep -n SysProcAttr master -- 'cmd/agent/cli_tasks*.go'` → empty, while `cli_tasks.go:1924` registers `Group: cmd.Process.Pid`), so the ESRCH is still swallowed by `//stylegate:ignore best-effort …`. The tag's own `TestWorkerStopReachesChildWhoseGroupWasAssumed` is RED on master / GREEN on the branch |
| U3 | descendants reached, and captured BEFORE the child is signalled — `descendantPIDs` / `readPPID` / `procRoot` / `procStatFileName` | **SALVAGE** | 0 hits on master, `-S` empty. The tag's own `TestWorkerStopReachesGrandchildren` is RED on master / GREEN on the branch |
| U4 | the missing `drain` reset in `resetWorkerStopState()` | **OBSOLETE** (already on master) | `master:cmd/agent/worker_drain_unit_test.go:69` carries `workerStopState.drain = false`; `git log -S'workerStopState.drain = false' master` names `ef772e72` (2026-09-17 — the same day) |
| U5 | the docs job (`35c280bf` + `dd5dde45`) — `sleep 600` via bash is killed at the 120s no-output net | **OBSOLETE** (record-only) | the finding is carried by master's own code: `internal/tools/timeout.go:37` `DefaultBashIdleTimeout = 120 * time.Second` (env `GOPI_BASH_IDLE_SEC`), `internal/tools/bash.go:451` `[command killed: no output for %s]`. The tag's `devdoc/worm-long-bash-hold.md` is ABSENT from master — it records a *run*, not a mechanism |
| U6 | `workerStopRegistrySettleGrace` (the tag's second settle under the registry deregistration race) | **OBSOLETE** (dead in the tag itself) | declared at the tag's `cli_tasks_worker_stop.go:565` and read NOWHERE in the tag tree — the doc comment claims it "is what makes the forwarder's work monotonic" while `forwardWorkerStopDuringRound` has exactly one settle. Never a landed unit, and not re-derived: re-deriving it would add a constant whose only reader does not exist (the Rule 21 shape) |

### The audit's absent-declaration list is polluted (a name test, not a content test)

The audit (`devdoc/archive-tag-audit-20260918.md`) lists 12 absent production
declarations for this tag, including `controlConflictFlags` and
`controlReadAndActRefusal`. Those two are **absent from the tag's own commit
delta**: `git diff a2f519e4 archive/feat/worker-term-exit` touches six files
(the three `cmd/agent` files, the tag's new test, and the two devdocs) and adds
neither name. They live in the tag's *base* snapshot — declared in the tag's
`cli_worker_control.go` and called from the tag's `cli_merger_stop.go`, files the
tag does not touch at all. They are pre-existing master drift (663 commits),
exactly the artefact a base-drift name list produces. Only the symbols the tag's
own commits ADD can be units of this tag, and those are the twelve in U1–U3 plus
`workerStopRegistrySettleGrace`.

## The red proof, and the green that follows it

The decisive instrument is the TAG'S OWN test file
(`cmd/agent/worker_stop_forward_regression_test.go`), transplanted VERBATIM
(`diff -q` against the tag blob: identical) onto a throwaway detached worktree
at the master sha.

**Whole file, on clean master:**

```
$ go vet ./cmd/agent/
vet: cmd/agent/worker_stop_forward_regression_test.go:85:19: undefined: startWorkerStopForwarder
```

**Per unit, on clean master** — the file cannot compile as a whole
(`startWorkerStopForwarder` is undefined), so the units were measured by a probe
file whose bodies are the tag's tests verbatim, with a single substitution each
(`startWorkerStopForwarder()` removed for U1; `descendantPIDs` → a literal
`/proc` walk for U2/U3 — that identifier is itself tag-only). The probe's own
names carry their ORIGINAL probe numbering, kept verbatim from the run:
`TestProbeU2_*` = the forwarder unit (U1 above), `TestProbeU3_*` = the group-
assumed reach (U2), `TestProbeU4_*` = grandchildren (U3):

```
$ go test -run 'TestProbeU2_ChildWhoseGroupWasAssumed' ./cmd/agent/
    zz_term_exit_probe_test.go:66: PROBE-RED U2: the run child (pid 4036876) never received
    the stop: its group was ASSUMED to be its pid, so kill(-pid) addressed no process and the
    ESRCH was swallowed. The child survived a stop that reported success.
--- FAIL: TestProbeU2_ChildWhoseGroupWasAssumed (20.01s)
$ go test -run 'TestProbeU4_GrandchildReach' ./cmd/agent/
    PROBE-RED U4: the run child's grandchild (pid 4048985) was never signalled …
--- FAIL: TestProbeU4_GrandchildReach (20.09s)
$ go test -run 'TestProbeU3_BareSignalMidRoundReach' ./cmd/agent/
    PROBE-RED U3: the in-flight run child was never signalled: the round is still waiting 20s
    after a bare SIGTERM (pre-fix: parked in futex_wait_queue_me until SIGKILL). master has no
    forwarder for the bare-signal shape.
--- FAIL: TestProbeU3_BareSignalMidRoundReach (20.01s)
```

**On the salvage branch, with the tag's test file VERBATIM (no edit at all):**

```
$ go test -run 'TestWorkerBareSignalReachesChildDuringRound|TestWorkerDrainStillLetsTheRoundFinish|TestWorkerStopReachesChildWhoseGroupWasAssumed|TestWorkerStopReachesGrandchildren' ./cmd/agent/
--- PASS: TestWorkerBareSignalReachesChildDuringRound (2.20s)
--- PASS: TestWorkerDrainStillLetsTheRoundFinish (20.02s)
--- PASS: TestWorkerStopReachesChildWhoseGroupWasAssumed (0.01s)
--- PASS: TestWorkerStopReachesGrandchildren (0.12s)
ok  	github.com/the operator/gopi/cmd/agent	23.250s
```

The red→green transition is on the SAME assertions, unmodified.

## What was re-derived (the SALVAGE)

Committed on `verdict/worker-term-exit-remeasure`, re-derived onto master's
current file shapes rather than replayed as the tag's patch:

- `cmd/agent/cli_tasks_worker_stop.go` — `workerStopNudge` +
  `notifyWorkerStopNudge` (one-slot non-blocking send, signal-handler safe),
  `workerStopDrainSettle` (= `workerFileWatchInterval`), `startWorkerStopForwarder`,
  `forwardWorkerStopDuringRound` (durable files read FIRST, then a settle, then the
  bare-signal forward — so a `--stop` whose file lands in the same instant degrades
  to a DRAIN and nothing is forwarded), `signalRunChild` (pgid MEASURED via
  `Getpgid`, group signal only when `pgid == pid`; otherwise the child plus its
  `/proc`-walked descendants, enumerated BEFORE the kill), `descendantPIDs`,
  `readPPID`, `procRoot`/`procStatFileName`, `workerStopUndeliveredNote`; the
  signal handler now nudges the forwarder.
- `cmd/agent/cli_tasks_worker.go` — the forwarder starts for the loop's lifetime
  and is stopped by its defer.
- `cmd/agent/cli_worker_control.go` — `consumeDrainRequests` nudges the forwarder
  on both the force and the stop file (the file is a stop too; the forwarder
  re-reads it and still forwards nothing for a DRAIN).
- `cmd/agent/worker_stop_forward_regression_test.go` — the tag's test file as its
  own new file. It is the tag's content, and it edits no master-authored test;
  the ONE difference from the tag blob is six `//stylegate:ignore` /
  `//stylegate:allow-sleep` markers appended to lines that discard an error or
  sleep (the marker must share the line it exempts, so a verbatim byte copy
  cannot carry the exemption — `devdoc/style-sweep-20260912.md`). Measured: with
  the markers, the stylegate ceiling report over the whole repo is IDENTICAL to
  master's (both green-ceiling-diff empty) while master's own gate test is red
  at HEAD on this host for both. Without the markers, the test file alone would
  have added 8 `ignored-error` + 3 `test-sleep` hits.

Deliberately NOT re-derived: `workerStopRegistrySettleGrace` (U5, dead in the
tag). Re-deriving it would add a constant whose only reader does not exist.

The drain guarantee is the thing to keep intact: a DURABLE drain still forwards
nothing, because the forwarder reads the request file before it decides;
`awaitWorkerStopChild`/`awaitWorkerDrainChild` still own the close-out and are
unchanged.

## Rule 27 / Rule 30 notes

- No load generator, no saturation, no whole-tree suite. The probes are bounded
  `go test -run` runs; the longest is the drain test's deliberate 20s hold.
- The deliverable worktree is porcelain-clean (below). The PROBE worktree
  (`/home/user/workspace/gopi-term-exit-probe`, detached at master) is the ONE
  place dirt is expected: it holds the throwaway probe file and the `.evid/`
  diff capture, and it is not a deliverable.

## Verification

| check | command | result |
|---|---|---|
| build | `CGO_ENABLED=0 go build ./...` (salvage worktree) | exit 0, no output |
| vet | `CGO_ENABLED=0 go vet ./cmd/agent/` | exit 0 |
| scoped tests | `go vet ./cmd/agent/ && go test -count=1 -run 'TestWorkerBareSignalReachesChildDuringRound\|TestWorkerDrainStillLetsTheRoundFinish\|TestWorkerStopReachesChildWhoseGroupWasAssumed\|TestWorkerStopReachesGrandchildren' ./cmd/agent/` | `vet_exit=0`; `ok github.com/the operator/gopi/cmd/agent 23.652s` (re-observed at close-out) |
| the tag's own tests | `go test -run '…' ./cmd/agent/` | 4/4 PASS (transcript above) |
| red proof | probe worktree at master `fa12d97f` | 3/3 FAIL (transcript above) |
| worktree clean | `git status --porcelain` | empty |
| tag untouched | `git rev-parse archive/feat/worker-term-exit` | `dd5dde4526d7cc7cbcd8ac44e7dd7ca1507cef5a` (unchanged) |
| nothing merged | `git rev-parse HEAD` in the MAIN repo | `fa12d97f` (master, unchanged; my commits are on `verdict/worker-term-exit-remeasure` only) |
| stylegate ceilings | the salvage tree's per-rule counts vs master's, `go test -run TestStyleGate ./internal/stylegate/` | IDENTICAL in both trees — master's own gate test is red at this HEAD (198 test `ignored-error` against ceiling 163, 1 test-setenv, 2 test-skip, 2 unchecked-close), a pre-existing condition; the salvage adds no ceiling delta |
| scoped suite | `go test -count=1 -run 'Worker\|Stop\|Child\|Drain' ./cmd/agent/ ./internal/tools/` | `cmd/agent: ok 224.539s`; `internal/tools: FAIL` on ONE test, `TestTaskRunWritebackTaggedStampsWorkerProvenance` — reproduced at close-out on the SALVAGE tree itself and its stderr names the cause, which is host config and not this change: `task_submit: REFUSED … this node declares role=client (a mirror leg) but no authoritative peer is configured (sync_to / GOPI_SYNC_TO)`. `cmd/agent` is the only package the salvage touches; `internal/tools` is untouched by it |

## Not Done (honest)

- The tag's `devdoc/worker-sigterm-futex-hang.md` is not re-landed. Its measured
  narrative (rig outputs, the pgid geometry) is superseded by the evidence in
  this report, and the tag still holds the original — re-landing a 188-line
  report duplicating this one would be noise. The tag's `devdoc/worm-long-bash-hold.md`
  (U4) is likewise not re-landed: it records a one-off observation, and the
  mechanism it describes is already pinned by master's own constants and tests.
- No merge, no tag deletion (Rule 6e/9: the PA merges).
