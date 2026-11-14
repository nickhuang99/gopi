<!-- doc-class: normative -->
# Task Report: a single-source test budget that NAMES a timeout

## Summary

`go test ./...` on this host printed
`FAIL github.com/nick/gopi/internal/tools 600.3xx s` — Go's **default
per-package wall-clock budget** expiring, phrased exactly like a failing
assertion (Rule 15: a failure must name itself). The fsync root cause was
already fixed upstream (`devdoc/suite-hang-fix-t1788361069.md`). This task
closes the remaining half: **one declared budget in one place**, a run that
stamps **wall time + load average**, a budget trip reported as **TIMEOUT** with
its own exit code — never as a bare FAIL — and an assessment of the residual
`internal/tools` wall time, which turned out to have a **real, measured,
non-load cause** (see §2).

Deliverables: `scripts/test-budget.sh` (the mechanism), `make test` /
`make test-budget` (the entry points),
`internal/testutil/testbudget_gate_test.go` (the anti-regression gate), and the
load-sensitivity baseline + recipe below.

## Completed

- **`scripts/test-budget.sh`** — the single source of the per-package budget.
  - `test_budget="25m"` is declared **once** (Rule 1a) and is the only budget
    the repo passes to `go test -timeout`.
  - One env override, one reader (`GOPI_TEST_TIMEOUT`, Rule 2), validated as a
    Go duration *before* use, so a typo fails loudly instead of silently
    falling back to Go's 10m default.
  - Runs `go test -count=1 -timeout "$test_budget" -json` and classifies the
    run: **green (0) / fail (1) / usage (2) / budget expired → TIMEOUT (3)**.
  - On a trip it prints a `TIMEOUT, NOT AN ASSERTION FAILURE` block whose
    evidence is split in two, explicitly labelled parts: **`the budget expired
    here:`** (the `panic: test timed out after …` line) and **`separate
    assertion failures in the same run (NOT the timeout):`**. The first
    acceptance run to trip the budget showed why: it printed ordinary
    `--- FAIL: …` lines directly under the timeout header, which reads as "these
    tests timed out". Both labels are pinned by the gate.
  - Extraction is **binary-safe** (`grep -a`): a NUL byte in the JSON stream
    made the pre-fix version print an *empty* evidence list — a silent failure
    inside the very message that exists to remove one (§4).
  - Stamps `time, host, pkg, budget, elapsed, load before, load after, cores,
    exit, outcome, stream` and appends it to a stamp log, so the numbers live in
    an artifact instead of a chat message.
  - Keeps a **per-run copy** of the stream (`run-<epoch>.json`) next to the
    convenient `last.json`, and stamps its name. Without it a reported verdict
    loses its evidence as soon as the next run starts — measured here: the
    first loaded run's stream was overwritten and its numbers became
    uncitable.
- **`Makefile`** — `make test` routes through the script (`make test-budget
  PKGS=./internal/tools/` for a scope/flags override). A bare `go test ./...`
  silently re-inherits the 10m default that caused the incident, so the entry
  point every worker cites carries the budget.
- **`internal/testutil/testbudget_gate_test.go`** — five static gates that make
  the mechanism a build failure if it regresses: the budget constant exists and
  parses; `make test` routes through the script; every other committed workflow
  line that invokes `go test` names its own `-timeout` (exemptions named,
  reasoned, asserted to still exist); the TIMEOUT label + its two part-labels
  survive; the timeout grep stays binary-safe; the per-run stream copy is still
  made and named in the stamp.
- **Three pre-existing /tmp-ban reds fixed and one pre-existing Rule-17 red
  annotated** (see §5 — reproduced on the base commit, not caused here).

## 1. Load-sensitivity baseline (measured, this host)

Same tree, same command (`go test -count=1 ./internal/tools/` unless noted),
only the host load differs. `load` = 1-min value at start → end.

| Run | Budget | Wall time | load (start → end) | Outcome |
|---|---|---|---|---|
| quiet baseline (upstream `devdoc/suite-hang-fix-t1788361069.md`) | default 10m | 291 s | quiet | ok |
| loaded (upstream doc) | default 10m | **600.3 s** | 42 → 63 | FAIL (default budget expired) |
| loaded, raised budget (upstream doc) | 25m | 1123.8 s | 42 → 63 | ok |
| `internal/tools` alone, load ~13 | 25m | 290 s | 12.6 → 19.0 | 8 fixture reds — see §5, `GOPI_HOME` pin REJECTED |
| `make test`, load ~21 | 25m | 498 s | 13.8 → 21.8 | 4 reds, no trip |
| `make test`, load ~36 | 25m | 807 s (`internal/tools` **719.8 s, pass**) | 36.2 → 19.6 | 3 reds, no trip |
| `make test`, **load 34–53 (acceptance)** | 25m | **1721 s** — `internal/tools` **1501 s, killed by the budget** | **32.0 → 43.8**, mean 48.4, max 53.5 | **exit 3 — TIMEOUT, named** |

The same tree spans **290 s → 1123 s** (≈4x) with no code difference, so the
budget — not the code — decides the verdict. Two of these rows are the sharp
ones: at load 36 `internal/tools` ran **719.8 s and passed** (past the old 600 s
cliff — exactly the run that used to print a bare `600.3xx s` FAIL), and at load
~48 the same package was killed by the declared budget and the output **named
the timeout** instead of printing a FAIL (§3).

## 2. Is the residual `internal/tools` slowness real? YES — and it is not sqlite

This is the substantive finding of the task, and it contradicts the first
draft's conclusion ("host starvation, no residual cause"). It was found by
timing the slowest test's **individual calls**, not by reasoning about load.

**The tail is real, per-test, and reproducible at any load.** `TestTaskStampRun`
(one test, ~14 calls) took **74.6 s quiet**, 120.7 s at load 36, 252.7 s at load
40, **327.4 s** in the acceptance run. A probe that timed each call in that exact
sequence at load ~16:

```
STEP TaskNew                      82.4 ms
STEP TaskShow                     17.5 s       <- ×5 in the test
STEP TaskShow                     15.3 s
STEP TaskShow                     18.7 s
STEP TaskShow                     20.5 s
STEP TaskShow                     24.8 s
STEP TaskRunProvenance             4.1 ms
STEP TaskStampRun                 14.6 ms
STEP TaskUpdate                    4.7 ms
```

`TaskShow` costs 15–25 s per call while its **siblings on the same DB** (same
open, same insert) cost milliseconds. So it is not the DB open, not the ledger,
and not CPU starvation — it is what `TaskShow` *renders*.

**Root cause (A/B proven at equal load, one process).** `TaskShow`'s
session-lookup helpers walk the operator's whole sessions tree:

`taskSessionLogPath` → `taskFindSessionJSONL` → `os.ReadDir(sessions/)` +
`os.ReadDir(<each subdir>)` + `taskSessionStartID` (opens and reads the first
line of every `*.jsonl`), and `taskSessionLogPath`, `taskRawLogPathFromSession`,
`taskModelFromSession` each call it, with `taskBriefingFromSession` calling it
again — up to five full walks per `TaskShow`. On this host
`~/.gopi/sessions` holds **56,729 entries / 16 GB**; a measured full walk is
**~8 s** on a quiet disk.

| sub-step | ambient `GOPI_HOME` (56k entries) | `GOPI_HOME` = empty tree |
|---|---|---|
| `taskSessionLogPath` | 8.90 s | **30 µs** |
| `taskRawLogPathFromSession` | 8.64 s | **28 µs** |
| `taskModelFromSession` | 8.64 s | **24 µs** |
| `taskBriefingFromSession` | 9.65 s | 7.03 s (hardcodes `os.UserHomeDir()+".gopi"`, ignores `GOPI_HOME`) |
| `taskWorkerOutputLogLine` | 24 µs | 22 µs |
| `taskEventLatest` | 178 µs | 444 µs |
| `TaskReportShowBlock` | 54 µs | 139 µs |
| `attachmentsRaw` | 0.7 µs | 0.6 µs |

**≈300,000x** on the tree-scanning helpers, **nothing** on the DB-only ones —
the scan is the cost. The load still inflates it (8.9 s at load 25 vs an ~8 s
quiet walk), but load is a multiplier on a real 8 s scan, not the cause.

**Distribution (acceptance run, load mean 48.4, 1016 tests, per-test sum 1511.6 s
≥ package wall 1501.1 s ⇒ nothing was blocked):** 50.5 % of tests ≤ 0.2 s
holding 2.0 % of the time; 83.6 % ≤ 1 s holding 11.6 %; **top 10 tests hold
58.7 %**, headed by `TestTaskStampRun` 327.4 s, `TestTaskLinkageSingleRowNoTwoRowRender`
131.9 s, `TestTaskShow` 70.1 s, `TestTaskBackfillSessionEmptySIDNoop` 64.8 s.
Every member of the tail is a `TaskShow`-family test. median 0.200 s, p90 1.41 s,
p99 39.3 s.

**The fix candidates, and why none is committed here.** Fixing this means
changing how the sessions tree is located — an option that must not change
behaviour (Rule: never weaken a test, never guess a path):

1. **Suite-side `GOPI_HOME` pin — MEASURED AND REJECTED.** Pinning the suite's
   `GOPI_HOME` to an unpopulated tree takes `internal/tools` from 1501 s to
   **290 s** at load ~13 (5.2x), which is the strongest signal that this is the
   cause. It was implemented, run, and **rejected**: it reds **8 legitimate
   fixture tests** — `TestMemoryDBPathResolution`, `TestTaskShowSessionLogPath`,
   `TestTaskShowRawLogPath`, `TestTaskShowModelFromSession`,
   `TestTaskUpdateWithResultBackfillsModel`, and the three deliverable-branch
   backfill tests — which construct session fixtures under an explicitly pinned
   `GOPI_HOME`/HOME and assert exactly the path resolution being measured. A
   pin that makes 8 assertions unreachable is a weakening, so it was not taken.
   The measurement is recorded here as the cost/benefit the fix must earn.
2. **Production-side memo of the resolved session path** (the honest fix): the
   helpers re-walk up to five times for one render, and the exact-then-prefix
   lookup deserves a cheap negative cache (e.g. "this session id is not in the
   tree") so a miss does not re-walk 56k entries. It changes no semantics and
   would cut the tail by the full scan cost, but it is a behaviour change to
   path resolution and must ship with its own red-proof tests.
3. **Directory-shape widening** (the layout is `<id>.jsonl`, `<task_id>/<uuid>.jsonl`,
   `<uuid>/session.jsonl`) — a bounded amount of the walk can be turned into a
   direct `stat` of each candidate path; the current code only does that for the
   flat layout's exact match.

Follow-ups 2 and 3 are the real fix and are **out of scope for a reporting
task**; they are recorded with numbers, not smuggled in.

**Nothing is blocked and no lock/leak defect hides behind the tail.** The
upstream root cause (one stalled fsync holding modernc's process-wide mutex →
idle `connectionOpener` goroutines) is fixed; this run's stream carries no
timeout panic of that kind, per-test sums equal the package wall, and the
package's own fsync guard is green (see §5) with its ratio at 1.29x against a
12x limit.

## 3. The acceptance clause, both branches, measured

Acceptance: *"on a loaded host (>30 load) the run either passes, or the output
explicitly names the timeout."*

**(a) the run PASSES where the old default cap would have killed it** —
`make test` at load 36.15: 807 s wall; **`internal/tools` 719.8 s and pass**,
i.e. **20 % past the 600 s default** whose expiry is the incident; exit 1
(unrelated reds, not a timeout); budget never consulted.

**(b) the run TRIPS the budget and the message NAMES it** — `make test` at load
**32.0 → 43.8, mean 48.4, max 53.5** (171 samples, all ≥ 32; a synthetic load
generator held the host there): **1721 s** wall, `internal/tools` killed at
**1501 s**, and the output was:

```
================== TIMEOUT, NOT AN ASSERTION FAILURE ==================
The per-package test budget (25m) ran out — a WALL-CLOCK cap, not a failing
assertion. The cap applies per package: the package(s) still running when it
expired were killed and reported as 'fail'.

the budget expired here:
  panic: test timed out after 25m0s

separate assertion failures in the same run (NOT the timeout):
  --- FAIL: TestStyleGate (21.41s)
  --- FAIL: TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild (3.31s)
  --- FAIL: TestWorkerOwnershipPointerPresent (0.38s)
```

**exit 3** (not 1), stamp
`… pkg=./... budget=25m elapsed=1721s load_start=32.01 22.96 25.56 load_end=43.78 46.52 44.68 cores=4 exit=3 outcome=timeout stream=run-1789236279.json`.

The trip is **not** a load artifact alone: the package that was killed is the
one §2 shows carries a real 8 s-per-lookup cost, at load 48 on 4 cores (~12x
oversubscription). That is the case this deliverable exists for — and it reports
itself instead of printing a bare FAIL.

### The trip path exercised in isolation (20× smaller budget, seconds not minutes)

`GOPI_TEST_TIMEOUT=5s scripts/test-budget.sh ./internal/standards/` →
**exit 3**, the block above with `panic: test timed out after 5s`, and the
per-run stream preserved and named in the stamp. This is the cheap, repeatable
form of the same proof.

### Defect found IN the timeout path, fixed, with a gate

Exercising the trip path showed the block announcing a TIMEOUT with an **empty**
evidence list. Root cause (5-why, not a parameter tweak): the JSON stream
carried **one NUL byte** (a test feeding binary data to a tool through `-json`'s
`Output` events), so `grep` classified the whole multi-MB stream as *binary* and
printed nothing (`grep: … binary file matches` on stderr). Rule 15's silent
failure wearing the disguise of a correct message. A/B on a minimal 3-byte
repro:

| | `grep -o -- 'panic: test timed out…'` | `grep -a -o …` |
|---|---|---|
| 1-NUL-byte JSON stream | **no output** (exit 0 — silent) | `panic: test timed out after 20s` |

Fixed with `grep -a`, and `assertBudgetTimeoutReportIsBinarySafe` now fails the
build if that `-a` is reverted.

## 4. The recipe (running the suite on a loaded host)

```bash
# 0. PREREQUISITE: the CLI E2E guards (cmd/agent, internal/testutil) drive the
#    REAL binary through GOPI_BIN (<repo>/bin/gopi) and FAIL rather than skip
#    when it is absent — `make build` produces it. The script exports it when
#    it is there, and says so when it is not.
make build

# 1. Preflight: the scratch home must be memory-backed and off `/`.
scripts/setup-scratch-home.sh --dry-run
ls -ld ~/workspace/gopi-test-scratch      # -> /dev/shm/... on an unprivileged host
df -h ~/workspace/gopi-test-scratch       # must be tmpfs, and NOT the `/` device

# 2. Know how loaded the host is BEFORE interpreting any timing.
uptime; nproc; pgrep -c -f '[.]test'

# 3. Run the suite with the DECLARED budget (25m, one place):
make test                                    # whole module
make test-budget PKGS=./internal/tools/      # one package
GOPI_TEST_TIMEOUT=45m make test-budget PKGS=./internal/tools/   # headroom

# 4. Read the verdict by exit code, never by eyeballing the tail:
#    0 = green   1 = a test/build really failed   2 = usage   3 = THE BUDGET EXPIRED
#    A 3 is NOT a failing test; the script says so in words.
# 5. Quote the numbers from the stamp, and cite the stream it names:
tail -1 "<scratch>/queue/test-budget/stamps.log"
```

Measured cost of the recipe at load ~48: **1721 s** for the module
(`internal/tools` alone 1501 s, then killed). On a quiet host the same module is
~300-500 s. Budget headroom on a loaded host is a **time** cost, deliberately:
a loaded 4-core host can no longer manufacture a timeout verdict for a package
that was merely slow.

## 4b. The START RECORD: a run is queryable from its first second (§2 of the artifact contract)

**The defect this closes (measured 2026-10-13, task `01a12f82`).** The script
writes its per-run stream from the FIRST byte, but every artifact that NAMES that
stream — the `stamps.log` line, the `last.json` pointer, the printed summary —
is produced after `go test` RETURNS. A run killed mid-suite therefore left an
**orphaned verdict**: `run-1791789379-784665.json` held 113 `pass` and 2 `fail`
events (`fail internal/standards 150.298` plus seven named failing tests) while
`stamps.log` carried no line for it and `last.json` never pointed at it.
Recovering it required matching the killed job's jsonl TIMESTAMP against the
file's epoch — forensic re-derivation — and in the meantime that run's own
report quoted a *different, later, filtered* run as "the final suite". The
verdict survived; only the query that would find it did not. That is Rule 15's
silent failure at the level of the RUN, not of a test.

**The contract now.** One flat file per run, beside its stream, under the same
age-GC'd scratch queue (Rule 22 — no new tree, no new knob):

```
<scratch queue>/test-budget/run-<epoch>-<pid>-start.txt
--- start record: written BEFORE go test started (survives a kill) ---
start_iso=2026-10-13T00:02:23+08:00
start_epoch=1791820943
host=hostb-Laptop
pid=3727868
pkg=./internal/heavytest/
budget=25m
stream=run-1791820943-3727868.json
```

Four properties, each machine-checked (`internal/testutil/testbudget_start_record_gate_test.go`):

1. **Before `go test`.** `write_start_record` is called on the line before the
   redirect — the static gate locates both lines and refuses a record written
   after it (the ordering IS the property).
2. **Atomic.** Written to a temp name and `rename(2)`d into place. This is not
   tidiness: `>"$start_record"` creates the NAME the instant it opens, so a
   reader polling for it can find a TRUNCATED file that looks like a record and
   names nothing (measured while writing the gate — the poll caught a two-line
   file and the SIGKILL kept exactly those two lines). With the rename, a reader
   sees no record or the complete one.
3. **It survives SIGKILL** by construction: complete on disk before `go test` is
   exec'd, and nothing removes it on the way out. Measured (acceptance (a)): the
   run's process group was SIGKILLed at t+150 s, and the record naming
   `run-1791820676-3708808.json` was still there with its 134,793-byte stream
   beside it.
4. **It is NOT a second stamp.** A line is appended to `stamps.log` on
   COMPLETION and only on completion, so "is there a stamps.log line for this
   run?" still answers "did it finish?" — the two jobs stay in two places
   (Rule 14). The completed-run contract is untouched: the stamp line's field
   set and ORDER are unchanged, `last.json` is still a symlink published after
   the run seals its stream, and the exit codes are unchanged.

**How a reader uses it.**

```bash
Q="$(gopi_scratch_queue)/test-budget"    # scripts/lib/tmp-ban.sh
# every run — completed OR killed — is findable by its own record:
grep -l 'pkg=./internal/standards/' "$Q"/run-*-start.txt
# SELECT the run you are asking about FIRST (a queue can hold several orphaned
# runs, and `grep -l` over a glob then returns one path PER LINE — a multi-line
# value the next step cannot parse). The newest:
R="$(ls -t "$Q"/run-*-start.txt | head -1)"
# did THAT run finish? ask the completion artifact, never the record:
S="$(sed -n 's/^stream=//p' "$R")"
grep -q "$S" "$Q/stamps.log" 2>/dev/null \
  && echo completed || echo "KILLED - its stream is $Q/$S"
```

**Classification (Rule 35).** The record is a fourth FLAT evidence class in the
same tree whose contents are already defined (`internal/scratch/declared_budget_runs.go`),
beside `run-*.json`, the package lists and `stamps.log`. It is a few hundred
bytes per run, bounded the same way `stamps.log` is; the tree's bytes are its
age-GC'd `run-*-tested-bin/` directories (10.27 GB measured), against which the
records are noise. It is explicitly NOT a collection candidate, and the test
that pins that class states why: collecting a run's record would delete the
index of the very runs whose streams are the tree's record.

## 5. Not Done, honestly

- **The real `internal/tools` fix is not implemented.** §2 names it
  (production-side session-path lookup: a negative cache / direct stat instead
  of up to five full re-walks). It changes path-resolution behaviour and needs
  its own red-proof tests — out of scope here, recorded with measurements.
- The **`GOPI_HOME` suite pin was measured and rejected** (8 fixture reds); it
  is documented above rather than landed, because making assertions unreachable
  is a weakening.
- The `internal/tools`-specific 25m applies module-wide; a package that really
  hangs still takes up to 25m to say so (bounded, but slower than the 10m
  default would have been). That is the intended trade.
- The budget gate audits `scripts/`, `devdoc/`, `testdata/`; a future workflow
  outside those roots is not covered by the static gate.
- **`go test ./...` on this branch is green only where the base commit is
  green.** Four pre-existing reds remain and are **not** this branch's; each was
  reproduced on a detached worktree of the base commit (and, for two of them, on
  current master's tip):

| Red | reproduced on the base commit? | what it is |
|---|---|---|
| `internal/stylegate` `TestStyleGate` (ignored-error 198 vs ceiling 182; unchecked-close 8; test-sleep 11; test-skip 1) | **YES** — identical counts on base and on master's tip | master's own ceilings drifted below its tree; nothing in this branch touches those counts |
| `internal/testutil` `TestWorkerOwnershipPointerPresent` | **YES** — 22 doc files listed on base and on this branch, byte-identical sets | a doc-series gate red, independent of this task |
| `cmd/agent` `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | **YES** — same failure on the base commit and on master's tip, in a detached worktree with its own `bin/gopi` | E2E: the worker round claims + drains but never reports the session cwd |
| `internal/standards` Rule 17 `NEW=2` (`cmd/agent/cli_voice_review_test.go:1301`, `cmd/agent/worktree_release_accept_test.go:225`) | **YES** on base (`NEW=2`, same two sites) | **annotated here** with reviewed `env-leak-ok` reasons — both sites use the documented `testutil.SnapshotEnv` pair, whose `os.LookupEnv` restore is the only way to restore *absence* (`t.Setenv` cannot). `internal/standards` is therefore **green on this branch** and was red on the base. |
| `scripts/acceptance-self-release*.sh`, `scripts/acceptance-zombie-auto-dispose.sh`, `cmd/agent/cli_tasks_worker_test.go` /tmp literals + ambient `os.MkdirTemp` | arrived with the base merge | **FIXED here**: the three scripts source `lib/tmp-ban.sh` with trap-cleaned scratch trees; the probe binary builds under the pinned scratch root. `TestScriptsAreBoundedAndOffTmp` is green. |

I deliberately did **not** raise the stylegate ceiling or dismiss the two
external gates: "green by raising the bar" is exactly the weakening this epic
forbids. The consequence is stated plainly above.

## Files

| Path | Role |
|---|---|
| `scripts/test-budget.sh` | the single source of the budget, the binary-safe timeout report, the per-run stream copy (`+x`) |
| `Makefile` | `test` routes through the script; `test-budget PKGS=…` for narrower scopes |
| `internal/testutil/testbudget_gate_test.go` | anti-regression gate (budget exists/parses, `make test` routes, every workflow names a budget, the timeout parts stay labelled apart, the grep stays binary-safe, the run stream stays preserved) |
| `internal/tools/testsuite_temproot_test.go` | documents the measured root cause of the tail (the sessions-tree rescan) next to the guard that first exposed the suite hang |
| `cmd/agent/cli_tasks_worker_test.go` | pre-existing /tmp-ban red: probe binary built at the AMBIENT temp root → pinned scratch root |
| `cmd/agent/cli_voice_review_test.go` | pre-existing Rule-17 red on the base: reviewed `env-leak-ok` reason |
| `cmd/agent/worktree_release_accept_test.go` | same Rule-17 red: reviewed `env-leak-ok` reason |
| `scripts/acceptance-self-release.sh` / `-stop.sh` / `acceptance-zombie-auto-dispose.sh` | pre-existing /tmp-ban reds: `/tmp` sandboxes → registered, trap-cleaned scratch trees |
| `devdoc/test-budget-single-source.md` | this report |

## Verification

### Acceptance run (the epic's clause, branch (b) — the trip path)

| | value |
|---|---|
| command | `make build` → `make test` (= `scripts/test-budget.sh ./...`, budget 25m) |
| wall time | **1721 s** (stamp `elapsed=1721s`) |
| load average during the run | **min 34.23, mean 48.45, max 53.46** (171 samples, all > 30) |
| load at start / end | 32.01 / 43.78 |
| cores | 4 |
| `internal/tools` | **1501.1 s, killed by the budget** |
| exit / outcome | **3** / `timeout` — with the `TIMEOUT, NOT AN ASSERTION FAILURE` block |
| stream (citable) | `…/test-budget/run-1789236279.json` |
| verdict | **the output explicitly names the timeout — no bare 600 s FAIL** |

### Acceptance run (branch (a) — passing where the default would have failed)

`make test` at load **36.15**: 807 s wall, `internal/tools` **719.8 s, pass**,
exit 1 (3 unrelated package reds), budget never consulted. Stream
`…/test-budget/run-<epoch>.json`, stamp line 4 of `stamps.log`.

### Command-level checks

- `go build ./...`: **OK**; `make build` → `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` — OK.
- Budget gate: `go test -run
  'TestTestBudgetHasASingleSourceAndNamesATimeout|TestBudgetExemptionsStillExist'
  ./internal/testutil/` — **ok** (7 `go test` invocations found, all named or
  exempt by name).
- /tmp gate: `go test -run TestScriptsAreBoundedAndOffTmp ./internal/testutil/`
  — **ok** on this branch (red on the base merge).
- Standards gate: `go test ./internal/standards/` — **ok** on this branch
  (red with `NEW=2` on the base).
- `internal/tools` fsync guard: `-run
  'TestFreshDBOpenCloseIsFast|TestSuiteScratchRootIsMemoryBacked'` — **ok**,
  scratch root `tmpfs (memory filesystem)`, ratio **1.29x** against a 12.0x limit
  → the fsync attribution is ruled out for this host.
- `gofmt -l` on the touched Go files: clean; `bash -n scripts/test-budget.sh`:
  OK.

### Deliverable declaration (Rule 6d)

- **Branch**: `fix/test-budget-single-source` (linked worktree
  `~/workspace/gopi-testbudget`; the commit is in the shared repo — no push
  needed).
- **Recovery**: `git log --oneline fix/test-budget-single-source`;
  `git show fix/test-budget-single-source:devdoc/test-budget-single-source.md`.
