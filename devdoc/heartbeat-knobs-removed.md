<!-- doc-class: normative -->
# The heartbeat knobs were advertised, read, stored — and consumed by nothing

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Task `01a0ed9f-2e60-7572-9d60-a63396cec9f3` · branch `task/01a0ed9f2e6075729d60a63396cec9f3`.
Rule 21's defect shape, measured on the env face: a knob an operator can set,
that `gopi --help-env` documents, and that changes nothing.

## The defect, as a chain (not as a claim)

Each link below was measured, not inferred. The arrow is the whole bug: the
chain is complete up to the point where a consumer should appear, and there is
none.

```
GOPI_HEARTBEAT_SEC / GOPI_HEARTBEAT_STALE        advertised in `gopi --help-env`
                                                 (internal/config/help.go, "Agent runtime")
  → read in NewAgent's env block                 internal/agent/loop.go
  → assigned to Agent.HeartbeatInterval /        internal/agent/loop.go (the Agent{} literal)
    Agent.HeartbeatStaleRounds
  → NOTHING reads either field                   0 production readers
```

And the module those two fields were meant to feed was equally unconsumed:

- `internal/agent/heartbeat.go` declared `HeartbeatState`, whose constructor
  `NewHeartbeatState(interval, staleRounds)` had **no production caller at all**
  — every call site was in a test;
- inside it, the `interval` field had **ZERO readers**: the struct stored the
  tick period and never consulted it, so even a wired timer could not have been
  configured by it;
- `SetOnRemind` / `ShouldRemind` / `ReminderCount` had no production callers
  either (`grep` over `internal/` + `cmd/` resolved every name to the file that
  declared it, plus tests).

So the knob's gradient ran `env → field → (nothing)`. Nothing in the tree could
notice: the value round-tripped, the field was written, and the only surface a
reader would check — the help page — advertised it.

**The live role was already occupied.** `maybeNudgeStaleTaskList`
(`internal/agent/loop.go`) fires on the same three conditions the dead tracker
described (no tool running, an established task list, unfinished tasks) at the
same 3-round cadence, and it does so *better*: it carries an anti-harassment cap
(`maxNudgeCount`) and a prompt-contract backup that states the emission cadence
to the model — neither of which the 30s-timer form had.

## Why option (a) — "wire it into maybeNudgeStaleTaskList" — was rejected

The repo's own audit named both fixes as acceptable (`devdoc/crosscutting-doc-truth-audit.md`,
R7: *"wire it into maybeNudgeStaleTaskList or delete the fields + env entries"*).
Wiring was rejected on evidence:

1. **The thing being wired is dead, not merely unwired.** Wiring `HeartbeatState`
   into the loop would have installed a state machine whose own `interval` field
   is unread — a 30s timer that cannot be re-timed — and whose three trigger
   conditions duplicate `maybeNudgeStaleTaskList`'s. Two mechanisms deciding the
   same question is how they drift apart.
2. **The knob could never have reached the live mechanism anyway.** The live
   comparison is against `maxRoundsWithoutTaskList`, a *different* hardcoded
   constant = 3 (`internal/agent/loop.go`). `HeartbeatStaleRounds` is not that
   constant and does not feed it, so "wire it" would have meant either
   multiplying two thresholds or silently ignoring the operator's value —
   a worse outcome than removing a knob that never worked.
3. **(a) does not remove the defect; it relocates it.** Rule 21's requirement is
   that an advertised knob has a consumer. Wiring a *second* stale-detector
   makes the original timer live and leaves the divergence in place.

Deleting was therefore the smaller, more honest change: the knobs never had a
consumer, so no operator behaviour depended on them.

## The divergence this exposed (follow-up, NOT fixed here)

**The nudge threshold is hardcoded while the prompt contract states a cadence.**
`maybeNudgeStaleTaskList` compares `staleRounds` against
`maxRoundsWithoutTaskList = 3`, a package constant with no configured channel;
the system prompt's task-list contract (rendered from `emissionContract`, which
interpolates that same constant) tells the model to re-emit the block on a
**3-tool-round** cadence. The two numbers agree today by coincidence of their
literals, not by construction: the constant is a *round-count* trigger and the
contract is a *tool-round* cadence, and nothing ties them together. Removing the
heartbeat knobs does not touch this — it is recorded here because the removal is
where the mismatch became visible, and because a future "let us make the nudge
threshold configurable" would otherwise re-introduce exactly the shape this task
removed: an advertised knob with no consumer, or a consumer with two sources.

## What changed

| surface | change |
|---|---|
| `internal/agent/heartbeat.go` | **deleted** (wholly dead module) |
| `internal/agent/heartbeat_test.go` | **deleted** (kept the symbols alive and nothing else) |
| `internal/agent/cov_agent_cr_test.go` | the `CurrentTasks` coverage block removed with its subject; the file's own count updated |
| `internal/agent/loop.go` | the two `Agent` fields, the env-read block and the two struct-literal assignments removed |
| `internal/config/settings.go` | both catalog rows removed |
| `internal/config/config.go` | `HeartbeatSec` / `StaleRounds` fields removed |
| `internal/config/env.go` | the two `setInt` publications removed |
| `internal/config/flags.go` | the `heartbeat-sec` / `heartbeat-stale` case arms removed |
| `internal/config/help.go` | both names removed from the "Agent runtime" section entry |
| `CONFIG.md` | both §2c rows removed |

The **section stayed**: the removal dropped two names, not the grouping an
operator navigates by. `internal/config/heartbeat_knob_removal_gate_test.go`
asserts exactly that (see below).

## The regression gate

`TestRemovedHeartbeatKnobsStayRemoved` (`internal/config/heartbeat_knob_removal_gate_test.go`)
freezes the removal in **three directions**, because the defect could return
through any of them — and a gate that only reads the catalog would miss a
re-wiring:

1. the **catalog** must not carry either env name or its settings key;
2. the **rendered front door** — `FormatEnvTable()`, the text `gopi --help-env`
   prints (NOT `FormatUsage()`, which renders the flags section) — must not
   carry them, and the "Agent runtime" section must still render;
3. **production code** must not name them, must not look up a
   `--heartbeat-sec`-style flag, and must not re-declare `HeartbeatState`; the
   deleted file must stay absent.

**Rule 13 note, so a later reader does not "fix" the literals**: the two
spellings appear as constants *in the gate* deliberately. Rule 13 bans a bare
literal standing in for a constant that exists; here no constant may exist —
declaring `heartbeatSecEnv` would resurrect the removed spelling as a
first-class knob name. The literal **is** the artifact under test.

The `HeartbeatState` predicate is a regexp over *declarations*, not a substring
match: `internal/agent/taskheartbeat.go` (a live, different module) names the old
type in a comment explaining the precedent it set, and a gate that failed on
prose is a gate its owners route around.

### Red proof (measured, both directions)

Reverting the removal by hand — restoring one catalog row, re-adding a
`os.Getenv("GOPI_HEARTBEAT_SEC")` call in `internal/agent/loop.go`, and recreating
`internal/agent/heartbeat.go` with the type — turns the gate red and it names
**all four** independent directions (catalog row, `--help-env` rendering,
production read, dead-file resurrection). Restoring the tree turns it green
again. Both runs are recorded in the task's report.

## Verification

- `CGO_ENABLED=0 go build ./...` — clean.
- `go test ./internal/config/ ./internal/standards/ ./internal/agent/ -count=1` — see the report.
- `gopi --help-env` executed for real: neither name renders, the Agent-runtime
  section does.
- The doc-anchor gate (`TestDocAnchorsResolveInNormativeDocs`) is compared
  against a **base measurement** taken from `git archive master`: this change
  adds zero new drift. Two permits went stale because the doc they excused no
  longer anchors the deleted file; they are deleted, not repointed. Note for a
  later reader: master itself is already red on this gate with eight
  pre-existing stale permits from an unrelated refactor, so "green" is not the
  available criterion here — "adds nothing new" is.

## Not done

- The threshold/contract divergence above is **recorded, not fixed**: it is a
  separate defect with its own decision (one source of truth for the cadence),
  and folding it into a knob-removal would have made this change argue two
  things.
- The operator's own settings.json may still carry `heartbeat_sec` /
  `heartbeat_stale` keys. They are now **inert** — unknown keys are not an error
  — and are deliberately not rewritten: deleting a key from a file this change
  does not own is not the removal's business.
