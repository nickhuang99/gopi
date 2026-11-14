<!-- doc-class: historical -->
# The cwd-release rig's readiness bound — the 1s was the defect, not the release

Measured 2026-09-26, host load 26.07 on 8 CPUs (`/proc/loadavg`), task
`01a0de7a-1087-7cba-ae95-ae98c4c1c3c3`.

## Symptom

`go test ./cmd/agent/ -count=1 -run '^TestCwdReleaseRig$'` was RED on master, both
subtests, with:

```
=== RUN   TestCwdReleaseRig/red_unreleased_holder_pins_the_removed_tree
    cwd_release_rig_test.go:213: holder 231295 did not report readiness within 1s
=== RUN   TestCwdReleaseRig/green_released_holder_lets_the_tree_go
    cwd_release_rig_test.go:213: holder 231697 did not report readiness within 1s
--- FAIL: TestCwdReleaseRig (2.27s)
```

## Root cause (5-why)

1. Why did the rig fail? The wait for the helper's readiness byte hit its bound.
2. Why did it hit the bound? The bound was `cwdReleaseProcWaitAttempts ×
   cwdReleaseProcWaitInterval` = `50 × 20ms` = **1s**.
3. Why was 1s too small? The byte is written by the helper only after it is *in
   place* — and the helper is `os.Args[0]`, i.e. **a re-exec of the whole
   `cmd/agent` suite binary**, so before the helper reaches its own body it runs
   its own `TestMain` (scratch-root pinning, daemon-lifetime arming, sweeps).
   The wait therefore measured the **child's cold start**, not the release.
4. Why was that not visible before? At low load the cold start fits 1s; the
   bound was a wall-clock guess that happened to hold.
5. Why is it a defect and not "flaky"? The value was overloaded: it bounded BOTH
   the readiness wait AND the procfs CWD poll, and it encoded an *expected
   duration* ("readiness arrives in ~1s") where the signal already existed (the
   byte). **The release machinery under test was never the failure** — the
   cleanup/release path passed the moment it was allowed to run.

Measured on the same host: running just the helper body costs ~2.5s standalone
(`GOPI_CWD_RELEASE_RIG_HOLDER=hold /tmp/rigbin -test.run=TestHelperCwdReleaseHolder`)
while the rig's own PASS time per subtest is ~1.2s — the helper's whole remaining
body is a single `Write` + `Sleep`. The missing time is cold start.

## Fix

The byte is the **signal**; the number is only a **failure detector**. So:

- `cwdReleaseReadyTimeout = 60 * time.Second` — its own named constant, generous,
  with `GOPI_CWD_RELEASE_READY_TIMEOUT_SEC` as the whole-second override read in
  exactly ONE place (`cwdReleaseReadyWindow`, Rule 2).
- The wait now also ends on the **child's own exit** (`errors.Is(err, io.EOF)`),
  so a helper that died before signalling is reported as such instead of being
  waited out. Event-driven, not `Wait()`-based: no goroutine of a finished rig is
  parked on a child that holds its CWD for 20s.
- The procfs CWD poll keeps its own, separate, generous bound (250 × 20ms = 5s).
  It stays a poll because procfs has no notification — but it runs *after* the
  byte, so the CWD is already in place by then. Long `cwdReleaseHolderHoldTime`
  (`20s`) safety property: the hold still outlives the whole `git worktree
  remove` + read back, so the rig can never "pass" a holder that died early.

The unparseable/non-positive override is ignored, never a silent zero (Rule 15a).

## Causality proof (A/B) — run both ways on the same loaded host

```
$ GOPI_CWD_RELEASE_READY_TIMEOUT_SEC=1 go test ./cmd/agent/ -count=1 -run '^TestCwdReleaseRig$'
    cwd_release_rig_test.go:253: holder 258888 did not report readiness within 1s — the bound is a failure detector, not a measurement; raise GOPI_CWD_RELEASE_READY_TIMEOUT_SEC if this host is slower still
--- FAIL: TestCwdReleaseRig (2.27s)          # RED: the old bound reproduces it

$ go test ./cmd/agent/ -count=1 -run '^TestCwdReleaseRig$'
ok  	github.com/nick/gopi/cmd/agent	8.643s    # GREEN
```

Same code, same host, only the bound differs → **the bound is the defect**, and
the release machinery passes for its real reason.

## The second RED: Rule 17's NEW hit

`cmd/agent/cli_tasks_list_order_test.go:236` is the reviewed per-call shape
(`testutil.SnapshotEnv` pair, restores *absence*, in a helper shared with a
subprocess-driving test where `t.Setenv`'s cleanup scope is wrong) but carried no
gate escape. Per the reviewed path — **never** a `baseline.go` entry, which is a
line-keyed permit that widens the ratchet — the escapes go on the flagged line
itself, because `linesAbove` (`internal/standards/gate.go:642`) reads the flagged
line **plus** only the contiguous comment block above it, and a doc comment on the
enclosing function is cut off by the intervening code lines. **Two gates flag the
same line** and each needs its own marker (`internal/stylegate` does not honour
`env-leak-ok`):

```go
if err := os.Setenv(memoryDBEnv, dbPath); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup // env-leak-ok: setter half of the reviewed testutil.SnapshotEnv pair above (restores absence), and this helper is used by a subprocess-driving test where t.Setenv's cleanup scope is wrong
```

Result: Rule 17 `escaped=34 NEW=1` → `escaped=35 NEW=0`, the site listed as
`escape: cmd/agent/cli_tasks_list_order_test.go:236` in the data report; stylegate's
`test-setenv` hit list 2 → 1 (the remaining `internal/lineedit/palette_test.go:102`
hit is pre-existing).

## What a future agent should take from this

- A readiness wait's bound is a **failure detector**, not a measurement. Pick it
  generous, and let the signal end the wait.
- If the awaited process is **another copy of the test suite binary**, its
  `TestMain` setup is on your critical path; 1s does not survive a loaded host.
- For the Rule 17/stylegate pair: suffix the flagged line, spell both markers,
  and never widen the baseline (details: devdoc/standards-gate.md).
