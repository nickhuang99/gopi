<!-- doc-class: historical -->
# Task Report: `gopi merger --stop` no longer reports a forensic failure as its outcome

Feature: signalrec stop-provenance quiet path (secondary finding of the
merger-stop wrong-target investigation, commit 5c1b1206).
Branch: `fix/signalrec-stop-provenance-quiet`. Date: 2026-09-14.

## Completed

1. **Root cause (5-why, not a guess).**
   - *Symptom*: a healthy operator stop — `gopi merger --stop`, exit 0, daemon pid
     gone — printed
     `[gopi] signal provenance NOT FILED (no public-dir fallback): signalrec: session dir
     "…/sessions/<id>" for task <id> is missing or unusable — refusing to write`.
   - *Mechanism*: `cmd/agent/cli_merger_stop.go` `mergerSignalPID` records the INTENT
     through `signalrec.Intent`; `sessionDest()` resolves most-specific-first, and step 2
     (the ledger id this process carries: `GOPI_TASK_RUN_ID` / `GOPI_TASK_WRAPPER_ID`)
     **outranks** step 3 (the process layout) and refuses hard when the id's session dir
     cannot exist.
   - *Why it fires*: the operator's shell inherits `GOPI_TASK_RUN_ID` from the agent that
     launched it — an id that names a real row in the *other* profile and no dir at all in
     this one.
   - *Why it was a bug and not cosmetics*: a foreign id **outranked** a branch that would
     have filed the record happily (`sessions/process-<pid>`, the layout the resident
     merger exists in), converting a filable record into a refusal — and the refusal was
     printed by the stop command, where it reads as "the stop did not happen".
   - Control run proving the mechanism: with no ledger id in the environment the *same*
     binary on the *same* profile prints nothing at all and files
     `sessions/process-<pid>/signal-provenance.jsonl`.

2. **Fix — a record that CAN be filed is never refused because of an id that cannot.**
   `internal/signalrec/signalrec.go` `sessionDest`: the ledger-id branch keeps its
   preference (a proven session dir wins, `LayoutTaskRun`), but when that dir cannot hold
   the record it now **falls through to the process layout for a RESIDENT mode**
   (`!forwardsDefault(label)`), with the record's `note` naming the unusable id — the same
   `processDest` helper step 3 uses (one placement implementation, not two). A **one-shot**
   mode keeps the refusal: its ledger id IS the session the record belongs to, and minting
   an empty bucket per invocation is the litter the process layout is deliberately not for.
   The refusal text now also names the retired public sink (as the sibling refusal does)
   and states that the refusal is about the RECORD only.

3. **Wording/severity on the operator paths** (`cli_merger_stop.go` `mergerSignalPID`,
   `cli_worker_control.go` `workerSignalPID`): a forensic failure is announced as a
   non-failure note that names the stop as unaffected. Reachable now only for a genuinely
   unusable destination (never the inherited-id case); exit codes are untouched — they are
   decided by the daemon, never by the record. `cmd/agent/child_stop.go` (the in-merger
   child-kill path, where the intent line is often the ONLY evidence of a SIGKILL, in
   `merger.log` rather than the operator's terminal) was deliberately left as-is.

4. **Regression tests (both fail on master source, pass on the fix).**
   - `internal/signalrec/signalrec_test.go`:
     `TestSignalRecResidentModeWithAForeignLedgerIDFilesIntoTheProcessDir` (record lands in
     the process dir, `layout=process`, `task_run` still carries the inherited id, the note
     names the unusable id, **no bucket minted for the foreign id**) and
     `TestSignalRecOneShotWithAMissingLedgerDirIsStillRefused` (the boundary: the
     fall-through is resident-only).
   - `cmd/agent/merger_graceful_restart_e2e_test.go` (the epic's acceptance, real binary):
     `TestMergerStop_ForeignLedgerIDSucceedsQuietlyAndStillFilesTheIntent` — fresh GOPI_HOME
     with **no** sessions dir, inherited foreign `GOPI_TASK_RUN_ID`: exit 0, daemon pid
     gone, neither the stop's output nor the daemon's carries a failure-reading line
     (`NOT FILED` / `refusing to write` / `could not record kill intent` / `could not
     signal`), and the intent IS filed naming the daemon pid — quiet is not the same as
     dropped (Rule 15).

5. **Docs corrected** (both claimed the warning was acceptable as-is):
   `devdoc/signalrec-session-attribution.md` (§2 resolution order) and
   `devdoc/worker-daemon-acceptance.md` ("Not Done" item re-written as FIXED with the
   mechanism).

## Not Done

- The victim path `signalrec.reportUnfiled` (the `… NOT FILED …` line) was **not**
  reworded. It writes to the *victim's* stderr — merged into `merger.log` for a detached
  daemon, not the operator's stop output — and it fires exactly once per process, where
  "the record could not be filed" is the honest, required signal (Rule 15). Changing it
  would weaken that signal without changing anything the operator sees. Evidence (master
  red-proof) shows it can surface on the operator's terminal when the daemon's stderr is
  the terminal; that path is pre-existing and independent of `--stop`.
- The wider question "should an inherited ledger id ever outrank the process layout for a
  resident mode?" is answered **narrowly** here (fall through only when the id's dir cannot
  hold the record). Re-ordering the resolution globally is a bigger policy change with its
  own blast radius (task-run/verify children depend on the ledger key) and was rejected as
  out of scope.
- No change to `internal/tools` or the stop contract (exit codes, wait semantics, lock
  targeting) — those were verified by 5c1b1206 and are untouched here.

## Verification

- **Repro (pre-fix, master-built binary)**: private `GOPI_HOME` + empty-repos
  `settings.json`, `GOPI_TASK_RUN_ID` inherited → the refusal line on a stop with
  `stop_rc=0` and the daemon gone. Without the inherited id: quiet, exit 0, record filed
  under `sessions/process-<pid>/`.
- **Red-proof (master source + new tests, source changes stashed)**:
  `go test ./internal/signalrec/ -run 'TestSignalRecResidentModeWithAForeignLedgerID|TestSignalRecOneShotWithAMissingLedgerDir'`
  → **both FAIL**; `go test ./cmd/agent/ -run TestMergerStop_ForeignLedgerIDSucceedsQuietlyAndStillFilesTheIntent`
  → **FAIL**, with the exact reported wording
  (`[gopi] signal provenance NOT FILED (no public-dir fallback): … refusing to write`)
  in the failure output.
- **Green (fix applied)**: `go test ./internal/signalrec/` → **ok**;
  `go test ./cmd/agent/` → **ok**; `go test ./...` → **no FAIL**;
  `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o /dev/shm/gopi-sigstop-bin ./cmd/agent/` → exit 0;
  `gofmt -l cmd internal` → empty; `go test ./internal/standards/` → **ok** (Gates 1–6,
  the new code introduces no violation).
- **Manual repro with the real binary (the epic's documented alternative)**:
  `GOPI_HOME=<fresh>`, empty-repos `settings.json`, inherited
  `GOPI_TASK_RUN_ID=0f5c5d3e-2222-4000-8000-000000000000`:
  `gopi merger --daemon --every 1h` → `merger daemon started: pid 1492588`;
  `gopi merger --stop --wait 30s` → **stop_rc=0**, output = the three normal lines
  ("asking pid …", "current state — …", "pid 1492588 exited 6.511s after the graceful stop
  request"), **no failure-reading line**, daemon pid gone. Filed:
  `sessions/process-1492761/signal-provenance.jsonl` (`kind=intent`, `layout=process`,
  `target_pid=1492588`, `reason="gopi merger: operator ran gopi merger --stop"`, note
  naming the unusable ledger id) and the daemon's own `sessions/process-1492588/…`
  (`kind=signal`). Sessions root holds exactly the two process dirs — no bucket for the
  foreign id.
- **Build / full suite / commit**: see the closing section below (filled from the actual
  command output).

## Build / full suite / commit (actual output)

```
go build ./...                                    → exit 0
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → exit 0
gofmt -l cmd internal                             → (empty)
go test ./internal/signalrec/                     → ok   (cached, full package)
go test ./internal/signalrec/ -run TestSignalRec  → ok   8.103s (both new tests included)
go test ./internal/standards/                     → ok   12.800s (Gates 1–6)
go test ./cmd/agent/                              → ok  310.306s   ← after the bin rebuild below
go test ./...                                     → every package ok
                                                    EXCEPT internal/stylegate + internal/testutil
```

### The two red packages are pre-existing on master — reproduced, not asserted

Run in a clean **detached worktree of master 48e623a2**, whose tree contains no branch content:

```
go test ./internal/stylegate/  → FAIL   (stylegate test-sleep: cmd/agent/cli_worker_status_test.go:91)
go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent → FAIL
      (devdoc/worker-once-vs-task-run-next.md, devdoc/worker-status-overreport-20260913.md
       lack "# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md")
```

Neither file is touched by this branch (`git diff --name-only master...HEAD` lists neither), so both
failures exist on master without this change.

### The one real red this task hit — root-caused, not parameter-tweaked

The first full-suite run reported 5 red e2e tests in `cmd/agent`
(`TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers`,
`TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`,
`TestWorkerChildDoesNotInheritTheDaemonsSessionHandle`,
`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`,
`TestFrontRunConsoleOutputLandsInItsSessionDir`).

5-why: they FAIL because their own `assertGopiBinIsFresh` guard rejects the run — *"bin/gopi (mtime …)
is OLDER than cmd/agent/cli_merger_stop.go"*. My edit to a production file made the worktree's
prebuilt `bin/gopi` stale; the guard exists precisely so an e2e never verifies code that is not in the
binary. The tests were right. Fix = rebuild the binary, then re-run:

```
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     → rebuilt 01:20:26
--- PASS: TestMergerStop_ForeignLedgerIDSucceedsQuietlyAndStillFilesTheIntent (1.92s)
--- PASS: TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers (1.61s)
--- PASS: TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds (2.19s)
--- PASS: TestWorkerChildDoesNotInheritTheDaemonsSessionHandle (1.67s)
--- PASS: TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild (1.63s)
--- PASS: TestFrontRunConsoleOutputLandsInItsSessionDir (0.07s)
ok  	github.com/nick/gopi/cmd/agent	9.486s
```

Lesson worth keeping: in a worktree, `go test ./cmd/agent/` is only meaningful AFTER
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`; the freshness guard turns a stale binary into a
loud red rather than a silent green-over-old-code.
