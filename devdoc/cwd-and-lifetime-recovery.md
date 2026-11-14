<!-- doc-class: normative -->
# devdoc: cwd-and-lifetime recovery — RECOVER verdict is **already superseded**, not lost

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: 01a0ab11-70e6-7ee1-a88b-beae6d618963 (recover `archive/fix/cwd-and-lifetime`)
**Branch**: `feat/cwd-lifetime-recovery` (fresh off master `9ce8e159`, Rule 23)
**Archive tag**: `archive/fix/cwd-and-lifetime` @ `b37b776f` — **kept, never deleted**
**Outcome**: **no production code re-landed.** The archived work is not missing; it is
superseded on master by equivalent-or-better mechanisms. Deliverable is the proof.

---

## 1. Failure reason (the briefing's first question)

`gopi task show 01a0a211-f9f5-764a-b4ba-e73969253c7c`:

```
status=failed, run_method=merger, claimed by peer1 2026-09-15 06:38→06:49 (11 min)
reason="resolve child failed to resolve the conflict: resolve landed but re-merge did not (error)"
tags: merge-conflict, auto-spawn
```

**The row is not the feature work.** It is a merge-conflict RESOLVER auto-spawn row:
the original `fix/cwd-and-lifetime` branch conflicted against master, a resolver child
committed a resolution (`b37b776f "resolve merge fix/cwd-and-lifetime into master"`),
and **the merger's re-merge then errored** — so the resolution commit was never landed.
`b37b776f` is a merge commit: parent `c35623ce` is the **feature tip** (branched, not an
ancestor of master), parent `83d1878d` **is** an ancestor of master. The 4 files came from
the feature lineage (`e3d05e14`, 2026-09-15 00:58:39), not from the failed row.

**This is the decisive evidence for the whole recovery.** The original feature branch
was **built on a pre-`worktreerelease` base**. Master meanwhile landed its own answer to
the same defect:

| master commit | date | what |
|---|---|---|
| `a2ccb31f` | **2026-09-15 00:25** | `fix(worktree): one cwd-release action, called on every worktree-removing producer path` → `internal/worktreerelease` |
| `b33dda6e` | 2026-09-12 03:49 | `fix(crashdump): classify orphan sessions by liveness, not by a missing tail` → `internal/procstat` (verified an ancestor of master) |
| `e3d05e14` | 2026-09-15 00:58:39 | `fix(cwd,lifetime): …` → the archived feature's OWN first commit |

`a2ccb31f` landed **33 minutes BEFORE** the archived feature work began writing
(`e3d05e14`, 00:58:39) and **6h20m before** the archived tip (`b37b776f`, 06:45).
Two independent implementations of the same defect, same night, master's first.
The archive tag is a **pre-conflict snapshot**, not a lost deliverable.

## 2. What the 4 production files were, and what master has instead

The briefing named 4 files / 37 symbols. All four are superseded. Verified by
`git rev-parse master:<f>` (**path does not exist** — fatal) for every file, and by
symbol-level comparison below.

### 2.1 `internal/procname/identity.go` + `liveness.go` (Triple / ParseStartTime / ReadStartTime / ReadCmdline) — SUPERSEDED

master `internal/procname/procname.go` (357 lines; the archive's same-named shared file was
352 — the 106-line `identity.go` + 187-line `liveness.go` were the additions) owns the same
concept under a **newer, strictly richer** API:

| archived | master equivalent | note |
|---|---|---|
| `Triple{Pid,Start,Cmd}` + `ParseTriple` | `Liveness{Pid,StartTime,Cmdline []string}` | master keeps argv as a VECTOR, so an argv token containing a space cannot forge a match |
| `TripleVerdict` → `LiveAlive/LiveDead/LiveRecycled` | `Verdict(procRoot, Liveness)` → `VerdictAlive/VerdictRecycled/VerdictUnknown` | master is **3-valued with an explicit UNKNOWN** — the archived "unreadable ⇒ degrade to weaker verdict" becomes "unreadable ⇒ Unknown" (Rule 15 cleaner) |
| `ReadStartTime` / `ParseStartTime` | `StartTicks(procRoot,pid)` / `StatField(statLine, field)` | master's `StatField` is the general form; both sides independently solved the same parse trap (count fields after the LAST `)`, because comm may contain spaces/parens) |
| `ReadCmdline` | `Cmdline(procRoot,pid) ([]string, bool)` | master adds a zombie-state clause: `state == Z ⇒ VerdictRecycled` |

Both sides independently solved the **same parse trap** (count fields after the LAST `)`)
— see master's `StatField` doc comment and the archived `ParseStartTime` doc comment.
Same conclusion, arrived at twice. `internal/testutil/procname_triple_redproof_test.go`
is master's own red-proof for the pid-reuse case the archive's `liveness.go` header
describes. Re-landing `Triple` would **fork the source of truth** (Rules 13/14) and
collide with `procname.go`'s package-level const block in the same package.

**One REAL leftover, measured and filed separately (not recovered here).** The archived
file claimed it "deleted three duplicate starttime parsers and made them delegate". On
master that cleanup is **still absent** — three independent `lastIndex(')')`-then-count
parsers coexist and each re-derives the same rule:

| file | symbol | note |
|---|---|---|
| `internal/procname/procname.go:204` | `StatField` | derives the index from the field number (`field - StatFieldState`) — the general form |
| `internal/scratch/holder.go:484` | `ParseProcStartTime` | delegates to `procname.StatStartTime` (`:485`) — the general form's only caller here |
| `internal/tools/taskproc.go:164` | `procStartTicks` | hardcodes `procStatStartTimeField = 22` minus `procStatFieldOffset = 3` |

All three are correct (they all count after the last `)`), and their own comments already
cite "the same rule … (Rule 14: one implementation of the parse)" — i.e. master *knows* it
is duplicated. This is a genuine, verifiable consolidation the archive would have done.
It is **filed as a separate task rather than done here** — task
`01a0b3d5-e8e4-7b9b-9837-e1d6ef7b212e`, whose payload carries the three file:line sites,
the constraints and the acceptance. Rationale: it is a cross-package refactor of three
live call paths, unrelated to the recovery question, and half-landing it in a recovery
session is exactly the second-source-of-truth defect this devdoc is about.

### 2.2 `internal/tools/process_cwd_release.go` (ProcessCwdRelease* / usableCwd / releaseFallbackDirs) — SUPERSEDED

master's `internal/worktreerelease/release.go` (landed `a2ccb31f`) is the same single
implementation: `ProcessRootPath = "/"`, `Release()`, `ReleaseReporting()`,
`TestProcessExecutableSuffix` / `inTestProcess()` — one-to-one with the archived
`ProcessCwdRelease` family. `internal/tools/task_reclaim_self.go` already carries
`CwdReleasePath = worktreerelease.ProcessRootPath` and documents it as the ONE definition
(`internal/tools/task_reclaim_self.go:79-90`).

Coverage check — measured per SPECIFIC symbol, because a name-prefix grep conflates an action
with a query (see below):

| archived, the RELEASE action (`ProcessCwdReleaseRoot` 4 + `ProcessCwdRelease` 3 + `ProcessCwdReleaseTo` 2 = **9 call sites**, all in production files) | master (`worktreerelease.Release*`, 5 non-test call sites in 5 files) |
|---|---|
| `cli_tasks_worker.go` round top | ✅ `cli_tasks_worker.go:201` |
| `cli_tasks.go` serve loop / merger pass / pre-pass, `task run` close-out | ✅ `cli_tasks.go:2063`, `cli_tasks.go:2771`, `internal/tools/tasks.go:6054` |
| `internal/agent/taskregister.go` executor close-out, `internal/agent/loop.go` exit funnel | ✅ `internal/agent/taskregister.go:364` (master's own wiring of the same seam) |
| `cli_resume.go` | ✅ (master-only addition, `cli_resume.go:433,459` via `resumeWorktreeRelease`) |
| `cli_worker_control.go` ×3, `cli_tasks_worker_stop.go` ×2, `cli_worker_daemon.go`, `cli_merger_daemon.go`, `cli_merger_stop.go`, `merger_graceful.go` ×2 | ❌ absent |

**Correction to the earlier draft of this devdoc, measured.** The archived devdoc's own
Appendix 6.1 lists those six files under a `grep -rn 'ProcessCwdRelease'` — but **not one of
their call sites is a release**: every one is
`tools.ProcessCwdReleaseFromWorktree(roleLabel)`, a *query* asking "is my cwd inside a
worktree?" (`git show …:<file> | grep -c 'ProcessCwdReleaseFromWorktree('` → 1/1/3/1/2/1;
`…Root(|…Release(|…To(` → **0** in all six). So the delta is **9 release sites → 5**, and the
6-file list is a *query* delta, not a release gap. Master has no `ProcessCwdInside`-style query
in `internal/worktreerelease` at all — the only "inside the worktree" questions on master live in
tests (`cmd/agent/cwd_release_composition_test.go:crcInsideWorktree`). Reported as a capability
master does not carry, not as a call site to re-wire.

**The delta is NOT a recoverable gap either way, and it needs no work.** Three of those six files do not exist on master at all: `cmd/agent/cli_merger_daemon.go`, `cmd/agent/merger_graceful.go`, `cmd/agent/cli_merger_stop.go` were deleted by `fb877170 refactor(merger)!: the merger is a cronjob — remove the daemon machinery` (verified: `git cat-file -e master:<f>` → ABSENT, present in the tag), so there is no merger-daemon process left to release out of a worktree. For the three that remain, master removed the
premise. Those sites were wait-for-exit loops of a process that had been *spawned into*
the worktree. master instead fixed the defect at the **spawn side**:
`daemoncwd.Resolve` (`internal/daemoncwd`, `cli_daemon_spawn.go:370`,
`merger_takeover.go:455`, `merger_restart.go:406`) resolves the child's cwd so it is
**provably outside the worktree and off `/`, with the degradation announced** before the
child exists. Confirmed on master: `os.Chdir` does not appear at all in the three surviving
files — `cli_worker_daemon.go`, `cli_tasks_worker_stop.go`, `cli_worker_control.go`
(`git show master:<f> | grep -c 'os.Chdir'` → 0 each; the other three files are the
merger-daemon machinery `fb877170` deleted, so there is nothing there to grep).

Archived-spawn-time-`Chdir` vs master-spawn-time-`Resolve`: master's is the better half —
it is upstream of the problem rather than a cleanup after it.

### 2.3 `internal/tools/daemon_lifetime.go` (DaemonOrphan / SweepOrphanDaemons / orphanDaemonIn / daemonStateRootIn) — SUPERSEDED, implemented BETTER

The archived file's own header called itself a **backstop**, and named the real fix as
"a lifetime bound the kernel enforces (PR_SET_PDEATHSIG, a death-pipe the daemon polls)".
**master shipped exactly that**, plus an equivalent sweep:

| archived | master |
|---|---|
| `internal/tools/daemon_lifetime.go` (329 lines) | `internal/testutil/orphansweep.go` — `FindOrphansUnderRoot` / `SweepOrphansUnderRoot`, whole-state-root scope, `orphanCandidate(pid)` ownership classification, `row.Identity` / `row.Proof` / `row.Outcome` per sweep |
| `daemonOrphanSweepEnabled()` reading the kill switch `DaemonOrphanSweepEnv = "GOPI_DAEMON_ORPHAN_SWEEP"` (default ON) | **not a kill switch in master** — master's one sweep env knob is an operator *root override*, `OrphanSweepRootEnv = "GOPI_TEST_ORPHAN_SWEEP_ROOT"` (`orphansweep.go:77`, read at `:343`). The OFF knob is not re-landed: master's sweep is armed/scoped explicitly by `testutil.ArmAndSweepForSuite` (`cmd/agent/testmain_test.go:38`), so there is no always-on backstop to switch off |
| the "kill is identity-checked twice, survived ⇒ REPORTED" rule | `SweptProcess` rows reported to stderr |
| — | **`internal/daemonlifetime` — the kernel death-pipe binding** (the mechanism the archived file said it could not build) |
| — | `testutil.ArmAndSweepForSuite` — BOTH halves wired by ONE call so a suite cannot end up half-protected |
| — | `cmd/agent/daemon_test_leak_e2e_test.go` — e2e asserting zero orphan daemons after a SIGKILLed spawner |

The archived file's **"Not Done (honest)"** said: *"The sweep is a backstop, not a lifetime
mechanism… The death-pipe binding is the sibling task's work (`~/workspace/gopi-test-leak`,
session `01a0a07e`) — declared overlap, left alone deliberately."* The sibling's work
landed on master. So the only genuinely unfinished part of the archive was completed
elsewhere, deliberately, by its own author's plan.

### 2.1b The one symbol with NO replacement on master — `TripleLinePrefix` / `EnvTriplePrefix`

Not every archived symbol has a master counterpart, and the honest accounting says so.
`internal/procname/identity.go` carries a **launcher→worker line protocol** that master
does not have at all:

```go
EnvTriplePrefix  = "GOPI_WORKER_TRIPLE_PREFIX"   // env var carrying the prefix
const TripleLinePrefix = "worker-triple: "       // heading each published worker liveness triple
```

Verified: `git grep -n "TripleLinePrefix\|EnvTriplePrefix\|GOPI_WORKER_TRIPLE_PREFIX" master`
→ **zero hits** (production and test). In the tag it is consumed by
`internal/testutil/procname_gate_test.go` — that file's `TestWorkerUpScriptNamesItsValues`
(one of three tests, `master` has all three at `internal/testutil/procname_gate_test.go:128/202/255`)
compares the **script-side env defaults** in `scripts/gopi-worker-up.sh` against these Go
constants. So the archived change would have taught `gopi-worker-up.sh` to publish each live
worker's liveness triple on a prefixed line.

Two consequences, both recorded rather than smoothed over:
1. **The briefing's "37 absent symbols" is an under-count of one file's true delta.** The 37
   come from a *symbol probe*; `identity.go` is 106 lines and its own const block also carries
   `TripleLinePrefix`, `EnvTriplePrefix`, `EnvProcStatName`/`EnvProcStartTimeIndex`,
   `ProcStatStartTimeField`… — a probe names what it was asked for, so a file can be "absent"
   while its symbol list is incomplete. The recovery was decided on the **file-level** reading
   (`git show` of all four files), which is the sounder instrument, and this devdoc states the
   probe's limit rather than trusting it.
2. **That one capability is genuinely not on master, and it is NOT recovered here** — master's
   live-worker count instead reads argv shape straight from procfs
   (`internal/procname/procname.go`, `internal/testutil/procident.go`,
   `internal/testutil/procname_gate_test.go` — master's own three gates on
   `scripts/gopi-worker-up.sh`). No consumer on master reads a triple prefix, so
   re-landing the constant alone would be a **dead constant** — the exact defect Rule 21's
   gate exists to catch. If the prefixed-line protocol is wanted, it is a feature (script +
   Go constant + gate, together), not a recovery; not filed, because no bug depends on it and
   the briefing's scope was the four files, not this protocol.

## 3. Verdict

**Nothing is re-landed. Nothing is lost.** The `RECOVER` flag on this tag is a false
positive for the same structural reason as the earlier one in memory (#3768/#3773): the
sweep compared master at sweep time against a **pre-conflict snapshot** and could not see
that master had already answered the question — twice, and in the cases that matter,
*better* (kernel binding over a sweep; spawn-side `Resolve` over post-hoc `chdir`;
argv-vector identity with an explicit `Unknown` verdict over the string triple).

Per Rule 23 the archived branch was never checked out and the archive tag was never
deleted; the branch `feat/cwd-lifetime-recovery` carries this disproof.

## 4. Verification

| check | command | result |
|---|---|---|
| branch | `git rev-parse --abbrev-ref HEAD` | `feat/cwd-lifetime-recovery` off `9ce8e159` |
| blob absence (the premise) | `git rev-parse master:<f>` for all 4 files | `fatal: path … does not exist` = absent ✔ |
| symbol absence | `git grep -nw <sym> master -- '*.go'` | absent for 11/12 probed. The 12th, `Triple`, is **not** a live identifier anywhere in master's Go tree: `git grep -nw Triple master -- '*.go'` → **0 hits** (`type Triple` → not defined). What matched a substring probe is the case-insensitive string `triple` in 3 files — master's own `internal/testutil/procname_triple_redproof_test.go` (12×, the pid-reuse red proof, which uses `procname.Liveness`), plus `internal/tools/merger_takeover_progress.go` (17×) and its test (6×), which describe the identity triple in prose/comments. So the archived `Triple` type has no master counterpart *and* no master user ✔ |
| build | `CGO_ENABLED=0 go build ./...` | exit 0 |
| vet | `go vet ./...` | exit 0 |
| tests | `go test ./internal/tools/ ./internal/procname/ ./cmd/agent/` | see `tasks/01a0ab11-recovery-report.md` |
| worktree clean | `git status --porcelain` | see report (Rule 30) |

## 5. What a future agent should do with this tag

Leave `archive/fix/cwd-and-lifetime` in place. It is a **pre-conflict snapshot** of an
epic master has since answered; it is not a backlog item. If a future sweep flags it again,
this devdoc is the standing disproof — and the sweep's own lesson is that a
`RECOVER` verdict derived from a diff against a moved master must be re-measured against
the *current* master before it is dispatched.
