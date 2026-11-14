<!-- doc-class: normative -->
# Worker log → session attribution (2026-09-12)

> **SUPERSEDED IN PART (2026-09-17, epic `child-stream-task-output`)** — see
> `devdoc/child-stream-task-output.md`. The *principle* below (a task's output
> belongs in its OWN session directory, reachable from the task id) is kept in
> full. The *mechanism* is not: the parent-side re-point of `os.Stdout`/
> `os.Stderr` at CLAIM time, and with it `--worker-log-root` /
> `GOPI_WORKER_LOG_ROOT`, were deleted. The destination is now set on the
> executor CHILD at the shared spawn seam (`runTaskRunOnce`), derived from the
> row — so a manual `task run`, a worker round and a serve tick all write the
> SAME file without any launcher telling anybody a root. Read this document for
> the reasoning that produced the destination; read the newer one for the
> mechanism that now enforces it.

Epic: **worker-log-session-attribution** · branch `fix/worker-log-session-attribution`
· worktree `/tmp/logattr-wt` (based on the unmerged `fix/gopi-home-hygiene` head
`24d341e5`, merged forward with `master`)

## Completed

- **The launcher no longer names a log file.** `scripts/gopi-worker-up.sh` spawns
  `gopi worker --worker-log-root <root>` with `GOPI_WORKER_LOG_ROOT=<root>`; the
  default root is `<gopi-dir>/sessions` (the same tree the session JSONL lives in).
  It writes exactly ONE fixed record of its own — `<gopi-dir>/logs/worker-startup.log`,
  APPENDED, for the pre-claim output that belongs to no task. The per-worker,
  timestamp-named `worker-<ts>-<i>.log` is gone, and the old positional `[logdir]`
  argument is now REFUSED (exit 2) instead of silently ignored.
- **The worker attributes its own output at claim time.**
  `cmd/agent/cli_tasks_worker.go` workerRound → `workerAttachOutputLog`
  (`cmd/agent/cli_tasks_worker_output.go`) re-points `os.Stdout`/`os.Stderr` into
  `<gopi-dir>/sessions/<task-id>/output.log`; `workerDetachOutputLog` restores them
  at the end of the round.
- **The path contract** lives in `internal/tools/worker_output.go` (constants +
  `TaskLogDir`/`TaskLogPath`/`TaskLogGlob`, `WorkerLogRootEnv`, `WorkerLogRootFlag`,
  `WorkerLogFileName` — no bare literals anywhere, Rule 1/13/14; the session root is
  `config.SessionsDirName`, the repo's single source).
- **`gopi task show <id>` renders `task_output:`** — the third leg of the id→output
  walk, beside `session_log:` and `raw_log:`. The terminal writeback also stamps the
  path into the row's result digest (`worker-output-log: <path>`), so the ledger
  itself carries it.
- **Loud refusal, never a fallback (Rule 15).** An unusable session dir → the worker
  reverts the claim (`tools.RevertClaimUnattributed`, tag `worker-output-unattributed`),
  writes the reason to the LAUNCHER's file and does NOT run; the launcher exits 1 with
  `cannot create the worker output root …` and launches nothing.
- **Re-claimed rows cannot destroy evidence.** Run 1 owns `output.log`; a later run
  on the same task takes `output.<pid>.log`; one glob (`output*.log`) reaches both.
- **Tests**: `cmd/agent/cli_tasks_worker_output_test.go` (attach/detach, negative
  case, re-run), `cmd/agent/cli_tasks_worker_output_e2e_test.go` (REAL binary + mock
  LLM child: submit → `worker --once` → `task show <id>` → read the file),
  `internal/tools/worker_output_show_test.go` (id→output walk through the RENDERED
  text, one-glob completeness, honest absence),
  `internal/testutil/gopi_home_hygiene_test.go` (the launcher's `--dry-run` contract,
  re-scoped to the new location, plus the launch + refusal paths).
- **`devdoc/worker-log-attribution/legacy-log-index.md`** — the measured index behind
  the legacy disposition below.

## Not Done

- **The 95 files were not moved or deleted on disk.** They are on the LIVE profile
  (`~/.gopi/logs`, outside the repo) and 6 of them are cited by line number in
  merged devdocs/briefings (see §The 95). The disposition is a RULING + an index, not
  a filesystem move, so that (a) no citation breaks and (b) the evidence of the
  broken era survives for exactly the walk this epic is about. Nothing writes there
  any more, so it stops growing.
- **The legacy `gopi-*.log` (71 files, 24 MiB of the directory) are out of scope** —
  they are the retired leveled FileLogger's output (see `devdoc/gopi_design_concern.txt`),
  a different producer with its own rotation question. This epic rules on the WORKER
  logs; a FileLogger-retirement task owns the rest.
- **The installed launcher is not reinstalled by this branch.** The live
  `~/.local/bin/gopi-worker-up.sh` still holds the bb559109 version; `--install` must
  be run AFTER the merge (that is the point of `--install`: the installed copy must
  come from the merged tree, never from a worktree that comes and goes).
- **`internal/stylegate` is RED on this branch and this work did not cause it.**
  Measured: `git stash -u` (my change set removed) → the SAME failure, 183 hits vs
  ceiling 182; it arrives with the `master` merge. Nothing else touched here is red.

## Verification

| gate | result |
|---|---|
| `go build ./...` | **clean** (`BUILD_OK`) |
| `gofmt -l cmd/ internal/` | **no output** (clean) |
| `go vet ./cmd/agent ./internal/tools ./internal/testutil` | **clean** |
| `go test -count=1 -run 'TestWorkerAttachOutputLog\|TestWorkerOutputLogSecondRun\|TestWorkerLogsAreReachable' ./cmd/agent/` | **ok** (0.36 s / 2.2 s) |
| `go test -count=1 -run 'TestTaskOutputLogReachable\|TestTaskShowWithoutOutputLog\|TestTaskShow$' ./internal/tools/` | **ok** (33.5 s) |
| `go test -count=1 -run 'TestGopiWorkerUp' ./internal/testutil/` | **ok** (9.2 s) |
| `go test -count=1 ./internal/stylegate/` | **FAIL — pre-existing**, reproduced with the change set stashed (183 vs ceiling 182) |

### The acceptance walk, measured on the real binary

Sandbox `GOPI_HOME=$(mktemp -d)`; the row claimed by a real `worker --once`.

```
$ gopi task submit "demo failed run whose output must be findable" --payload … --task-id t-attribution-demo
Task queued: t-attribution-demo (status=pending, source=human, tags=—)
$ GOPI_TASK_CHILD_FAIL_TEST=1 gopi worker --once
worker: claimed t-attribution-demo
worker: output log for the round: /tmp/tmp.94YXWabEAV/.gopi/sessions/t-attribution-demo/output.log
```

Then, **from the task id alone** (the row's own status is `failed`):

```
$ gopi task show t-attribution-demo
task_id:        t-attribution-demo
status:         failed
task_output:    /tmp/tmp.94YXWabEAV/.gopi/sessions/t-attribution-demo/output.log

$ ls -la /tmp/tmp.94YXWabEAV/.gopi/sessions/t-attribution-demo/
-rw-r--r-- 1 nick nick 671 … output.log

$ head -3 …/output.log
# gopi worker output — task t-attribution-demo session  pid 826592 (find it again: gopi task show t-attribution-demo)
worker: output attributed to task t-attribution-demo → /tmp/tmp.94YXWabEAV/.gopi/sessions/t-attribution-demo/output.log
worker: resumed claimed task t-attribution-demo

$ ls /tmp/tmp.94YXWabEAV/.gopi/logs/
ls: cannot access '…/.gopi/logs/': No such file or directory     ← the public area is never created
```

The ledger event carries the same path, so the answer survives even a pruned
directory:

```
event=failed  detail=gopi run failed: synthetic child failure (test escape) — run: elapsed=0s
                       — worker-output-log: /tmp/tmp.94YXWabEAV/.gopi/sessions/t-attribution-demo/output.log
```

And the e2e test's NON-failing run (mock LLM child, so the child really executes)
shows all three legs resolving inside ONE task dir:

```
session_log: /…/.gopi/sessions/t-attribution-e2e/1512e941-…jsonl
raw_log:     /…/.gopi/sessions/t-attribution-e2e/1512e941-….raw.log
session_id:  1512e941-73f2-4794-ab84-93dc512bcff9
result:      gopi run completed — run: elapsed=0s — worker-output-log: /…/t-attribution-e2e/output.log
```

## The principle, and the design answers

**The principle (operator, highest priority):** everything a worker produces is
attributed to the SESSION it serves. A public flat area is where "谁的都算，谁都不认"
— nobody's responsibility. The workflow needs the opposite, and this is the test the
epic set: *hand over a failed task id, consult NO other record, and still walk to
every output the run produced.*

**The judgement test** (write it into the report, the briefing said):
given a failed task id, can you reach every byte it produced without another record?
**Yes — `gopi task show <id>` prints the three paths, and all three files sit in ONE
directory: `<gopi-dir>/sessions/<task-id>/`.** Before: the id reached nothing (the
output was in a timestamp-named file in a public directory, and the ledger row had no
line pointing at it either).

### Q1 — does the worker know its session id at launch?

**No, and that single fact decides the whole design.** The launcher spawns a worker
for the QUEUE, not for a row; the id first exists at the worker's atomic claim
(`workerRound` ② `TaskNextFiltered`). So a *launcher-side* filename can never carry
the id — which is exactly why `worker-<ts>-<i>.log` was unfixable in place.

Two further measured facts shaped the choice:

1. **The row's `session_id` is EMPTY at claim time and is BACKFILLED mid-run**
   (`TaskBackfillSession`, a moment after the child session appears). Keying the
   directory on it would MOVE the log under the running worker's feet: the worker
   would open `…/sessions/<session-uuid>/output.log` while the terminal `task show`
   looked under a different directory. I measured this on the e2e mock-LLM path
   (the backfill does land), caught the mismatch, and re-keyed the directory by the
   TASK id — stable from claim to forever, and the one thing the caller is required
   to hold.
2. **The executor of a `task run` child logs its JSONL under the SESSION uuid**
   (`internal/agent` `sessionDirForID`) while the task-domain container
   (`sessions/<task-id>/`, the attachment area) is keyed by the TASK id. Both live
   under `sessions/`; `task show` resolves both from the id.

### Q2 — which route was chosen, and why

**The worker process owns the log; the launcher contributes only the root.**

| option | verdict |
|---|---|
| launcher creates the file per worker (status quo) | impossible — no id at spawn |
| launcher pre-mints a session, hands it to the worker | rejected: a worker serves MANY tasks; one minted session per worker is a session that owns nothing, which is the public-area sin one level down |
| worker opens the log at claim time, into the claimed task's session dir | **adopted** — the only point where the id exists; one round = one task = one log |
| keep `~/.gopi/logs/worker-*.log` and symlink into the session dir | rejected: a public name remains the record, and the symlink direction silently becomes load-bearing |

Consequences, all deliberate: the launcher passes a ROOT
(`GOPI_WORKER_LOG_ROOT`, mirrored by `--worker-log-root`) and holds the pipe for
pre-claim noise in ONE fixed `worker-startup.log`; the worker's stdout/stderr are
RE-POINTED (not wrapped), so the child inherits the same fd and the child's output
lands in the task's log too; the writeback stamps the path into the ledger result.

### Q3 — what happens to the launcher's `logdir` argument?

It is **retired as a per-worker file name and replaced by `--output-root DIR`** —
the ROOT, not the file. The `--dry-run` seam survives and now asserts the NEW
location: it prints `output_root=<root>`, one command line per worker carrying
`--worker-log-root <root>`, and the per-task shape `<session-id>/output.log`; the
test also greps the whole dry-run output for the RETIRED name shape
(`worker-<yyyymmdd>-<hhmmss>-<i>.log`) and fails if it reappears. A positional
`[logdir]` is now refused with exit 2 rather than silently ignored.

### Q4 — disposition of `~/.gopi/logs/` (references measured, not felt)

**The directory SURVIVES; the launcher stops putting a file per worker in it.**
Measured writers, all still legitimate:

| entry | writer | status |
|---|---|---|
| `scratch-gc-watchdog.log` | `~/.local/bin/gopi-scratch-gc-watchdog.sh` (cron `*/30 * * * *`, line 56) | **live** — mtime 2026-09-12 06:00 |
| `nightwatch.log`, `night-progress.log`, `glm-night-watch.log` | the night-watch host scripts | **live** (mtime 2026-09-12 03:15 / 09-11 09:03 / 09-10 23:01) |
| `gopi-*.log` (71) | the retired leveled FileLogger (design kept in `devdoc/gopi_design_concern.txt`) | **historical** — a different producer, out of scope here |
| `worker-<ts>-<i>.log` (20) | `gopi-worker-up.sh` | **retired by this change** — no producer left |

So "delete `logs/`" would have broken a live cron writer. The epic-accurate ruling
is: `logs/` is the HOST-COMPONENT log area (a legitimate category), and worker
output is NOT a host-component log — it is a TASK artifact and belongs to the task's
session dir. After this change the launcher's only entry there is the fixed,
appended `worker-startup.log`.

### The 95 (measured: 20 worker logs + 75 other entries)

The briefing's "95 timestamp-named files" is the raw `ls | wc -l` of the directory.
Classified by name:

| class | count | disposition |
|---|---:|---|
| `worker-<ts>-<i>.log` | **20** (23.1 MiB) | **RETAINED + INDEXED.** Indexed by CONTENT in `devdoc/worker-log-attribution/legacy-log-index.md`: 17 of 20 name at least one task/session id inside; the other 3 are 371-byte startup stubs. Deletion is refused because 6 are cited by FILE AND LINE in merged docs — `brief-worker-cohort-kill.md` (8 names), `brief-worktree-recycle-2.md` (`worker-20260912-040617-1.log`, "有真实进展，先去读它"), `devdoc/tools-flake-diagnosis.md` (`…-033016-1.log:2126,2185,4309`), `devdoc/gopi-home-hygiene.md` — so deleting them would break live citations and destroy the incident evidence. |
| `gopi-<ts>.log` | **71** | out of scope: retired FileLogger output (its own retirement task). |
| host-component logs | **4** | live writers — retained, documented above. |

The index is generated from the directory itself, so "which file mentions my task
id" is one grep over it.

**Ruling**: nothing is deleted; nothing new is written; the directory is
reclassified from "worker output dump" to "host-component log area", and every
future worker output has a path instead of a search.

## Relation to `fix/gopi-home-hygiene` (bb559109): BASE + COVER, never supersede

`scripts/gopi-worker-up.sh` is **not in master** — it exists only on the unmerged
`fix/gopi-home-hygiene` branch (checked: `git ls-tree master scripts/gopi-worker-up.sh`
→ empty; the 4 commits above `bb559109` are `b3a78eeb`…`24d341e5`, and the live
installed `~/.local/bin/gopi-worker-up.sh` is byte-identical to that branch's copy).
Two branches carrying that file would be a guaranteed add/add conflict, and the
merge order would silently decide where worker logs go.

**Resolution — this branch is stacked on that branch, then merged forward:**

```
fix/gopi-home-hygiene  24d341e5  (bb559109 = the half fix: logdir → <gopi-dir>/logs)
        └── fix/worker-log-session-attribution   ← this branch (-b off 24d341e5, then `git merge master` for fresh code)
```

- **BASE**: `fix/worker-log-session-attribution` starts at `24d341e5`, so bb559109's
  launcher content is an ANCESTOR — when it lands, git sees modifications, not
  two independent creations.
- **COVER**: this branch supersedes the log-location half of bb559109 (the per-worker
  file is retired), and **keeps its other half**: `scripts/gopi-home-gc.sh`, the
  `--install` seam and the sandboxed-HOME test contract are untouched except where
  the log-location ruling requires re-scoping (`TestGopiWorkerUpLogsIntoDesignatedDir`
  → `TestGopiWorkerUpAttributesLogsToSessionDirs`).
- **MERGE ORDER**: land `fix/gopi-home-hygiene` FIRST, then this branch (it is a
  descendant, so it merges without conflict). The reverse order would require
  re-cutting bb559109's launcher; if that becomes necessary, this branch's file WINS
  — it is the later, corrected contract.
- **After the merge**: run `gopi-worker-up.sh --install` so the stable
  `~/.local/bin` copy follows the merged tree (never `--install` from a worktree).

## Branch + recovery (Rule 6d)

- Branch: **`fix/worker-log-session-attribution`** (in the shared repo `~/work/GoPi`;
  the pre-merge worktree was `/tmp/logattr-wt`).
- Recover the report: `git show fix/worker-log-session-attribution:devdoc/worker-log-session-attribution-20260912.md`
- Recover the legacy index: `git show fix/worker-log-session-attribution:devdoc/worker-log-attribution/legacy-log-index.md`
- Fix commit: `776cb789` (plus the docs commit on top).
