<!-- doc-class: historical -->
# Task Report: session accounting counts SESSIONS, not `.jsonl` files

Task: `cmd/agent: TestWebToolsSignalShutdownWritesSessionEnd counts signal-provenance.jsonl as a session — pre-existing red`
Branch: `fix/web-signal-session-accounting` (worktree `/home/user/workspace/gopi-session-accounting`, base = master `571c3d30`)

## Completed

- **Root cause** (reproduced, not guessed): `testutil.LoadSession` collected **every** `*.jsonl` one level
  deep under `<sandbox>/.gopi/sessions` and `t.Fatalf`ed on `len(files) > 1` with the message
  *"N session files … a single-session run must write exactly one"*. A session dir legitimately holds
  `signal-provenance.jsonl` (`internal/signalrec.FileName`) **beside** its `session.jsonl` — the SIGTERM
  provenance record of the SAME session's process, written by `signalrec.Install` (panic-safe, so it
  survives the very signal the test sends). One session therefore looked like two.
  - **The bug was broader than the epic's single test**: `testsuite.TestTask_BarePrompt_SIGTERM_WritesSessionEnd`
    was red on the same base for the same reason (verified: `session: 2 session files …`). Both are fixed by
    the one accounting change.
- **Fix** — the unit counted is the SESSION, grouping is by **layout**, and it can never degrade into
  "count nothing":
  - one level down = session **DIR** form: the record is `session.jsonl`, or a single `*.jsonl` run file
    (task-run child). Other `*.jsonl` there are side files: a **known** name (`signal-provenance.jsonl`) is
    ignored with a `t.Logf` naming it; an **unknown** name is reported in the sessions list as
    `"<name> (unknown .jsonl)"` — a name allow-list, never a blanket "extra file is fine".
  - directly under the sessions root = legacy **flat** form: every `*.jsonl` **is** a record (a flat layout
    has no session dir for a side file to live in), so a stray `signal-provenance.jsonl` at the root still
    counts as a session — no silent excuse. `sessions/process-<pid>/signal-provenance.jsonl` (a resident
    role with no session) is not a session: it has no record.
  - the file is **never** deleted or renamed to force green (that was the forbidden bypass).
- **Single source of truth on the test side**: the side-file list lives once in `internal/testutil`
  (`sessionSideFileNames`), *not* mirrored into `internal/signalrec` — testutil is imported by tests of
  nearly every package, so a production import of `signalrec` there would be a cycle risk; the regression
  test pins the list against the production constant (`signalrec.FileName`) instead.
- **Readiness probe shares the accounting**: `cmd/agent`'s hand-rolled `firstSessionJSONL` /
  `isSessionJSONLName` are replaced by the exported `testutil.SessionRecordPath`, so a test's readiness
  probe and its final assertion cannot disagree about what a session is (the hand-rolled probe also
  accepted the provenance file as "the session artifact").
- **Rules**: `jsonlSuffix` and `sessionRecordName` are named constants (Rules 1b/13 — `"session.jsonl"`
  was a bare literal in `AssertSingleSession`); `LoadSession`/`parseSession` take `testing.TB` so the
  regression test can observe the diagnosis without failing itself.

## Not Done

- **No production code changed.** The wave of production readers that also glob `*.jsonl` in a session
  tree (`internal/gopilog/reader.go`, `internal/gopilog/rawlog.go`, `internal/agent/crashdump.go`,
  `internal/agent`'s orphan-session scan, `internal/tools/memory_brew_run.go`) is **left as is**: each is
  a *reader* whose unit is a file it then classifies, not an "exactly one session" *assertion*; the epic's
  acceptance is test-side. They are unequipped to reject a future stray session artifact, and that framing
  is tracked as a queued follow-up task (see below).
- `cmd/agent`'s spawn-guard family (`TestAgentSpawnGuard*`, worker-lock fd 3 handshake) fails on this base
  **both** before and after the change. Verified pre-existing on the untouched base `d333382e`
  (worktree `/home/user/workspace/gopi-guardenv-prefix`) — unrelated to this fix, not touched here.
- A cross-package exported constant for the bare-session record name was **not** added (Rule 14's
  `toolnames` pattern applied to a NEW production constant): the production holders
  (`internal/agent`'s `bareSessionLogName`, `internal/gopilog`'s `bareSessionFileName`,
  `internal/tools/tasks.go`'s inline literal) are engine-internal and the testutil constant is the only
  test-side consumer. Rationale recorded here so a future agent does not "re-add" it as a gap.

## Verification

- **Repro (pre-fix, base worktree)**:
  `go test -count=1 -run TestWebToolsSignalShutdownWritesSessionEnd ./cmd/agent/` → FAIL
  `session: 2 session files … [<uuid>/session.jsonl <uuid>/signal-provenance.jsonl]`;
  same failure for `./testsuite/ -run TestTask_BarePrompt_SIGTERM_WritesSessionEnd`.
- **Fixed (live binary rebuild first)**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK;
  `go test -count=1 -run TestWebToolsSignalShutdownWritesSessionEnd ./cmd/agent/` → `ok 0.730s`;
  `go test -count=1 -run TestTask_BarePrompt_SIGTERM_WritesSessionEnd ./testsuite/` → `ok 0.241s`.
- **Regression tests** (new, `internal/testutil/session_accounting_test.go`):
  `TestSessionAccounting_SideFileIsNotASession` (record + `signalrec.FileName` = ONE session, and the
  legacy file-count still reports 2 on the same fixture — the executable contrast),
  `TestSessionAccounting_LeakedSessionIsStillCaught` (two session dirs → fatal naming both),
  `TestSessionAccounting_UnknownSideFileIsReported`, `TestSessionAccounting_LayoutKeying` (flat form, flat
  stray, task-run dir, process-scoped provenance dir), `TestSessionRecordPath`.
- **MUTATION red-proof (load-bearing, measured)**: with the fix reverted to the file-counting loop, the
  regression tests go RED — `found 2 sessions, want 1 …` (side file) and `found 1 sessions, want 0 …`
  (`process-4242/signal-provenance.jsonl`); restored, they PASS. The test is not vacuous.
- **Suites**: `go test -count=1 ./internal/testutil/` → `ok 17.364s`; `./internal/standards/` → `ok 5.256s`;
  `./internal/sessionevent/` → `ok`; `./internal/signalrec/` → `ok`; `./testsuite/` → `ok` (see commit note).

## Files

- `internal/testutil/session.go` — session-record accounting (`findSessionRecords`,
  `sessionRecordInDir`, `sessionRecordList`, `SessionRecordPath`), `jsonlSuffix` + `sessionRecordName`
  constants, `testing.TB` signatures.
- `internal/testutil/session_accounting_test.go` — new regression + red-proof fixture.
- `cmd/agent/cli_web_align_test.go` — readiness probe uses `testutil.SessionRecordPath`; hand-rolled
  scanners deleted; the assertion carries the note on why `signal-provenance.jsonl` is not a session.
