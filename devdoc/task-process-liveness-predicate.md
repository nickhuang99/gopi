<!-- doc-class: historical -->
# Task Report: one exported liveness predicate over the three-factor task-process identity

Epic: **task-process-identity-convergence** (branch
`feat/task-process-liveness-predicate`, worktree
`~/workspace/gopi-taskproc-live`).

## Completed

### 1. The exported predicate

`TaskProcessLiveness(taskID string) (live bool, verdict string, measured bool,
reason string, err error)` — `internal/tools/taskproc.go`.

- A **free-standing exported function**, not a `*Store` method: `internal/tools`
  has no `Store` type (the epic's "or an equivalent free-standing exported
  form" — substituting one would have invented a type the package does not
  have). Its neighbours in the same file (`TaskProcessStatusFor`,
  `TaskProcessStatusJSON`) are free functions over the same ledger.
- It loads the row once (`taskByID`), judges its **recorded** identity through
  the existing single ladder `taskIdentityVerdict`, and adds **no /proc parsing
  of its own** — it reads procfs only through `procStartTicks` / `procCmdline`
  / `pidAlive`, exactly the readers the ladder already used (Rule 14: no second
  liveness implementation).
- Answer set: `running` (live=true) · `dead` (measured: pid gone) · `recycled`
  (measured: pid alive, other incarnation) · `unknown` (evidence unsettled) ·
  `not-measured` (NEW: no identity on the row — nothing was measured).
- **`measured` is returned separately from `live`** (Rule 15b): a
  `measured=false` answer means "no evidence exists"; `measured=true &&
  live=false` means "I looked and the recorded process is not there". The two
  are different values, so a caller cannot collapse them by reading the bool
  alone.

`TaskProcessStateNotMeasured = "not-measured"` is a new verdict constant. It is
deliberately **not** added to `taskProcessStateSet` (the vocabulary
`gopi task status` prints): that set belongs to rows whose status names a
holder, where a claim stamp always exists to judge; not-measured belongs to the
identity predicate, where the absence of a record **is** the question.

`taskIdentityVerdict` now returns `taskProcVerdict{State, Reason}` instead of a
bare string, so the evidence line travels with the ruling instead of being
re-derived at each call site. Its ladder and vocabulary are unchanged.

### 2. Call sites routed through it

| # | site | before | after |
|---|---|---|---|
| 1 | `internal/tools/taskproc.go` `taskShowIdentityLine` (the `task show` `process:` line) | re-derived the ladder INLINE (`pidAlive` + `taskPidIsRunner` + start-tick compare) | `taskRowProcessLiveness(t).State` → `taskShowLiveWord` |
| 2 | `internal/tools/taskproc.go` `taskHolderVerdictFor` | inline `taskProcIdentity{...}` literal | `taskRecordedProcIdentity(t)` (ONE extraction) |
| 3 | `internal/tools/taskrunguard.go:161` `taskRunHolderVerdict` | inline `taskProcIdentity{...}` literal inside the call | `taskRowProcessLiveness(t).State` |
| 4 | `internal/tools/tasks.go:1610` `taskInProgressStateNow` | inline `taskProcIdentity{...}` + `== TaskProcessStateRunning` | `taskRowProcessLiveness(t).State == TaskProcessStateRunning` |

Plus one fix inside the routed site itself: the old display ladder consulted
the coarse comm-name guard **first**, so a row whose (pid, starttime) matched a
live process still rendered `alive-but-recycled` when that process's comm was
not `gopi*` — a live, verified incarnation displayed as "the runner is dead".
The routed line inherits the ladder's documented precedence. This is the
red-then-green proof below.

### 3. Sites deliberately left alone (with reasons)

| site | why NOT routed |
|---|---|
| `finishCleanupProcessHolderLive` (`task_finish_cleanup.go:1404`) and its consumers (`branch_attribution.go:526,689`, `task_reclaim.go:768`, `worktree_sweep.go:985`, `task_holder_resume.go:148`, `task_finish_cleanup.go:1367,1485`) | **Veto polarity, not a verdict.** Its contract is "answer false ONLY when the host can positively prove the runner is gone"; it never has a not-measured state, because a refusal to free a directory needs no evidence line. Routing it through a 5-valued predicate would force a translation that INVENTS a not-measured answer for a caller whose semantics have none — and it feeds the DESTRUCTIVE reclaim arm, where a wrong value costs a worktree. |
| the same file's `pidIdentityStartTicks` / `finishCleanupProcParent` readers | They read `/proc/<pid>/stat` in the probe order that fits the veto; changing the reader changes the arm. |
| `pidAlive` (`taskzombie.go:120`) | It IS the shared primitive (signal 0 + zombie rule) the ladder is built on — the floor, not a duplicate. |
| `taskLiveHolderProbe` (`tasks.go:1649`) | The claim-stamp probe (pid + comm name) used only when the row has NO identity; it is the ladder's documented legacy rung, exposed as a test seam. |
| `taskIdentityVerdict` itself | The single ladder — the predicate is built ON it, never in parallel. |
| `merger_takeover_progress.go:665` `progressTripleMatches`, `merger_takeover_live.go` | A different record shape (`TakeoverProgressRecord`, JSON on disk, not a ledger row) whose identity may be PARTIALLY known per side (`StartTicksKnown`, `LogMeasured`), with its own vocabulary (`attached`/`foreign`). Routing it would require flattening "one side could not measure this factor" into a row verdict — the merger would lose the distinction its own notes print. It already documents that it follows the same ladder. |
| `scratch.LiveHolders` / `HeldByProcesses` / `branchAttributionCheckout` | The worktree-occupancy implementation the epic names as the single one; the destructive reclaim arms consult it. Untouched. |

### 4. Tests (`internal/tools/taskproc_liveness_test.go`)

All four arms the epic names, plus the display route:

- `TestTaskProcessLivenessLiveRow` — live row → `live=true`, `running`, `measured=true`.
- `TestTaskProcessLivenessExitedProcessIsGone` — real child spawned, identity
  captured, killed+reaped → `false`, `dead`, `measured=true`.
- `TestTaskProcessLivenessRecycledPidIsNotTheRunner` — forged start tick on a
  live pid → `false`, `recycled`, `measured=true`.
- `TestTaskProcessLivenessWithoutIdentityIsNotMeasured` — no recorded identity →
  `not-measured`, `measured=false`, asserted ≠ `dead`; an absent row is an error.
- `TestTaskProcessLivenessDistinguishesNotMeasuredFromGone` — the two answers
  side by side on one host (one row measured gone, one row with nothing to
  measure): different verdicts, different measured flags.
- `TestTaskProcessLivenessRoutesTheDisplayLine` — the routed `task show` line;
  also the red-then-green instrument.

## Not Done (honest)

- **No caller outside `internal/tools` uses `TaskProcessLiveness` yet.** The CLI
  surfaces (`cmd/agent`) reach the same ladder through `TaskProcessStatusFor` /
  `TaskRunGuardFor` / `TaskShow`, which were left on their existing entry points
  — those entry points answer more than liveness (claim stamp, freshness,
  foreign host) and rewriting them to thread a 4-value predicate through their
  gates is a behaviour change with no defect behind it today.
- The predicate's own DB read is not wired into the `task list` renderer (that
  path holds a row image and uses the row-image half `taskRowProcessLiveness`,
  deliberately, to avoid an N+1 read).
- `not-measured` is not printed by `gopi task status`: that verb's rows always
  carry a claim stamp, so `unknown`/`no-holder` remain its absent-evidence
  words. Making the verb print `not-measured` for a stamped-but-identity-less
  row is a follow-up if the operator wants it.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0.
- Vet: `CGO_ENABLED=0 go vet ./internal/tools/` → exit 0.
- Tests: `CGO_ENABLED=0 go test -run 'TestTaskProcessLiveness|TestTaskShowRendersProcessIdentity|TestTaskStatus' ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 18.535s`; the six-arm run on the frozen commit →
  `ok github.com/nick/gopi/internal/tools 15.172s` with
  `--- PASS` on `LiveRow` (2.14s), `ExitedProcessIsGone` (1.76s),
  `RecycledPidIsNotTheRunner` (2.58s), `WithoutIdentityIsNotMeasured` (1.20s),
  `DistinguishesNotMeasuredFromGone` (4.74s), `RoutesTheDisplayLine` (1.88s).
- Package smoke, branch-scoped (Rule 27 / `internal/smokegate`):
  `make test-budget PKGS='./internal/tools'` → budget EXPIRED, not an assertion
  failure (see Evidence); re-run with `GOPI_TEST_TIMEOUT=45m` for a decisive
  verdict.
- Assertion-totality gate: FAIL here and identically at the parent commit
  (pre-existing; see Evidence).
- Standards gates: `go test ./internal/standards/` — see Evidence (RED on
  `master` too, for a pre-existing reason unrelated to this diff).
- Red-then-green: observed twice, quoted in Evidence; the raw transcripts are
  filed at
  `/home/user/.gopi/sessions/01a0b1b1-2c57-73f7-a031-abcc58776a2d/red-then-green-display-route.txt`
  and `…/red-then-green-display-route-rerun.txt`.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- `CGO_ENABLED=0 go vet ./internal/tools/` → exit 0, no output.
- Six-arm run (`-run TestTaskProcessLiveness -v`) → six `--- PASS` lines,
  `ok github.com/nick/gopi/internal/tools 15.172s`.
- `CGO_ENABLED=0 timeout 600 go test -run 'TestTaskProcessLiveness|TestTaskShowRendersProcessIdentity|TestTaskStatus' ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 18.535s`.
- `make test-budget PKGS='./internal/tools'` → see the job output filed with
  this run (reported here verbatim by whoever closes the report).
- `go test ./internal/standards/` → `FAIL github.com/nick/gopi/internal/standards`.
  NOT caused by this diff: the same command on the parent commit
  (`a9a96cd7`, worktree `~/workspace/gopi-procid-base`) fails identically
  (`Rule 13 (no hardcoded worktree root literal): hits=1 baselined=0 escaped=1
  NEW=0`, escape `internal/shot/x11.go:137`, plus the same `Rule 17 NEW
  violation at cmd/agent/merge_lock_test.go:200`). On this branch every gate
  line for `internal/tools` carries `NEW=0`; the only NEW hit is that
  pre-existing `cmd/agent` one.
- RED, observed: with the pre-route inline ladder restored at
  `taskShowIdentityLine` and the guard test present —
  `--- FAIL: TestTaskProcessLivenessRoutesTheDisplayLine (1.13s)`, and the
  failure text printed the routed line itself:
  `process: pid=2674158 start=... cmd=".../tools.test ..."  (live: alive-but-recycled)`
  i.e. a live, tick-matching incarnation reported as dead by the old ladder.
- GREEN, observed: same test after restoring the routed implementation →
  `ok github.com/nick/gopi/internal/tools 1.244s`.
- RED→GREEN re-observed independently on the frozen commit (independently
  mutated `taskShowIdentityLine` back to the pre-route inline ladder in the
  worktree, ran, restored from a backup copy, re-ran): RED
  `--- FAIL: TestTaskProcessLivenessRoutesTheDisplayLine (2.06s)` printing
  `process: pid=2964386 start=… cmd="…/tools.test …"  (live: alive-but-recycled)`,
  then GREEN `--- PASS … (3.30s)`; `git status --porcelain` empty after the
  restore. Transcript:
  `~/.gopi/sessions/01a0b1b1-2c57-73f7-a031-abcc58776a2d/red-then-green-display-route-rerun.txt`.

## Call-site inventory (file:line, at the frozen commit)

### Routed through the ONE ladder (the predicate or its row-image half)

| file:line | symbol | before → after |
|---|---|---|
| `internal/tools/taskproc.go:684` | `taskShowIdentityLine` (`task show` `process:` line) | inline ladder (`pidAlive` + `taskPidIsRunner` + tick compare) → `taskRowProcessLiveness(t).State` via `taskShowLiveWord` (`:694`) |
| `internal/tools/taskproc.go:589` | `taskHolderVerdictFor` | inline `taskProcIdentity{…}` literal ×2 → `taskRecordedProcIdentity(t)` (`:435`) |
| `internal/tools/taskrunguard.go:161` | `taskRunHolderVerdict` (run door) | inline literal → `taskRowProcessLiveness(t).State` |
| `internal/tools/tasks.go:1610` | `taskInProgressStateNow` (`task list` renderer) | inline literal + `== TaskProcessStateRunning` → `taskRowProcessLiveness(t).State == TaskProcessStateRunning` |
| `internal/tools/taskproc.go:393` | `TaskProcessLiveness` (NEW, exported) | the DB-loading face over the same half |

### Not routed — and why

| file:line | symbol | reason |
|---|---|---|
| `internal/tools/taskzombie.go:120` | `pidAlive` | the shared /proc primitive the ladder is BUILT ON (signal 0 + zombie rule) — the floor, not a duplicate probe |
| `internal/tools/taskproc.go:294` | `taskIdentityVerdict` | the single ladder itself; the predicate is built on it, never beside it |
| `internal/tools/task_finish_cleanup.go:1404` | `finishCleanupProcessHolderLive` (`bool`) | veto polarity: "false ONLY on positive proof the runner is gone"; it has no not-measured state, and it feeds the DESTRUCTIVE reclaim arms — translating it into a 5-valued verdict would invent an answer its contract lacks. Shares the shared readers anyway. |
| `internal/tools/task_finish_cleanup.go:1367`, `:1485` | `finishCleanupOwnerRowLive` (`:1352`) and `finishCleanupDeadLocalHolder` (`:1469`) | the same veto ladder's two callers (the negated twin included), one file, same readers |
| `internal/tools/branch_attribution.go:526`, `:689` | `branchAttributionCheckout` legs | occupancy attribution, gated on `row.Ident.Pid/StartTicks` — consulted by the destructive reclaim path; the epic names this family as the one not to touch |
| `internal/tools/task_reclaim.go:768` | reclaim skip guard | the veto, feeding a destructive arm |
| `internal/tools/worktree_sweep.go:985` | sweep skip guard | the veto, feeding a destructive arm |
| `internal/tools/task_holder_resume.go:148` | resume-holder probe | the veto (`ident` shape, not a task row) |
| `internal/tools/tasks.go:1646` | `taskLiveHolderProbe` | claim-stamp pid+comm probe used only when a row has NO identity — the ladder's documented legacy rung, a package var so `taskstate_test.go:32` can stub it as a test seam |
| `internal/tools/merger_takeover_progress.go:661` | `progressTripleMatches` | different record shape (`TakeoverProgressRecord` on disk) whose factors carry their own per-side known-flags and its own vocabulary (`attached`/`foreign`); flattening "one side could not measure a factor" into a row verdict would delete the distinction its notes print. It documents that it follows the same ladder. |
| `internal/scratch/holder.go:149`, `:276`; `internal/tools/branch_attribution.go:480` | `scratch.LiveHolders`, `HeldByProcesses`, `branchAttributionCheckout` | the single worktree-OCCUPANCY implementation the epic names; untouched (no second /proc liveness probe added) |

### Outside `internal/tools` — no new call sites

`cmd/agent` reaches the ladder only through `tools.TaskProcessStatusFor`
(`cmd/agent/cli_resume.go:97`), `TaskRunGuardFor` and `tools.TaskShow`, which
answer more than liveness (claim stamp, freshness, foreign host). None of them
re-derives a `/proc` comparison: `grep -rn 'procStartTicks|pidAlive|taskProcIdentity' cmd/agent/*.go`
(non-test) returns no hit. They were left on their existing entry points (see
Not Done).
