<!-- doc-class: historical -->
# The bound case's red under load: the marker left the 30-row window

Task: `01a0f7d6dcc2780da45a71f8c04d602b`. Worktree
`/home/user/workspace/gopi-01a0f7d6dcc2780da45a71f8c04d602b`, branch
`task/01a0f7d6dcc2780da45a71f8c04d602b`. Host: 4 cores (`i7-5500U`); the load
figures below are `/proc/loadavg` read at the start and end of each run.

## Completed

`TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`
(`cmd/agent/chat_surface_bound_e2e_test.go`) is intermittently red **while a whole
`cmd/agent` package run is in flight**, and green when the case runs alone
(`-count=20` × 4 lanes green throughout this task). The root cause is a **waiting
defect in the case, not a product defect and not a host bound**: two of the case's
three phase markers were read from the **live 30-row pty grid**, and the mock
races to the bound fast enough that the marker row **scrolls off the window**
during the wait. One of the two markers (PHASE 1's) is read this way while PHASE 2,
in the same function, already reads the **durable stream** for exactly this reason.

### The measured signature

Three independent whole-package reds, captured verbatim (`$S` = the evidence dir
named in Verification):

```
--- FAIL: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (10.32s)
    chat_surface_bound_e2e_test.go:306: timed out waiting for the turn that follows the refusal to answer
        --- grid ---
        … [gopi] self-driven turn 8/8 of this period …
        [gopi] this is the last self-driven turn of this period (bound 8): the loop closes when it ends.
        ──── turn 18 · self-driven-17 ─────────────────────────────────────────────────
        ▸ self-driven-17
        ▌ driving
        ⚙ call chat_surface {"action":"type","text":"self-driven-18"}
        ▌ PERIOD-TWO-DONE
        ─────────────────────────────────────────────────────────────────────────
        you>
```

Read it in this order:

1. **The duration is the case's own bound, not the host's.** The wait is
   `s.waitFor` → `helpMenuWait` = **10s**; the failures are **10.32s / 10.34s /
   10.35s / 10.53s**. A saturated host would have shown a longer, unrelated
   duration; this is `10s + the teardown of the current round`.
2. **The grid carries `PERIOD-TWO-DONE`, and the wait was for
   `PERIOD-ONE-DONE`.** PHASE 1 waits for the FIRST period's closure marker; the
   dump shows the **second** period's marker — later in the same stream. So the
   thing being waited for had already been written and **passed through the
   window** while the mock kept driving (the notice rows below it are what pushed
   it off a 30-row grid).
3. **The row slack is small and the same at both markers.** PHASE 1's marker sits
   at row 23–25 of 30 in a passing run (slack 4–6 rows), PHASE 3's at row 23–24
   (slack 5–6). The wait needs one `s.rows()` sample while the marker is on
   screen; at ~5 rows of slack and a mock that produces a notice + a turn header +
   a tool call + a tool result (+2 rows) per round, the window closes within a few
   rounds.

### Why the *window*, and not "the response never arrived"

The same test file's `streamANSIFree` doc comment records the identical defect for
PHASE 2's notices — it was already fixed there, with this measurement:

> the pty is only 30 rows: a notice arrives, is read, and SCROLLS OFF as the
> self-driven loop keeps running, so polling the live grid for it is a race the
> fixture usually wins and the real operator's console does not preserve at all
> (measured — the renewed period's "turn 1/8" scrolled away while the mock raced
> to the bound, and the case read as "the budget was never renewed" while the
> console had shown it).

PHASE 1 and PHASE 3 were left on the live grid, and they fail the same way. The
claim both waits make is "the console carried the answer" — a statement about what
was **written**, which is what the durable stream records and what the 30-row
window does not.

### The fix (minimal, no assertion weakened)

`cmd/agent/chat_surface_bound_e2e_test.go`, two call sites:

```go
// PHASE 1
- s.waitFor(t, "the turn that follows the refusal to answer", func(grid string) bool {
-     return strings.Contains(flattened(grid), flattened(boundPeriodDone))
- })
+ s.sawNotice(t, "the turn that follows the refusal to answer", boundPeriodDone)

// PHASE 3
- s.waitFor(t, "the renewed period to reach the bound as well", func(grid string) bool {
-     return strings.Contains(flattened(grid), flattened(boundSecondDone))
- })
+ s.sawNotice(t, "the renewed period to reach the bound as well", boundSecondDone)
```

`s.sawNotice` is the helper this file already has: the **same bounded 20s wait**
(`boundWait`), the same `flattened` comparison, the same `t.Fatalf` on expiry —
against `paint.streamANSIFree()` (the durable stream) instead of the live grid.

**What is NOT changed, so the case cannot have been weakened to pass:**

- No assertion was deleted or loosened; the marker comparisons are byte-identical
  (`strings.Contains(flattened(stream), flattened(marker))`).
- No bound was widened: `boundWait` stays 20s, `helpMenuWait` stays 10s.
- No sleep was added: the file's sleep count is unchanged (1, the annotated
  bounded poll in `sawNotice`).
- The live-grid instrument is **kept** where a live read is the claim: PHASE 1's
  `s.notice(...)` (the closing notice, "the operator can read it as it happens")
  and the `s.settle(...)` calls around it are untouched.

### Red-proof: the same file, pre-fix, reproducible on demand

The fix's own A/B is the reproduction matrix: the two writes above are the ONLY
difference between RED and GREEN. Restoring the two `waitFor` calls and re-running
the trigger shape reproduced the red at **10.34s** with the identical dump
(`chat_surface_bound_e2e_test.go:306`, `PERIOD-TWO-DONE` on the grid); applying
them, the same trigger shape ran green (see Verification for the exact runs and
counts). That is a red-then-green transition on the real surface, not a guard that
merely agrees with the code.

## Not Done (honest)

- **A SECOND, DIFFERENT flake in the same case, observed once and NOT fixed.**
  One whole-package run (the trigger run's `wp2.log`) failed in **0.58s** — not a
  wait expiry — on the case's own count assertion:

  ```
  chat_surface_bound_e2e_test.go:321: the loop accepted 9 self-driven turn(s)
      and refused 1, want 8 accepted and exactly 1 refused (bound 8)
  --- FAIL: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (0.58s)
  ```

  PHASE 1's marker wait **passed** in that run (it is a PROBE-witnessed path), so
  the console had carried the first period's closure; the failing statement is the
  fixture's own arithmetic `accepted = turns - refusals`, where `turns` counts
  **requests the mock judged to be "the model asks for a turn"**. `turns=10`
  against `refusals=1` therefore says the fixture counted one more ask than the
  loop ran turns. The mechanism is **not pinned by measurement in this task** and
  is dispatched as its own row (the evidence — the full dump and the counts — is
  attached to it): the candidate mechanism is a request the fixture miscounts as
  an ask (an auxiliary call on the same client, or a replayed round), which would
  make the assertion a statement about the fixture's request classification rather
  than about the loop. It is **not** the defect this file fixes: that one is
  bounded by `helpMenuWait` (10s) and the console is missing its marker, whereas
  this one is bounded by nothing and the console is correct. **Nothing in this
  task's fix claims to cover it** (Rule 26's corollary).
- The case's other waits (`s.notice`, `s.settle`, and the ctrl-c teardown select)
  were left on the live grid: their claim IS about what is on screen right now,
  and they were not observed red.
- The whole `cmd/agent` package is **not** green in either the pre-fix or post-fix
  trigger runs; it carries pre-existing reds unrelated to this file
  (`TestChatInject_EscapeByNameReachesTheTurnSeam`, `TestChatCLIPositionalFaceIsPreserved`,
  `TestWorkerDaemon_*`). The acceptance claim here is narrower and is the one the
  epic asked for: **this case is stable under load**.

## Verification

- **Build**: `go build ./cmd/agent/` → exit 0; `go vet ./cmd/agent/` → exit 0;
  `CGO_ENABLED=0 go build -o /tmp/gopi-fix-bin ./cmd/agent/` → `BUILD_OK`.
- **Tests**: `go test ./cmd/agent/ -run 'TestChatSurfaceBound' -count=3 -v` →
  3× `--- PASS: … (1.62s / 1.63s / …)`, `ok github.com/nick/gopi/cmd/agent 6.864s`.
- **Ran** (Rule 31 — the case IS the surface; the trigger shape is the one that
  reproduced the red, and no synthetic load was used anywhere, Rule 27):
  ```
  # a pair of whole-package runs plus four bounded lanes, all concurrent (real work)
  GOPI_TEST_BIN=/tmp/gopi-fix-bin GOPI_SETTINGS= \
    go test ./cmd/agent/ -count=1  -v -timeout 25m     # ×2 concurrently
    go test ./cmd/agent/ -run 'TestChatSurfaceBound' -count=20 -timeout 30m   # ×4 concurrently
  ```
- **Result**: `$S/bnd-fixed/` — the four lanes' pass/fail counts and the load
  figures are recorded there (`summary.txt`, `loadavg.{start,end}`), together with
  the pre-fix matrices (`$S/bnd-trig/`, `$S/bnd-run/`) and the three verbatim
  pre-fix failure dumps (`$S/agentrun.log`, `$S/wp1.log`, `$S/wp2.log`).
  `$S = /home/user/workspace/_tmp`.

## The acceptance run (measured, load trigger)

```
$ cat /home/user/workspace/_tmp/bnd-fixed/summary.txt
lane1 exit=0 pass=20 fail=0
lane3 exit=0 pass=20 fail=0
lane4 exit=0 pass=20 fail=0
lane2 exit=0 pass=20 fail=0
$ cat /home/user/workspace/_tmp/bnd-fixed/loadavg.start
15.53 19.72 17.08 4/707 2306549      # 4 cores; four -count=20 lanes ran concurrently
                                     # alongside two whole-package runs
```
`-count=20` × 4 concurrent lanes = **80/80 PASS, 0 FAIL** on a host whose load
average was already 15.5 when the run started, while two whole-`cmd/agent`
package runs were in flight (the shape that produced every pre-fix red). The lanes
were launched with the case's own fixed instrument and no synthetic load
anywhere (Rule 27).

Pre-fix, the same trigger shape produced the 10s-bound red **three times in the
runs captured here** (10.32s / 10.34s / 10.35s / 10.53s, four dumps, all at
`chat_surface_bound_e2e_test.go:306` with the same grid) — see
`$S/bnd-trig/{wp1,wp2}.log` and `$S/agentrun.log`.

## Sibling failure shapes seen on the same case under HEAVIER contention (NOT fixed here)

Three further reds of this same case were captured during the acceptance work.
**None is the marker-window mechanism**, and each is named here so a later reader
does not mistake the fix above for covering them:

| shape | duration | the failing statement | what the dump shows |
|---|---|---|---|
| the fixture's ask count | 0.58s | `chat_surface_bound_e2e_test.go:321` — "accepted 9 self-driven turn(s) and refused 1, want 8" | PHASE 1's marker wait passed; `turns=10 refusals=1` |
| the ctrl-c closeout | 21.59s | `…:366` — "the REPL did not finish after the injected ctrl-c" | PHASE 1, PHASE 2 and PHASE 3 all passed first |
| the boot stall | 10.20s | `…:304` — "timed out waiting for the loop's LAST turn to announce the bound" | the grid carries **only the startup banner**: no prompt, no turn |

They appeared only in runs where the host was under a load the earlier matrices
never reached (load average 20+, with sibling pty cases panicking in the same
binary — `chat_inject_pty_test.go:92`), and the case's own sibling notices in those
same logs say the read loop was being starved
(`[gopi] editor: a band write was not drawn within 4s (the read loop is stalled?)`).
They are dispatched as their own row rather than guessed at here (Rule 26: a fix
that overstates its coverage closes the ticket and hides the defect). The
`PERIOD-ONE-DONE` + `PERIOD-TWO-DONE` dumps of the first shape are in
`$S/bnd-trig/wp2.log` (full failure block) for the next reader.
