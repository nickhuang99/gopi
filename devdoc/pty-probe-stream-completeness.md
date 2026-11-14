<!-- doc-class: historical -->
# The pty capability probe read a PARTIAL answer stream — a subset completeness predicate, and a fixture that now proves it

Status: fixed. Scope: `internal/lineedit/probe.go` (`probeAnswersComplete`),
`internal/lineedit/probe_pty_test.go` (the fake-terminal fixture),
`testdata/flakes.tsv` (the registry row).

## The symptom, and the hypothesis it falsified

`TestProbeMeasuresDepthFromRealAnswers` failed intermittently (measured here:
**17/100** before the fix, **31/100** in a later instrumented run) with dumps
like:

```
probe=partial cpr=yes da1=- osc11=rgb:0000/0000/0000 colors=256 rgb=unknown bytes=60 elapsed_ms=0
probe=partial cpr=yes da1=\e[?1;2c osc11=- colors=0 rgb=yes bytes=32 elapsed_ms=0
```

The briefing this task arrived with hypothesised a **wall-clock window that
expired too early**, and the obvious repair was a longer window or a settle
delay. The artifact says otherwise: **every** failing dump carried
`elapsed_ms=0`, and no dump anywhere in the run — passing or failing — carried a
non-zero elapsed time. The probe did not run out of time; it **decided it was
finished**. Rule 10.3's ordering is why this mattered: the mechanism had to be
named before any constant was touched, and the constant (`probeWindowMS`) was
never the problem.

## The mechanism

A pty is a byte **STREAM**, not a message queue: Linux pty output has no packet
mode, so `read(2)` returns whichever replies have *landed*, and nothing marks
where one reply ends. Two halves made that observable as a flake:

1. **The fixture wrote in a random order.** The responder looped
   `for prefix, reply := range r.answers` — a Go **map**, whose iteration order
   Go randomizes — and wrote one reply per query with no framing between them.
2. **The stop predicate tested a SUBSET.** `probeAnswersComplete` was
   `CursorPositionReport && (Colors > 0 || RGBKnown)` — "the answers a decision
   needs". A subset test is satisfiable while the *rest* of the stream is still
   in flight, so the probe exited as soon as its two needed answers arrived and
   the last reply(s) were never read.

Instrumenting **both sides** (replies written by the responder vs `read(2)`
calls the probe made, per repetition) put a number on it — the failing rows:

```
rep=61 wrote=2 reads=2 readSizes=[17 15] bytes=32 dump=… colors=0 rgb=yes elapsed_ms=0
rep=22 wrote=5 reads=1 readSizes=[77]         bytes=77 dump=… da1=- colors=256 rgb=yes elapsed_ms=0
rep=4  wrote=5 reads=3 readSizes=[15 25 27]   bytes=67 dump=… colors=256 rgb=unknown elapsed_ms=0
```

The answer set is 84 bytes. At failure the probe returned after having read
**32–77 of 84**, with the responder at **2–5 of its 5 replies**: replies were
still being written when the probe stopped asking. `colors=0` was therefore not
a fact about the terminal — it was a fact about *how much of the stream one
read happened to catch*.

## The fix: the stop condition became a fact about the stream

`probeAnswersComplete` now requires **an answer to every question the probe
asked**:

```go
if !m.CursorPositionReport || m.DA1 == "" || m.BackgroundColour == "" {
    return false
}
return m.depthAnswers >= probeDepthQueries || (m.Colors > 0 && m.RGBKnown)
```

* `depthAnswers` counts depth questions **answered either way** — a value
  (`colors=256`, `RGB=1`) or an XTGETTCAP **refusal** (`0 + r <name>`), which is
  the terminal stating it does not have the capability. Both arms are answers,
  so the predicate's truth does not depend on which reply the kernel delivers
  first.
* `probeDepthQueries` is **derived** — `len(sgr.CapabilityQueryNames())` — not
  typed (Rules 1/13), so a third depth name added upstream moves the count with
  it; `TestProbeStopPredicateCountsTheDepthQueriesItAsks` drives the predicate
  over one answer per query the probe actually writes and fails if the two
  disagree.
* The window is untouched: it stays the bound for a terminal that **never**
  answers (`TestProbeOnASilentTerminalMeasuresNothing` still asserts the wait).
  **No constant changed** — no longer sleep, no longer deadline.

## The fixture: the split stream is now a property of the test

The guard, `TestProbeReadsEveryDepthAnswerOneReadBoundaryAtATime`, does not hope
for a split stream; it **makes** one, from two observed facts:

1. **A declared order.** The responder's answers are a **list of `probeAnswer`**
   (name, query, reply) instead of a map, so the order is stated rather than
   randomized. The guard declares the two depth replies **first**, which is what
   removes the race from the red proof: a subset predicate is satisfied *while
   the three later replies are still owed*, so it cannot sneak past the gate.
2. **An enforced read boundary.** Each reply is held until the **kernel** reports
   that everything written before it has been read —
   `TIOCINQ`/`FIONREAD`, measured on the host: `0` fresh → `4` after a 4-byte
   write → `0` once the reader consumed them. The count is taken on the
   **reader's own descriptor** (the slave — where the probe reads); the master's
   own count stays `0`, and an early observation of `4` on the master turned out
   to be the tty's **echo**, not the reply. `probeGateCeiling` (500ms) bounds the
   wait only so that a reader which has **stopped reading** reports itself
   instead of parking the suite — it is a failure bound, never a delay an
   assertion is timed against, and the gate never calls `t.Fatalf` from the
   responder goroutine.

## Red-then-green, with the red shown by reverting only the predicate

| tree | guard (`-count=20`) |
|---|---|
| pre-fix predicate restored, fixture as shipped | **20/20 FAIL** — captures of 35, 52 and 59 bytes of 84 |
| fix in place | **20/20 PASS** |

The acceptance arm, `TestProbeMeasuresDepthFromRealAnswers -count=50`: exit 0,
0 failures (pre-fix rate on the same host: 17/100).

## What this cost, and the residual

* The measurement that made the fix possible was **two-sided instrumentation**
  (writes at the responder vs reads at the probe) — one side alone would have
  shown "no answer" without showing that an answer had been *written*.
* `TIOCOUTQ` on a pty reports **0 even with bytes queued**, so there is no
  kernel gauge of "the responder is still writing"; a quiet-stream rule for the
  probe was therefore impossible and the fix could not be stated as "wait for
  silence". This is why the predicate had to name the **questions**, not the
  line's state.
* Residual: **none observed** in the changed surface — 50 consecutive passes of
  the arm that flaked, and the same for the three other pty-driven probe tests
  (`-count=10`). The registry row is nevertheless recorded
  (`testdata/flakes.tsv`, this task's id): this test was measurably flaky at a
  named commit, and if the predicate or the fixture is ever re-opened the row is
  how the next reader finds the mechanism instead of re-deriving it.
  **Corrigendum (commit c249392c):** that row was INVISIBLE as first committed —
  the file's previously-last line had no trailing newline, so the appended entry
  inherited its leading `#` and parsed as a comment. The registry held zero rows
  for this fix and `TestFlakeRegistryMatchesTheTestsItNames` read green over it,
  because that gate checks only the direction "a registered row names an existing
  test". Proof the row is now live: renaming its test field turns the gate RED
  (`registers TestZZZBogusRow … but no such test function exists`,
  `flake_registry_gate_test.go:178`); restoring the name turns it green. Anyone
  appending a row to a `testdata/*.tsv` registry should re-read the file's tail
  after writing — the swallowing defect leaves no trace in either the file or the
  gate. The uncovered direction ("a flaky test registered by nobody") is filed as
  its own task rather than half-fixed here.
* The same predicate family is worth noticing elsewhere: **any** early-exit
  condition over a stream ("do I have what I need?") is satisfiable on a partial
  stream. The safe form is "has every question been answered, or is the bound
  reached".
