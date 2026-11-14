<!-- doc-class: historical -->
# `gopi cli "<prompt>" --once` owns its terminating signals — measured verdict and fix

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Historical (record). Epic `0x01a0d9e0`, 2026-09-26. This doc records one run's
measurement, the fix it justified, and the guard that now catches a regression.

## The hypothesis, and how it was tested

Hypothesis: `--once` installs no SIGTERM handler, so the only handler in the
process is `internal/signalrec.Install`'s (early in `main`); its argv table
cannot classify `cli`, it FORWARDS, and after its grace it
`signal.Reset(SIGTERM)` + self-kills — killing the one-shot turn before
`CloseChatSession` can write the session's `session_end`.

Measured on the real binary (not a unit test, Rule 31): `bin/gopi` built from
the branch's base commit, driven against a throwaway stand-in model server that
accepts the first round and holds the SSE stream open, then one `kill -TERM`:

```
$ ./bin/gopi cli "start a long analysis and report back" --once   # mid-turn, then kill -TERM
EXIT_CODE=143
$ grep -o '"type":"[a-z_]*"' $SB/.gopi/sessions/<id>/session.jsonl | sort | uniq -c
      1 "type":"session_start"
      1 "type":"signal_verdict"
```

`EXIT_CODE=143` is Go's default disposition — nothing consumed the signal — and
the session JSONL carries **no `session_end`**. The recorder still filed its
record (`role":"cli"`), so the process was caught, recorded, and then re-raised
into death exactly as the hypothesis said.

## The criterion, applied

Master's own criterion for a door that owns its signals: does it have in-flight
work worth finishing? `--once` does — one turn, one session, one ledger row, and
its whole point is that the closeout lands (`CloseChatSession` → `session_end`,
`CloseChatRow`). So the answer is the first arm: `signal.Notify` +
`signalrec.ClaimSignalHandlers()` + a handler that actually unwinds.

## The two mechanisms, and why both are needed

1. **The claim** stops the recorder's re-raise (`signalrec.signalForwarding()`
   is evaluated at SIGNAL TIME, so it reads the claim). Without it the recorder
   resets the disposition globally and kills the process mid-unwind.
2. **The unwind** must actually be reachable. `--once` runs its turn
   SYNCHRONOUSLY, so a handler that merely set a flag could not abort it: the
   turn would keep holding `srv.mu` while the handler tried `srv.mu` — the abort
   would hang behind the thing it is aborting. The turn therefore runs in its
   own goroutine and the main goroutine `select`s on it against the signal
   channel; a signal calls the run-scoped cancel
   (`ag.InterruptRun()`), which reaches the in-flight model request and lets the
   engine's loop unwind through its own end-of-run path.

Two consequences worth naming, because they are what makes the guard's arms
deterministic rather than lucky:

- `runTermOnce` is up to `cmd/agent/cli_term.go:116`, which does
  `os.Exit(runTermOnce(rest))` — the goroutine IS main's goroutine, so a
  synchronous turn would make the handler's goroutine unschedulable during the
  whole turn (no preemption while main blocks in the netpoller). The arm the
  guard measures would then depend on the Go scheduler's goroutine-wakeup
  latency (`sysmon` polls ~20µs–10ms) against the recorder's 250ms grace. Real,
  but exactly the scheduling luck the epic forbids as an arm decider.
- With the turn off the main goroutine, both clocks start at the SIGTERM and the
  unwind is `termOnceSignalUnwindWait` (5s, ~6× the grace; measured ~1.5s, ~6×).
  The race has to be lost by an order of magnitude for the arm to flip.

## Measured after the fix

```
$ ./bin/gopi cli "start a long analysis and report back" --once   # mid-turn, then kill -TERM
EXIT_CODE=1
$ grep -o '"type":"[a-z_]*"' $SB/.gopi/sessions/<id>/session.jsonl | sort | uniq -c
      1 "type":"request"
      1 "type":"session_end"
      1 "type":"session_start"
      1 "type":"signal_verdict"
      1 "type":"user"
$ grep -o '"exit_code":[0-9]*' …        # exit_code:1
```

`exit 143 → exit 1`, no `session_end` → exactly one `session_end`
(`exit_code: 1`), and the recorder's `signal_verdict` + provenance record both
survive (`sourced:false`, `role":"cli"`) — the claim silences the re-raise and
nothing else.

## The guard

`testsuite/term_once_signal_test.go` — `TestTermOnce_SIGTERM_WritesSessionEnd`
(heavy, `heavytest.CriterionDeviceExec`, so it is listed in
`testdata/heavy-tests.tsv`). It drives the REAL binary through
`testutil.StartGopi`, waits for the mock's Hold to reach 1 (the turn is
genuinely mid-round-trip), sends SIGTERM, and asserts three things: the exit
status is the face's own failure status (never `-1`/143/137), the session JSONL
carries exactly one `session_end` with that status, and the provenance record is
still on disk.

The grace it reasons about is read from the SAME package that owns the
recorder's 250ms (`internal/signalrec`) rather than re-typed — one home for the
ownership policy, on both sides.

Red proof, on the same tree with master's `cli_term_once.go` restored (the fix
reverted, the guard kept), same command:

```
$ GOPI_HUMAN_TERMINAL=1 GOPI_HEAVY_TESTS=1 go test ./testsuite/ \
    -run TestTermOnce_SIGTERM_WritesSessionEnd -count=1 -v
    term_once_signal_test.go:110: gopi cli --once exit = -1 after SIGTERM, want 1 —
      the recorder re-raised the signal and killed the turn before its closeout
--- FAIL: TestTermOnce_SIGTERM_WritesSessionEnd (0.43s)
```

0.43s is the 250ms grace and nothing else — the arm is the code's.

## Not fixed here (honest, and dispatchable)

- **`gopi cli "<prompt>" --tools --once` with a turn that eats its context.**
  Unmeasured, but the same abort path applies.
- The recorder's 250ms grace remains shorter than a cold `CloseChatSession`'s
  fsync. That is not a defect *now* (the claim removes the re-raise, so nothing
  races the closeout on this path), but any FUTURE door that installs a handler
  without claiming still loses that race — the sibling residue named in
  `devdoc/signalrec-claim-vs-reraise.md` (`installWorkerStopSignals` on
  `gopi worker --stop`) is the same family and remains explicitly out of scope
  for this task.
- `cmd/agent/merge_run.go`'s own `signal.Reset` + kill on the exit path is a
  deliberate default-disposition restore and was not touched.
