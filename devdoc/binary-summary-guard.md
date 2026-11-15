<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Binary task summary — root cause, fix, and the failed-task survey

Epic: `gopi task list --status failed > /tmp/failed_tasks.txt` produced a file with
binary bytes. Date: 2026-09-15. Repo: `/home/user/work/GoPi`, branch
`fix/binary-summary-guard`.

## 1. The reported symptom

The captured file is `data` per `file(1)` (150 NUL bytes, 8036 bytes). The
contamination is **one ledger row**:

```
#01a09f6d  01a09f6d-7753-7321-ba33-8b32b09213e7  [failed]  [auto] \x7fELF\x02\x01\x01…  (model deepseek-flash, source auto, created 2026-09-14 18:19:06)
```

Its stored `summary` column is `"5B6175746F5D20" + 7F 45 4C 46 02 01 01 …` — the
`[auto] ` prefix followed by 351 bytes of a Linux ELF64 executable image.

## 2. Root cause (measured, not inferred)

5-why, each step backed by evidence on this host:

1. **Why binary in the ledger?** The start hook stores
   `autoTaskSummaryPrefix + normalizeTaskSummary(briefing)` as the row summary
   (`internal/agent/taskregister.go`). The briefing here was binary.
2. **Why was the briefing binary?** The session's own first user message is
   `[EPIC GOAL]\n\x7fELF\x02\x01…` (`sessions/a3d34ed1-…/session.jsonl`, offset 366).
3. **Why was that the prompt?** `readInput` (cmd/agent/main.go) classifies a
   positional that names an EXISTING non-image file as a **text input file** and
   uses its bytes as the prompt. The run's `workdir` was
   `/home/user/workspace/peak-bin` — the directory the operator had just built
   `bin/gopi` into. The invocation was `gopi gopi help`: the positional `gopi`
   named the executable, so 351 bytes of ELF became the prompt.
4. **Why "help"?** The row's `run_cmd` is `gopi gopi help`; `help` was the
   operator's leak-check invocation, not a claim that help output is binary.
5. **Why did it reach the terminal?** `TaskList` rendered `t.Summary` verbatim
   (no text boundary anywhere on the store or render path).

**Why this is a general defect, not a one-off**: `<dir>/gopi` is the standard
built-binary path in this very workflow, and this directory sat on the
operator's PATH after any build — so "the prompt positional names a file that
exists" silently reads whatever that file is (an executable, a log, a PDF, a
core dump). The ledger then persists and renders it as a headline.

## 3. The fix — three layers, one contract

The contract: **a ledger summary/payload is TEXT.** Any layer can produce
arbitrary bytes; no layer may persist or display control bytes as a headline.

| Layer | File | What it does |
|---|---|---|
| Source | `cmd/agent/inputtext.go` + `readInput` in `cmd/agent/main.go` | A positional that resolves to a file is refused when it is not UTF-8 text (NUL byte or invalid UTF-8). fast error names the path, the offending byte offset and the raw byte window; the two deliberate overrides are named (`pass the prompt as an argument`, `@<file>`). |
| Store | `insertTaskRow` in `internal/tools/tasks.go` | Every row's summary/payload passes `sanitizeTaskSummary` / `sanitizeTaskPayloadText` at the ONE insert path (Rule 2: one writer), so no caller can plant control bytes — including callers added later. |
| Render | `TaskList`, `TaskShow`, `TaskProcessStatusReport` in `internal/tools/` | Sanitized again at display time, so rows written by an OLDER binary (or synced from a peer) render as text too — this is what makes `gopi task list > file` a text file. |

`sanitizeTaskSummary` (`internal/tools/tasksummarytext.go`):

- **identity for ordinary text** — CJK, emoji, quotes and punctuation pass
  byte-for-byte (a regression test pins this: rewriting a legitimate Chinese
  briefing would trade one display defect for another);
- tabs/CR/LF fold to a space (the row is the single-line headline);
- **text with a little corruption** → each non-text run is escaped IN PLACE
  (`…resolution[bin \x00]steps…`), so the surrounding words keep their shape;
- **a summary that IS a captured blob** (≥50% non-text bytes — the measured
  row: 160/160) → ONE marker, `[bin \x7fELF\x02\x01\x01\x00…]`, carrying the
  raw leading bytes so the magic number stays recognisable. Escaping it
  run-by-run was tried first and measured at 35 interleaved markers — noise,
  not a headline;
- rune-capped at 200 (`taskSummaryMaxRunes`, mirroring the harness's own
  `autoTaskSummaryMaxChars`) → always valid UTF-8, NUL-free.

**Deliberate design choice — never silently strip**: the escape keeps the
evidence in the row. A strip would leave a future reader unable to tell "a
binary was captured here" from "the summary was short" (Rule 15's spirit).
Payloads keep their line structure (a YAML/JSON briefing survives) and are not
length-capped; only their non-text bytes are escaped.

Observed after the fix, on a **copy of the live ledger** with the real CLI:

```
$ GOPI_MEMORY_DB=<copy> ./gopi task list --status failed > failed_tasks_fixed.txt
$ file failed_tasks_fixed.txt
failed_tasks_fixed.txt: Unicode text, UTF-8 text
$ tr -cd '\000' < failed_tasks_fixed.txt | wc -c
0
```

## 4. Failed-task survey (94 rows, `status='failed'`)

Read from `~/.gopi/memory.db` (all 94 rows; `reason` + `session_id` + the
current state of the artefacts each row names).

### 4a. Noise — capture artefacts, no work lost (no retry)

`[auto]` rows whose "briefing" was a bare word an operator or probe typed, plus
the reported row itself: `[auto] help env`, `[auto] status`, `[auto] config dir`,
`[auto] -z`, `[auto] t`, `[auto] Reply with OK` / `Reply with just: OK` /
`Reply with exactly: OK` / `GOPI-DO-SMOKE-OK`, the `[auto] Run the failing
deploy task` / `failing chat turn` fixtures (12 rows), and **`01a09f6d` — the
ELF row itself** (the prompt was the executable; nothing was attempted).

### 4b. ACCIDENTAL — not a code failure (no retry)

- **`01a092db` — "ORPHANED BY MY TEST"**: a 12s-timeout guard test SIGTERMed the
  worker that owned the row; the test PASSED (that was its assertion), the row
  was collateral. Its own reason says the reissue was **blocked by the pending
  cap (9 > 3)**. The queue is now at **3**, so the reissue is unblocked.
- **8 rows killed 2026-09-12 04:12-04:13** (`worker holder pid dead … signal,
  not OOM`): one event took out the whole worker cohort. Their subject matter is
  already on master (see 4d).
- **10 merger/resolve rows** (`resolve child failed to resolve the conflict: …`):
  merge automation, each naming a branch.

### 4c. GENUINE — the work was real and is NOT done

**The highest-value group is the code-review fix ladder** (all `llm`-sourced,
2026-09-11/12/13). I re-measured each named defect **on current master**:

| Failed row | Its defect | Measured now |
|---|---|---|
| `01a091fb-1cff` — TestCLIContractInventoryMatchesDispatch / main.go divergence | cmd/agent test build | **STILL RED** |
| `01a091fb-1e7d` — stylegate test fails on master (4 error-discard/Close sites) | internal/stylegate | **STILL RED** |
| `01a091ea` — orphan-crash-dump sweep cost on every start | perf | fixed (merged) |
| `01a091ed` — crash-dump backfill misreports live sessions | correctness | fixed (merged) |
| `01a08e7b` / `01a08e80` — `TestMemorySearchNonBlocking` flaky (~2/20) | flake | **GREEN on master (20/20, -count=20)** |
| `01a09950` / `01a0998c` — cmd/agent TestAgentSpawnGuard daemon-env isolation | test isolation | **GREEN — `agent_spawn_guard_env_isolation_test.go` is on master** |
| `01a095af` / `01a096b5` — worker log in task dir / rawlog test home leak | test isolation | **GREEN — merged** |
| `01a09587` — installed `~/bin/gopi-worker-up.sh` stale vs repo | installed artefact | **GREEN — the installed script carries `--worker-log-root` (4 hits)** |
| `01a098af` — master does not compile (missing `sync/atomic`, `remaining` out of scope) | build | **GREEN — master builds** |
| `01a09435` — `task list` cannot distinguish running / finished-unclosed / dead | display honesty | **GREEN — `taskStateSuffix` ships the honest labels** |
| `01a096c9` — `feat/tab-order-actuate-poc` | merge | merged (`f178840f`) |
| `01a0965e-b3e4` / `-c117` — [3/6] HP voice E2E, [6/6] node coverage | voice epic | **not verified** (needs HP/rpi5 — out of scope here) |

**Conclusion: the genuinely valuable failures are the two master-reds** — both
from the "code-review fix ladder" family, both still live. Everything else in
that family has since landed on master.

### 4d. Branch outcomes for the automation rows (no retry needed)

Every branch named by a failed merger/resolve row is **gone and archived** —
recovery tags present: `archive/fix/cwd-and-lifetime`, and the merge landed on
master (`b37b776f resolve merge fix/cwd-and-lifetime into master`). Same for
`feat/worker-graceful-drain` (`903fa1bf`), `feat/tab-order-actuate-poc`
(`f178840f`), `feat/job-family-t3` (`5452a3e0`), `feat/hippocampus-host`
(`35423b4e`), `feat/safe-merge-guard-answer` (`d75818e8`),
`feat/guard-test-daemon-env-isolation` (test on master). The 04:12-kill family's
outcomes are likewise merged (`0eeb6344 crashdump tail-read`, `36fe9404` the
orphan-sweep daily gate, `6a42b82d` the installed-launcher audit) — so those
rows are historical, not outstanding work.

### 4e. Retry candidates (ranked, all dispatched as their own tasks — see report)

1. **master is red: `cmd/agent` test build** — `go vet ./cmd/agent/` fails:
   `merger_verify_peak_child_test.go:71 undefined: config.PeakHoursEnv` (also
   `IgnorePeakHoursFlagSpelling` / `IgnorePeakHoursFlag`). The peak-guard
   cost-class rework never landed; its branch was archived, but the tests
   referencing the new constants ARE on master. This blocks the whole
   `cmd/agent` suite (the row `01a091fb-1cff` family, still open).
2. **master is red: `internal/stylegate`** — `TestStyleGate`: `stylegate
   errorf-verb: 10 production hit(s), ceiling 9` (plus `TestSetenvScopeIsPerCall`,
   `TestScanFixtureRules`, `TestChdirRuleFlagsStatementPositionChdir`,
   `TestNilSafetyIsObjectScoped`). This is the row `01a091fb-1e7d` family,
   still open.
3. **`01a092db`** — reissue now that the pending queue is at 3 (its own recorded
   blocker: `9 pending > cap 3`).
4. **`01a0965e-b3e4` / `-c117`** — the voice epic's HP/node-coverage legs;
   retry only where the HP/rpi5 testbed is reachable.

## 5. Files

- `internal/tools/tasksummarytext.go` (new) — the sanitizer + its contract.
- `internal/tools/tasksummarytext_test.go` (new) — identity-for-text, in-place
  escape, blob form, rune cap, and two end-to-end ledger tests (write path +
  legacy raw row; `task show`).
- `cmd/agent/inputtext.go` (new) — the binary-file predicate + evidence renderer.
- `cmd/agent/main.go` — `readInput` refuses a non-text positional file.
- `internal/tools/tasks.go` — write-path guard in `insertTaskRow`; render-time
  guard in `TaskList` / `TaskShow` (summary, payload, children).
- `internal/tools/taskproc.go` — render-time guard in `TaskProcessStatusReport`.

## 6. Verification

- `go build ./...`: OK.
- `go test ./internal/tools/ -run 'TestTaskSummary|TestTaskListNeverRenders…|TestTaskShowNeverRenders…'`: 6/6 PASS.
- `go test ./internal/tools/` (full package): PASS.
- End-to-end on a copy of the live ledger with the rebuilt CLI:
  `file` → "Unicode text, UTF-8 text"; NUL count → 0; the offending row renders
  `[auto] [bin \x7fELF\x02\x01\x01\x00…]`.
- End-to-end refusal: `gopi elf.bin` → exit 1, stderr names the path, `\x7fELF`
  window and byte offset; a text file positional is still accepted.

## 7. Not done (honest)

- **The two master reds are NOT fixed here** (4e.1 / 4e.2): they are separate,
  well-scoped work and are dispatched as their own tasks. This branch measures
  them; it does not repair them.
- The 94-row survey classifies by `reason` + the artefacts each row names. It
  does not re-run the historical work of the 8 rows killed at 04:12 or of the
  voice-epic legs (`[3/6]`, `[6/6]`), which need the HP/rpi5 testbed.
- `cmd/agent`'s own suite cannot be run on this tree at all while the build is
  red — the layer-3 refusal is verified by the real-binary end-to-end above
  instead of by a `go test ./cmd/agent/` case (that case belongs to the repair
  task, which will have a compilable tree).

## 8. Environment note

`/dev/shm` was **100% full** during this task (5.7G in `gopi-test-scratch`,
including a 4.0G `queue/` subtree), so Go builds and test scratch failed with
`no space left on device` / `disk I/O error`. Work ran with
`GOPI_TEST_HOME=/home/user/workspace` (scratch off the full tmpfs) — the scratch
GC did not reclaim the stale `queue/` subtree. Worth its own task.
