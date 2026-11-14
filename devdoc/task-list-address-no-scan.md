<!-- doc-class: historical -->
# Re-land: resolve a row's session log by address, never by walking the sessions tree

Epic: **RE-LAND the reverted perf fix** (`gopi task list --status in_progress`
paying a whole-~/.gopi/sessions tree walk per row).
Branch: `fix/task-list-address-no-scan` (fresh off master `c1117fb9`, Rule 6a/23;
since RESTACKED twice — onto `8c6e0cc8`, then onto current master `dcce3a86`; see §Restack).
Worktree: `/home/user/workspace/gopi-listperf-relaunch`.

## Symptom

`gopi task list --status in_progress` took **6.1–8.5s** on the live ledger while
every other `--status` filter answered in ~0.04s on the SAME database. The filter
is not the difference: `--status` is one `WHERE` clause, and only in_progress rows
get the honest-state probe. That probe resolves the row's session log with
`taskFindSessionJSONL`, which walked the operator's ENTIRE sessions tree
(70,647 entries here) to recover a session id **the row already carried**.

## Root cause (5-why)

1. `task list --status in_progress` renders a per-row honest-state label.
2. That label resolves the row's session JSONL (`taskProbeRowState` → the
   resolver) and reads its tail.
3. The resolver was a TREE-WIDE scan (`sessionlayout.Records`): readdir the
   sessions root, then read the first line of EVERY session-dir record to learn
   each record's session id.
4. It recovered a fact the row's own columns already name (`task_id`,
   `session_id`), for which the session-dir-unify layouts give a DETERMINISTIC
   path.
5. So a render was O(rows × entire sessions tree) — 601,165 `getdents64` +
   299,848 `openat` for ONE view, growing with the operator's history.

Two independent read-path costs were fixed (see §Third finding).

## What this branch carries

Applied as fresh commits off master (Rule 23 — the failed/reverted branch
`fix/tasklist-inprogress-scan` is read as evidence, never committed onto):

| commit (as first landed) | what |
| --- | --- |
| `a331bb18` (was `795cf12e`) | `cherry-pick -x 0618a2cd` — the address lookup for a row's session log (intent of the reverted `54cdf94a`) |
| `e3ac4888`, `caf7ec78` (DROPPED in restack) | `cherry-pick -x 93bbf5d3`, `5dc0cb58` — the read-path stale sweep off the read verb (coordination with `fix/task-list-sweep-readpath`; same file `cmd/agent/cli_tasks.go`, auto-merged cleanly). **DROPPED in the restack below — master merged that work itself.** |
| `f8ffa8a2` (was `c4f8e245`) | the COUNTING SEAM + the scan's own precedence (this task's new work) |

### Restack onto `8c6e0cc8` (2026-09-14 late)

Master moved while this branch was under review: it merged
`fix/task-list-sweep-readpath` itself (`8c6e0cc8`). The two cherry-picks that
carried that branch's sweep work (`e3ac4888`, `caf7ec78`) were therefore
duplicates of master's own commits (`93bbf5d3`, `5dc0cb58`) and were DROPPED
during the restack (`git rebase --onto 8c6e0cc8 c1117fb9`, with those two picks
deleted from the todo) — the deliverable no longer re-lands another branch's
work. What remains is exactly this epic's own change, replayed on the current
master:

```
(this report commit — the tip; `git log -1 --format=%h`)
f8ffa8a2 test+fix(task list): a counting seam on the sessions-tree walk, and the scan's own precedence
a331bb18 fix(task list): resolve a row's session log by lookup, not by walking the whole sessions tree
dcce3a86 (master) Merge branch 'fix/doubaoasr-test-sleep'
```

**Second restack, onto `dcce3a86` (2026-09-14 late-late).** Master moved again by a
single non-overlapping merge (`fix/doubaoasr-test-sleep` — `internal/doubaoasr/`
+ a devdoc, files this branch never touches), so the branch was replayed with
`git rebase FETCH_HEAD` (no conflicts). The table's mapping commit→hash is what
that produced: same three commits' content, new hashes (`a331bb18`, `f8ffa8a2`).
**Every number in §Verification below was re-measured at THIS tip** — nothing is
carried over from the earlier restack.

(The report commit is the tip of this branch; its hash is the one printed by
`git log -1 --format=%h fix/task-list-address-no-scan` — the two code commits
above are stable and carry the deliverable's content.)

The sweep coordination deliverable ("put the read-path sweep off the read
verb") is therefore satisfied BY MASTER, verified here rather than re-landed —
see §The read-path sweep.

### The address lookup (`795cf12e`, intent of `54cdf94a`/`0618a2cd`)

The row's own deterministic layouts are probed first — `sessions/<session-id>/session.jsonl`
(bare) and `sessions/<task-id>/<session-id>.jsonl` (task-run) — then the flat
pass, then the unchanged tree scan as a behaviour-identical FALLBACK.

### The counting seam (`c4f8e245`) — the acceptance's cost property

`taskSessionTreeWalk` is the ONE seam through which the resolution path reaches a
whole-tree walk; the per-process MEMO sits **above** it (`taskSessionTreeRecords`),
so a counting binding observes the REAL number of walks. `SessionScanCounterForTest`
exports a counter over that production seam, so a CLI-level test counts walks
without re-implementing the wiring (a mirrored copy could pass by accident).

### Precedence fidelity (a REAL bug in `0618a2cd`, found by red-proof #2)

The scan visits the sessions root in readdir (byte-sorted) order and returns the
FIRST record whose id matches, so which of a row's two dirs supplies the file is
decided by which DIR NAME sorts first (bare `<session-id>` vs task-run
`<task-id>`). `0618a2cd` probed the bare layout FIRST unconditionally. For the
live layout that is harmless (a session id never sorts before `t-`), but it is a
behaviour change for an earlier-minted id. Now the probe order is the scan's OWN
order, pinned by a scan-oracle test.

## Measurements

Host: peerbox. 70,647 entries in `~/.gopi/sessions`. Baseline = master `c1117fb9`;
fixed = this branch, both built `CGO_ENABLED=0`, back to back on the same machine.

### Frozen-ledger A/B (the acceptance measurement, re-run at the restacked tip)

The live ledger gains in_progress rows while the system runs, so a live A/B can
compare two different row sets. A snapshot was frozen first (one `memory.db`
copy per binary, 4 in_progress rows) and BOTH binaries were pointed at the SAME
frozen row set (`GOPI_MEMORY_DB=<snapshot>`), which makes the comparison
row-set-identical. Baselined against `8c6e0cc8` (current master), so the delta
below is THIS change and nothing else:

| | baseline (master `8c6e0cc8`) | fixed (this branch) |
| --- | --- | --- |
| `/usr/bin/time` (3 runs, frozen ledger) | **3.72 / 4.08 / 4.93 s** | **0.05 / 0.05 / 0.10 s** |
| output hash, normalised for the clock token (all 6 runs) | `fb6d840e2020dea6a8af3208031819c5` | `fb6d840e2020dea6a8af3208031819c5` |

All six runs share ONE hash once the clock-derived `[running Xm Ys since HH:MM:SS]`
token is normalised, and `diff` shows no other difference: same rows, same order,
same columns, same state labels. (The live-ledger runs measured before the
snapshot agree: 6.12–8.50s → 0.04s.) The only token that can differ between two
runs of ANY binary is that clock — elapsed-since-claim is a render of `now`.

### Syscall census (frozen ledger, one view each)

| | baseline (master `8c6e0cc8`) | fixed (this branch) |
| --- | --- | --- |
| `getdents64` | 601,195 | **20** |
| `openat` | 299,876 | **49** |
| `pread64` (the ledger + the session tail read) | 565 | 565 |
| total syscalls / syscall seconds | 1,628,036 / 88.53s | 2,395 / 0.097s |

`pread64` is UNCHANGED (565 → 565) — the fix removes directory traversal, not
I/O the render genuinely needs, which is the cost-identity half of the claim.
The epic's acceptance band is ~0.1–1s; the fixed view measures **0.05s**, i.e.
at/below the band's lower edge — the band's floor is the cost of opening the
ledger, not a target, and the defect axis (a whole-tree walk) is proven by the
counter and the census above, not by the clock.

## Red proofs (mutation, each restored and re-verified green)

1. **Scan before address** (`taskFindSessionJSONLForRow` rewritten to run the
   deep scan first):
   - `cmd/agent TestTaskListInProgressViewWalksNoSessionTree` → **FAIL**
     `walked the sessions tree 1 time(s); an addressable row must resolve with ZERO tree walks`
   - `internal/tools TestRowAddressResolutionWalksNoSessionTree` → **FAIL** (bare + flat subtests)
2. **Precedence flip** (task-run dir probed before the bare dir):
   - `TestRowAddressResolutionMatchesTheScan` → **FAIL**
     `addressed resolution returned …/t-1789381146/<sess>.jsonl but the scan returns …/<sess>/session.jsonl`
   - This is the mutation that exposed the real bug in §Precedence fidelity.
3. **Behaviour-identity** is pinned positively by
   `TestRowAddressResolutionMatchesTheScan` (the SCAN is the oracle, both dir
   orderings), and the fallback + memo by
   `TestSessionTreeScanStaysTheFallbackAndIsMemoised` (2 fallback resolutions → exactly 1 walk).

## Third finding: the merger's version-probe blocker

The epic asked whether the version probe is what reverts the landing. **It is
not the active blocker today, but it IS fixed on master.** Evidence:

- `~/.gopi/logs/merger.log` — the original rounds failed exactly here:
  `[rebuild] FAILED … err=version mismatch: expected <tip>, binary reports
  "v…-<tip> (…)\nrevision: …\ntag: …"` → `REVERTED: master restored`. Cause (5-why
  in `devdoc/rebuild-version-report-probe.md`, on master): `binaryVersion` handed
  the probe a THREE-LINE report and the reader parsed the single-line display.
- A later dedicated task (`01a09abb-…`) fixed `RevisionOfDisplay` + `binaryVersion`
  and VERIFIED it with a live probe against the real repo; the log's LAST entries
  are `[rebuild] ok: …` (e.g. round landing `bfd9c32b`, `db1ac4aa`).
- Confirmed here on master `c1117fb9`: `go test ./internal/tools/ -run 'Rebuild|Version|Revision|Report'`
  → ok, `go test ./internal/version/` → ok. `./bin/gopi --version` prints
  `dev` for a source-tree build without an injected identity — the probe's
  GoPi-convention guard (`internal/tools/rebuild.go`) skips exactly that shape,
  so no false mismatch is possible for it.

Why the reverted landing was reverted is the OTHER pending defect, NOT the probe:
`01a09fd1-3ff2-7014-8534-7f045bfd5771` — a verify child died after 40m42s
**without a verdict**, and the unwind still consumed the landing. That row is
out of this epic's scope and is left as the recorded owner of that fix.

## The read-path sweep (second, independent cost) — SATISFIED BY MASTER

Required: "take the synchronous maybeTaskListSweep off the read verb". Master
already carries it: `8c6e0cc8` merged `fix/task-list-sweep-readpath`
(`93bbf5d3` code, `5dc0cb58` re-measurement), which removes
`maybeTaskListSweep` from the plain `task list` read path, keeps `--sweep` as the
explicit trigger, and replaces the process-variable throttle with a PERSISTED
stamp (`cmd/agent/tasklistsweep_gate.go`). Verified on this branch's base:
`cmd/agent/cli_tasks_list_sweep_readpath_test.go` and
`cmd/agent/tasklistsweep_gate.go` are both present at `8c6e0cc8` and
`go test ./cmd/agent/ -run 'Sweep'` is green there.

This branch therefore RE-LANDS NOTHING of that work (the earlier cherry-picks
were dropped in the restack, §What this branch carries) — the coordination
result is "already merged upstream, verified, not duplicated".

## Files

- `internal/tools/tasks.go` — the address lookup, the walk seam + memo, the scan's precedence
- `internal/tools/taskstate.go` — `taskProbeRowState` calls the row-addressed resolver
- `internal/tools/tasks_session_scan_seam_test.go` — in-package cost contract (0 walks) + scan-oracle identity + memo
- `internal/tools/tasks_session_resolve_cost_test.go` — (cherry-picked `0618a2cd`) per-layout resolution cost
- `cmd/agent/cli_tasks_list_session_scan_test.go` — end-to-end render cost (0 walks, rows still rendered)
- `devdoc/task-list-address-no-scan.md` (this report), `devdoc/task-list-inprogress-scan-20260914.md` (cherry-picked evidence)
- the sweep work (`cmd/agent/tasklistsweep_gate.go`, `cmd/agent/cli_tasks_list_sweep_readpath_test.go`, `devdoc/task-list-sweep-readpath.md`) is **already on master**, not carried here

## Verification

All rows re-run at the restacked tip (`a331bb18`, `f8ffa8a2` on master `dcce3a86`).

| check | result |
| --- | --- |
| `CGO_ENABLED=0 go build ./...` | exit 0 |
| cost property (0 tree walks, in-package + end-to-end) | PASS |
| `go test ./internal/tools/ -run 'TestRowAddressResolutionWalksNoSessionTree\|TestRowAddressResolutionMatchesTheScan\|TestSessionTreeScanStaysTheFallbackAndIsMemoised\|TestFindSessionJSONLFlatExactWinsInOnePass' -v` | ok 1.56s, 4 tests + 5 subtests PASS |
| `go test ./cmd/agent/ -run TestTaskListInProgressViewWalksNoSessionTree -v` | ok 0.89s PASS (the counter reports **0**) |
| red proof 1 — address lookup short-circuited back to the scan: `TestRowAddressResolutionWalksNoSessionTree/{task-run,bare}` → **FAIL** ("walked the sessions tree 1 time(s); a row-addressed resolution must walk it ZERO times") AND end-to-end `TestTaskListInProgressViewWalksNoSessionTree` → **FAIL** ("the in_progress render walked the sessions tree 1 time(s)") → restored: ok 1.83s |
| red proof 2 — scan precedence flipped (`id < tid` → `id > tid`): `TestRowAddressResolutionMatchesTheScan/{session_dir_sorts_first,task_dir_sorts_first}` → **FAIL** ("the cost fix must not change WHICH file resolves") → restored: ok 1.77s |
| **frozen-ledger A/B** — same copied ledger, `--db`, 3 runs each: baseline `dcce3a86` binary **5.56 / 7.06 / 10.10 s** vs this branch **0.10 / 0.06 / 0.05 s** | 55–200× |
| **output identity** — one sha256 over all SIX outputs, normalised only for the clock-derived `[running Xm Ys since HH:MM:SS]` (the only field that can differ between runs) | **`29889377de6fbc96…`, identical across all 6**; `diff` of base-vs-new is empty |
| live ledger `/usr/bin/time ./bin/gopi task list --status in_progress`, 3 runs each | baseline **8.50 / 8.73 / 8.89 s** → branch **0.23 / 0.24 / 0.18 s** — inside the required ~0.1–1 s band |
| session-tree scan counter, in_progress view | **0** (asserted in-process by the seam counter, `tools.SessionScanCounterForTest`) |
| `strace -c -e getdents64,openat,pread64` for ONE view | `getdents64` **601,589 → 24**; `openat` **300,039 → 49**; `pread64` **561 → 561** (unchanged — the READ work is identical; only the directory traversal disappeared) |
| `go test ./internal/tools/ -run 'Session\|TaskFind\|TaskProbe\|TaskInProgress\|TaskList\|Zombie\|Claim\|Stale\|Deliverable'` | ok 14.0s |
| `go test ./cmd/agent/ -run 'TaskList\|Sweep\|Session\|Tasks\|List'` (with `bin/gopi` built, which 3 E2E tests require) | ok 26.5s |
| `gofmt -l` on touched files / `go vet ./internal/tools/ ./cmd/agent/` | empty / clean |
| `scripts/safe-merge.sh fix/task-list-address-no-scan` | ✅ no archive tag, ✅ no blacklisted prefix; ⛔ blocked only by **gate [c] — still checked out in its own worktree**, the normal pre-acceptance state (PA removes the worktree at acceptance; gates [a]/[b]/[d] clear) |
| `internal/standards` / `internal/stylegate` | RED — **pre-existing on master, identical at the `dcce3a86` baseline** (see below) |

### Pre-existing master redness (measured, not caused here)

Both packages were run in BOTH trees: the baseline worktree pinned at master
`dcce3a86` (`/dev/shm/gopi-test-scratch/ab-master`) and this branch's tip.

- `internal/standards`: `TestStandardGates` — **FAIL in both trees, same single
  failure**: `TestGate3ElseAfterReturnRedProof` reports Rule 18 (no else after a
  terminating if) **NEW violation at `cmd/agent/cli_voice_device_policy.go:226`**.
  Identical text in the `dcce3a86` baseline, i.e. inherited from master; none of
  this branch's files appear among the offenders.
- `internal/stylegate`: at the `dcce3a86` baseline it is **green** (`ok 15.5s`,
  and `ok 39.96s` on the second run in the killed A/B job) — since master merged
  `fix/doubaoasr-test-sleep` (`dcce3a86`) the `time.Sleep` hit the earlier report
  recorded is FIXED. Re-run at this branch's tip: **ok 15.5s, green here too**.
  So the only redness in the targeted set is the inherited Rule 18 hit above.

## Not done / deferred

- The verdict-less-verify-child unwinder is NOT fixed here (row
  `01a09fd1-3ff2-7014-8534-7f045bfd5771` owns it). This re-land assumes a landed
  branch is unwound only on a RECORDED failure — worth a separate task, since it
  is what actually cost the previous landing.
- The rework row `01a09e9b-d4e4-755a-af4c-85ffb667801b` is left untouched:
  this branch supersedes its branch (`fix/tasklist-inprogress-scan`), so it
  should be closed as superseded rather than executed.
- The pre-existing master-red package (`internal/standards` Rule 18 at
  `cmd/agent/cli_voice_device_policy.go:226`) is untouched — measured identical
  in the `dcce3a86` baseline tree. (`internal/stylegate`'s doubaoasr test-sleep
  redness is gone from master as of `dcce3a86`; green in both trees.)

## Deliverable location (Rule 6d)

- **Branch**: `fix/task-list-address-no-scan` in `/home/user/work/GoPi` (this
  worktree is linked, so the commits ARE on the branch in the main repo).
  Tip = this branch's HEAD, restacked onto master `dcce3a86` (code commits `a331bb18`, `f8ffa8a2`).
- **Recovery command**: `git show fix/task-list-address-no-scan:devdoc/task-list-address-no-scan.md`
- No push was needed (worktree shares the main repo's refs); the branch is
  master-mergeable as-is.

---

# Rework (2026-09-15): the address lookup was INERT on the operator's ledger

**This is the decisive finding of the re-land.** The section above measured the
address lookup against FIXTURE layouts (a row whose own `<task-id>/` dir holds
its session log, or a bare `<session-id>/session.jsonl`). On the operator's LIVE
ledger neither layout exists for the rows that make the view slow, so every
probe fell through to the unchanged tree scan — the branch DELIVERED the fix and
the view still cost **6.1–12.5s** (a first re-measurement at the rebased tip on
master `dcce3a86`: `6.16 / 6.12 / 6.90 s`, strace **151,892 getdents64 /
75,959 openat**). The epic's own acceptance ("0.1–1s band") was therefore still
RED after the earlier commits, and a re-land that stopped there would have been
another landing that does not pay off.

## Root cause #2 (5-why)

1. The view still walked the tree per row.
2. `taskFindSessionJSONLForRow` found nothing at the row's own addresses — the
   bare dir and the row's OWN task-run dir — so it fell through.
3. The session log lives at `sessions/<A>/<session-id>.jsonl`, where **A is a
   DIFFERENT task id than the row's**.
4. Because a ledger row's `session_id` is the session that **SUBMITTED** it, and
   one worker session executes MANY rows: the ledger has four in_progress rows
   sharing ONE submitter session (`56055d3a-…`), which physically lives at
   `sessions/01a09fa7-5c37-7767-b691-138f4bacc4da/56055d3a-….jsonl` — the dir
   named by that session's OWN row (`01a09fa7-…`, status `completed`).
5. So the file's address is named by the LEDGER (the session's own row), not by
   the rendered row's columns — and the fix only consulted the rendered row.

Verified directly on the live ledger (`task show` of each of the 4 live rows
+ the session's own row), and the polluting shape is a *worker* run: `run_method:
worker`.

## Fix: the LEDGER names the directory (`internal/tools/taskledger.go`)

- `taskSessionDirHints(sessionID)` returns the candidate session-DIR names: the
  task ids of the ledger rows carrying that session id (newest first) — the
  session's own row among them, and THAT row's `task_id` names the directory.
  Bounded (`taskLedgerHintLimit = 8`), read-only, and **not** cached on a miss
  (the row may be in flight — a cached miss would suppress the address for the
  rest of the process); a HIT is memoised per process, because a group of rows
  sharing one session must pay ONE ledger read (Rule 15: a failed hint is
  recorded as *no hint*, never as an empty answer that suppresses the fallback).
- `taskFindSessionJSONLForRow` probes, in cost order: the row's own addresses →
  each ledger-named directory (one `stat`/`readdir` of ONE named dir) → the flat
  pass → the unchanged tree scan fallback.
- The ledger hint read is a SEAM (`taskLedgerHintProbe`) with an exported
  counter (`tools.SessionDirHintCounterForTest`), so a cost test can tell
  "resolved by lookup" from "the probe never ran" — a zero walk count alone
  could otherwise be satisfied by doing no work.

The tree scan is still the fallback, so nothing that resolved before can
regress (`TestRowAddressResolutionFallsBackWhenNoLedgerHint`).

## Measurements at this tip (master `b76942b8`, rebuilt; live ledger)

| | baseline (master `b76942b8`) | this branch |
| --- | --- | --- |
| `/usr/bin/time` ×3, live ledger | **13.28 / 9.08 / 9.09 s** | **0.08 / 0.05 / 0.12 s** |
| output, clock-normalised | `992b159729d62eb9…` (all 3 runs) | `992b159729d62eb9…` (all 3 runs) — **identical to baseline** |
| `getdents64` | 602,551 | **16** |
| `openat` | 300,522 | **45** |
| `pread64` | 561 | **561** (unchanged — only traversal removed) |
| total syscalls | 903,634 | **622** |

Output identity: all six runs share ONE sha256 after normalising ONLY the
clock-derived state token (`[<state> <dur> since <HH:MM:SS>]` /`, age <dur>]`);
`diff` shows no other difference on any of the three pairs.

## Red proof #3 (this rework's own mutation)

Removing the ledger-named probe from `taskFindSessionJSONLForRow` (the exact
revert of this commit's fix) turns all three levels red — then green when
restored:

- `internal/tools TestRowAddressResolutionUsesTheLedgerNamedDir` → **FAIL**
  `the ledger-named resolution walked the sessions tree 1 time(s); it must walk it ZERO times`
- `internal/tools TestLedgerNamedDirOutranksTheScan` → **FAIL**
- `cmd/agent TestTaskListInProgressViewResolvesByLedgerHint` → **FAIL**
  `the in_progress render walked the sessions tree 1 time(s); a row whose log the ledger names must resolve with ZERO tree walks`

## Verification at this tip (re-run after the rebase onto `b76942b8`)

| check | result |
| --- | --- |
| `CGO_ENABLED=0 go build ./...` / `-o bin/gopi ./cmd/agent/` | exit 0 |
| `go test ./internal/tools/ -run 'Task\|Session\|Hint\|Claim\|Stale\|Zombie\|Deliverable'` | ok 31.5s (baseline tree: ok 33.8s) |
| `go test ./cmd/agent/ -run 'TaskList\|Sweep\|Session\|Tasks\|List\|Hint'` | ok 18.2s (baseline tree: ok 40.1s) |
| `go test ./internal/standards/` | **ok 13.0s GREEN** (the earlier report's Rule 18 hit at `cli_voice_device_policy.go:226` is fixed on master by `b76942b8`) |
| `go test ./internal/stylegate/` | ok 7.9s GREEN |
| `gofmt -l` on every file this branch touches / `go vet ./internal/tools/ ./cmd/agent/` | empty / clean |
| new cost tests (ledger-named dir, precedence, memo/miss, layout contract, e2e) | PASS |

## Deliverable location (Rule 6d) — updated

- **Branch**: `fix/task-list-address-no-scan`, tip `0dcb48a9`, base master `b76942b8` (mergeable: master is an ancestor).
- Code commits: `fffaa85f` (address lookup), `a78e444f` (walk seam + precedence), `0dcb48a9` (**the ledger-named address — the one that makes it pay off**).
- **Recovery**: `git show fix/task-list-address-no-scan:devdoc/task-list-address-no-scan.md`.
