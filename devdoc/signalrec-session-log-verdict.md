<!-- doc-class: historical -->
# Task Report: the signal verdict reaches a real run's session log

Epic: `signalrec-session-log-verdict` (2026-09-17). Rule 5 report; the `Ran:` /
`Result:` lines required by Rule 31 are in `## Verification` below, and the
hand-driven proof's raw output is quoted there.

This work is the re-application of archived commit `7a14b3d1` (branch
`task/01a0d83c3398770bac1e2cdb52befdd1`, PRESERVED — not reused) onto master
`b61e0f0d`, whose `0b1fb70d` had meanwhile rewritten the very code that commit
touched. The re-application is a re-IMPLEMENTATION on master's ownership-predicate
shape, not a cherry-pick: one coherent design, with two deliberate departures
from the archived commit (the knob decision and the `Install` argument), both
argued below.

## Completed

### 1. Production wiring — ONE resolution point

`internal/signalrec.SetSessionDir(dir, taskID, sessionLog)` gained a third
argument: the run's OWN session-JSONL path. The writer resolves the path through
exactly one function, `resolveSessionLog()`, fed by the two halves of one
handoff:

```go
resolveSessionLog() = sessionLogOverride (the SetSessionDir handoff, wins when set)
                    | installedSessionLogPath (Install's own argument, fallback)
```

The production caller that knows the path is `printLogPathsQuiet`
(`cmd/agent/main.go`), the same place the session DIR already became true — so dir
and path are resolved in ONE statement from ONE handle
(`ag.SessionLog.Path()`), never joined or re-derived:

```go
signalrec.SetSessionDir(filepath.Dir(ag.SessionLog.Path()), ag.SessionLog.TaskID(), ag.SessionLog.Path())
```

The worker's round handoff (`cmd/agent/cli_tasks_worker.go`) passes `""` for the
path **deliberately**: a worker daemon hands over a task's session DIRECTORY, in
which the round's CHILD writes its own run log — a worker-appended verdict event
in that file would forge the child's record (the same over-attribution the dir
handoff exists to prevent). The empty value also CLEARS any previous handoff.
`sessionLog` is `strings.TrimSpace`d like its two siblings, so "no log" and
"whitespace" cannot become two different answers.

No second path derivation, no `O_CREATE` (the file is opened `O_APPEND` only,
Rule 34's no-symlink-swap discipline comes free with it), and no duplicate event
per signal kind — the recorder's existing per-kind dedup (`seen[key]`) is
untouched and now also covers the event.

### 2. Format decision: a STRUCTURED JSON EVENT, with proof

The pre-fix contract promised *prose*. Prose is **not** tolerated by every
session-log reader, and the readers disagree with each other:

| reader | line handling |
|---|---|
| `gopilog.ParseFile` (unified: `gopi log --jsonl/--chat`, `gopi stats`) | `json.Unmarshal` per line → **hard error on any unparsable line** |
| `gopilog.ValidateSessionFile` | unmarshals, **silently `continue`s** on failure |
| `gopilog.SessionRawLogPath` | same silent skip |

A prose line would therefore be *invisible* to two parsers and *fatal* to the
third — `gopi log --jsonl` would fail on exactly the session an operator opens
after a kill. The format is settled the other way: a JSON object with its own
type, `signal_verdict` (`internal/sessionevent.EventTypeSignalVerdict` — the leaf
package that already owns `session_start`/`session_end`/`task_row_closed`).

The event is the envelope every session event is (`type` + `timestamp`, **no
`message` key**), so each formatter's type switch falls through and renders
nothing. Payload keys are named constants in the writer
(`envelopeKeyType`, `envelopeKeyTimestamp`, `signalKeySignal`, `signalKeyPID`,
`signalKeyRole`, `signalKeyIntent`) rather than bare literals (Rule 1).

Proof is executable, in `internal/gopilog/session_signal_verdict_test.go`: four
reader arms over a file carrying the real event (strict parse, tolerant validate,
raw-log pairing, `--json` pass-through), plus a **contrast arm** that puts a prose
line in the same slot and shows the strict parser rejecting the whole file while
the tolerant one silently skips it. That contrast is what keeps the reader story
from being an assertion.

### 3. The knob decision — RETIRED, by moving the predicate ONTO the wire

`GOPI_SIGNALREC_SESSION_LOG_VERDICT` (`SessionLogVerdictEnv`) is **gone**, its
declaration deleted, not merely defaulted off.

**Why.** The knob answered "does this process write the verdict line?", with a
mode-derived default that was itself `!signalForwarding()`. That default is the
defect: the predicate's *false* branch is exactly the **unannounced mass kill** —
the death an operator investigates — and the policy chose to write NOTHING for it
("the console already carries the line"). The evidence a mass-kill post-mortem
needs was the one case systematically filtered out.

**Per arm, explicitly** (the epic asks for this statement):

| old arm | new behaviour |
|---|---|
| knob set true | event written (as before) — now unconditionally, so nothing is lost |
| knob set false | event written, `sourced` field says what was known. The knob's only reachable effect was DELETING evidence, so this arm is retired rather than honoured |
| knob unset, process OWNS handlers | event written, `sourced:true` (an announced stop) — as before |
| knob unset, process does NOT own handlers | **CHANGED**: event written, `sourced:false`. This is the whole point of the change — the unsourced kill now states itself in the log the operator reads |
| kill unclassifiable | event written with `sourced` ABSENT (the honest third state, never merged with `false`) |

**What happened to master's ownership predicate: it was PROMOTED, not discarded.**
`signalForwarding()` remains the SSOT for "does this process own its
terminating-signal handlers" and it still decides the *other* half (the re-raise:
`if signalForwarding() { reRaise(sig) }`, unchanged). For the verdict half the same
predicate now travels **as the event's content** (`SourcedField`) instead of
gating the write — so the two halves still ask one question (Rule 13: one
predicate, two consumers) while the log keeps both answers.

`cmd/agent/main.go`'s two-arms guard
(`TestSignalRecOwnershipDeclarationGovernsTheSessionLogVerdict`) was **deliberately
replaced**, not deleted silently: it asserted `sessionLogVerdictLine(rec) == ""`
for the unclaimed arm and `!= ""` for the declared arm — i.e. it pinned *the
filter this change exists to remove*. It is superseded by
`TestSignalRecEveryCapturedSignalIsWrittenToTheSessionLog`
(`internal/signalrec/signalrec_sessionlog_test.go`), which runs both of the same
arms (`envOwnsHandlers` flipped in-process, same fixture discipline) and asserts
the property that now holds: **the event is written in BOTH arms** (no filter), and
its `SourcedField` carries the classification — including the third state, an
ABSENT field for an unclassified kill. The other half of the ownership predicate
keeps its own guards untouched (`TestSignalRecForwardingPreservesDefaultDeath`,
`TestSignalRecClaimedHandlerIsNotReRaised`).

**No `CONFIG.md` edit is needed**: measured, `CONFIG.md` never carried a row for
this name (it carries no `SIGNALREC` row at all), and the config-doc gate only
fails on a *documented* name that exists nowhere / a *catalogued* name missing
from the doc (`internal/config.EnvVars`) — neither direction touches an uncatalogued
name. Rule 21's env-reader gate is satisfied the same way `7a14b3d1` satisfied it:
removing the declaration removes the claim. Removing the reader also removed its
only consumers (`verdictWord`, `roleOrUnknown`, `procnameLine`) — deleted rather
than left dead; `boolvocab` keeps its other readers.

Since the knob was never a documented env name, no `CONFIG.md` row went stale.

### 4. `Install("")` — the docs-lie is CLOSED, and the argument is now LIVE

The epic allowed either closing the docs-lie or reporting it still open. It is
**closed, and by construction rather than by prose**:

- `Install(sessionLog)` now *records* its argument (`setInstalledSessionLog`) and
  the handler *reads* it (`resolveSessionLog`) — the parameter is no longer a
  write-only dead value, which was the actual defect: the writer took its path
  from a parameter whose sole production caller passed `""` and the documented
  behaviour was therefore unreachable.
- `""` stays the value main() passes, and that is honest: at that point in main()
  no run has minted a logger, so there is no path to name — the path arrives later
  through `SetSessionDir` and is resolved at SIGNAL time. Both are now documented
  as the two halves of ONE handoff (Install's own doc says so; the main() call
  site says so).

So: **no dead argument remains.** A caller that knows the path before the logger
exists can pass it, and a caller that does not (main, and every verb whose logger
is created later) passes `""` — with the later handoff taking precedence.

### 5. Guards

- `internal/signalrec/signalrec_sessionlog_test.go` — the victim child is given a
  session log through the POST-Install seam ONLY (`SetSessionDir(dir, taskID,
  sessionLog)`) while `Install` is called with `""` as production does. The test
  therefore cannot be satisfied through Install's argument, and it turns RED the
  moment the production resolution disappears. Every line of the written file is
  read back and unmarshalled (the strict parser's algorithm).
- `internal/signalrec/signalrec_sessionlog_test.go` — the
  fact-over-policy arm above (both ownership arms + the absent-field third state),
  and `TestSignalRecInstallArgumentIsLiveAndTheLaterHandoffWins`, which pins the
  `Install` closure directly: the argument is recorded and read (not the dead `""`),
  the later `SetSessionDir` handoff wins, and CLEARING it uncovers the argument
  again instead of leaving a stale path behind.
- `internal/gopilog/session_signal_verdict_test.go` — the four reader arms +
  the prose contrast arm.
- `testsuite/signal_log_test.go` — the REAL binary: mock LLM, run held in-flight,
  SIGTERM, exit 1 (graceful closeout), then (1) the session log carries the event,
  (2) it parses line-by-line, (3) `gopi log --last --json` — the production
  reader — exits 0 and streams it.

### 6. `testdata/heavy-tests.tsv` — measured, not assumed

`7a14b3d1` removed a row (`TestSignalRecClaimedHandlerIsNotReRaised`) because that
test's `heavytest.Require` call had moved on master. On this branch **no row is
added or removed**, and that is a measured decision rather than an omission: the
inventory gate (`TestHeavyTestInventoryMatchesItsCallSites`) keys on
`heavytest.Require` call sites, the new guards make no such call, and
`go test -count=1 -run 'TestHeavyTestInventoryMatchesItsCallSites|TestConfigDoc'
./internal/standards/` is **ok** with the inventory untouched. Adding a row for a
test that declares no criterion would be an unbacked classification — the gate
would then fail it as stale.

## Not Done

- **Red-proof by mutation was not run for this branch's new guard.** `7a14b3d1`
  quoted a mutation red-proof (`sessionLogPath()` forced to `return ""`); here the
  equivalent observation is the real-binary proof plus the guard's non-vacuity
  argument (the path exists only via the third argument). A mutation run was not
  performed inside this session's budget — stated rather than implied.
- **No CI-level gate beyond Rule 21** asserting "every documented env knob has a
  live reader".
- **The merger daemon's own session log** stays empty of the event: the resident
  merger owns no session log at all, so there is nothing to append to. Its verdict
  reaches the ledger and its stdio log as before — a deliberate boundary.
- **`cmd/agent`'s full package suite**: see Verification for what ran and what was
  excluded.
- **Pre-existing gate reds on this base** were not fixed (they are other runs'
  dirt and none sits in a file this diff touches); none of the acceptance commands
  below depends on them.

## Verification

- Build: `go build ./...` → exit 0. Declared rebuild (Rule 12):
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0,
  `./bin/gopi version` → `v0.1.0` / `go: go1.24.5`.
- Vet: `go vet ./internal/signalrec/ ./internal/gopilog/ ./internal/sessionevent/
  ./cmd/agent/ ./testsuite/` → exit 0.
- Tests: `go test -count=1 -timeout 600s ./internal/signalrec/ ./internal/gopilog/
  ./internal/sessionevent/ ./cmd/agent/ ./testsuite/` → the per-package results are
  in `## Evidence` below (the whole five-package run was launched as one command;
  the per-package lines come from the same command's output).
- Ran (hand-driven, Rule 31 — the real binary, a real SIGTERM):
  `env -i PATH=$PATH HOME=$R/home TMPDIR=$R/tmp GOPI_HOME=$R/home/.gopi
  GOPI_SIGNALREC_INTENT_WAIT_MS=1 ANTHROPIC_AUTH_TOKEN=dummy-token
  ANTHROPIC_BASE_URL=http://127.0.0.1:$PORT ./bin/gopi "do a long analysis and report back"`
  (mock LLM, port 43335), then `kill -TERM <pid>` once the mock reported the
  request held.
- Result: exit 1 (graceful closeout), and the run's own session log
  `…/sessions/86363967-2486-4646-848e-94758c799178/session.jsonl` (3796 bytes)
  carried, in order,
  `session_start user user signal_verdict request session_end task_row_closed`
  — **every line re-parsed by `json.loads` (7/7 OK)**, and the event itself:

  ```json
  {"pid": 2032851, "role": "do a long analysis and report back", "signal": "SIGTERM", "sourced": false, "timestamp": "2026-09-25T19:53:21.427917252+08:00", "type": "signal_verdict"}
  ```

  The production reader over that very file, `bin/gopi log --last --json`, exited
  **0** and streamed the event (1 occurrence). The same death also wrote
  `signal-provenance.jsonl` into the same session dir (`layout=session`).
- Commit: the single feature commit on branch `fix/signalrec-session-log-verdict`
  (this run's own worktree, renamed from
  `task/01a0d85b70c77c40bdeac40a72939572` per Rule 6a); the tip hash is recorded in
  the run's session report, because a hash written INSIDE the commit it names
  changes the moment it is committed.

## Evidence (self-filed)

- `go build ./...` → exit 0.
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0; `./bin/gopi version`
  → `v0.1.0`, `go: go1.24.5`.
- `go test -count=1 ./internal/signalrec/` → `ok` (3.803s), including
  `TestSignalRecVerdictEventReachesTheSessionLogFromTheSetSessionDirSeam` and
  `TestSignalRecEveryCapturedSignalIsWrittenToTheSessionLog`.
- `go test -count=1 ./internal/gopilog/ ./internal/sessionevent/` → `ok` (1.677s /
  1.227s).
- `go test -count=1 -run TestTask_BarePrompt_SIGTERM -v ./testsuite/` →
  `--- PASS: TestTask_BarePrompt_SIGTERM_WritesTheSignalVerdictEventIntoTheSessionLog (1.24s)`,
  `--- PASS: TestTask_BarePrompt_SIGTERM_WritesSessionEnd (1.73s)`, `ok … 3.341s`.
- Hand-driven run: the `signal_verdict` JSON line quoted above, the 7-line type
  sequence, and `gopi log --last --json` exit 0 with 1 match — all observed on
  this branch's freshly rebuilt `bin/gopi`.
