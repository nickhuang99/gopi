<!-- doc-class: normative -->
# auto-memory candidate schema — the provenance pointer (epic 自动记忆, item 2/5)

## Context: where scheme C was ruled

The upstream decision this document implements is **scheme C** — self-contained
full-text distilled QA storage, 4 KiB body cap, thinking excluded — ruled by the
epic's item 1/5 (task `01a11df4-abca-7214-99e9-4e389057883e`, epic 自动记忆). That
ruling's own document was written outside this branch and is **not on this tree**
(its path is not present here), so it is cited by task id rather than by file: the
ruling is what `gopi task show 01a11df4-abca-7214-99e9-4e389057883e` reads back.
Item 2/5 — this document — lands the storage shape that ruling presumes.

The storage shape for the epic's item 2: a captured `[IMPORTANT]` note becomes a
**self-contained** memory row (scheme C, above) whose text is its own body,
**plus a provenance pointer** back to the session round it came from.

A self-contained body is what makes the row correct across hosts: the row and its
text are born and die together, so a cross-host sync never needs an "unreachable
detail lives on the other node" state. The pointer is what keeps the row
**addressable** — without it, "where did this come from?" is answerable only by
the sentence inside the note.

**Body vs pointer, plainly:**

| | what it is | who needs it | where it lives |
|---|---|---|---|
| body | the distilled note's own text | the answering model, always | `distilled_qa.answer` (already there) |
| pointer | session + round + host | whoever asks "where did this come from" | the three columns added here |

## The three columns

Added to `distilled_qa`, all additive (`CREATE TABLE` for a fresh DB, `ALTER TABLE
ADD COLUMN` for an existing one):

| column | DDL | meaning |
|---|---|---|
| `session_id` | `TEXT DEFAULT ''` | the session the note was captured in — `''` = not recorded |
| `round_index` | `INTEGER DEFAULT 0` | the session's own **1-based** round number — `0` = not recorded |
| `source_host` | `TEXT DEFAULT ''` | the node that wrote the row |

`round_index` is **INTEGER, not TEXT**: the value is a number, and a text column
would sort `"10"` before `"9"`. The two TEXT columns use `DEFAULT ''` because an
`ALTER`-added column cannot be `NOT NULL` without one, and `''` is the value that
already means "not recorded" for this table's other additive columns.

## "Round index" — the definition, and why it lives in a leaf package

`internal/sessionevent/rounds.go` owns it (PROJECT.md Rule 14):

```go
const RoundIndexBase    = 1  // round 1 IS the session's first API call (iteration 0)
const RoundIndexUnknown = 0  // "no round was recorded" — below the base, so it can never pass as a round
func RoundNumberOf(iteration int) int   // iteration → the session's own 1-based round
func RoundIndexRecorded(n int) bool
func RoundIndexOf(n int) (int, bool)    // the inverse, with an ok so absence is TOLD, not given as -1
```

**The measured numbering.** A session's rounds are its API calls. The session
JSONL numbers them from **zero** — its `request` event carries `iteration=0` for
the session's first send (measured on this run's own log: 35 `request` events,
iterations 0..34) — while every user-facing surface counts from **one**
(`gopi session log <id> <round>`, the `--rounds` table's ROUND column). So the
offset is real, it is named once, and `internal/gopilog/sessionround.go`'s
`SessionRoundNumber` (the `--rounds` table's numbering) is now a thin delegation
to it instead of a second `idx + 1`.

**Why not put it in `internal/tools`** (the package that stores the value): the
same number is produced by `internal/agent` (`hippocampusRound`, the loop's own
stamp) and by `internal/gopilog` (derived from the JSONL). `internal/agent` and
`internal/gopilog` import each other, so neither can hold a definition the other
reaches, and `internal/tools` may import neither. `internal/sessionevent` is the
leaf with zero project imports that all three already share for exactly this
reason — it already owns the event names.

**Absence is a value, not a fake round.** `RoundIndexUnknown` (0) sits *below*
`RoundIndexBase`, so "no round recorded" can never be confused with round 0 or
with a real round somebody forgot to stamp (Rule 15: state absence; never
fabricate round 1).

## The write path

```go
tools.MemoryStoreWithOrigin(topic, question, answer, source, keywords, origin)
```

`MemoryStore` keeps its old signature and passes the zero `MemoryOrigin` — every
existing caller (the CLI, sync import, brew import, tests) behaves exactly as
before.

**Two shapes are storable, one is refused:**

| origin | verdict |
|---|---|
| absent (no session, no round) | stored — the row claims nothing (CLI store, import, pre-feature row) |
| session, ± a round | stored — a session whose round was never stamped is honest, `round_index` stays `0` |
| **a round with no session** | **REFUSED, before any write** — a round is a *cursor into a session*; without one it resolves to nothing, and storing it would be a claim no reader can follow |

The refusal is checked in `memoryStoreRow` (the shared core), so it applies on
every path — the tool, the CLI, the sync import — and it is checked before the
insert, so a refused store creates no row at all (pinned by
`TestOriginRefusesRoundWithoutSession`).

**A row with no pointer gets no host either.** The host is stamped only when a
session is — a row carrying `source_host` with no `session_id` would read exactly
like a row captured somewhere and never resolvable, which is the state the refusal
above exists to prevent.

## The stamp is the RUN's, not the model's

`internal/agent/memory_origin_stamp.go` stamps session + round at the store call
site. **The model passes nothing.** Session and round are properties of the run,
not judgments the model makes, and asking the model for them would (a) put two
spellings of one fact on the wire, (b) invite a hallucinated id, and (c) change the
tool schema and routing text for a value the loop already knows better — which is
also why this change enters no Rule 11 adoption loop: no model-facing surface
changed.

A run with no session log (embedded, a test) stamps **no** pointer rather than a
made-up one; `hippocampusRound` unset reports `RoundIndexUnknown`, never round 0.

A human or a script that actually HAS the fact names it explicitly:

```bash
gopi memory store --topic T --question Q --answer A \
    --session-id <id> --round-index N
```

A `--round-index` that is not a recorded round is a **usage error** (Rule 15), not
a silently dropped pointer.

## The read path

`MemoryHit` gains an exported, `omitempty` pointer field:

```go
Origin *MemoryOrigin `json:"origin,omitempty"`
```

Three properties make it safe to add:

1. **Absent stays absent.** For a row that recorded nothing the pointer is `nil`
   and the JSON field is omitted entirely, so the pre-feature render contract is
   byte-identical for every such row — and every pre-feature row is such a row.
2. **The search projection and the scan agree.** `searchExtraSelect` appends the
   columns (when the schema has them) and `scanHit` reads them in that exact
   position, both derived from the one `originColumns` list. A legacy or foreign
   schema simply has no columns to project and the scan adapts.
3. **The hit's meta line carries it**, after the existing fields and only when
   there is something to say:

   ```
   origin=sess=<id> round=<n> host=<h>
   ```

## Migration

- `ensureOriginSchema` — the same probe-then-`ALTER` pattern as
  `ensureProvenanceSchema` / `ensureFeedbackSchema`. It swallows exactly one
  error, SQLite's `duplicate column name` (the TOCTOU window where two processes
  both see a missing column and both `ALTER` — the measured #1145 incident), and
  returns every other error.
- It runs on the **first-open heal** (`initKnowledgeBase`) and on the default-DB
  migration path (`migrateDefaultTriggers`), so a fresh DB and a migrated one end
  up identical.
- A **non-default** ledger (a task's `--db`, a peer's file) is not rewritten
  behind its owner's back — only the columns this process is about to write into
  are added, and only when missing.

Nothing is dropped, nothing is rewritten: the pointer is additive metadata about a
row that already exists. An old DB keeps every row and reads the new columns as
"nothing recorded".

## Verification

See the task report (`## Verification`): `go build ./...`, the branch-scoped test
run for `./internal/tools ./internal/agent ./cmd/agent ./internal/sessionevent
./internal/gopilog`, and the real `store → search` invocation whose result is the
`origin=…` segment quoted above.

Tests pinning this document:

| test | what it pins |
|---|---|
| `TestOriginMigrationIsAdditiveAndKeepsRows` | old DB gains the columns, keeps its rows, reads them as "nothing recorded" |
| `TestOriginMigrationIsIdempotent` | three heals + a second open: no change, no loss |
| `TestOriginPointerRoundTrip` | store → search returns session, round, host; the meta segment renders |
| `TestOriginRefusesRoundWithoutSession` | refused by name, and **no row is created** |
| `TestOriginAbsentPointerStoresNoHost` | no session ⇒ no host either |
| `TestOriginLegacySchemaRendersNothing` | a hit with nothing recorded renders no segment |
| `internal/sessionevent` `TestRound*` (red-then-green) | the numbering's base, absence, and inverse |

## Not done / deferred

- **No capture path yet.** Nothing in this change *creates* an auto-memory row
  from an `[IMPORTANT]` block; this item lands the storage shape only. The capture
  and 4 KiB body cap are the epic's later items.
- **`gopi session log --origin` / a walk-back verb**: the pointer is stored and
  returned by search; a surface that resolves `session_id` + `round_index` back
  into the round's text is not built here.
- The sync layer does **not** carry the pointer across hosts (imported rows get
  the zero pointer, by design — they were not captured in a session on this host).
  Whether a pointer should travel is a decision for the cross-host item.

## Anchors retired by this change (and why that is the gate working)

Adding three columns to the `distilled_qa` const block shifts `internal/tools/memory.go`
by 3 lines. Three pre-existing docanchor permits went stale as a result, and the gate
names the remedy on each one (`delete the entry from internal/agent/testdata/docanchor-baseline.tsv`):

| permit retired | why it went stale |
|---|---|
| `devdoc/requirements/cli.md\|blank\|cmd/agent/cli_memory.go:392` | `:392` was a blank line and is now code — the permit protected blankness that no longer exists |
| `devdoc/docanchor-gate-bare-lineno.md\|blank\|memory.go:1483` (and its `(bare)` twin) | same: `:1483` is no longer blank |

One **new** permit was added, in the same style as the file's existing entries and with
the reason inline: **`devdoc/hippocampus.md|blank|(bare) internal/tools/memory.go:479`**.
That document's own header (its line 8) declares the symbol the authority and the line
number "a locator only", and its `hot layer` row cites `hotResidentInjectLimit`/
`hotResidentMaxChars` at the 2026-09-10 base `2516e09` — the constants now sit at
`memory.go:526/527`. Re-keying a landed spec's measurement is what the permit exists to
avoid; the alternative (rewriting another doc's record) would falsify it.

Evidence that this is the gate working and not drift: the docanchor failure set is
**byte-identical to the pre-change baseline** once these three rows are retired and the
one is added — measured by running the same test in a throwaway worktree at `f5ffc27e2`
and diffing the per-anchor output.

## The defect this item shipped once, and the rule it produced

**Symptom.** Three `TestAgent_Run_*Slimming` tests failed: `expected 3 rounds, got 4`.

**Root cause.** `memoryStoreOrigin` was built field-by-field:

```go
return tools.MemoryOrigin{SessionID: a.storeSessionID(), Round: a.storeRoundNumber()}
```

A run with **no session log** (an embedded agent, a test harness) resolved
`SessionID` to `""` and `Round` to the loop's real round — producing exactly the
shape the store **correctly refuses**: a round with no session. So:

1. the stamp manufactured a pointer the store's own rule rejects;
2. every memory write in such a run failed;
3. the harness's completion checkpoint saw an unsatisfied memory requirement and
   **forced another round** to make the store happen — the extra iteration the
   test measured.

The two halves were each right; the composition was wrong. That is the whole
point of the refusal being a *rule* rather than an `if` at one call site.

**The fix, and the rule it encodes** (`memory_origin_stamp.go`): **the stamp is
SESSION-FIRST**. No session ⇒ no round either, because a round is a cursor into a
session and half a pointer is a claim nobody can follow. The guard is
`memory_origin_stamp_test.go` — `TestMemoryStoreOriginIsSessionFirst` and
`TestMemoryStoreOriginNeverSendsAHalfPointerToTheStore`, the latter feeding
whatever the stamp produces back through the store's own `Complete()` rule so the
stamp and the rule cannot drift apart again.

**The red proof** (Rule 10.5): with the field-by-field form restored, the guard
fails —

```
--- FAIL: TestMemoryStoreOriginIsSessionFirst
    memory_origin_stamp_test.go:33: a session-less run stamped {SessionID: Round:4 Host:} — a round with no session is the shape the store refuses
--- FAIL: TestMemoryStoreOriginNeverSendsAHalfPointerToTheStore
    ...: round 1 with no session log produced a REFUSED stamp {SessionID: Round:1 Host:}
```

— and passes again with the session-first form. (This is also why the earlier
pre-existing-failure analysis in the task report was corrected: those three tests
are **not** baseline noise, they were this defect.)

**One deliberate asymmetry**, so a later reader does not "fix" it: the CLI's
`--round-index` without `--session-id` is a **usage error** (exit 1), while the
loop's stamp with no session is **silently empty**. That is not an inconsistency —
the flag is an explicit human claim (a typo must not be swallowed), the stamp is a
derived value (with nothing to derive it from, it claims nothing).
