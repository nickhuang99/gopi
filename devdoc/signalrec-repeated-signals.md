<!-- doc-class: historical -->
# Repeated signals are recorded, never silently dropped

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class:** historical — the record of the run named above. The standing part is
enforced by the tests it names (`internal/signalrec/signalrec_repeat_test.go`),
not by this prose.
**Task:** `01a1432c-d2de-7291-8117-35170a2c00e7` (branch
`task/01a1432cd2de7291811735170a2c00e7`, baseline `1c5b086`).
**Filed as:** Rule 41 finding of task `01a14316-b7cb-71b6-9426-48f788a85a9c`
(`gopi worker --stop` Ctrl-C), which found the second delivery of the same
signal being discarded.

## The defect

`Install` (`internal/signalrec/signalrec.go`) registers a handler for every
watched signal in every gopi process. The handler kept a set of signal kinds it
had already recorded:

```go
seen := map[string]bool{}
...
    key := classifySignal(sig)
    if seen[key] {
        continue          // ← the second and every later delivery, dropped in
    }                     //   silence: no record, no line, no fact at all
    seen[key] = true
```

Three observed consequences, all of them the same root:

- **Three SIGTERMs and one SIGTERM leave identical evidence.** The count in the
  provenance sink (`kind=signal`) is 1 either way, so an operator pressing
  Ctrl-C four times cannot tell from the trail whether their second, third and
  fourth requests arrived, were seen, or were lost.
- **A re-sent stop request is unobservable.** `gopi worker --stop` (and every
  other stop surface) sends the signal only as a *nudge*; the durable request is
  the `TERM_<pid>` file. The nudge being re-delivered is a normal operator
  action — and it left no trace that it was ever delivered more than once.
- **Rule 15 (no silent failure).** The branch is a bare `continue` on the
  process's shutdown path, which is precisely where the evidence matters most.

The **once-per-kind** dedup itself is deliberate and stays: a fleet-wide kill
must not look like twice as many dead processes as there were.

## The change

A repeat is now a **re-affirm**: recorded, and deliberately inert.

| | first delivery | repeat (2nd, 3rd, …) |
|---|---|---|
| provenance record | `kind="signal"` (`KindSignal`) | `kind="signal-repeat"` (`KindSignalRepeat`) |
| session log | `"type":"signal_verdict"` | `"type":"signal_repeat"` |
| note | — | `re-affirm: delivery N of SIGTERM — the first delivery's disposition stands (no re-raise, no duplicate death record)` |
| publications (`noteSignalReceived` / `noteVerdict` / `noteRecordFiled`) | published | **not** published |
| `reRaise` | per the existing predicate | **never** |
| handler disposition | — | **unchanged** |

Three decisions carry the design:

1. **A kind of its own, not a second `KindSignal`.** `scripts/night-throughput.sh`
   counts `kind == "signal"` as terminations and the operator's incident grep is
   `grep -r '"kind":"signal"'`. Writing repeats as deaths would inflate exactly
   the number the dedup exists to protect. The two sentences — "a gopi process
   died here" and "the same request arrived again" — are different facts, so
   they get different words.

2. **A session-event type of its own, not a second `signal_verdict`.** That
   event answers "was this run killed, and did a gopi stop path announce it?" —
   ONE answer per run. Folding re-affirms into it would turn four Ctrl-C presses
   into four verdicts. `EventTypeSignalRepeat` is a plain JSON object with no
   `message` key, so every reader that switches on event type without a default
   skips it structurally (the format decision `EventTypeSignalVerdict` already
   documents).

3. **`write` was split, and the repeat arm must not reach `noteRecordFiled`.** A
   claiming door (`AwaitRecordFiled`) waits for the DEATH record's filed
   answer; a re-affirm that published FILED would let a door settle on the wrong
   record. So the append moved to `appendRecord` (no publication) and `write`
   keeps the publication step plus a comment saying why it is there. The
   re-affirm additionally reports a failure *its own way*
   (`repeated signal NOT RECORDED`) instead of through `reportUnfiled`, whose
   `refuseOnce` latch and whose `noteRecordRefused` answer belong to the death
   record alone.

**Nothing about the disposition moves**: `signalForwarding()`,
`modesThatOwnTheirSignalHandlers`, `ClaimSignalHandlers()`, `--force`/SIGUSR1
are untouched (asserted by
`TestSignalRecRepeatNeverMovesSignalForwarding`, and by the untouched
`TestSignalRecForwardingPreservesDefaultDeath` table).

## Why the re-affirm does not re-raise

`signalForwarding()` decides the death; the repeat inherits a *finished*
decision. If a repeat re-raised, an operator's second Ctrl-C would apply the
default disposition to a process that was deliberately unwinding — the exact
damage `ClaimSignalHandlers` exists to prevent. And for a process that *does*
forward, the first delivery's re-raise already ended it, so a second delivery
can only arrive while the process is still alive precisely in the case where it
must **not** be killed early.

## Verification

```
Baseline (pristine 1c5b086, red proof — repro only, no new symbol):
  --- FAIL: TestSignalRecRepeatedDeliveryLeavesEvidenceInsteadOfSilence (5.15s)
      3 deliveries of SIGTERM left 1 provenance record(s), want 3 —
      every delivery but the last of its kind was DROPPED IN SILENCE

Fixed tree:
  ok  github.com/nick/gopi/internal/signalrec  9.167s
  ok  github.com/nick/gopi/internal/sessionevent  0.832s
  gofmt -l internal/signalrec internal/sessionevent   → empty

Live, a real gopi binary, 3 × SIGTERM to one live process
(artifacts/live-repeat-run.txt in this run's session dir):
  1 "kind":"signal"
  2 "kind":"signal-repeat"
  re-affirm: delivery 2 of SIGTERM
  re-affirm: delivery 3 of SIGTERM
```

## Honest limits

- The repeat record is written by the SAME goroutine as the death record, so two
  deliveries that arrive inside one handler iteration collapse into a single
  channel delivery (Go's `signal.Notify` channel is buffered and coalesces). The
  ordinal therefore counts *handler iterations*, not syscalls — the honest
  claim is "the delivery was seen", which is what the defect lost.
- A repeat is recorded only while the process is alive to receive it. A stop
  path that re-sends after the victim is gone leaves nothing in the victim's
  files (the sender's own `kill(1)` refusal is the evidence there — see
  `devdoc/worker-control-ctrl-c.md`).
