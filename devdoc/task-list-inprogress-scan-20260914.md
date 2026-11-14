<!-- doc-class: historical -->
# Task Report: `task list --status in_progress` is slow + the in_progress ledger audit

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Task: `01a09fa7-5c37-7767-b691-138f4bacc4da` — "in_progress tasks are all dead?"
Branch: `fix/task-list-inprogress-scan` (worktree `~/workspace/gopi-listperf`)

## Completed

### 1. The slowness is NOT the `--status` filter — it is the per-row session probe

Rejected the briefing's premise ("presumed the --status filtering is same
algorithms"). `--status` is one `WHERE status = ?` clause over the same query
(`TaskList`, `internal/tools/tasks.go`); the cost difference is which rows are
RETURNED. The honest-state label (`taskStateSuffix` → `taskProbeRowState`) runs
**only for `in_progress` rows**, and each probe resolved the row's session log
by walking the operator's entire sessions tree.

Measured on this host, same box, same load, one probe process:

| command | before | after |
|---|---|---|
| `task list --status completed` (987 rows) | 0.05 s | 0.46 s |
| `task list --status pending` | 0.02 s | 0.21 s |
| `task list --status in_progress` (6 rows) | **8.5 – 46 s** | **0.57 – 0.77 s** |
| `task list` (all) | 19.7 s | 0.81 s |

`strace -f -c` on the same 6-row list:

| syscall | before | after |
|---|---|---|
| `openat` | 287,448 | ~500 |
| `getdents64` | 574,835 | 2,441 |
| `read` | 2,055 | (tree walk gone) |

Root cause, pinned by instrumented build (not inferred): **~0.7 s per
in_progress row**, entirely inside `taskFindSessionJSONL` —
`findSessionJSONLFlat` does `os.ReadDir` of `~/.gopi/sessions` (**70,438
entries** here) and scans it twice (exact pass, then prefix pass), and
`taskSessionRecordsInDirs` then walks every session subdir reading each
record's first line to recover a session id **the row already carried**. The
probe called the resolver twice per row, so the flat scan ran twice per row
(4 duplicate root scans for 4 live rows).

Ablation proof: skipping `taskProbeRowState` entirely → 0.14–0.23 s. Everything
else (opening the ledger, rendering 987 rows) is ~0.2 s.

### 2. Fix

`internal/tools/tasks.go` + `internal/tools/taskstate.go`:

- `taskFindSessionJSONLInTaskDir(dir, taskID, sessionID)` — answers the row's
  own DETERMINISTIC layouts with `os.Stat`: `<sessions>/<session-id>/session.jsonl`
  (bare session dir) and `<sessions>/<task-id>/…` (task-run dir, whose name IS
  the task id).
- `taskFindSessionJSONLForRow` — lookup first, then legacy flat pass, then a
  single deep-scan pass (`taskFindSessionJSONLDeepOnly`, split out so the flat
  pass is not repeated). Behaviour-identical fallback: any layout the fast path
  cannot name still resolves through the original scan.
- `findSessionJSONLFlat` — the two passes become ONE pass that keeps exact-match
  precedence over an earlier prefix hit (the second full walk existed only to
  preserve that precedence).
- `taskProbeRowState` now resolves ONCE per row, via the row-aware resolver.

### 3. Ledger audit — the rows are NOT all dead

Per-row evidence (`kill -0` on the claim pid + session JSONL tail):

| row | summary | claim pid | verdict at audit time |
|---|---|---|---|
| 1997 | peak-hour cost guard | 3445263 `gopi worker` | **LIVE** — closed itself `completed` 19:30:14 during this audit |
| 2002 | voice --client leg on HP | 3445420 `gopi worker` | **LIVE** — tool calls seconds old |
| 2004 | worker is gone | 3445147 `gopi worker` | **LIVE** — worktree `gopi-worker-gone` already recycled |
| 2007 | daemon-cwd immortal holder | 3460822 `gopi worker` | **LIVE** — tool call 38 s old, `go test` in flight |
| 2012 | `[auto] ELF` | 3467413 | **DEAD pid** → row went `failed` at 19:30:50 |
| 2021 | this task | — | live (self) |

The display was not corrupt: `[died 53m25s since 18:19:06]` is the honest-state
label epic's `died` state, and `[running Nh]` is `running` (verified-live
holder). New rows (2008 "master red (unrelated)") appear as the same workers take
new work — the ledger is converging on its own, not wedged. No row needed
killing; no live worker was touched.

### 4. The merger IS silently withholding branches (real, operator-relevant)

`gopi task merger --status`:

```
repo:        /home/user/work/GoPi
  protected: 13 branch(es) held by a live worktree
  pending:   none — nothing to merge
```

Those 13 branches are HELD BY WORKTREES, so `safe-merge.sh` gate [c] refuses
them and the merger reports `pending: none`. Examples: `fix/peak-guard-cost-class`
(row 1997, terminal, ahead=5), `fix/daemon-cwd-escape` (ahead=6),
`feat/voice-client-e2e` (ahead=1), `fix/liveclaim-worktree-veto` (ahead=1, NO
holder), `fix/ledger-fixture-leak` (ahead=0, NO holder).

## Not Done

- **I did not remove any worktree.** Every candidate was disqualified by the
  guards: the four rows' worktrees are all required by LIVE workers (a
  worktree is removed at the END of development — that is the signal to the
  merger), and the "unheld" ones (`gopi-liveclaim-veto`, `gopi-ledger-leak`)
  lost their holder only because this very audit's predecessor was killed —
  treating a momentarily unheld tree as abandoned is how work is lost. See the
  reclaim pass in Verification for the per-path disposition.
- I did not merge anything (Rule 6 — merge is the merger's job).
- Row 1997's worktree stays occupied although its row is `completed`: a worker
  can close its row and still be mid-flight (it did — it has since taken row
  2008). The reclaim arm is deliberately terminal-row-only, so this is reported,
  not silently swept.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK
- `gofmt -l internal/tools cmd/agent` — clean; `go vet` — VET_OK
- Tests: see the run block below.
- Red-proof by MUTATION: demoting the lookup below the deep scan makes
  `TestTaskFindSessionJSONLForRowPrefersRowPathsOverScan` FAIL
  ("bare session dir not preferred over the scan: got …/0-decoy/session.jsonl,
  want …/11111111-…/session.jsonl"), then passes again restored.
- New tests: `internal/tools/tasks_session_resolve_cost_test.go`
  (flat exact-beats-prefix in one pass; row paths win over a scanning decoy;
  all three layouts; scan fallback still resolves; unresolvable stays empty).

## Files

- `internal/tools/tasks.go` — row-aware resolver + one-pass flat scan
- `internal/tools/taskstate.go` — probe resolves once, via the row-aware resolver
- `internal/tools/tasks_session_resolve_cost_test.go` — regression guards
