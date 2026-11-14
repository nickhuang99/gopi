<!-- doc-class: historical -->
# Nested gopi overwrote the parent session's task report — env-inheritance root cause and fix
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: **nested-report-path-clobber** (2026-09-15). Branch:
`feat/nested-report-path-clobber`.

## The defect (measured)

`GOPI_TASK_REPORT_PATH` (`taskreport.PathEnv`) is a **per-session delivery
contract**: the `gopi task run` wrapper resolves the report destination of the
run it owns and exports it on the child before spawn. The variable is correct in
that child — and a **lie in every process below it**, because the environment
travels down the whole tree:

```
task run wrapper ──exports GOPI_TASK_REPORT_PATH=<parent's report>──▶ executor child
                                                                          │ bash tool
                                                                          ▼
                                          a nested gopi (self-probe, `go test`,
                                          worker child, merger child) inherits it
                                                                          │
                                        its system prompt says "with $GOPI_TASK_REPORT_PATH
                                        set, write the report there" ─────┘
                                                                          ▼
                                        writes ITS OWN report ▶ the PARENT's task-report.md
```

Observed: session `01a0a4ee`'s `task-report.md` was replaced by a child probe's
`ls` report. The child did nothing wrong — nothing in the environment said whose
path it was.

## The fix: three cooperating faces

The variable carries its **owner** now, and every boundary enforces it.

| # | Face | Where | What it does |
|---|------|-------|--------------|
| ① | owner marker | `cmd/agent/cli_tasks.go` (the `task run` spawn seam) | exports `taskreport.OwnEnv=<task id>` **beside** `PathEnv` — one writer (Rule 2) |
| ② | spawn strip | `cmd/agent/report_contract_child.go` (`reportContractStripAndLog`) — wired into every gopi spawn: `task run` (`taskChildExecEnv`), daemon spawn, merger resolve, merger verify, `task verify` (internal/tools) | a spawned gopi never inherits a report contract |
| ③ | read guard | `internal/agent/journalreport.go` (`SanitizeReportContract`), called from `cmd/agent/main.go`'s `printLogPaths` — the moment the session's own directory becomes true | a process that does not OWN the destination drops the pair **and says so** (Rule 15) |

Plus the conditional half at the shell seam: `internal/tools/childenv.go`
(`isForeignReportContract`, reached through the existing `stripWrapperIdentity`
used by the bash tool and `job start`) keeps the pair only when the owner marker
names **this** process, and drops it otherwise — including the `task run`
wrapper's own shell, which is what makes the fourth-generation gap unreachable
(`worker → task run W → bash → gopi`).

### Why the shell strip is CONDITIONAL (the non-obvious constraint)

The executor writes its narrative **through its bash tool**:
`cat > "$GOPI_TASK_REPORT_PATH"` — that is the documented transport, exercised by
`cmd/agent/cli_tasks_report_seam_test.go`. An unconditional strip at the bash
boundary would destroy every report that is ever written. Ownership is what
distinguishes the executor (own id == owner ⇒ kept) from every other shell
(foreign ⇒ dropped).

`task run` itself uses the **unconditional** strip
(`tools.StripInheritedReportContract`): that wrapper is about to export the
CHILD's pair, and an inherited path must not survive as a destination any
descendant could read in the window before the child's own export lands.

### Ownership rule (one predicate, one place)

`taskreport.OwnedBy(reportPath, ownerMarker, sessionDir)` + its process-level
wrapper `taskreport.ProcessOwnsContract(sessionDir)` (both in the leaf package,
Rule 14 — the startup guard and the shell sanitizer share them):

1. **the destination's DIRECTORY must be this session's directory** — the
   general rule, and the one that decides every real case;
2. **an owner marker naming a different session refuses the contract** whatever
   the directory says (the marker exists to catch a shared-directory carry-in).

Both paths are resolved before comparison (symlinks, relative forms) — an
equivalent spelling of the same directory is the same directory.

**Why the rule is not the id (the first design was wrong and measurably so).**
The initial rule was "owner marker == own task id" (`tools.TaskRunIDEnv`). It
refused the **harness hand-off**: the harness injects `GOPI_TASK_REPORT_PATH`
with **no owner marker** and a destination in the **session uuid's** directory
(measured on this task's own session: the path pointed at
`/home/user/.gopi/sessions/<session-uuid>/task-report.md` while `GOPI_TASK_RUN_ID`
carried the *task* id). A rule keyed on a matching id therefore refused the very
contract it was written to protect — it would have broken every harness-driven
session's report. The directory is the authority, because a session's report
lives INSIDE the session's own directory by construction (both exporters:
`task run` → `sessions/<task id>/`, harness → `sessions/<session uuid>/`).

`sessions/<name>` also holds the two OTHER session dirs a gopi process
legitimately has in play — the wrapper id it was handed
(`tools.TaskWrapperEnv` → its own session dir) and the session dir recorded for
signal provenance — which is why the predicate takes the directory explicitly
rather than reading one env var itself.

## Deliberate non-goals

- **Not stripped from the agent process itself.** The executor legitimately holds
  the pair; the guard only removes it when it is not the owner (i.e. in a nested
  gopi).
- **Not a whitelist.** Both strips are subtractions from the launch
  environment; the next variable someone adds is not silently dropped.
- **Not a fix to the third-party launcher.** A launcher outside this tree still
  hands the environment over; face ③ is what covers it, and it covers it by
  refusal + a printed reason rather than by trusting the launcher.

## Tests

| Test | Package | What it proves |
|------|---------|----------------|
| `TestNestedGopiCannotOverwriteTheParentsReport` | `cmd/agent` | **the repro**: a real gopi (built from this tree) launched with an inherited foreign contract does not touch the parent's report (sha256 before/after) and prints the refusal. **RED on master** — measured: sha256 `c1f26c59…` → `3deb78b8…` |
| `TestNestedGopiKeepsItsOwnReportContract` | `cmd/agent` | the counterweight: the process that OWNS the contract still writes its report |
| `TestSpawnedShellDoesNotInheritAForeignReportContract` | `internal/tools` | the shell seam drops a foreign/unowned pair, surgically; **RED on master** (it is a plain `os.Environ()` copy there) |
| `TestSpawnedShellKeepsThisRunsOwnReportContract` | `internal/tools` | the executor's own contract survives the same seam |
| `TestSpawnedShellDropsAnUnownedReportContract` | `internal/tools` | an unowned path is dropped **and reported** |
| `TestStripInheritedReportContractIsUnconditional` | `internal/tools` | the wrapper's own strip removes the pair whatever it says |
| `TestSanitizeReportContract` (8 cases) | `internal/agent` | the guard's decision table, including the **harness hand-off** (marker-less path inside the session's own dir ⇒ KEPT) and a foreign marker ⇒ REFUSED |
| `TestReportContractOwnershipResolvesEquivalentPaths` | `internal/agent` | a symlinked spelling of the session's own directory still counts as ownership |
| `TestSpawnedShellKeepsAMarkerlessOwnContract` | `internal/tools` | the same harness hand-off survives the SHELL seam |

Red-proof method (reusable): create a detached worktree on master, copy
`cmd/agent/report_contract_nested_test.go` in, substitute the not-yet-existing
constant (`taskreport.OwnEnv` → its literal) in that throwaway copy only, and run
it. The test must fail on the sha comparison — and it does.

## Pre-existing master breakage found on the way

`cmd/agent`'s **test binary does not compile on master**: `funcBody` is declared
twice in package `main` (`merger_resume_arm_wiring_test.go:104` from 362278e2 vs
`cli_tasks_rebuild_pass_test.go:204` from db1fcef6) — *every* test in that
package was unrunnable, including the report-seam acceptance tests. Repaired
minimally in this branch (the go/ast helper renamed to `parseFuncBody`,
semantics unchanged) and dispatched as its own task
(`01a0a73b-f209-73a9-9c52-7f602d5a403a`, tag `bug`) so master is fixed by
whoever lands first. **This is required for the red proof to be runnable at
all.**

`internal/stylegate` is red on master and on this branch with the **same 209
hits** (measured both sides) — verified as a baseline, not chased.

## Files

- `internal/taskreport/own.go` (new) — `OwnEnv`, `OwnerMatches`
- `internal/tools/childenv.go` — `isForeignReportContract`, `envValue`,
  `StripInheritedReportContract`
- `cmd/agent/report_contract_child.go` (new) — the shared spawn strip + its record
- `cmd/agent/task_child_env.go` — `taskChildExecEnv` subtracts the pair
- `cmd/agent/cli_tasks.go` — exports `OwnEnv` beside `PathEnv`
- `cmd/agent/main.go` — the read guard in the shared pre-dispatch block
- `internal/agent/journalreport.go` (new) — `SanitizeInheritedReportContract`
- `cmd/agent/cli_daemon_spawn.go`, `cmd/agent/merge_resolve_child.go`,
  `cmd/agent/merger_verify_child.go`, `internal/tools/tasks.go` — spawn sites
- tests: `cmd/agent/report_contract_nested_test.go`,
  `internal/tools/report_contract_childenv_test.go`,
  `internal/agent/journalreport_test.go`
