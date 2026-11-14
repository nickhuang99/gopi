<!-- doc-class: historical -->
# The ctrl-c closeout wait: a progress criterion instead of a wall clock

**Task** `01a10a47-de78-765f-b125-4eda36babf6f` · branch `task/01a10a47-de78-765f-b125-4eda36babf6f`
**Case** `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`
(`cmd/agent/chat_surface_bound_e2e_test.go`)

## The defect, stated as the measurement took it

The case's ctrl-c closeout wait was a **fixed wall-clock window**
(`boundWait = 20 * time.Second`, shared with every other wait in the file). Inside
`make test-budget PKGS='./cmd/agent'` on 4 cores at load 16–18 it expired while the
run was **still working**, and the failure it printed was indistinguishable from a
wedged session.

Read out of the failing run's own stream
(`/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791172835-100811.json`,
`load_start=16.38 15.65 11.75`, reproduced by
`run-1791172666-79126.json`, `run-1791171012-4097155.json`, … — twelve streams
carry the same sentence):

```
1.61  PERIODS: period 1 accepted 8 turn(s) numbered 1..8 (wire 1..8) with 1 refusal(s);
              period 2 accepted 8 numbered 1..8 (wire 10..17) with 1 refusal(s)      ← green
1.61  ctrl-c injected: accepted=true readByEditor=true pending=0 took=1ms            ← the key ARRIVED
3.61  ctrl-c closeout: the run has not left yet (2.001s of 20s … — 46 model round(s) served so far)
…
19.63 ctrl-c closeout: the run has not left yet (18.021s of 20s … — 46 model round(s) served so far)
21.61 the REPL did not finish within 20s after the injected ctrl-c;
      the goroutine is still running and the mock served 46 model round(s)
        --- durable tail ---
        … [gopi] turn interrupted by ctrl-c (operator, 7 chars shown) — the session is
          still open; type the next prompt, /quit to exit. …
bye.                                                                                 ← one poll later
21.61 --- FAIL: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (21.61s)
```

Four facts, each about the **run**, not about the tail text:

1. **The key was delivered and taken** — the injector's own observation,
   `readByEditor=true took=1ms`, present on every red stream.
2. **The interrupt was claimed** — the durable tail already carried
   `turn interrupted by ctrl-c (operator, 7 chars shown) — the session is still
   open`, i.e. `claimTurnInterrupt` (cli_chat.go) claimed the key for a turn in
   flight and the loop kept going. So this is the fixture's *designed* shape, not
   a lost key.
3. **The mock's served-round count did not move at all** (46 at 2s, 46 at 18s)…
4. **…while the session finished and left one tick after the expiry** (`bye.`,
   `chat table: … ended (normal)`, `exitAfterInterrupt`'s sibling — the goroutine
   did exit).

So the wait measured **the host**, not the session. On 4 cores a fixed window is a
contention meter: the same 20 s that is generous when the box is idle expires when
four whole-package runs are on it, and it says so in a sentence — "did not finish"
— that reads identically for "the run is still working".

## What was ruled out first (so it is not re-walked)

The per-turn seam is the obvious suspect and it is **not** this failure: a probe
that injected ctrl-c the instant period 2's closure marker was visible read
`MinTurnActive=false` at that instant — the key arrived **at the prompt**, where
ctrl-c means *leave*, so a wait keyed on that predicate would have been an
unjustified change wearing a fix's clothes. It was removed rather than shipped.

## How this landed on current master (rework 2026-10-05)

The original branch (`task/01a10a47-de78-765f-b125-4eda36babf6f`) was deleted before
it could be merged, so this landed from its diff alone, on master `99e65d50`.

Two files conflicted, and both are the SAME situation: **master already carries a
later, self-consistent version of the same idea** (commit `f760e920`, "the bound
case's ctrl-c closeout waits on PROGRESS, not on a wall clock" — an ancestor of
this work's own base `f991bf6d`, so the diff was written without it). The ruled
outcome (see the three-way rule in memory #5720) is **take master**, and union in
only what master does NOT have:

| what | master `f760e920` | this landing |
|---|---|---|
| criterion | progress, decided by `boundCloseoutVerdict(stillFor, waited)` | **kept** — same rule, same three states |
| where it lives | **inside** `exitAfterInterrupt`'s body: a private loop no test can drive | **the branch's contribution**: factored into `waitForCloseoutProgress`, driven directly by the red proof below |
| stall window / ceiling | `boundStallWindow = 5s`, `boundCloseoutCeiling = 3min` | **master's values kept** — the diff proposed a second pair (`boundCloseoutStallWindow = 30s` / a 5-minute ceiling) that would have duplicated a decision already merged, so it was NOT reintroduced; the wait takes them as PARAMETERS, which is what makes it drivable |
| progress note pacing | `boundStallNoteInterval` | **kept** (the diff's `boundWait / boundExitNoteEvery` form references a constant master deleted in the same commit) |
| the wait's three exits | inside the method's `switch` | named type `closeoutWaitOutcome` (`closeoutLeft`/`closeoutStalled`/`closeoutCeiling`) so a caller — and the red proof — can read which exit happened without parsing a log |
| `DOC-INDEX.tsv` | one row per devdoc | the diff's row is re-generated through the repo's own writer (`gopi doccheck --fix`), because master had to place the same row in the same alphabetical block (a same-block conflict, not an EOF append) |

The branch's red proof (`chat_surface_bound_closeout_wait_test.go`) came over whole:
the wait it drives is the function above, and every value it asserts is a PARAMETER
of that function, so none of master's constants were re-opened to satisfy it.

## The fix

The claim "one operator ctrl-c terminates the session" is unchanged; what changed
is **what the wait is a statement about**.

`exitAfterInterrupt` now waits on the run's **own progress**, through one factored
predicate (`waitForCloseoutProgress`, same file):

```go
progress := func() (served, painted int) { return rounds(), s.paint.painted() }
```

- **`served`** — the mock's answered model rounds (the fixture's statement about
  the wire);
- **`painted`** — the bytes the editor has written (the console's statement about
  itself).

**Both halves are load-bearing**, and fact (3) above is why: on the measured
failing run the round count stood still at 46 for the whole 20 s while the editor
repainted throughout, so a round-only predicate would have declared a working run
stuck, and a paint-only predicate would call a wedged editor live. *Either one
moving* is the session doing something.

Three exits, and only the first satisfies the claim:

| outcome | when | meaning |
|---|---|---|
| `closeoutLeft` | `s.exited` closes | the session ENDED — the case's claim |
| `closeoutStalled` | nothing moved for `boundStallWindow` | the defect this case exists to catch — **still a failure**, same message, same durable tail |
| `closeoutCeiling` | still moving at `boundCloseoutCeiling` | a livelock, deliberately **not** the claim |

The numbers are named constants (Rule 1), and the ones the wait uses are the ones
master already carries, each with its own provenance in the const block:

- `boundStallWindow = 5 * time.Second` — how long the closeout tolerates a run that
  makes NO progress at all (no round served AND not one byte painted). The wait
  reports the **longest no-progress gap it saw** on every run, on both paths, so
  the window can be sized from observed behaviour instead of guessed: a working
  session's silence has measured sub-second here.
- `boundCloseoutCeiling = 3 * time.Minute` — a **hang detector**, the same role
  `scripts/test-budget.sh`'s per-package budget plays: a run that keeps moving can
  never trip the stall window, so without a ceiling a livelock would *hang*
  `cmd/agent` instead of failing the case (trading a flake for a hang).
- `boundProbeStallWindow = 300 * time.Millisecond` / `boundProbeProgressStep =
  10 * time.Millisecond` — the red proof's own scale, the same pattern as
  `chatTurnStatus.liveTick`. Not a second policy: the release path passes
  `boundStallWindow`.

One further instrument was added at the injection site (not a behaviour change):
the case now logs `ctrl-c injection-time state: minTurnActive=… modelRoundsServed=…
bytesPainted=…`, which is the pair the closeout wait reads — so a future red run
carries the state of the wait's own criterion at the instant the key was taken.

**Nothing was loosened**: no assertion was removed, no bound was widened in the
claim, no sleep was added to make a run pass. `boundWait` still bounds every other
wait in the file; it is no longer the closeout's bound because a wall clock was
the wrong dimension for that one claim.

## The red proof (Rule 10.5)

`cmd/agent/chat_surface_bound_closeout_wait_test.go` drives the predicate directly
at its own scale — no pty, no mock model, no HTTP:

| test | what it pins |
|---|---|
| `TestBoundCloseoutWaitFiresWhenTheRunStopsProgressing` | **RED PATH**: a run that produces nothing for the whole stall window ends `closeoutStalled`, and it is the stall window that ended it (the elapsed time is asserted to be below the ceiling and at/above the stall) |
| `TestBoundCloseoutWaitStaysAliveWhileTheRunKeepsProgressing` | a run that keeps **painting only** for 4 stall windows ends `closeoutLeft` — the measured failing shape, with the clock stretched past every bound the old wait had |
| `TestBoundCloseoutWaitCeilingCatchesALivelock` | a run that never stops moving is bounded by the ceiling, so a livelock can never hang the package |

Master's own guard (`TestBoundCloseoutVerdictIsProgressNotAClock`, `f760e920`) is
kept where it stands: it pins the VERDICT's rule (`boundCloseoutVerdict`), while
these three pin the WAIT that reads it — the two are the two halves of one
criterion, and neither is a substitute for the other.

## What remains unmeasured

- **Where the key goes between one turn returning and the read loop parking on the
  next read.** The closeout still takes ~20 s under load *because the run really
  was still finishing work*; the wait no longer misreports it, but the latency
  itself is not explained (the per-turn claim was ruled out by the probe above).
  It is dispatchable on its own.
- **Whether 30 s is the right margin** beyond this host. The number is honest — a
  loading margin over an observed, now-reported gap — but the gap has only been
  observed here (sub-second at load 18–27, on runs where the console paints a
  status row between rounds).
- The whole-package acceptance run's own duration (~15 min at load) is unchanged;
  this change neither speeds the suite up nor slows it down.
