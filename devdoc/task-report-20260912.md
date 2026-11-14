<!-- doc-class: normative -->
# Task Report: bounded task-report seam (epic task-report)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

**Branch** `feat/task-report` (worktree `~/workspace/gopi-taskreport`) · **base** master `8aa1f9b5`

Goal: every `gopi task run` leaves a BOUNDED, human-readable report in its OWN
session dir, written at a DETERMINISTIC time by the HARNESS, and
`gopi task show <id>` surfaces its content or an explicit path — so an operator
can answer "what did this task actually do?" without opening a 10k-line jsonl.

---

## 1. The seam (three faces, all keyed on the task id)

| face | where | what it does |
|---|---|---|
| HAND-OFF | `cmd/agent/cli_tasks.go:1422` | resolves the report path and exports `taskreport.PathEnv` on the child's env BEFORE spawn |
| TRANSPORT | prompt contract (`internal/agent/loop.go:657`, `taskManagementRules` rule 5) | the executor writes its narrative to that path as its wrap-up artifact — the file survives the child's death |
| SEAL | `cmd/agent/cli_tasks.go:1693` → `internal/tools/task_report.go:98` | at the run's terminal writeback: bounds the report, writes an honest skeleton when the model wrote nothing, and stamps path+size+missing-sections into the run's result digest |
| READ | `internal/tools/tasks.go:2027` → `TaskReportShowBlock` | renders the body (bounded) in `task show`, or the explicit path / honest degradation line |

Single source of truth for the shared values: **`internal/taskreport`** (new
leaf package, stdlib only — no project imports, so `internal/tools`,
`internal/agent` and `cmd/agent` all import it without a cycle; the
`internal/tasknames`/`tasktags` precedent, Rule 14).

```go
// internal/taskreport/taskreport.go
FileName         = "task-report.md"        // :38
PathEnv          = "GOPI_TASK_REPORT_PATH" // :49
MaxBytes         = 32 * 1024               // :55
ShowBodyMaxBytes = 8 * 1024                // :61
DigestLinePrefix = "task-report: "         // :67
RequiredSections = [## Completed, ## Not Done, ## Verification, ## Files]
```

Both consumers reference them, never re-type a literal (Rules 1b/13):
`cmd/agent/cli_tasks.go:1423` (`taskreport.PathEnv+"="+reportPath`) and the
prompt gate, which interpolates `taskreport.PathEnv` + `taskreport.MaxBytes`
(`internal/agent/loop.go:657-636`) — a typed-in path or cap in the prompt would drift from the
writer/seal that enforce them. `internal/agent/prompt_test.go:286` asserts the
prompt renders `taskreport.MaxBytes/1024`, so the drift is a red test.

### Session-dir creation (the "own session dir" requirement)

`TaskReportPath` never joins `"sessions"/<id>` by hand — it delegates to
`taskSessionDir` (`internal/tools/task_session.go:143`), which joins
`config.GopiDir()` with `taskSessionsRootName` (`= config.SessionsDirName`,
`task_session.go:71`) and runs the legacy-root migration. One root, one key: the
report can never disagree with the attachments area, the worker output log or
the session JSONL about where a task's artifacts live. The dir is created by the
seal (`os.MkdirAll(filepath.Dir(path), 0o755)`, `task_report.go:104`), so a run
that dies before the child ever writes still leaves a directory + a report.

---

## 2. Design verdicts

### A. Harness-supplied path in the environment — ACCEPTED

The child is HANDED the absolute path (`cmd.Env` at
`cmd/agent/cli_tasks.go:1423`), rather than deriving it. Rationale: the session
directory is a harness-side fact — relocatable by `GOPI_HOME`, migrated from the
retired `task-sessions` root, prunable. A model that guesses the path writes its
report where nobody reads, and the leak is silent. `cmd.Env` (not `os.Setenv`)
because the child reads its env at auto-registration, milliseconds after spawn.

*Rejected:* having the model derive `<GOPI_HOME>/.gopi/sessions/<id>/…` from the
briefing. It re-derives a fact the harness owns, breaks under `GOPI_HOME`
overrides and legacy-root migration, and makes the acceptance test depend on the
model's path arithmetic instead of on the seam.

### B. Seal at the terminal writeback (harness) — ACCEPTED, with "never clobber"

`SealTaskReport` is called first thing in `writebackOutcome`
(`cli_tasks.go:1693`) — the single shared completed/failed exit — so the report
is written at ONE deterministic moment regardless of what the model did. Order
inside the seal is load-bearing: read first, skeleton only when the file is
absent or whitespace-only (`task_report.go:105-116`), so a model-authored report
is bounded and otherwise left byte-identical (`TestSealTaskReportPreservesModelReport`).

*Rejected:* sealing in the child's own wrap-up. The child can be SIGKILLed, can
crash, or can exit non-zero — the report is exactly the artifact an operator
needs then, so it must not depend on the child reaching its own last statement.

*Rejected:* letting `task show` synthesize a report on read. That fabricates
history for rows that never had one and re-derives facts at read time.

### C. Bounded, and the bound is recorded — ACCEPTED

`taskreport.Bound` caps the file at `MaxBytes` on a line boundary and appends an
explicit truncation marker; `taskreport.TruncateBody` caps the *displayed* body
at a smaller `ShowBodyMaxBytes` (show is a terminal digest; the path is printed
right above the body for the operator who wants every byte). A silently
shortened report would read as the author's own ending (Rule 15), so both cuts
announce themselves.

*Rejected:* no cap ("the model will keep it short"). An unbounded report defeats
the seam's entire reason to exist.

### D. Record the path in the ledger, degrade honestly — ACCEPTED

The seal returns a digest line (`taskreport.DigestLine`) which
`writebackOutcome` appends to `resultSummary` → the append-only
`task_events.detail` (`internal/tools/tasks.go:359`). The ledger — not the
filesystem — is therefore the index that survives session-dir pruning, and
`task show` renders the recorded path when the body is gone
(`TaskReportShowBlock`'s `PathFromDigest` branch).

Degradation is explicit and distinguish-ish by evidence, never pretense:

| situation | `task_report:` line |
|---|---|
| body present | path, then the bounded body, `+ task_report_warn:` if a required section is missing |
| file empty (model wrote nothing, seal never ran) | `<path> (empty — the model wrote no report)` |
| body pruned, path in the ledger | `<path> (not found — recorded at run end; file pruned or removed)` |
| pre-seam row, no ledger record | `no report recorded (pre-2026-09-12 task)` |
| post-seam row, no ledger record | `no report recorded` |

*Rejected:* backfilling historical rows with a fabricated report. "No fake
backfill" is the epic's explicit constraint — a synthesized narrative for a task
that never wrote one is worse than an admission. `LegacyCutoffUnix` is keyed to
the seam's ship date, and an UNKNOWN timestamp (0) is treated as post-seam: an
unknown must not be dressed up as history.

---

## 3. Verification (all on this tree)

**Build / format**

```
$ go build ./...        → exit 0 (clean)
$ gofmt -l .            → (no output)
$ go build -o bin/gopi ./cmd/agent/   → BUILD_OK
```

**Tests** (`-count=1`; job-5 run of the touched packages)

```
go test -count=1 ./internal/taskreport/ ./internal/tools/ ./internal/agent/ ./internal/standards/
  ok  github.com/the operator/gopi/internal/taskreport
  ok  github.com/the operator/gopi/internal/tools
  ok  github.com/the operator/gopi/internal/agent
  ok  github.com/the operator/gopi/internal/standards
go test -count=1 ./cmd/agent/                          → ok   82.321s
go test -count=1 -v -run TestTaskRunSeam ./cmd/agent/
  --- PASS: TestTaskRunSeamWritesReportAndShowRendersIt (1.13s)
  --- PASS: TestTaskRunSeamFallsBackToSkeleton (0.18s)
```



**Real run E2E** (sandboxed `GOPI_HOME=/dev/shm/gopi-report-e2e`, binary built
from this tree; `GOPI_ALLOW_AGENT_SPAWN=1` is the documented one-off override for
driving `task run` from inside an agent session)

```
$ ./bin/gopi task submit "e2e: write a hello file then report" --task-id t-report-real …
Task queued: t-report-real (status=pending, source=human)

$ ./bin/gopi task run t-report-real        # 29s, one bash tool call + one wrap-up
$ ls -la /dev/shm/gopi-report-e2e/.gopi/sessions/t-report-real/
f4f2e987-2b70-418e-a96e-29c93c754e97.jsonl      39583
f4f2e987-2b70-418e-a96e-29c93c754e97.raw.log  1903586   ← the 10k-line jsonl nobody should read
metadata.json                                     274
task-report.md                                    642   ← the deliverable

$ cat …/task-report.md
# Task Report: e2e — write a hello file then report

## Completed
- Created `/tmp/gopi-report-e2e-hello.txt` containing exactly the single line `hello` (verified: 6 bytes, `cat -A` shows `hello$` — one line, trailing newline, nothing else).

## Files
- `/tmp/gopi-report-e2e-hello.txt` (created, 6 bytes)

## Verification
- `printf 'hello\n' > /tmp/gopi-report-e2e-hello.txt` → exit 0
- `ls -l` → `-rw-r--r-- 1 the operator the operator 6 …`
- `cat -A` → `hello$`  (single line, no hidden characters)

## Not Done
- Nothing deferred; no git repo/branch involved (artifact is a `/tmp` file, not a repo deliverable).

$ ./bin/gopi task show t-report-real
…
result:         gopi run completed — run: elapsed=29s — task-report: /dev/shm/gopi-report-e2e/.gopi/sessions/t-report-real/task-report.md (642 bytes)
task_report:    /dev/shm/gopi-report-e2e/.gopi/sessions/t-report-real/task-report.md
# Task Report: e2e — write a hello file then report
… (full body rendered) …
```

Provenance of the model-authored bytes (from the run's own jsonl): the ONE
tool_use that carried the env var is a `bash` call whose command echoes
`${GOPI_TASK_REPORT_PATH}` — i.e. the path came from the harness, not from the
model's guess. Report is 642 bytes ≪ 32 KB cap, and all four required headings
are present.

**Degradation, exercised on the real CLI** (sandbox sandbox, synthetic rows):

```
$ gopi task show t-report-legacy          # created_at backdated to 1780000000
task_report:    no report recorded (pre-2026-09-12 task)

$ gopi task show t-report-legacy          # body deleted, path recorded in task_events
result:         gopi run completed — task-report: …/t-report-legacy/task-report.md (512 bytes)
task_report:    …/t-report-legacy/task-report.md (not found — recorded at run end; file pruned or removed)

$ gopi task show t-report-real            # before the run: post-seam, nothing yet
task_report:    no report recorded
```

**Pre-existing, unrelated red:** `go test ./internal/stylegate/` fails in the
MAIN repo too (`d3dd268d`: 6 `time.Sleep` + 1 `t.Skipf` hits in
`cli_serve_test.go`, `cli_tasks_dead_holder_accept_test.go`,
`glm_night_watch_test.go`, `claim_holder_freshness_test.go`). None of those files
is touched by this branch (`git diff --name-only HEAD -- <file>` → clean) and no
hit names a file this branch adds. Not this task's business; left alone.

---

## 4. Honest limits

- **What the report guarantees:** at the end of a `task run`, a file exists at
  `<gopi-dir>/sessions/<task_id>/task-report.md`, ≤ `MaxBytes` + marker bytes,
  carrying the four required headings, and its path is in the ledger. That is
  guaranteed by the harness (seal), not by the model.
- **What it does NOT guarantee:** that the *content* is true. A model can write
  a confident false narrative inside a perfectly-shaped report. The skeleton
  path is the honest one (the harness states only the facts it owns: task_id,
  status, session_id, branch, commit, created_at, each rendered `(unknown)` when
  unresolvable — never invented). The report is a *narrative*, not evidence;
  verification evidence stays `task show`'s `result:` line + the ledger row.
- **Empty/garbage model output:** an absent or whitespace-only file is replaced
  by the skeleton, with `SkeletonPlaceholder` ("not recorded — the model wrote
  no report…") on every required section — an explicit hole, never a fabricated
  "what was completed". A file with content keeps its content, even if it is
  junk or missing sections: the seal bounds it, records the missing sections in
  the digest and `task show` prints `task_report_warn:` — the operator sees the
  gap in the same output as the report.
- **Best-effort by contract:** a seal failure (unwritable dir, unreadable file)
  is logged to stderr and the run's terminal writeback proceeds WITHOUT the
  digest line. The outcome is the authoritative fact; the report is its
  narrative. A failing report must never fail a task.
- **Pruning:** session dirs are prunable. After pruning, `task show` can only
  print the recorded path + "not found" — the file itself is gone. The ledger
  line is the durable half.
- **Only `task run` vehicles:** the env export lives on the `task run` spawn
  path. A child started by some other vehicle (direct `echo … | gopi`) gets no
  `GOPI_TASK_REPORT_PATH`, so its prompt contract simply does not fire and no
  report file is expected — that is why rule 5 reads "with `GOPI_TASK_REPORT_PATH`
  set, the report written there".

## 5. Coordination

The requirement "report under the task's OWN session dir, no public flat area"
is satisfied by routing every path through `taskSessionDir`
(`internal/tools/task_session.go:143`) — the same helper `01a09257-fc64`'s
output-attribution work uses. No parallel path helper was introduced, so the two
epics cannot diverge about where a task's artifacts live.

## Not Done

- No backfill for historical rows (deliberate — the epic forbids a fake one).
- `internal/stylegate`'s pre-existing red (Rule 17 test-sleep/skip sweep) —
  untouched by this branch, fixed by whoever owns that sweep.
- No new tool was added (Rule 3): the read face rides `task show`, the write
  face rides the executor's existing `bash`/`write` tools.
