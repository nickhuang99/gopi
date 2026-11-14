<!-- doc-class: historical -->
# WS5's red was a FIXTURE BUG, not host load — `GOPI_HOME` first-match vs exec's last-wins

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a0ff15-c494-737d-98e2-dc60b160ae1f` (bug: `make test-budget PKGS='./cmd/agent'`
red because `TestWorkerDaemon_StopEndsTheDaemon` took ~21.5s under host load)
**Branch:** `task/01a0ff15c494737d98e2dc60b160ae1f`
**Measured:** 2026-10-03, peerbox, 4-core, load 12–18 with other sessions' suites running.

## The claim that was on the table, and why it was wrong

The briefing came with an attribution already written: the 21.5s red is the daemon's
**start-up prologue** (cgroup escape, resident scratch sweep, first ledger read)
outlasting the operator's `--stop --wait 20s` bound, while the daemon's own record
still read `rounds 0, note: daemon starting`. The evidence for it was real: the
failing run's stream (`run-1790984880-3077529.json`, load 15.9) carries exactly
those lines, and a probe daemon measured against this host did show a long
prologue (3s at load 4, 8s at load 10, >20s at load 15.9).

That story is wrong, and the way it was wrong is worth keeping: **the prologue
measurement was taken on the wrong process's behalf.** An instrumented run
(`wsStartDaemon`, 60 one-second reads of the path the fixture polls) printed
`no such file or directory` sixty times in a row while an INDEPENDENT watcher
sampling `/dev/shm` saw the very same daemon's record reach `"claiming":true` at
**t+2s**. The daemon was never slow. The fixture was knocking on a door that does
not exist.

## Root cause, by line

`wsDaemonEnv` → `wdEnv` (`cmd/agent/worker_daemon_flag_e2e_test.go`) builds the
child's environment as

```go
stripAgentSignature(append(os.Environ(), append([]string{
    "GOPI_HOME=" + home, … }, extra...)...))
```

— i.e. **`os.Environ()` first, the case's own `GOPI_HOME` later**. `exec.Cmd.Env`
semantics: when a variable appears twice, **the LAST occurrence is the one the
child gets.** The test binary itself runs under the suite's `GOPI_HOME`
(`…/suite-gopi-home`), so the vector carried two `GOPI_HOME` entries.

`wsHomeFromEnv` returned the **FIRST** match — the suite's, not the case's:

```go
for _, kv := range env {
    if home, ok := strings.CutPrefix(kv, "GOPI_HOME="); ok && home != "" {
        return home            // ← the child never used this one
    }
}
```

So `wsWaitClaimLoopOpen` polled
`/dev/shm/suite-cmd-agent-run-*/suite-gopi-home/.gopi/worker/worker-state-<pid>`
while the daemon wrote to `<t.TempDir()>/.gopi/worker/worker-state-<pid>`. The
file could not appear, so every stop case in the file — WS1/WS2/WS4/WS5/WS6 —
burned its **entire** bound (90s in the new code, and in the shipped
`--stop --wait 20s` path the operator's 20s) and then reported the daemon's
start-up as the culprit. The reported symptom shaped like a host-load story
because a loaaded host lengthens every one of those bounds.

| what | measured |
|---|---|
| polled path, 60 consecutive reads | `no such file or directory` × 60 |
| the daemon's real record (`/dev/shm` watcher) | `claiming:true` at **t+2s** |
| pre-fix, standalone, `-count=1` | **FAIL 99.35s / 100.48s / 99.57s** (3/3 red) |
| post-fix, standalone, load 12.43 | **PASS 16.34s** |

The 21.5s figure in the package gate is the same defect seen through the
pre-existing `--stop --wait 20s`: the daemon was never asked at the right moment
because the fixture never proved the moment it thought it had proved.

## The fix

`wsHomeFromEnv` takes the **LAST** occurrence — the child's rule is the reader's
rule. One loop, one word changed, plus the comment that says why (`cmd/agent/worker_daemon_singleton_e2e_test.go`).

Nothing else changed shape: the claim-loop-open record (`workerControlState.Claiming`,
`MarkClaiming`, `OpenedClaimLoop`, the binary's own `workerStartSettleBudget` /
`workerClaimLoopOpened`) was already in the working tree and is committed here
unchanged — it is what made the wrong path visible at all, by turning "the
daemon is slow" into "the record never appeared", a sentence a reader can check.

## WS6 is still RED — the same prologue shape, now attributed (NOT fixed)

`TestWorkerDaemon_RestartReplacesTheDaemon` (WS6, same file) fails at the 20s
`--restart` bound, and the fixture fix did not touch it. Measured, standalone,
load ~19:

```
--- FAIL: TestWorkerDaemon_RestartReplacesTheDaemon (30.48s)
    `gopi worker --restart` must exit 0, got 1:
        asking pid 3457452 to finish the in-flight round and exit (SIGTERM, bound 20s)
        current state — pid 3457452, rounds 0, note: daemon starting
        pid 3457452 is STILL running 20s after the graceful stop request
```

This is the ORIGINAL structure the briefing described, and here it is real rather
than a misread: the daemon had already published `claiming:true` (so the fixture's
`wsStartDaemon` gate passed — the record was found in the right directory this
time), and the drain still was not honoured inside 20s. The phase that swallows
the bound sits BETWEEN the claim-loop-open publication and the next round
boundary:

```go
workerCtx.markClaiming()            // ← published
if workerDaemonGuardIdentity() {…}
workerCtx.consumeDrainRequests()    // ← the drain is consumed HERE
…
if !once { scratchGCSweep(workerRoleLabel) }   // ← and this can outlast 20s
```

`markClaiming` is emitted at the TOP of round 1, but `consumeDrainRequests` runs a
few statements later, after the guard-identity pass — and the resident sweep that
follows blocks on the memory base (`/dev/shm`, 28 suite roots observed, held-root
scans). A `--stop` arriving in that window is a durable request the round will not
read until the NEXT boundary, and a signal is only noticed through the same
bounded path.

**One-sample A/B, and it is the only measurement of it this run made:** with the
sweep disabled (`GOPI_TEST_SCRATCH_GC_MIN=0`) WS6 PASSED at 25.02s; with the sweep
on it failed 20.58s / 21.00s in the two gate runs and 30.48s standalone. That is
suggestive, not conclusive — one sample per arm, taken while two other agents were
running their own package gates (load 12–20), and a 20s bound is exactly the
host-length quantity under debate.

**This is left for a fresh task, by name** (Rule 26: an incomplete fix is repaired
by dispatching again).

## Nothing weakened

The claim-loop wait is still a live detector, and this run has its red proof in
hand: before the fix it **fired**, three times in a row, naming the pid and
printing the daemon's log — `the daemon pid N never published its claim-loop-open
record within 1m30s`. The fix did not shorten or bypass that wait; it made the
fixture look where the daemon actually writes. A daemon that genuinely never
reaches a round boundary is still a FAILURE with a name, never a silent pass.

## Not Done (honest)

- **Precision on which code produced which red.** Three reds at 99.35s / 100.48s /
  99.57s were measured with the NEW fixture in the tree (the 90s claim-loop wait)
  and the OLD `wsHomeFromEnv` — i.e. the same defect seen through the new bound.
  The original 21.5s comes from the package gate's OLD fixture, whose bound was
  the operator's `--stop --wait 20s`. Both numbers are the same bug; neither was
  re-measured at load 15.9 after the fix.
- `devdoc/goroutine-signature-red-proof.md` (the sibling task's report) still
  carries the earlier, wrong attribution of this red in its
  `## Package-level gate` section. It is another task's document, this run's
  diff does not own it, and it is left as it stands — the correction is here.
- **WS6 (`TestWorkerDaemon_RestartReplacesTheDaemon`) is still RED** and was not
  fixed — see its own section above. The package gate therefore did NOT go green
  in this run, and the acceptance item "`make test-budget PKGS='./cmd/agent'`
  green" is **NOT met**. What this run did deliver is: WS5's root cause (a
  fixture path bug, fixed and proven red→green) and WS6's attribution (the window
  between `markClaiming` and the round's `consumeDrainRequests`, with the
  resident sweep suspected and one A/B behind that suspicion).
- **The package gate was not run to completion.** Two other sessions were running
  their own `make test-budget` on this 4-core host for most of this window (load
  12–20), and two attempts at the three-consecutive-green requirement were killed
  on the session budget before printing a verdict. The full-gate evidence this run
  has is partial and is quoted as such.
