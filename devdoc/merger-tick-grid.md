<!-- doc-class: historical -->
# Task Report: `gopi merger --status` invented a tick boundary — the grid is `work(D) + interval`, not `interval`

Branch: `fix/merger-tick-grid` · worktree `~/workspace/gopi-tick-grid`
Date: 2026-09-17 · Item: the operator's report of a permanently "past due" merger

## The report

`gopi merger --status` printed, on a healthy resident merger:

```
last tick:   23:16:55 (elapsed 10m38s — the next boundary is 5m37s past due,
             so a tick is running right now)
```

The operator's objection is exact: *"how does status report merger last tick due
without knowing the next tick time?"* — the line names a boundary it never
measured, and it does so on every read of a working daemon.

## Root cause (arithmetic, reproduced to the second)

The render derived `next = last boundary + interval` and compared it with now.
The loop is **not** a boundary grid:

```
boundary → WORK(D) → sleep(interval) → boundary
```

so two boundaries are `D + interval` apart. A `last + interval` grid is short by
exactly the work half `D`, and a status read during the *sleep* half — the
common case, since sleeping is most of the cycle — is always "past due" by `D`.

The operator's own numbers reproduce `D` exactly:

| fact | value | evidence |
|---|---|---|
| last boundary | 23:16:55 | `[merger] pass start 23:16:55` (legacy spelling, pre-2026-09-16 build) |
| status read at | 23:27:32 | 23:16:55 + 10m38s (the "elapsed" the operator saw) |
| the grid's claim | 23:21:55 | = 23:16:55 + 5m |
| reported overrun | **5m37s** | = 23:27:32 − 23:21:55 |
| the loop's real sleep edge | 23:24:58 | the tick's work half was **8m3s** |
| the loop's real next boundary | 23:29:58 | = 23:24:58 + 5m — still **2m26s in the future** |

Nothing was late. Log evidence (same day, `~/.gopi/logs/merger.log`): ticks at
23:11:46 / 23:16:55 / 23:29:58 / 23:48:56 / 00:24:38 — gaps up to **14m58s**,
i.e. tick durations up to ~10m, on a loop whose poll interval is 5m.

The "so a tick is running right now" clause was false for the same reason: the
reader had no way to tell a *running* tick from a *sleeping* one, because the
sleep line the loop printed (`[merger] sleeping 5m0s`) carried **no stamp** —
the boundary of the idle phase was not written down anywhere.

**One contributor, not three.** No clock drift, no second-resolution rounding,
no scheduler lateness was needed to explain the figure: a boundary grid that
ignores the work half over-reports by exactly D. (The second-resolution bound
`mergerStampResolution` is used now, but only to decide when a *measured*
boundary has genuinely passed.)

## The fix — read the loop's structure, claim only what is measured

`internal/tools/merger_schedule.go` (reader) + `cmd/agent/cli_tasks.go` (writer):

1. **The last EVENT in the log tail decides the phase.**
   - a tick boundary and no sleep line after it → the tick is **in progress**:
     no next boundary is claimed at all; the render reports "running now —
     NmN s so far".
   - a sleep line → the loop is **idling**, and the next boundary is
     `sleep edge + interval` — an anchor that is *measured*, and that needs no
     knowledge of `D`.
2. **The writer states the anchor.** The loop now prints
   `[merger] sleeping 5m0s from 23:24:58` using the shared constant
   `tools.MergerSleepLineFormat` (Rule 14: one spelling of the line for writer
   and reader; Rule 1b: no bare literal on either side).
3. **Lateness is measured, never invented** — `Overdue`, `OverdueMeasured`:
   claimed only against a boundary the loop's own stated sleep should have
   reached, and only once now is past it by more than the stamp's resolution.
   With no live holder nothing is predicted at all.
4. **Degradations are named, never faked** (Rule 15): a pre-2026-09-17 sleep
   line without a stamp renders
   `next tick: unknown (the log's sleep line carries no start stamp (a merger
   built before 2026-09-17), so the loop's idle edge could not be measured)`
   — and deliberately does **not** fall back to the old grid.
5. **The work half is shown when measured**: "— the last tick's work took 8m3s
   before it slept", so a slow tick is visibly a slow tick, not lateness.

The word **"past due" is gone from the surface entirely** (the regression tests
fail if it reappears).

## Completed

- Root cause established arithmetically and confirmed against the live log
  (D = 8m3s reproduces the operator's 5m37s exactly).
- Structural reader (`mergerScheduleFrom` / `mergerScheduleDerive`) — phase from
  the log tail, anchor from the sleep edge, `TickInProgress` /
  `SleepBeginKnown` / `WorkDurationKnown` / `OverdueMeasured`.
- Shared writer/reader spelling `tools.MergerSleepLineFormat` +
  `tools.MergerClockLayout`; the daemon prints the stamped sleep line.
- Renderer rewritten (`renderScheduleLines`, `mergerWorkNote`): no fabricated
  boundary, named reasons for every unknown, running-tick and overrun branches.
- 14 test functions in `internal/tools/merger_schedule_test.go`, including two
  red proofs of the operator's exact defect:
  `TestMergerScheduleRed_OperatorSawPastDueOnASleepingMerger` (pins both the
  defect's arithmetic — 5m37s — and the corrected reading) and
  `TestMergerScheduleRed_MeasuredThroughThePipeline` (drives the real
  `MergerStatusReport` and fails if the pipeline's assignment lines are
  dropped, i.e. if the measurement is dead while the struct tests stay green).

## Not Done (honest)

- **The running daemon still writes the old unstamped line.** The fix is in the
  source; the resident merger on this host was built before it. Until the
  merger restarts on the new binary, `--status` correctly reports
  `next tick: unknown (… carries no start stamp …)` — a *naming* fix, not a
  measurement fix, and the message says so. No restart was performed by this
  task (restarting the merger is an operator-visible service action).
- **The tick's work half D is not recovered from the log.** The tick-start line
  carries a wall-clock stamp and so does the sleep line, but subtracting them
  across midnight is not an instrument; D is therefore reported only when the
  two stamps are unambiguously in the same day, and the reader prefers the
  sleep edge (which needs no D) everywhere else.
- No change was made to the merger's tick scheduling itself — the loop was
  never late; only its *report* was wrong.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0); `go vet ./internal/tools/ ./cmd/agent/` → clean.
- Tests: `go test ./internal/tools/ -run 'Merger|Schedule' -count=1` → ok (8.7s);
  full `go test ./internal/tools/ ./cmd/agent/ -count=1` → see below;
  `go test ./internal/standards/ -count=1` → ok (gates 16–21 green, 11.6s).
- Live surface: `./bin/gopi merger --status` on this host renders
  `next tick: unknown (the log's sleep line carries no start stamp …)` with the
  real interval `5m0s (measured from the log's last sleep line)` — the
  fabricated boundary is gone, and the degradation is named rather than faked.
- Commit: see the branch tip (this report is committed with it).

## Files

- `internal/tools/merger_schedule.go` — the reader/derivation (core of the fix)
- `internal/tools/merger_status.go` — the render + `mergerWorkNote`
- `internal/tools/merger_schedule_test.go` — regression + red-proof tests
- `cmd/agent/cli_tasks.go` — the loop's stamped sleep line (`MergerSleepLineFormat`)
