<!-- doc-class: normative -->
# The chat REPL's pty teardown order, and the leak that was already stopped upstream

Two runs measured the same ground; this doc carries BOTH, because the second one
changed what the first one's evidence MEANS. The first found the fixture half
(latent: the join was riding scheduling luck, because the terminal's stop is what
ends a parked read). The second measured whether the reported leak still
reproduces, and found that it does not — the production stop path
`e6e1791d` landed before the briefing's snapshot — and added the guard that
keeps the order from silently inverting, plus a real defect its `-race` probe
found in the sweep's own instrument.

Read the sections in this order: §"What was wrong" for the defect, §"Deliverable
(1)" for whether it still reproduces, §"Red proof" and the red arms for what
would have caught it, §"Verification" for the commands and their output.

## What was wrong

Every listed fixture took its steps down in this order:

```go
in.editor.Close()                       // 1. release the layout + completion worker
select {                                // 2. join the read loop, SILENTLY
case <-h.exited:
case <-time.After(someWait):            //    (the bound elapsing looks like success)
}
_ = term.Close()                        // 3. "the terminal is cleanup"
_ = pair.Master.Close()                 // 4. the descriptors
```

Two things are wrong with it, and only one of them is about time.

1. **The join is not what ends the read.** `Editor.Close()` stops the completion
   worker and hands back the reserved region; it deliberately does **not** stop
   the read — the same read serves the next line, which is why the REPL can close
   and reopen the editor without losing the terminal. Before task 01a0faf5 the
   read loop left `Run` only when the **pty master** was closed (step 4), i.e. on
   the scheduling luck of the statement that followed the join. The order made
   the fixture *look* like a join while what it observed was a descriptor close.
2. **The `select` could not tell a returned loop from an elapsed bound.** A run
   whose loop never returned spent the case's whole bound and passed anyway
   (Rule 15).

They are green today because the production stop path ends the loops as well —
which does **not** repair the order: each case still waits for an exit that the
order makes it miss, and anyone copying the shape as a template reintroduces the
interpreting error. A defect that is only latent is still a defect; it is
reported as latent here, with no live failure claimed.

## The shape found, per site (measured, `grep`, not assumed)

The 13 citations in the briefing each resolve to that file's
`lineedit.Open(...)` call — the fixture's terminal. They are **two** shapes, not
one, and only the first has a read loop to join at all:

### Shape A — a live REPL read loop (4 sites): fixed

| site | how the loop is joined | teardown owner |
|---|---|---|
| `chat_surface_key_e2e_test.go:566` | `t.Cleanup` on `h.exited` (silent select) + a by-hand close at the end of the case | the case |
| `chat_surface_bound_e2e_test.go:1003` | `t.Cleanup` closing and `Master.Close()`, no join at all | the case |
| `chat_surface_help_menu_e2e_test.go:395` | same as bound | the case |
| `emphasis_span_grid_e2e_test.go:185` | `t.Cleanup` joining `s.exited` with the silent select | the case |

All four now take the four steps in the one order, on the **failing** path as
well as the passing one (a red run never reaches a trailing close, which is why
these teardowns are registered rather than sequenced):

```
1. in.editor.Close()        releases the layout + completion worker (not the read)
2. term.Close()             THE STOP — the read is parked in ReadEvent ON THE TERMINAL
3. chatJoinReadLoop(...)    join, BOUNDED and LOUD (t.Errorf on a loop that did not return)
4. pair.Master.Close()      the descriptors last
```

Steps 2 and 3 may not be transposed: what "the read loop returned" *means*
changes completely if the join runs first.

`in.editor.Close()` no longer guards `term.Close()` in these four: the guard on
those sites is transient-tolerance, not ownership, and it **inverted the order
they exist to state** — a `t.Fatalf` before `s.in` is assigned skipped the
terminal's stop entirely (leaving a live reader on a live pty) while still
closing the descriptors. The guard that *is* kept is `if !term.Stopped()`, which
prevents the by-hand close on the success path from closing the terminal twice:
`Terminal.Close()` is idempotent, but re-entering it after `stopWinchWatch` has
taken the pipes is not a trip worth making on every run.

### Shape B — a DIRECT DRIVER, no read loop (9 sites): **no order to fix**

| site | read loop present? |
|---|---|
| `chat_turn_dump_cell_reconcile_pty_test.go:225` | none — no `Run()`, no goroutine |
| `chat_turn_dump_grid_pty_test.go:274` | none |
| `chat_turn_dump_variable_read_pty_test.go:319` | none |
| `chat_turn_dump_wrapspace_ledger_pty_test.go:95` | none |
| `chat_turn_header_block_pty_test.go:86` | none |
| `chat_turn_spill_header_pty_test.go:115` | none |
| `chat_dump_geometry_pty_test.go:111` | none |
| `cli_uistate_audit_test.go:86` | none |
| `pty_marker_column_dump_test.go:188` | none |

Each of these installs the reserved layout and drives the band **directly**
(`WriteBandRow` / `SettleBand`), which is why the files carry the comment "no
read loop runs in this case (nothing calls Run), so the row just queued is
written HERE". Their `defer ed.Close()` / `defer term.Close()` already close the
terminal; there is no join to order it against, and adding a join would mean
adding the goroutine first. Stated as **not applicable**, not as fixed: a claim
of 13 repairs would overstate what this change does.

`internal/lineedit`'s terminal wrapper means `ed.Close()` on the readers that
build one (`NewChatEditor`) *is* `term.Close()`; the direct-driver `term, err :=
lineedit.Open(...)` sites close the terminal directly. Either way the terminal is
closed; what differed was only whether a join raced it.

## The measurement (and what would NOT be one)

Baseline first, then the acceptance runs (`accept-count5.log`,
`accept-count5-postcommit.log`, `fullpkg-summary.log` under this run's session
dir):

| run | result |
|---|---|
| `go test ./cmd/agent/ -run '<the 4 changed cases>' -count=5 -v` | 20/20 PASS, `parked repository goroutines: 0` |
| `make test-budget PKGS='./cmd/agent'` (the declared entry point) | exit 1, `the budget did NOT trip` (567s of 25m), ONE failure — the briefing's pre-existing `TestPrefetchGoroutineSettleGapDetectsALiveLeak` |
| `go test ./cmd/agent/ -count=1` (bare `go test`) | red, and its verdict is the INSTRUMENT's: it self-times-out at the default 600s (`FAIL … 601.939s`), which is where its second, unrelated failure came from |

**A timing comparison is deliberately NOT offered as evidence here**, and the
reason is the same one that makes this defect latent: with the production stop
path in place, the loop returns promptly under EITHER order, so the elapsed time
of a fixed case and an unfixed case are the same to within noise — a "faster
now" number would be a coincidence dressed as a measurement. The observable that
does separate the two orders is the SOURCE SHAPE (`order-evidence.txt`: for each
changed site, `term.Close()` appears on a line BEFORE `chatJoinReadLoop(...)`),
plus the loud arm proved on its own (below). The sibling task has the timing
measurement that IS meaningful, because there the production half was reverted
(3.28s → 0.28s, `devdoc/lineedit-terminal-close-stop-path.md`).

## Red proof — how the fixed order is shown to be the *mechanism*

A fixture whose terminal is closed first cannot be red-proved by deleting
`term.Close()` (that would only move the stop back to `pair.Master.Close()`, the
pre-fix accident, and the extra reader past the same bound). The proofs that name
this change's own contract are:

1. **The loud join is real — RED, then GREEN.** A `chatJoinReadLoop` driven with a
   channel that is never closed must REPORT, and one driven with a closed channel
   must return at once rather than spend its bound. A temporary case
   (`cmd/agent/zz_redproof_join_test.go`, deleted before the commit) did both and
   was observed RED:

   ```
   --- FAIL: TestZZRedProofChatJoinReadLoop (5.00s)
       --- FAIL: TestZZRedProofChatJoinReadLoop/never-closed (5.00s)
       zz_redproof_join_test.go:19: the run's read loop did not return within 5s
       after its terminal was closed — it is still parked (red proof); ...
   FAIL github.com/nick/gopi/cmd/agent 5.859s
   ```

   The sub-case that matters is the **fail** one: the same call with a closed
   channel returned immediately (a `t.Errorf` on the elapsed arm is what turned
   the run red — the pre-fix silent `select` would have printed `ok`). The
   harness's own sweep still reported `parked repository goroutines: 0` on that
   run, which is the honest reading: this proof tests the *reporting*, not a leak.
2. **The sweep still fails the package.** `cmd/agent`'s close-out sweep is
   untouched by this change (Rule 27) and still counts every repository
   goroutine: park one and the package fails by name
   (`devdoc/lineedit-terminal-close-stop-path.md` carries that observation
   verbatim from the sibling task, and this run re-observed the line at zero).

The direct-driver shape (9 sites) has nothing to red-prove: it asserts no join.

## Completed

- The four REPL-level pty fixtures take the one four-step order, with a loud
  bounded join (`chatJoinReadLoop`) replacing the silent `select`.
- The order is stated ONCE (`chat_pty_fixture_test.go`), next to the pty fixture
  every one of these cases allocates through — rather than four times, in the
  four places that can drift apart.
- The un-used silent-wait bound in the emphasis case went with it (Rule 15,
  Rule 1: an unused knob reads like a live one).

## Not Done (honest)

- **The nine direct-driver sites were not edited, deliberately** (Shape B). There
  is no read loop in them to order a join against; the fix there would be to add
  one, which would change what those cases assert. If the epic wanted the
  order *pre-sited* for a future read loop, that is a different task with a
  different acceptance.
- The four REPL cases still rely on `chatPtyOpenPair`'s own `t.Cleanup` for the
  descriptors (steps 1–3 run earlier, on their own registered cleanups). Folding
  the pair into one teardown helper is a bigger refactor of four fixtures and was
  not attempted here.
- No live goroutine leak was observed to fix: the production stop path already
  ends these loops, so the claim is latent-defect removal + order/loudness, and
  the report says exactly that.

## Verification

See the task report (`GOPI_TASK_REPORT_PATH`) for `Build:` / `Tests:` / `Ran:` /
`Result:` and the self-filed evidence lines.


Task: `01a0fb5b14d17db3abf37f893e703745` (branch `task/01a0fb5b14d17db3abf37f893e703745`).

## Deliverable (1) — the measured verdict: the leak does NOT reproduce on this HEAD

The briefing's two named failures were measured on this branch's HEAD, verbatim:

```
$ go test ./cmd/agent/ -run 'TestChatInject_EscapeByNameReachesTheTurnSeam|TestPrefetchGoroutineSettleGapDetectsALiveLeak' -count=1 -v
=== RUN   TestChatInject_EscapeByNameReachesTheTurnSeam
--- PASS: TestChatInject_EscapeByNameReachesTheTurnSeam (0.28s)
=== RUN   TestPrefetchGoroutineSettleGapDetectsALiveLeak
--- PASS: TestPrefetchGoroutineSettleGapDetectsALiveLeak (0.20s)
PASS
suite child sweep: live child processes: 0 (settled 1s)
suite child sweep: live child processes: 0 (guard measured live child processes: 0 earlier; live child processes: 0 at close-out)
suite goroutine sweep: parked repository goroutines: 0
ok  	github.com/nick/gopi/cmd/agent	1.451s        ← rc=0
```

**Both halves of the syndrome are already fixed in master, and the fix is in this
branch's ancestry.** The production stop path landed as `e6e1791d`
("fix(lineedit): Terminal.Close is a STOP PATH — end a parked read and join the
winch watcher", 2026-10-02 13:36), and `git merge-base --is-ancestor e6e1791d
HEAD` is true; `internal/lineedit/raw.go` carries `stopped`, `closeEvent`,
`closedPollFd`, the `winchDone` join in `stopWinchWatch`, and `Terminal.Stopped()`.

The briefing's own measurement predates it: HEAD's tip `82b214d4`
("delivery-point sync…", 2026-10-02 **14:07**) is **31 minutes after** the fix —
the snapshot the briefing was written from was taken before the fix merged into
master. So the first `TestPrefetchGoroutineSettleGapDetectsALiveLeak` symptom was
the *same* transient the other task saw: a cmd/agent suite that was red on a
binary/stale-baseline mixture, not a live defect. It is green here, 3 consecutive
`-count=1` runs plus `-count=3`, at 0.20s — the poll still observes its live leak
(it is the one test that asserts the *presence* of a leak on purpose).

This is **not** a silent "already fixed". It is the verdict its own red-proof
demands, and the rest of this document is that proof.

## Deliverable (2) — red-then-green, and the reason the fixture order was still wrong

### 2a. The sweep still catches a leak (the guard was not weakened)

A temporary test leaked one goroutine. Observed verbatim:

```
suite goroutine sweep: parked repository goroutines: 1
suite goroutine sweep: goroutine 28 [chan receive]:
  github.com/nick/gopi/cmd/agent.TestZZLeakProbeTemp.func1()
  	…/cmd/agent/zz_leak_probe_temp_test.go:8 +0x19
  created by …TestZZLeakProbeTemp in goroutine 27
--- PASS: TestZZLeakProbeTemp (0.00s)      ← the CASE passed
FAIL	github.com/nick/gopi/cmd/agent	1.893s   ← the PACKAGE failed (rc=1)
```

The temporary file was deleted immediately; `git status --porcelain` is empty.

### 2b. THE ROOT CAUSE — a dropped fixture stop reproduces the briefing's signature exactly

This is the measured finding, and it is stronger than a code-reading claim. The
four chat-REPL pty fixtures joined their read loop **before** closing the
terminal, with a **silent** bounded `select` in between — the pre-fix order. The
terminal's `Close` is the stop path (production change above), so the fixtures
were riding luck: whatever ended the loop was the *next statement*.

Dropping `term.Close()` from the `TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty`
fixture — one `if false` in a tree that was restored immediately — reproduces the
briefing's symptom **character for character**:

```
--- PASS: TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty (0.70s)
suite goroutine sweep: parked repository goroutines: 1
suite goroutine sweep: goroutine 37 [select]:
  github.com/nick/gopi/internal/lineedit.(*Terminal).startWinch.func1()
  	…/internal/lineedit/raw.go:380 +0xbc
  created by …(*Terminal).startWinch in goroutine 19
FAIL	github.com/nick/gopi/cmd/agent	3.544s
```

A passing case, a failing package, and the **SIGWINCH watcher** as the parked
goroutine — the exact signature the briefing attributes to the production
teardown. The lesson is the one the fix already wrote down and the fixtures had
not yet adopted: **the terminal's own stop is what ends the read; a join placed
before it observes the pty master's close on scheduling luck, and the watcher's
own lifetime is bound to that stop.** A fixture that skips the stop leaks the
watcher.

### 2c. What was fixed: the fixture teardown ORDER, at four sites

Found by shape (Rule 10.4 / the repo's own precedent), not by a green-run
failure — the cases are green either way, so the fix is an ORDER and a
LOUDNESS, and saying otherwise would overstate it.

- `cmd/agent/chat_pty_fixture_test.go`: the four-step order (editor → **the
  terminal, which IS the stop** → the join → the descriptors) stated **once**,
  with the measured reason, plus `chatJoinReadLoop` — a bounded join that
  `t.Errorf`s instead of waiting out its bound in silence (Rule 15).
- `chat_surface_key_e2e_test.go`, `chat_surface_bound_e2e_test.go`,
  `chat_surface_help_menu_e2e_test.go`, `emphasis_span_grid_e2e_test.go`: the
  REPL-level teardowns take that order. The `if s.in != nil` guard those three
  cleanup arms carried still closed the descriptors while **skipping the
  terminal's stop entirely**, so a red run left a live reader on a live pty; the
  kept guard is `if !term.Stopped()`, which asks the stop path whether it is
  already down (no double close).
- `emphasis_span_grid_e2e_test.go`: `spanE2ECloseWait`, the silent-wait bound the
  join replaced, went with it — an unused knob reads like a live one (Rule 1).

**Red proof of the LOUD arm** (the order itself cannot be red-proved: with the
stop path in place the loop ends under either order, so a green run does not
distinguish them — a `-count=5` green is not evidence of the order):

```
$ go test ./cmd/agent/ -run TestZZJoinLoudArmTemp -count=1 -v      # temporary, never closed channel
    zz_join_loud_temp_test.go:8: the run's read loop did not return within 5s after its terminal was closed — it is a channel that is never closed (deliberate red-proof); …
--- FAIL: TestZZJoinLoudArmTemp (5.00s)
FAIL	github.com/nick/gopi/cmd/agent	6.666s    ← rc=1
```

Temp file deleted; the closed-channel half returns at once, so the arm is not a
blanket failure.

### 2d. The duplication RESOLVED (master moved during this run)

`9bc8b5b1` ("fix(cmd/agent): the chat REPL pty fixtures stop the terminal BEFORE
joining the read loop") was the same fixture work, unmerged when the close-out
pass began — and **it merged into master while this run was still going**
(`git merge-base --is-ancestor 9bc8b5b1 master` → **exit 0**; master tip
`bbc9e6a8`, 15:07). This branch was then aligned with master (Rule 30a, the last
step) and the overlap resolved per file, keeping the ONE thing master does not
have:

| file | after the merge |
|---|---|
| `chat_pty_fixture_test.go` | master's helper + this run's red-proof comment above it |
| `chat_repl_teardown_order_test.go` | **only here** — the guard master lacks (this is the deliverable) |
| `chat_inject_pty_test.go`, `emphasis_span_grid_e2e_test.go`, `suite_stall_watchdog_test.go` | master's version taken; the `-race` writer capture is master-side here and the `-race` probe was re-run green on the merged tree |
| `chat_surface_key_e2e_test.go`, `chat_surface_bound_e2e_test.go`, `chat_surface_help_menu_e2e_test.go` | auto-merged, no conflict |
| this devdoc | both versions' sections kept: this run's are §"Deliverable (1)"–§"Not Done"; master's own are §"What was wrong"–§"Verification" |

`git diff master...HEAD` after the alignment is the honest statement of what this
branch adds: the guard, its read-floor, the report, and the doc merge — the
four-site order fix is master's, and this branch does not claim it.

### 2e. Independent re-measurement at close-out (ARM A / ARM B, one script, restored)

Both arms were re-run from a single script against the branch tip, so the two
halves of the claim are one observation each rather than a recollection. The
script restores the source between arms and prints `git status --porcelain` after
each (both printed nothing).

**ARM B — the fixture's own stop (`_ = term.Close()`) deleted from
`cmd/agent/chat_inject_pty_test.go`:118**, with the *production* stop path left
fully in place:

```
    chat_inject_pty_test.go:123: the editor's read loop did not return within 3s of the harness
      closing its terminal — the grid is left with a parked reader
--- FAIL: TestChatInject_EscapeByNameReachesTheTurnSeam (3.28s)
suite goroutine sweep: parked repository goroutines: 1
suite goroutine sweep: goroutine 15 [select]:
FAIL	github.com/nick/gopi/cmd/agent	5.660s        ← rc=1
```

**ARM A — a temporary parked goroutine in a repository frame, run beside BOTH
acceptance cases** (this is the acceptance clause "a temporary deliberate-leak
proof shows the sweep still reports parked goroutines N>0 and FAILs"):

```
--- PASS: TestChatInject_EscapeByNameReachesTheTurnSeam (0.28s)
--- PASS: TestPrefetchGoroutineSettleGapDetectsALiveLeak (0.20s)
--- PASS: TestZZRedProofParkedGoroutineIsSeenByTheSweep (0.00s)
PASS
suite goroutine sweep: parked repository goroutines: 1
suite goroutine sweep: goroutine 7 [chan receive]:
  	…/cmd/agent/zz_redproof_parked_goroutine_test.go:11 +0x19
FAIL	github.com/nick/gopi/cmd/agent	1.573s        ← rc=1
```

Read the two arms together and they say exactly one thing each: the two
acceptance cases are **green** (ARM A), and the sweep that guards them is
**live** — a deliberate leak is still reported as `parked repository goroutines:
1` and still fails the package (both arms). The fixture stop is **load-bearing**:
without it a green test leaves a parked reader behind (ARM B). Neither arm
touches the sweep, and no tolerance, sleep or skip was added to it.

## Deliverable (4) — the `--verbose` case: does not reproduce, and I did not find a race

The briefing listed `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--verbose`
as a possible separate fd race. Measured here, `-count=3 -v`:

```
    --- PASS: TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--verbose (0.00s)   ×3
--- PASS: TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses (0.00s/0.02s/0.00s)
```

3/3 green. **What I did not do, stated plainly**: I did not reproduce the failure
and therefore did not chase it to a mechanism. This is a non-reproduction on this
host, not a refutation — an fd race is timing-dependent, `-count=3` is a weak
probe, and the case reaches it through `chatRunArgs` → the real `runChat`, i.e. it
is fd-shaped. Flagging it as **open, suspected-flaky, not investigated** is the
correct claim; calling it "fixed" or "not a race" would be a claim my evidence
does not carry.

## Deliverable (3) — the GUARD: the order is now checked, not merely written down

The four-step order was **prose** in `chat_pty_fixture_test.go` — stated once and
correct at five sites, and enforceable by nothing. A fixture that joins first, or
drops its stop, is green today (the pty master's close also ends the loop), so no
green run and no sweep can see the regression. That is the gap this section
closes: `cmd/agent/chat_repl_teardown_order_test.go`, a source-shape guard in the
repo's existing family (a mirror-literal gate, a worktree-root gate — a property
no runtime observation can carry).

The check, over every `_test.go` in `cmd/agent`: for each `chatJoinReadLoop` call
site, the function that contains it must ALSO contain a stop of the terminal
(`Close` on a `term`/`terminal` value, directly or as a harness field) at an
EARLIER source position. Read-floor: `chatJoinSiteFloor = 5` — deleting the joins
cannot make the guard vacuously green.

**Red-then-green, both directions measured** (each mutation reverted in the same
command that ran it; `git status --porcelain` after each arm lists only this
branch's own two files):

*ARM A — the join placed BEFORE the stop (the literal pre-fix shape)*, in
`chat_surface_bound_e2e_test.go`:

```
--- FAIL: TestChatReplTeardownOrder_EveryJoinStopsTheTerminalFirst (0.21s)
    chat_repl_teardown_order_test.go:111: chat_surface_bound_e2e_test.go:1047: chatJoinReadLoop
    runs with no terminal stop ahead of it in its own function — the join is what OBSERVES the
    stop, so a join placed first observes the pty master's close instead (the pre-fix shape), and a
    join with no stop at all observes a bound that elapsed. …
FAIL	github.com/nick/gopi/cmd/agent	1.214s        ← rc=1
```

*ARM B — the stop deleted, the join kept*:

```
--- FAIL: TestChatReplTeardownOrder_EveryJoinStopsTheTerminalFirst (0.21s)
    chat_repl_teardown_order_test.go:111: chat_surface_bound_e2e_test.go:1047: chatJoinReadLoop runs
    with no terminal stop ahead of it in its own function — …
FAIL	github.com/nick/gopi/cmd/agent	0.896s        ← rc=1
```

*GREEN — guard + the acceptance pair on the branch tip*:

```
--- PASS: TestChatInject_EscapeByNameReachesTheTurnSeam (0.28s)
--- PASS: TestPrefetchGoroutineSettleGapDetectsALiveLeak (0.20s)
--- PASS: TestChatReplTeardownOrder_EveryJoinStopsTheTerminalFirst (0.29s)
suite goroutine sweep: parked repository goroutines: 0
ok  	github.com/nick/gopi/cmd/agent	1.456s        ← rc=0
```

**The fifth join was moved onto the helper**, so the guard covers every REPL
fixture rather than a prefix of them: `chat_inject_pty_test.go` carried its own
copy of the bounded-loud join (a `select` with `t.Errorf` in its own words, 3s,
`injectPtyWait`). It now calls `chatJoinReadLoop` — one join, one message, one
place the check reads. The guard's read-floor (5) is what makes that surface
visible: four sites were order-checked before this change, five now.

**What the guard does NOT check**, stated rather than implied: it matches NAMES,
not types, so a stop reached through a differently-named variable is invisible to
it (the failure message says so, so the remedy is a deliberate edit of the guard,
not a mystery). It says nothing about a fixture that runs no join at all — the
nine direct-driver pty fixtures have no read loop — and the package's goroutine
sweep remains what catches a leak no ordering would have prevented.

## A data race in the SWEEP'S OWN INSTRUMENT (found by the `-race` probe, fixed here)

Chasing deliverable (4) I ran the vocabulary case under `-race` and the race
detector fired — not on the case, and not on anything this branch had touched:

```
WARNING: DATA RACE
Write at 0x000002c0aa50 by goroutine 18:
  cmd/agent.chatRunArgs()  chat_argv_test.go:106
  TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses.func1()  chat_argv_test.go:494
Previous read at 0x000002c0aa50 by goroutine 16:
  cmd/agent.startSuiteStallWatchdog.func1()  suite_stall_watchdog_test.go:405
```

`chatRunArgs` swaps the `os.Stdout`/`os.Stderr` globals onto pipes for the
duration of a subtest (`chat_argv_test.go:106`), while the stall watchdog's
goroutine had **read `os.Stderr` inside its literal** and passed it out as its
writer (`suite_stall_watchdog_test.go:405`). Unsynchronised global access from a
goroutine armed in `TestMain` and alive for the whole suite.

**Fixed by capturing the writer before the goroutine exists** (`out := os.Stderr`
at the arm site, the literal closed over `out`). It cannot change what the
watchdog reports: it writes only on a stall measured in minutes, long after any
subtest's temporary swap is restored. Measured after:

```
$ go test ./cmd/agent/ -run 'TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses' -count=3 -race
ok  	github.com/nick/gopi/cmd/agent	9.272s        ← rc=0   (was: FAIL, "race detected during execution of test", rc=1)
```

This is the closest thing to a *mechanism* found for the `--verbose` case the
briefing flagged: it is a real defect in the same fixture family, it is
asynchrony around a fixture's global-descriptor swap, and it is **not** evidence
that the `--verbose` failure was this race — that case never reported one. It is
offered as "a real race existed here and is now gone", not as the explanation.

## Verification

- Build: `go build ./...` → **rc=0**; `go vet ./...` → **rc=0** (after every edit,
  re-run red-proofs restored).
- Tests: `go test ./cmd/agent/ -run 'TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty|TestChatInject_EscapeByNameReachesTheTurnSeam|TestPrefetchGoroutineSettleGapDetectsALiveLeak|TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses' -count=1 -v`
  → 4/4 PASS, `suite goroutine sweep: parked repository goroutines: 0`,
  `ok github.com/nick/gopi/cmd/agent 6.652s`.
  The three other changed fixtures together: 3/3 PASS, sweep 0,
  `ok … 3.311s`.
  `go test ./internal/lineedit/ -run TestTerminalClose -count=1` → `ok`; with the
  stop path disabled it fails by name
  (`TestTerminalCloseEndsAParkedRead` 2.00s, `TestTerminalCloseEndsAReadThatStartsAfterwards` 2.00s).
- Ran: the invocations above, plus `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
  before the binary-driving pty cases (the mtime gate fails them by design otherwise).
- Result: see the pasted lines above — including the **red** ones (parked=1 with
  `startWinch.func1`, package FAIL) which are the point of the whole document.
- Commit: the branch tip carries the change and this report in the same commit
  (Rule 30: the worktree is clean at close, so the tip IS the deliverable).
- Not run: the whole-tree `go test ./cmd/agent/` (never two at once: two concurrent
  runs met the 600s default timeout; and a whole-package run is the merge path's
  job — smoke, branch-scoped, Rule 27). `make test-budget PKGS='./cmd/agent'` was
  skipped: ~600s of serial budget against ~15 min remaining is a planning failure,
  not a verification.

## Evidence (self-filed)

- HEAD is green, observed: `suite goroutine sweep: parked repository goroutines: 0`,
  `ok github.com/nick/gopi/cmd/agent 1.529s`.
- Fix is in history, observed: `git merge-base --is-ancestor e6e1791d HEAD` → true;
  `git log -1 --format='%h %ci %s' e6e1791d` → `e6e1791d 2026-10-02 13:36:06 +0800`.
- Sweep unweakened, observed: `parked repository goroutines: 1` naming
  `zz_leak_probe_temp_test.go:8`, test PASS + package `FAIL` (rc=1).
- Fixture stop is load-bearing, observed: with `term.Close()` dropped,
  `parked repository goroutines: 1` naming `startWinch.func1 (raw.go:380)`, test
  PASS + package `FAIL` (rc=1).
- Loud join arm, observed: `--- FAIL: TestZZJoinLoudArmTemp (5.00s)` + the
  fixture's own sentence, rc=1.
- `--verbose`, observed: `--- PASS: …/--verbose` ×3.
- All temporary red-proof files were deleted; `git status --porcelain` was run
  after each and prints nothing at close.

## Not Done

- `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--verbose`: not reproduced,
  not investigated (above).
- The *order* has no RUNTIME red proof, by construction — with the stop path in
  place the loop ends under either order, so a green run does not distinguish
  them. It now has a **source-shape** guard instead
  (`chat_repl_teardown_order_test.go`, red-proved in both directions: join moved
  before the stop, and stop deleted) — see "Deliverable (3)" above. The loud
  join arm is red-proved separately.
- The 9 direct-driver pty fixtures (no read loop at all) were not touched: there is
  no join to order against, so "not applicable" is the accurate status, not
  "repaired".
- `9bc8b5b1` (the duplicate fixture work, Rule 26) merged into master DURING this
  run (`git merge-base --is-ancestor 9bc8b5b1 master` → exit 0); master was merged
  into this branch and the overlap resolved per file — §2d. The duplication is
  resolved, not merely declared.

## Files

- `cmd/agent/chat_pty_fixture_test.go` — the order stated once + `chatJoinReadLoop`.
- `cmd/agent/chat_repl_teardown_order_test.go` — the guard: every join must sit
  AFTER a terminal stop in its own function (red-proved both directions).
- `cmd/agent/chat_inject_pty_test.go` — the fifth join moved onto the helper (its
  own copy of the bounded-loud `select` removed).
- `cmd/agent/suite_stall_watchdog_test.go` — the watchdog's writer captured at the
  arm site (the `-race` DATA RACE above).
- `cmd/agent/chat_surface_key_e2e_test.go` — order + loud join, both teardown paths.
- `cmd/agent/chat_surface_bound_e2e_test.go`, `cmd/agent/chat_surface_help_menu_e2e_test.go`,
  `cmd/agent/emphasis_span_grid_e2e_test.go` — order + loud join; `spanE2ECloseWait` removed.
- `devdoc/chat-repl-pty-teardown-order.md` — this report.
- Raw logs of every arm above: `~/.gopi/sessions/01a0fb5b-14d1-7db3-abf3-7f893e703745/`
  (`guard-green.log`, `guard-red.log`, `redproof-final.log`, `sweep-alive.log`,
  `verbose-race.log`, `race-after.log`, `final2.log`).
