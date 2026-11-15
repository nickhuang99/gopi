<!-- doc-class: normative -->
# Task Report: signal provenance → session-dir attribution

Epic: **signalrec-session-attribution** · branch `fix/signalrec-session-attribution`
· worktree `~/workspace/gopi-signalrec-attribution` (base `124a6860`)

## Completed

**1. The write destination is the owning process's own session dir — never a public file.**

`internal/signalrec` used to append every record to ONE host-global file
(`<gopi-dir>/logs/signal-provenance.log`) — the exact artifact this epic removes, and
doubly wrong because the file exists to explain *why a run died* while being
un-walkable from the run. Every record now lands in the session dir of the process that
writes it, beside the artifacts it explains (`session.jsonl`, `*.raw.log`,
`task-report.md`):

| scenario | destination | layout |
|---|---|---|
| task-run child / merge-verify child / worker round | `<gopi>/sessions/<ledger task id>/signal-provenance.jsonl` | `task-run` |
| bare prompt / chat / resume | `<gopi>/sessions/<session id>/signal-provenance.jsonl` (handed in-process) | `session` |
| child handed a dir by its launcher | the handed dir (validated, never trusted blindly) | `task-run` |
| resident role, no task, no session log (merger daemon) | `<gopi>/sessions/process-<pid>/signal-provenance.jsonl` | `process` |

The file name moved too: `signal-provenance.log` → **`signal-provenance.jsonl`**
(`signalrec.FileName`), because the contract is one JSON object per line and the `.log`
extension is what a reader is tempted to treat as prose.

**2. Resolution order (`sessionDest`), most specific first.**

1. the in-process handoff `signalrec.SetSessionDir(dir, taskID)` — `cmd/agent`'s
   `printLogPaths` is the single writer, the one place this run's session dir becomes
   true, wired right beside the existing `TaskExecutorSessionEnv` export (Rule 2);
2. the exported `GOPI_SIGNALREC_SESSION_DIR` + `GOPI_SIGNALREC_TASK_ID` — the same fact
   handed to a CHILD, which cannot derive it (see §4);
3. the ledger id the process carries (`GOPI_TASK_RUN_ID`, then `GOPI_TASK_WRAPPER_ID`)
   → `<sessions>/<id>` — but ONLY when that dir proves itself. An id whose dir cannot
   exist (the operator's shell inherits the agent's id; the profile has no such session)
   must not turn a FILABLE record into a refusal: for a RESIDENT role the resolution
   falls through to 4 and the record's `note` names the unusable id (fix 2026-09-14,
   `devdoc/signalrec-stop-provenance-quiet.md` — before it, `gopi merger --stop` printed
   the refusal line on a stop that had succeeded); for a one-shot role the id IS its
   session, so the refusal stands;
4. the process layout `<sessions>/process-<pid>`, **only** for a role that owns no task
   and no session log — decided by `forwardsDefault(label)`, the same policy table that
   governs signal forwarding, so there is ONE classifier, not two;
5. otherwise a **refusal**: error returned + one stderr line
   (`[gopi] signal provenance NOT FILED (no public-dir fallback): …` + the record's own
   JSON line, so the evidence survives in a captured transcript).

**A destination must PROVE itself** (`isSessionDir`): a real directory carrying
`metadata.json` or a `*.jsonl`. A directory that merely matches an id is rejected — a
record filed under a typo'd id reads as evidence, which is worse than a refused record.
A handed-over dir that fails the proof is an ERROR, not a reason to fall through to
another resolution (the caller believed it knew better; Rule 15).

**3. No public-dir fallback, structurally enforced.**
`signalrec.LogFileName` / `LogDirName` survive only as (a) the migration's input name and
(b) the refusal message's hint, joined in exactly one place (`legacySinkHint`).
`TestSignalRecNoProductionPathJoinsTheLegacySink` fails if the join is spelled twice.

**4. The one resolution a child cannot do itself, and its fix.**
`gopi task run` MINTS the child's run uuid at claim time and keys the ledger row by it —
so the child's own argv names the ledger id while the session-dir key is the uuid. The
launcher knows both, so `cmd/agent/cli_tasks.go` hands the child
`GOPI_SIGNALREC_SESSION_DIR=$(tools.TaskSessionDir(target))` + `GOPI_SIGNALREC_TASK_ID=target`
**in the child's environment before spawn** (the same single-writer shape as
`taskreport.PathEnv` and the wrapper identity above it). The worker round does the same
in-process at claim (`signalrec.SetSessionDir`) and CLEARS it at close-out, so a later
round never files under an earlier round's task.

**5. Report, never silence (Rule 15).** The victim path (`Install`'s handler) still never
blocks a shutdown, but a failed write is now LOUD (`reportUnfiled`, once per process).
`Intent` returns the error it always returned.

**6. The handoff vars are stripped from spawned shells.**
Both names describe THIS process's own session dir, so a descendant shell that
re-launches gopi would file under its parent's session — over-attribution in exactly the
file post-mortems are read from. They joined `wrapperIdentityEnvVars` in
`internal/tools/childenv.go` (referenced from `internal/signalrec`, not mirrored — no
import cycle).

**7. Migration of the existing public file — `scripts/rehome-signal-provenance.sh`.**
Per-LINE classification from the record's OWN fields, in priority order
`session_dir` → `task_run` → `session`, each candidate required to PROVE itself a session
dir. Byte-for-byte copy (timestamps preserved by construction), append-only,
dry-run-by-default, resumable, and unattributable lines are printed in full and keep the
source in place unless `--discard-unattributable` is passed. A self-contained rig,
`scripts/verify-rehome.sh`, exercises all three priorities + the unattributable path.

**8. The rest of `<gopi>/logs/` was audited — see §C.** One file was mis-attributed
(`signal-provenance.log`) and is now re-homed; the other six are process-scoped records
(or another epic's ruling) and stay.

## Migration record (live profile, measured)

| | |
|---|---|
| source | `~/.gopi/logs/signal-provenance.log`, 36 lines, sha256 `73a9046e54044f993f855cb0d8726331afec2b33703ea5a7ac06044afca6dda9` (**backed up** to `/tmp/signal-provenance.log.bak-pre-migration`) |
| before | 36 lines, 1 file |
| after | 36 lines filed, **0 unattributable**, 3 destination dirs, 0 source lines lost |
| destinations | `sessions/01a0986d-d476-7f1d-8d99-d154ab2bc7b4` ← 20 · `sessions/01a09805-6dae-7892-b19c-69c8ce9c5008` ← 14 · `sessions/01a0981f-d909-7d7a-921f-201e40ff8f7d` ← 2 |
| integrity | multiset compare src↔dst: `extra(dst-src)=0 missing(src-dst)=0 byte-identical=True` (no destination had pre-existing records, so no arithmetic was needed outside the compare) |
| source file | removed after every line reached a destination |
| **residue (re-measured 2026-09-13, after the fix was already in the branch)** | the public file REAPPEARED carrying 2 lines, both `session=2453fa6b-0ac1-4d1b-b2c8-3beaa5e589c2` / `task_run=01a0987d-3187-778a-bbe4-cfe5c7504d3c`, ts 12:49:26. Filed with `--apply`: **2 filed, 0 unattributed**, 1 destination dir, source removed. Root cause is a stale test-coverage binary, not a live write path — see **§D**. |
| post-fix integrity (all 5 session dirs) | every one of the 36 backed-up legacy lines byte-identical inside a session dir → **0 missing**; pool 46 lines / 5 files |

## §C — `<gopi>/logs/` audit: global-exception vs mis-attributed

**Method (re-runnable, not prose):** for every file in `<gopi>/logs/` — (1)
`lsof <file>` names the live process holding it (fd 1/2 = a resident component's
stdio; nothing = produced by a host script); (2) grep the file for
`sessions/<uuid>` tokens and check whether those sessions ALREADY own their own
record on disk; (3) grep the repo for the file's name to find its producer
(`--include='*.go' --include='*.sh'`) and ask whether that producer has a
session identity at all. A file that survives all three as "one owner with no
session identity" is a global exception; anything else is mis-attributed.

| file | ruling | argument |
|---|---|---|
| `signal-provenance.log` | **mis-attributed → FIXED (this epic)** | session-scoped evidence in a host-global path; migrated, and the producer no longer writes there. |
| `merger.log` | **global exception (keep)** | the MERGER DAEMON's own resident log, one per host, owning no task and no session: it is the process's console, not a run's record. Its session-scoped output already goes to `<sessions>/<task>/output.log`; the provenance OF the merger process (should it ever be killed) now goes to `sessions/process-<pid>/` per §2.4, which is where the daemon's own artifacts belong. |
| `worker.log` | **global exception (keep) — measured, not deferred** | it is the resident worker DAEMON's stdio, not one run's record: `lsof ~/.gopi/logs/worker.log` shows a live `gopi` daemon holding fd `1w`/`2w` on it (`COMMAND gopi PID 2140436`), and its content interleaves MANY sessions' console transcripts (`sessions/01a0986d-…` = run `3eecb631-…` and `sessions/01a0987d-…` = run `2453fa6b-…` both appear). Each of those sessions ALREADY owns its own record inside its session dir (`<run>.jsonl` 1.2 MB + `<run>.raw.log` 97 MB / 79 MB) — so attributing the daemon's merged stdout to any one of them would be false, and no session-scoped evidence exists ONLY here. The `worker-log-session-attribution` epic (merged: `d9b3f6ad`) already moved the worker's per-task OUTPUT to `<sessions>/<task>/output.log` and kept exactly one pre-claim startup record; this entry records the same ruling with the lsof/duplication evidence its own devdoc asserts in prose. |
| `scratch-gc-watchdog.log` | **global exception (keep)** | produced by `scripts/scratch-gc-watchdog.sh`, a HOST janitor with no session, no task, and no gopi process identity: the thing it reports on (the scratch root) is host-global by definition. |
| `nightwatch.log`, `night-progress.log`, `glm-night-watch.log` | **global exception (keep)** | produced by `~/.gopi/nightwatch.sh`, `night-progress.sh`, `glm-night-watch.sh` — operator harness scripts that OUTLIVE every session they start (a night run spans many sessions and reports on all of them). Attributing the harness's own log to any one of those sessions would be false. |

**No duplicate attribution logic vs `--worker-log-root`.** `internal/tools/worker_output.go`
owns the worker's *output* log (`TaskLogDir`/`TaskLogPath`, root from
`WorkerLogRootEnv`); `internal/signalrec` owns *provenance* and reuses the same
`config.SessionsDirName` root and the same `tools.TaskSessionDir(id)` key function rather
than re-deriving a path. Neither writes the other's file; the two share the tree, not the
mechanism.

## §D — Why a public-path record reappeared at 12:49 (stale-binary finding)

The migration removed the public file; ~40 minutes later it was back, holding 2 records whose
fields are `session=2453fa6b-0ac1-4d1b-b2c8-3beaa5e589c2`,
`task_run=01a0987d-3187-778a-bbe4-cfe5c7504d3c`, `parent_name="agent.test"`, and an `argv`
pointing at
`/dev/shm/gopi-test-scratch/suite-cmd-agent-run-4083213605/gopi-suite-coverbin-4170504249/gopi`
— a `gopi` built for `TestVoiceHTTPServerEndToEnd`.

Evidence that blob is the OLD writer, not this tree:

| probe | on the coverbin | on a build of THIS branch |
|---|---|---|
| `go version -m` | `build -cover=true` | — |
| `strings \| grep signal-provenance.log` | 1 | 1 (migration input constant only) |
| `strings \| grep signal-provenance.jsonl` | **0** | 1 |
| `strings \| grep GOPI_SIGNALREC_SESSION_DIR` | **0** | 2 |
| `strings \| grep SetSessionDir` | **0** | present |

So the writer of those 2 lines predates `9e482ea8` and joins the retired public path by
construction. **The resurrection is a STALE BINARY, not a production write path** — which is
exactly the ambiguity this epic removes (a public-path record produced by no live process).
No `//go:build`-tagged legacy blob exists (`grep -rn 'go:build' internal/signalrec/` → empty;
the only test-side mention of the legacy path is a comment), so the stale artifact is
specifically the `gopi-suite-coverbin-*` exec archived in a test scratch dir. Rebuilding it is
what the next `go test ./cmd/agent/` does; it belongs to the test-infrastructure owner, not to
`internal/signalrec`.

Residue disposition: migrated (2/2 filed into `sessions/01a0987d-3187-778a-bbe4-cfe5c7504d3c`,
0 unattributable, source removed) and the whole post-fix pool re-checked — **all 36 backed-up
lines present byte-identical, 0 missing, 5 session dirs**.

### §D.1 — CORRECTION (post-merge, measured): the DOMINANT stale binary is the installed `bin/gopi`

The 12:49 residue was the first sighting of stale-writer output, not the only one, and the
coverbin was not the main culprit. Measured AFTER the merge:

| fact | value |
|---|---|
| fix `9e482ea8` commit time | 12:44:04 |
| merge `9f79d19f` landed | ~13:03 |
| `bin/gopi` mtime at that moment | **12:51:03** — built from PRE-fix master |
| `strings bin/gopi \| grep -c signal-provenance.jsonl` | **0** |
| `strings bin/gopi \| grep -c GOPI_SIGNALREC_SESSION_DIR` | **0** |
| `strings bin/gopi \| grep -c signal-provenance.log` | 1 (the legacy writer) |
| the same three probes on a fresh build of master `9f79d19f` | 1 / 2 / 1 |

`/usr/local/bin/gopi` is a symlink to `bin/gopi`, so the STALE binary was the one the operator,
the merger and every `task run` child were executing — the Rule 12 close loop (merge → cleanup →
**REBUILD**) had not run, and the public sink therefore kept being re-created by a live process,
not merely by an archived coverage build.

Closing the loop fixed it, and the closure is what makes the fix real:

- rebuilt `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0, mtime 13:07:39, probes 1/2/5;
- the public sink reappeared once more before the rebuild (13:07, 22 lines) → migrated 22/22,
  0 unattributable, source deleted by the script;
- one last 4-line residue → 4/4 filed, source deleted;
- then the **live closure proof**: a real `gopi task list` launched with
  `GOPI_SIGNALREC_SESSION_DIR=<a real session dir>` and SIGTERM'd filed its record IN the
  handed-off dir (`role:"task list"`, the signalled pid, `session_dir` = the handoff) and created
  **no** public path. The same probe against the stale 12:51 binary wrote NOTHING anywhere (it has
  no such env hook) — the differential that identifies a stale writer.

**Lesson for the epic's own follow-ups:** a merge that lands a routing change is not done until
the declared build has run; a source-reader grep proving "no reader of the old path" says nothing
about a stale BINARY that still holds the old WRITER. Verify the installed artifact with
`strings`, not the source.

## Verification

- Build: `go build ./...` → OK
- Tests: `go test ./internal/signalrec/` → **ok** (11 tests, incl. the exhaustive
  no-legacy-path gate and the child-process end-to-end victim test);
  `go test ./internal/standards/` → **ok**; `./internal/stylegate/` → **ok**;
  `./internal/testutil/` → **ok**; `./cmd/agent/` → **ok**;
  `go test ./internal/tools/ ./internal/agent/` → OK
- Migration: `bash scripts/verify-rehome.sh` → 2 filed / 2 unattributed / 1 dir NOT
  invented / source kept / dry-run changed nothing; live run table above.

### The acceptance paths are asserted, not described

| test | asserts |
|---|---|
| `TestSignalRecInstallRecordsTheDeathInTheSessionDir` | the record FILE exists at `<sessions>/<id>/signal-provenance.jsonl` and carries `session_dir` + `layout`; the legacy dir is empty |
| `TestSignalRecTaskRunChildWritesIntoTheLedgerTaskSessionDir` | `GOPI_TASK_RUN_ID`-carrying process → `sessions/<ledger id>/…`, `task_run` preserved |
| `TestSignalRecExportedHandoffWinsAndIsValidated` | the env handoff is used; a handoff that is NOT a session dir is refused and writes nothing |
| `TestSignalRecBareSessionWithNoHandoffIsRefused` | the bare/auto-prompt shape writes NOWHERE, invents no bucket, and the error names the retired sink |
| `TestSignalRecResidentRoleGetsAProcessScopedSessionDir` | the resident role gets `process-<pid>` + a `Note` |
| `TestSignalRecNoProductionPathJoinsTheLegacySink` | the retired path is spelled once, only to report it |
| `cmd/agent` `TestStopChildGracefully*` | the killer's intent records land in a session dir; the public dir is empty |

## Not Done (honest list)

- **The installed binary is not rebuilt on the live profile.** Per the merge loop's
  contract the rebuild happens after the merge (`repos.<path>.build`), not from a
  worktree — the worktree's build is verification only.
- **`glm-night-watch.sh` / `nightwatch.sh` still write their own harness logs into
  `<gopi>/logs/`.** Ruled a global exception above (a night harness outlives and spans
  many sessions), so no change — but if the operator wants the harness logs elsewhere,
  that is a separate epic about the harness, not about provenance attribution.
- **The stale `gopi-suite-coverbin-*` coverage binary was not rebuilt.** It still carries the legacy
  writer (§D) and can keep depositing public-path records into `~/.gopi/logs/` whenever the suite
  that owns it re-runs that archived exec. Detected, proved, and documented here; the fix
  (rebuild/purge in the cmd/agent coverage-binary helper, `cmd/agent/gopibin_test.go`) is
  test-infrastructure work, out of `internal/signalrec`'s scope. The DOMINANT stale writer was the
  installed `bin/gopi` itself — see §D.1, now closed by rebuilding.
- **`log` → `jsonl` renames nothing on disk.** Records that a PREVIOUS run already filed
  under the new scheme do not exist yet (the scheme is new); only the migrated legacy
  lines exist, and the migration tool reads them by their fields, not by extension.
- **`session` (the run uuid) is still recorded but is no longer the primary key** of the
  destination for task-run children — kept because it is the field a human greps by.
