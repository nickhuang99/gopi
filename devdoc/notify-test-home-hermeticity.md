<!-- doc-class: normative -->
# A notify rig owns its HOME — the sink's fallback anchor is not a test target

**The defect (measured 2026-11-03).** `scripts/notify/notify.sh` refuses a
`GOPI_HOME` that names a CODE CHECKOUT and then **anchors the payload where the
value should have pointed**: `$HOME/.gopi/notify/task-events.jsonl`. That
fallback is deliberate — refusing to anchor would lose real events from
production worktree children, which is the very failure the guard exists to
prevent — and it is pinned from the other side by
`notify_slot_contract_test.sh` case 6 ("the event is ANCHORED in the operator's
own profile, never lost").

So the sink is right and it must stay right. **The danger is on the TEST side.**
`notify_peer_health_test.sh` case 7 hands the sink a worktree-shaped
`GOPI_HOME=$H7` (which carries `.git`) and let `HOME` be **inherited** — so every
full-suite run appended

```json
{"event":"completed","task_id":"t-1","status":"completed","summary":"local"}
```

to the operator's live evidence file. Measured on `~/.gopi/notify/task-events.jsonl`
before the fix: **12** rows carried `"task_id":"t-1"` (the epic predicted 9 — the
count is measured, not assumed), among **6136** lines. Those rows are NOT deleted:
an append-only evidence file is not edited by a test fix, and a delete would
destroy evidence of the very leak being fixed. They are named in the report.

**A rig owns its inputs; HOME is one of them.** The fix is at the test seam, not
in the sink:

1. Every sink call in `scripts/notify/*_test.sh` now names `HOME` explicitly
   (`$RIG_HOME`, or a per-case `$RIG/case*-home` where the case asserts on it),
   so the refusal's fallback lands inside the rig and cannot reach a home the rig
   did not create.
2. `notify_hermeticity_test.sh` makes the class non-recurring, in both directions:
   * **STRUCTURAL** — a scan of every sibling rig: a sink invocation that carries
     `GOPI_HOME` but no explicit `HOME` is reported at its line. This fires on the
     SOURCE, before a payload is written. A planted control call site proves the
     scan is not vacuous.
   * **BEHAVIOURAL** — every rig is run with `HOME` pointed at a FRESH EMPTY
     directory (`GOPI_TEST_SCRATCH_HOME` pinned too, or `tmp-ban.sh` derives the
     children's own scratch from `HOME` and the guard reports its subjects'
     scratch trees as leaks — measured, 20+ false paths). Afterwards no
     `task-events.jsonl` may exist there and no `"task_id":"t-1"` may appear
     anywhere under it.

**The two halves catch different failures.** The scan is a text gate and can be
routed around (a helper, a sourced file); the behaviour half only sees today's
fixtures. The pair is cheap and names the file when it fires.

**What the guard does NOT own, and says so.** A rig that is red for its own
subject still ran its sink calls, so its status is REPORTED and not judged —
binding this guard to a sibling's unrelated red would make it go red for a cause
it does not own. A vacuum floor (`>= 5 rigs scanned`, `>= 4 green`) refuses a run
in which almost nothing executed, so a green is never the green of an empty
corpus.

**The alternative that was REJECTED** is "make the sink refuse and write
nothing", which would silently lose production events to close a test leak —
notify.sh's own fallback text names `$HOME/.gopi` as the destination and
`notify_slot_contract_test.sh` case 6 pins it from the other side. Rule 35 also
rules out a new knob: `HOME` and `GOPI_HOME` already exist and are exactly the
channel needed.
