<!-- doc-class: normative -->
# A killed test binary is an INTERRUPTION, not "a panic or a TestMain exit"

**Bug row:** `01a164f6`. **Measured:** 2026-10-23, host `host-b`.

## The defect, verbatim

Two `cmd/agent` budget runs (streams `run-1792694068-3762702` at 02:34 and
`run-1792694665-3875354` at 02:44) ended with this report:

```
  failing tests: 0
  package verdict: FAIL: github.com/the operator/gopi/cmd/agent
--- package-level failures (build/panic — no test to name) ---
  PACKAGE FAILED: github.com/the operator/gopi/cmd/agent (a package-level verdict — a panic or a TestMain exit; no test to name)
```

Both halves of that sentence are false. There was no panic (no `panic:` row
anywhere in either stream) and no `TestMain` exit (`exit status N` is a different
os/exec line and neither stream carries one). The test BINARY had been killed by
SIGHUP; `go test` records that as a package-level `fail` plus one output row:

```
signal: hangup
```

`failing tests: 0` and a red package arriving **together** is the signature — a
killed binary leaves no test-level verdict, so no test can be named.

## Why the budget reporter could not see it

`gopi_stream_package_failures` classified a package-level failure by asking
"does this package have any `test` event?" A killed package sometimes does (the
tests that ran before the kill), and sometimes does not. In the 02:44 run the
`signal: hangup` row arrived **test-scoped**:

```
{"Action":"output","Package":P,"Test":"TestWorkerRoundClaimsWhenNoRequestStands","Output":"signal: hangup\n"}
{"Action":"fail","Package":P,"Elapsed":440.462}
```

— `hastest` was set by the very events that prove the interruption, so the
package fell through to the "no test to name ⇒ panic or TestMain" arm. In the
minimal repro (a throwaway module whose only test raises SIGHUP on itself) the
row is **package-scoped** and the same arm catches it by the other route. A fix
that read only one shape would have repaired the repro and left the measured
defect live, so the classifier reads the text of every `output` row, whichever
normalised kind it landed in.

**The two shapes are not two bugs — one repro produces both, measured.** The same
minimal module, run twice:

```
20:44:53  {"Action":"output","Package":P,"Output":"signal: hangup\n"}                               ← package-scoped
20:53:44  {"Action":"output","Package":P,"Test":"TestKilledBySignal","Output":"signal: hangup\n"}   ← test-scoped
```

`test2json` attributes the row to whichever test was live when the process died,
so the shape depends on timing alone. That is precisely why the classifier must
not key on it, and why a fixture carrying only one shape would let the gate pass
while the defect stays live.

## Who sends the SIGHUP — measured, not inferred

The signal is `SI_USER` and **the test binary sends it to itself**.

```
$ strace -f -e trace=kill,tgkill --signal=SIGHUP -o …/strace-5c1eb6c-twikill.txt \
    go test ./cmd/agent/ -run '^(TestWorkerDrainsOnSIGHUP)$' -count=1
…/strace-5c1eb6c-twikill.txt:
1297900 kill(1297897, SIGHUP)           = 0
1297900 --- SIGHUP {si_signo=SIGHUP, si_code=SI_USER, si_pid=1297897, si_uid=1000} ---
```

`si_pid` equals the test binary itself, not a supervisor, a terminal, cron or the
kernel. The same capture on the shipped arm, where the suite root handler also
subscribes to SIGHUP, shows **two** deliveries from the same sender (the test's
own kill, then the handler's mandatory re-raise after `signal.Reset`):

```
1297900 kill(1297897, SIGHUP)   ← the test raising it on itself
        … handler: "testutil: hangup — moved this run's scratch root … aside" …
1297900 kill(1297897, SIGHUP)   ← the arm's re-raise, signal.Reset'd first
```

### Excluded candidates, with the criterion used for each

| candidate | criterion | reading |
|---|---|---|
| a cron/at job | `crontab -l` | `no crontab for the operator` |
| a systemd timer | `systemctl --user list-timers` | `0 timers listed` |
| terminal / logind teardown | `journalctl -b` around 02:51:44–02:51:50 | zero session/logind/scope-stop lines |
| a sibling budget run | the test binary's own pid in `si_pid`, above | it is the sender |
| the kernel (OOM, tty hangup) | `si_code=SI_USER`, not `SI_KERNEL` | userspace |

**Test-side source (`TestWorkerDrainsOnSIGHUP`).** The failing runs executed
`5c1eb6c`, whose arm subscribed to `SIGTERM, SIGINT, SIGHUP, SIGQUIT` and
re-raised. That commit's own handler line —
`testutil: hangup — removed this run's scratch root …` — appears in BOTH failing
streams, which is the proof of which revision ran. `774c9cf` (02:54) also
contains `5c1eb6c` and not `c9c0534`; `c9c0534` (07:58, the current `master`)
narrowed the arm to `SIGTERM` only, so on today's tree SIGHUP takes the default
disposition and still kills the binary — the reporter's blind spot is unchanged
either way.

**The re-raise is not decoration.** `signal.Notify` *disables a signal's default
disposition*, so an arm that catches SIGHUP, reaps the root and returns would
leave the process RUNNING — the re-raise after `signal.Reset` is what actually
ends it. That is why the handler's own kill appears in the capture, and why a
killed binary is a legitimate, self-inflicted shape rather than an external
insult.

## The evidence reader had to change too — measured on a REPLAY, not inferred

The first shape of this fix rendered the section correctly and still printed the
wrong evidence under it. Replaying the real capture
(`run-1792694665-3875354.json`, 5.2 MB, the very stream the misattributed report
came from) through `scripts/test-budget.sh` showed:

```
--- SIGNAL INTERRUPTION (the test binary was killed — not a panic, not a TestMain exit) ---
  INTERRUPTED: github.com/the operator/gopi/cmd/agent — killed by hangup (no test carries a verdict: the process died)
      testutil: suite "cmd-agent": scratch root /dev/shm is tmpfs …     ← the suite's startup log
      testutil: cmd-agent: kept suite run root /dev/shm/suite-…        ← more startup log
      FAIL github.com/the operator/gopi/cmd/agent 440.462s
```

The `signal: hangup` row was **nowhere in it**, because the quote came from
`gopi_stream_package_output_for` (package-scoped rows only) while the real row is
**test-scoped**. The claim was asserted and somebody else's output was shown
under it — the same class of defect as the misattribution itself. The quote now
comes from `gopi_stream_signal_rows STREAM PACKAGE`, which reads both shapes and
returns the row's verbatim TEXT:

```
--- SIGNAL INTERRUPTION (the test binary was killed — not a panic, not a TestMain exit) ---
  INTERRUPTED: github.com/the operator/gopi/cmd/agent — killed by hangup (no test carries a verdict: the process died)
      signal: hangup
  ^ this package's red is an INTERRUPTION, not a code verdict: the run was ended by a
    signal, so the tests that never reported a verdict did not fail — they never ran.
```

**Why a replay and not a fixture:** the fixture streams (below) are the gate's
contract, and a contract can be written to match the code. The replay feeds the
5.2 MB capture that actually produced the wrong report, so it answers the only
question that matters — does today's script, given that input, still lie. Two
detached worktrees at `c9c0534` (old scripts, new scripts) against the same
capture: `evidence/replay-old.txt:70-73` vs `evidence/replay-new.txt:70-83`.

## What changed

`scripts/lib/stream-failures.sh`

- new kind `stream_kind_signaled` (`signaled`) and `stream_signal_prefix`
  (`"signal: "`, os/exec's rendering, `os/exec_posix.go:123`);
- `gopi_stream_signals STREAM` → `package<TAB>signal-name`, reading the text of
  every `output` row regardless of which kind it normalised to;
- `gopi_stream_signal_rows STREAM PACKAGE` → the verbatim `signal: <name>` text
  for one package, from either row shape (this is what the section QUOTES);
- `gopi_stream_package_failures` classifies a signalled package as `signaled`
  **first**, so the "no test to name" arm cannot swallow it.

`scripts/test-budget.sh`

- a `SIGNAL INTERRUPTION` section of its own (`report_signal_interruptions`),
  printed BEFORE the package-level block, naming the package, the signal and
  quoting THE PACKAGE'S OWN `signal: <name>` ROW, and stating that the tests
  which never reported a verdict did not fail — they never ran;
- the `package verdict:` line gains ` (killed by <signal> — see the SIGNAL
  INTERRUPTION block)`;
- `report_package_failures` **skips** the `signaled` kind instead of letting it
  reach the `panic or a TestMain exit` sentence. A genuine panic keeps that
  sentence: it is the arm the second test pins.

## Why a signal is its own class and not a footnote

A panic and a `TestMain` exit are claims about the CODE — a reader goes and looks
at their code. A signalled run is a claim about the RUN — a teardown, a budget
timeout, a supervisor. Saying the wrong one costs a debugging session in the
wrong file, which is the cost Rule 15 exists to prevent. It is also why the
report names WHICH signal: `SIGHUP` is a teardown, `SIGTERM` is a budget or a
supervisor, and the next step differs.

## Check

```bash
go test ./internal/testutil/ -run 'TestBudgetSignalInterruption|TestBudgetPanicKeepsThePackageLevelVerdict'
```

Three arms, each running the REAL `scripts/test-budget.sh` end to end against a
stub `go` that answers `go test … -json` with a captured stream shape:

- `TestBudgetSignalInterruptionIsNotAPanic` — the package-scoped minimal repro
  must render the signal section, must not render the killed package under the
  package-level heading, and must not describe it as
  `panic or a TestMain exit`;
- `TestBudgetSignalInterruptionTestScopedIsClassified` — the REAL run's
  test-scoped row shape, which a package-scoped-only fix would have missed; also
  pins that a test which PASSED before the kill is not named;
- `TestBudgetPanicKeepsThePackageLevelVerdict` — the anti-overreach arm: a real
  panic must still be reported as one, or "never say panic" would be satisfiable
  by deleting the sentence.

Both signal arms also carry an EVIDENCE arm: the section must quote the verbatim
`signal: <name>` line under its claim, not the package's general output.

**Red-then-green, measured** (a detached worktree at `c9c0534` — `git stash push`
is refused by this repo's tool guard because the stash stack is shared by both
linked worktrees — see `evidence/red-arm.txt`):

- **arm 1**, scripts untouched, only the gate file added: both signal arms FAIL,
  `the report has no "--- SIGNAL INTERRUPTION" section`, and the panic arm passes;
- **arm 2**, the diff in place with ONE line reverted
  (`gopi_stream_signal_rows` → `gopi_stream_package_output_for`): the
  test-scoped arm FAILS with "classifies but its own `signal: hangup` line is
  not quoted under the claim", while the package-scoped arm still passes — which
  is exactly why both shapes are pinned;
- with the diff intact, all three arms PASS (plus
  `TestBudgetPackageFailureArmSlicesSilently`, which needed the new
  `stream_kind_signaled` constant supplied to its preamble).
