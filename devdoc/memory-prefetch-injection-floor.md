<!-- doc-class: normative -->
# Memory prefetch — the injection floor

**Task**: `01a11fbd-d040-786c-8bd6-6aa63f5d349c`
**Status**: shipped in this branch; the numbers below are from this run.

## What changed, in one paragraph

A typing-time memory prefetch that returns **fewer than 2 hits** now injects
**nothing at all** — not the hits, and not the header that would have announced
them. Everything else about the path is untouched: the recall ladder, the
keywords, the scores, the rune budgets, the three-channel switch
(`--memory-prefetch` > `GOPI_MEMORY_PREFETCH` > `settings.json` > default), and
the read-only ledger open. Two hits behave exactly as they did before.

## Why a floor, and why it is not a knob

A single hit is the shape a bag-of-words retrieval produces **by coincidence**:
the typed line shares one token with one record, and the section then asserts a
relation the ledger never made. The cost is not the bytes — it is the reader. A
block headed `1 hits` rides into the model's own request and spends attention on
an association that is not there, on **every** turn the line is sealed.

The operator's own corpus measures the size of that class. Across
`~/.gopi/sessions/*/memory-prefetch.jsonl` (407 lookup lines, 144 sessions):

| hits returned | lookups | of which `adopted=yes` |
|---|---|---|
| 0 | 136 | 57 |
| **1** | **77** | **46** |
| 2 | 10 | 5 |
| 3 | 9 | 5 |
| 4 | 10 | 9 |
| 5 | 1 | 0 |
| 6 | 164 | 152 |

**77 of 407 lookups (19%) returned exactly one hit, and 46 of those 77 were
adopted** — that is the population this floor removes from the prompt. (The
generator is `python3` over the records' own `hits` field, quoted in the report;
the corpus is the one the numbers were read at, not a re-derivable census — Rule
38 — so the figures are labelled with the file glob rather than asserted as a
property of the tree.)

The floor is **framework-level policy, not a setting**: it states what a
retrieval is *worth* (one hit is not an answer), so a knob for it would be a knob
for "inject noise, please". No channel carries it — not the CLI flag, not
`GOPI_MEMORY_PREFETCH`, not `settings.json`. `Config` has the field so a caller
can RAISE the floor, and the one direction that would weaken it is refused by
name:

```go
if c.MinInjectHits != 0 && c.MinInjectHits < DefaultMinInjectHits {
    return fmt.Errorf("%w: MinInjectHits=%d below the framework floor %d (the floor cannot be lowered)", …)
}
```

and `Config.withDefaults()` **clamps the field up** at every entry point, so a
caller that never calls `Validate` still cannot reach the weaker behaviour. The
refusal (`Validate`) and the clamp (`withDefaults`) are both here because either
alone leaves a door open: the refusal without the clamp is advice, the clamp
without the refusal is silence (Rule 15).

## Where the decision is made, and why there

**`internal/memoryprefetch/render.go` — `InjectSection`**, on `len(hits)`, the
count the caller's own query returned.

It is judged **before anything is rendered**, and that ordering is the whole
mechanism. Rendering first and suppressing afterwards would still have written
the header line (`…; 1 hits; …`), and a header **is** injection: it costs the
prompt bytes and tells the reader a retrieval happened. "Inject nothing" has to
mean no text was produced, not that produced text was dropped.

It is judged on `len(hits)` rather than on `len(seg.Hits)` (what survived the
rune trim) for the mirror-image reason: a 3-hit result whose bodies were all too
thin to render would otherwise read as "below the floor" and be reported as the
floor's doing. Those are two different refusals, and the executor now names them
with two different words:

| outcome | meaning |
|---|---|
| `empty` | the query produced no renderable section (0 rows, **or** every candidate trimmed away) |
| `below-floor` | the query returned rows, but fewer than the floor — the hits count is recorded beside it |

`below-floor` is the new vocabulary word. It is not folded into `empty` because
the two answer different questions and a reader acting on the wrong one reaches
the opposite conclusion: `empty` says the ledger had nothing on the question;
`below-floor` says it had something and the floor declined to forward it.

The reason and the decision read the **same** number: the executor asks
`memoryprefetch.InjectFloor(cfg)` rather than reading the field (Rule 14 — one
home for the value, and this is its reader).

## The A/B (Rule 31: the surface, executed)

Four arms, two binaries, a fixture ledger, each arm in its own sandbox
(`gopi tool sandbox create`, Rule 34) so the operator's profile is never touched.
The typed line is byte-identical within a pair; only the binary differs.

| arm | binary | query returns | what must happen |
|---|---|---|---|
| `base-1hit` | pre-floor (`113e2254b`) | 1 hit | a section IS injected (the defect) |
| `base-2hits` | pre-floor | 2 hits | injected |
| `new-1hit` | this branch | 1 hit | NOTHING injected, 0 bytes |
| `new-2hits` | this branch | 2 hits | injected, unchanged |

The raw streams, the session records and the driver are in
`~/.gopi/sessions/01a11fbd-d040-786c-8bd6-6aa63f5d349c/artifacts/`
(`run-<arm>.txt`, `pty_floor_probe.py`, `seed_floor_ledger.py`, `floor_ab.sh`).
The measured result is in the task report's `## Evidence` section.

**A trap this A/B walked into, recorded so the next reader does not.** The first
1-hit/2-hit fixture used short answer bodies, and the 2-hit arm **also** recorded
`below-floor`: the hits were under `MinHitRunes` (24) and the *renderer* trimmed
them away, which `InjectSection` sees as "no section" — a different refusal that
the executor was, at that point, reporting with the floor's word. Two fixes came
out of it: the fixture's bodies were lengthened past `MinHitRunes` (so the arm
measures the floor), and the executor was split into the two outcomes above (so
the reason it prints is the reason it acted on). **A fixture that measures the
wrong refusal looks exactly like a passing test.**

## What this deliberately does not do

- **No change to retrieval**: ordering, scoring, keyword selection and the recall
  ladder are untouched — the floor is consulted only after the query has
  returned its hits.
- **No per-turn throttle, no token-budget trimming, no token accounting.**
- **No new dependency** (Rule 35): no daemon, no script, no schedule, no config
  file, no second database. The change *removes* output; it adds nothing that
  outlives the process.
- **No new tool** (Rule 3), and no new environment variable.

## Files

| file | change |
|---|---|
| `internal/memoryprefetch/config.go` | `DefaultMinInjectHits = 2`, `Config.MinInjectHits`, the clamp, the refusal |
| `internal/memoryprefetch/render.go` | `InjectFloor`, the floor check in `InjectSection`, `sectionHeaderPrefix` |
| `internal/memoryprefetch/inject_floor_test.go` | new: the floor's own red proof (1 hit / boundary / unchanged-above / cannot-be-lowered) |
| `cmd/agent/chat_prefetch.go` | `prefetchOutcomeBelowFloor`, the two-outcome split at the adoption seam |
| `cmd/agent/chat_prefetch_test.go` | the shared fixture's records paired, so its lookups reach the floor |
| `cmd/agent/chat_prefetch_ladder_test.go` | same, for the ladder fixture |
| `internal/memoryprefetch/standalone_test.go` | the documented-defaults test lists the new number |

## Evidence

```bash
go test ./internal/memoryprefetch/          # floor's own red proof + the branch's
scripts/test-budget.sh -run 'Prefetch' ./cmd/agent/
```
