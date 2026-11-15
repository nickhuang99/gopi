<!-- doc-class: historical -->
# Task Report: auto mirror rows carry no run_cmd (two-row provenance gap)

Branch: `feat/auto-mirror-runcmd` (worktree `~/workspace/gopi-autorun-cmd`, base 67d56d3)

## Goal
Auto-submitted tasks (the `[auto]` mirror rows created by the harness start
hook) were missing the `run_cmd` field that manually-submitted wrapper rows
carry — `gopi task show <auto-row>` could not answer "how was this run".

## Root cause (5-why)
1. **Symptom**: source=auto rows show no `run_method`/`run_cmd`; manual
   wrapper rows do.
2. **DB evidence** (live ledger): 114/114 auto rows with a `wrapper:` tag have
   EMPTY run_cmd; 26 human wrapper rows all carry `run_cmd=gopi task run …`.
   The gap is total for the mirror path.
3. **Mechanism**: `runTaskRunOnce` (cmd/agent/cli_tasks.go ~L1122) stamps the
   WRAPPER row (`runMethodFor` → manual/worker) right before spawn — but the
   CHILD's auto-register hook (`maybeRegisterSessionTask`,
   internal/agent/taskregister.go) only tagged `wrapper:<id>`; it never
   stamped the mirror row's own provenance.
4. **Norm**: devdoc/run-method-provenance.md says every EXECUTING row records
   how it was launched. The child row IS an executing row — it ran the
   session. The parent-side stamp (bug057 two-row linkage) only covered the
   wrapper side of the same execution.
5. **Fix point**: the mirror row must inherit the wrapper's launch command at
   auto-registration time (the hook already knows the wrapper id from
   `GOPI_TASK_WRAPPER_ID`).

## Completed
- **internal/tools/tasks.go** — new `TaskRunProvenance(taskID) (method,
  cmdline)` helper (best-effort; missing/empty row → ("","")).
- **internal/agent/taskregister.go** — `maybeRegisterSessionTask`: when the
  wrapper env is present and the wrapper row carries a run_cmd, the mirror
  row is stamped `method=child` + the wrapper's exact launch command
  (best-effort: log-and-continue, never blocks registration).
- Schema comment updated: `run_method ∈ serve|worker|manual|dispatch|resume|
  child`.
- The `child` method value is render-only (TaskShow/TaskList `[via X]`) — no
  consumer switches on method values, no sync-wire change needed (columns
  already travel; the stamp is a plain UPDATE).

## Not Done
- No backfill of the 114 historical mirror rows (they are closed/terminal;
  provenance is stamped at run start, rewriting history would be noise).
  Future runs record correctly.
- `dispatch` wrappers: mirror stamps with the wrapper's `gopi task run <id>
  --report-to …` remote command — truthful on the node's own DB.

## Verification
- Build: `go build ./...` OK
- Tests (regression-guarded):
  - `go test ./internal/agent/ -run TestAutoTaskRegister` → ok (new
    `TestAutoTaskRegisterMirrorRunProvenance`: mirror row carries
    `run_cmd: gopi task run t-wrap-prov`; plain auto rows carry none)
  - `go test ./internal/tools/ -run TestTaskStampRun` → ok (extended with
    `TaskRunProvenance` assertions: unstamped/missing → empty, stamped →
    manual/cmdline)
  - `go test ./internal/tools/ -run 'TestSyncDeltaRunProvenanceTravels'` → ok
  - A/B: test FAILS with the mirror-stamp block disabled (true regression
    guard)
- Commit: on `feat/auto-mirror-runcmd` (worktree-shared into the main repo)
- Post-merge independent verification (t-1788526902 follow-up, 2026-09-04):
  - `go build ./...` OK on master @ 324b54b; regression tests re-run green
    (`TestAutoTaskRegisterMirrorRunProvenance`, `TestTaskStampRun`,
    `TestSyncDeltaRunProvenanceTravels`).
  - A/B re-confirmed: the mirror-provenance test FAILS on the pre-fix tree
    (67d56d3) and PASSES on the fix — true regression guard.
  - Live-ledger evidence: wrapper row t-1788526902 (manual) stamped
    `run_cmd: gopi task run t-1788526902`; its pre-fix child mirror
    t-1788528723 (spawned 21:32, old binary) carried NO run_cmd — the exact
    bug shape, caught live.

## Stale-bin closure (Rule 12 — rebuild after merge)
The fix merged into master at 21:40 (324b54b), but `bin/gopi` was NOT
rebuilt (still 21:31, pre-fix): the long-lived `task merger` daemon (started
Sep 03, pre-fix code in memory) fast-forwarded the branch but its post-merge
declared build never produced a fresh binary — the recurring stale-bin
pattern. Closure: ran the declared build
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → bin/gopi 21:47, version
`324b54b…`. New task-run children exec `os.Executable()` = `./bin/gopi` →
the fresh binary, so every auto-mirror row registered from 21:47 onward
carries the mirrored run_cmd. No daemon restart required (auto-registration
lives in spawned children, not the merger process).

## Provenance live smoke (t-1788529736, 2026-09-04 21:49) — PASS
Live end-to-end smoke: wrapper `gopi task run t-1788529736` (post-rebuild
binary) spawned an auto mirror child (session c1c8c31b). Verified in the
live ledger + TaskShow/TaskList rendering:

| row | task_id | run_method | run_cmd | source |
|---|---|---|---|---|
| 1419 | t-1788529736 (wrapper) | `manual` | `gopi task run t-1788529736` | human |
| 1420 | t-1788529742 (mirror)  | `child`  | `gopi task run t-1788529736` | auto |

- DB (`sqlite3 ~/.gopi/memory.db`): mirror row 1420 carries run_method=child
  + run_cmd copied from the wrapper — the two-row provenance gap is closed.
- `gopi task show t-1788529742`: renders `run_method: child`,
  `run_cmd: gopi task run t-1788529736`.
- `gopi task list`: wrapper shows `[via manual]`, mirror shows
  `[via child]` + `[dup-of t-1788529736]` — render markers work.
- Same-execution two-row linkage confirmed: mirror tags dup-of wrapper;
  both rows share the same session c1c8c31b.
- A/B contrast preserved: pre-fix child t-1788528723 (spawned 21:32 on the
  pre-fix binary) still shows NO run_cmd, while post-fix children inherit.
  PASS.
