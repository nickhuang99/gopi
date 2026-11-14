<!-- doc-class: historical -->
# Independent verification: signalrec session attribution (ledger 01a0986d-e176-7c59-a3ee-907a9be5ec91)

Read-only verifier. Merge commit `9f79d19f` on `master` (parents `707c7560` = pre-merge
master, `1ae47678` = branch tip). Branch commits `9e482ea8` / `7a3d9f57` / `1ae47678` all
asserted ancestors of `master` (`git merge-base --is-ancestor` → YES ×3).

Worktrees (self-created, Rule 6a — main dir untouched): `/home/user/workspace/gopi-verify-signalrec`
(detached @ `9f79d19f`) and `/home/user/workspace/gopi-verify-base` (detached @ `707c7560`).

## Verdict: PASS

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` in the merged worktree → `BUILD_RC=0`
- **`go test -count=1 ./internal/signalrec/`** → `ok … 1.748s` — 13/13 PASS, including the
  path assertions. `-v` transcript: `TestSignalRecInstallRecordsTheDeathInTheSessionDir`,
  `…TaskRunChildWritesIntoTheLedgerTaskSessionDir`, `…ExportedHandoffWinsAndIsValidated`,
  `…BareSessionWithNoHandoffIsRefused`, `…ResidentRoleGetsAProcessScopedSessionDir`,
  `…RecordsOncePerSignalKind`, `…IntentNamesTheKiller`,
  `…AppendOnlyOneJSONPerLineUnderConcurrency`, `…OffIsHonored`,
  `…ChildVictimRecordsEndToEnd`, `…IntentFailsLoudlyWithoutASink`,
  `…ForwardingPreservesDefaultDeath`, `…NoProductionPathJoinsTheLegacySink`.
- **`./internal/standards/`** → `ok` (both at the merge tip and in the pre-merge baseline)
- **`./internal/stylegate/`** → `ok`
- **Commit:** verification only — no commit to `master`, no merge, no edit of the deliverable.

### Baseline delta table

Baseline `gopi-mergeverify-base-3183655490.baseline.log` (the one named in the briefing);
`base` = my own run in the `707c7560` worktree.

| package | baseline FAIL set | merge `9f79d19f` | base `707c7560` | verdict |
|---|---|---|---|---|
| build | — | OK (rc 0) | — | green |
| internal/signalrec | ok | **ok** | ok | green (baseline green too) |
| internal/standards | ok | **ok** | — | green |
| internal/stylegate | ok | **ok** | — | green |
| internal/testutil | PinCoverageTable, ScriptsAreBoundedAndOffTmp, EveryTempUsingSuiteIsPinned, EveryTestMainSuiteIsNotifyHermetic, WorkerOwnershipPointerPresent | PinCoverageTable, ScriptsAreBoundedAndOffTmp, EveryTempUsingSuiteIsPinned, WorkerOwnershipPointerPresent | same 5 | **pre-existing** (merge FIXES `EveryTestMainSuiteIsNotifyHermetic`) |
| cmd/agent | 3× TestAgentSpawnGuard* (`netpoll failed`) | same 3 × `netpoll failed` | same (`fatal error: runtime: netpoll failed`) | **pre-existing** |
| internal/tools | TestTaskStatusRunningForOwnRow (+ spawn-guard) | TestTaskStatusRunningForOwnRow (+ spawn-guard, same names) | — | **pre-existing** |

No package newly red. The only in-gate delta is *downward* (3 `internal/signalrec`
scratch/notify findings — `pin_coverage_test.go:92`, `scripts_gate_test.go:185`,
`scripts_gate_test.go:229` — are GONE at the merge, because the branch added the
`testutil.PinSuiteForTestMain` TestMain the gate demanded).

## Path-assertion findings (the epic's actual claim)

Asserted by PATH on the real filesystem, not by return value:

| acceptance | evidence |
|---|---|
| record lands in `<gopi>/sessions/<id>/signal-provenance.jsonl` | `TestSignalRecInstallRecordsTheDeathInTheSessionDir` asserts the file path and `session_dir`; live: 5 task session dirs carry the file (80 lines total, 0 malformed, one JSON object per line) |
| **no public-dir fallback** | `srAssertNoLegacySink` fails when anything exists under `<home>/logs` — asserted in every test; `TestSignalRecNoProductionPathJoinsTheLegacySink` counts the retired-join spelling in `signalrec.go` (≤1, in `legacySinkHint`, report-only); `sessionDest` returns an ERROR at step 4 instead of a global path |
| bare-prompt / `[auto]` case writes to its OWN session dir | two halves: `TestSignalRecBareSessionWithNoHandoffIsRefused` — a one-shot mode with no handoff writes **nowhere** (no bucket invented, error names the retired sink, public dir empty); the live bare front run reaches `printLogPaths(ag)` → `signalrec.SetSessionDir(filepath.Dir(ag.SessionLog.Path()), …)` (`cmd/agent/main.go`, one writer), so a normal bare prompt resolves to its own session dir |
| append-only, one JSON per line under concurrency | `TestSignalRecAppendOnlyOneJSONPerLineUnderConcurrency` + single `O_APPEND` write; independently re-checked against the 6 live provenance files |
| migration per-record to owning session dirs | recomputed independently from `/tmp/signal-provenance.log.bak-pre-migration`: 36/36 lines present **byte-identical** in session dirs, 0 missing; destinations from the records' own fields = 20 / 14 / 2 into `01a0986d-d476…` / `01a09805-6dae…` / `01a0981f-d909…`; public file absent. `scripts/rehome-signal-provenance.sh` rig re-run with `TMPDIR=/dev/shm/sigverify-tmp`: dry-run changed nothing, apply filed 2/4, **no directory invented** for the gone-id line, source kept while unattributable lines remain |
| `<gopi>/logs` classified | live `lsof` confirms `merger.log` (pid 1842463, 2360864) and `worker.log` (pid 2140436, 2140461, 2161545, 2184238) are held by resident `gopi` daemons on fd 1w/2w; `scratch-gc-watchdog.log` held by nobody (host cron). devdoc §C carries the argument per file |
| devdoc carries path contract, migration record, exception rationale, "no fallback" argument | all four present (`devdoc/signalrec-session-attribution.md` §1–§3, §Migration record, §C, §D) |

## Findings the deliverable should know about (not merge regressions)

1. **A NEW in-gate finding inside an ALREADY-RED gate:** `scripts/verify-rehome.sh`
   (a file this branch adds) is flagged by `TestScriptsAreBoundedAndOffTmp:155` —
   it uses `mktemp -d` without sourcing `scripts/lib/tmp-ban.sh`, so with `TMPDIR`
   unset its fixture lands on `/tmp` (the 87 %-full `/` partition) — the exact hazard the
   gate exists for. The gate was already failing in the baseline (14 `:153` findings), so
   this changes no verdict, but it is the deliverable's own file. I ran the rig with
   `TMPDIR=/dev/shm/sigverify-tmp` so the verifier itself wrote nothing to `/tmp`.
2. **The retired public file REAPPEARED while I verified** — `~/.gopi/logs/signal-provenance.log`,
   14 lines, ts 13:12:45 → 13:15:24, mtime 13:15:03 (i.e. AFTER the 13:03:57 merge).
   Every line is **old-format** (`layout`/`session_dir` absent — 0 of 14 new-format). The
   writers are `gopi-suite-coverbin-*` binaries whose cwd is another agent's PRE-merge
   worktree (`gopi-daemon-ctx`, `gopi-tmpdir`, `gopi-worker-daemon`) plus an `agent.test`
   build, and those coverbins carry `signal-provenance.jsonl = 0`, `GOPI_SIGNALREC_SESSION_DIR = 0`,
   `signal-provenance.log = 1` — pre-`9e482ea8` binaries. This independently confirms the
   devdoc §D stale-coverbin root cause and shows it is a **test-infrastructure artifact of the
   other pre-merge worktrees**, not a live write path of the merged tree: no line written by
   this merge's code reached the public sink, and the new-format path is field-proven
   (`sessions/…-e176…/signal-provenance.jsonl`, 15 lines, `layout=task-run`, `session_dir` =
   the containing dir). Left as the devdoc's own "Not Done" item (rebuild/purge in
   `cmd/agent/gopibin_test.go`).

## Cleanup

Both detached worktrees are verification-only and are removed after this report;
`git status --porcelain` in `/home/user/work/GoPi` is empty.
