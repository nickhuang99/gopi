<!-- doc-class: normative -->
# Task Report: doc-anchor gate — make bare `:NNN` continuations resolvable, and re-key every drifted anchor

Branch: `fix/docanchor-bare-lineno` (worktree `~/workspace/gopi-docanchor`, off master `17cec86b`)

## Completed

### 1. The gate now sees both anchor forms (`internal/agent/docanchor_test.go`)

The old gate matched only `` `path.go:NNN` `` — one backtick pair carrying the
path AND the line. The two gated specs write most of their anchors as a **bare
continuation**: the first row of a table names the file, later rows continue it
with `` `:NNN` ``. Those never matched, so they were never checked, while the
gate printed a green `doc anchors verified: 72`.

Added:

- `docAnchorGoPathRe` — every backticked `.go` token on a row (the file context).
- `docAnchorBareRe` — the bare `` `:NNN` `` form.
- Resolution for a bare anchor, in precedence order:
  1. nearest preceding `.go` token **on the same row**;
  2. else an earlier row of the **same table block**;
  3. else the file named by the nearest preceding **markdown heading**
     (how §1.1's prose anchors are written under
     `### 1.1 记忆面：\`internal/tools/memory.go\``).
- Same range check (past EOF) + blank-line check as the explicit form, plus
  unique-basename resolution with an AMBIGUOUS rejection.
- A bare anchor with no `.go` token in reach is counted **out of scope** and
  reported in the summary — never silently counted as verified.
- `minDocAnchorsChecked` raised 20 → 200: the floor now covers BOTH families, so
  dropping either one, or gutting the docs, fails the build instead of passing
  with a smaller number.

### 2. Repro test for the hole (Rule 10.5)

`TestDocAnchorBareLineRules` drives the scan over synthetic docs and asserts:
bare resolves via the row token; bare past EOF is RED; bare on a BLANK line is
RED; a later row inherits the block's file; the block's file is range-checked;
the row's own token wins over the block's; an unattributable bare is out of
scope (counted 0, verified 0). The scan was refactored into the pure predicate
`docAnchorScan` so it is testable without the repo tree.

### 3. Every drifted anchor re-keyed by SYMBOL IDENTITY

Both specs' rows were re-pointed at the line that **defines** the symbol the row
names (func/type/var/const line), not at the line the old number happened to
hit. Before/after per row: `devdoc/docanchor-beforeafter.txt` (committed)
(109 lines, every changed row with old→new per anchor).

Headline drift, all from master:

| doc:row | before | after | what moved |
|---|---|---|---|
| `hippocampus.md:11` | `internal/agent/loop.go:2695` / `:2685` | `:3296` / `:3296` | `runEngine` moved ~120 lines |
| `hippocampus.md:37` | `memory.go:1015` | `:1499` | `MemorySearch` |
| `hippocampus.md:38` | `:43` | `:55` | `memoryDefaultLimit` |
| `hippocampus.md:45` | `:282` | `:467` | `type MemoryHit` |
| `hippocampus.md:95` | `internal/agent/loop.go:1418` | `:1393` | `type Agent struct` |
| `hippocampus.md:99` | `:2729` | `internal/agent/loop.go:3043` | the job-notice injection point |
| `hippocampus.md:152` | a line number past that file's EOF (**BLANK/EOF**) | `:11`/`:12` | the dead heartbeat state machine (file since deleted — 2026-09-29) |
| `ontology.md:131` (§4.2) | `memory.go:1244` (**BLANK**) | `:1483` | `hits := make([]MemoryHit, …)` |
| `ontology.md:163` (§4.2/4.4) | `job.go:89` | `:91` | `jobManager` |
| `ontology.md:148` (§4.2) | `:2386` / `:2397` / `:2406` | `:2519` / `:2530` / `:2539` | `Run`/`Resume`/`runEngine` |
| `ontology.md:154` (§4.2) | `:170` / `:158` / `:146` | `:172` / `:160` / `:148` | the three forced-message consts |
| `ontology.md:155` (§4.2) | `:1578` / `:2305` | `:1679` / `:2427` | `NoEnforceMemory` / its env |
| `ontology.md:157` (§4.2) | `:2341` / `:2358` / `:2368` | `:2474` / `:2491` / `:2501` | the interrupt trio |
| `ontology.md:158` (§4.2) | `:4146` | `:4491` | `maybeNudgeStaleTaskList` |
| `ontology.md:172`–`183` (§4.5) | dead/blank | measured | whole heartbeat/rhythm block |

Two genuine doc defects found while re-keying, beyond line drift:

- both docs mixed up the two heartbeats: `taskHeartbeatStaleThreshold` was
  described as living at `taskheartbeat.go:44` (the env-const comment), and
  `hippocampus.md:148` said "not `agent/heartbeat.go`" while its own anchors
  resolved INTO `agent/heartbeat.go`; the rows now spell their file explicitly.
- the dead-field row cited `:2322`/`:2323` as the write sites; measured writes
  are `internal/agent/loop.go:3178`/`:3179`.

### 4. Verification

- `go test ./internal/agent/ -run TestHippocampusDocAnchorsResolve -count=1 -v`
  → `doc anchors verified: 271 across 2 spec files (114 explicit + 157 bare
  continuations; 3 bare anchors out of row/table scope)` — **PASS**.
  Was 72 on master (all explicit, all silent).
- Mutation proof (the gate must go red), run END-TO-END on the real doc, not
  only on synthetic fixtures — both times by editing
  `devdoc/agent-five-organs-ontology.md` row 131 and re-running the gate:
  - **the exact regression this task exists for**: `memory.go:1483` →
    `memory.go:1244` (the original blank-line defect) →
    `anchor internal/tools/memory.go:1244 points at a BLANK line — the anchor
    drifted off its symbol` **FAIL**. On master this same value printed
    `doc anchors verified: 72` and passed.
  - **the bare form**: the row's continuation `:1526` → `:9999` →
    `anchor (bare) internal/tools/memory.go:9999 is past EOF (3517 lines)`
    **FAIL** — and note the resolved file is `internal/tools/memory.go`, i.e.
    the bare anchor inherited the file from its row's token. That inheritance is
    precisely what master never performed, which is how the §4.2 anchor column
    drifted unexamined.
  - Both mutations were reverted; `git diff` vs HEAD is empty (byte-identical)
    and the gate is green again.
- `go test ./internal/agent/ -count=1` → ok (11.2s).
- `go test ./internal/standards/` → ok; `go test ./internal/stylegate/` → the
  gate's own pre-existing failures only (identical on master at `17cec86b`; the
  new test file contributes 0 hits after the two `//stylegate:ignore` annotations).
- `CGO_ENABLED=0 go build ./...` → OK; `gofmt -l` clean; `go vet ./internal/agent/` clean.

## Not Done (honest)

- **The gate checks range and blankness, not CONTENT.** A non-blank wrong line
  is still green — e.g. the bare `:1483` in §1.1 prose would pass even if the
  anchored function moved, because line 1483 stays non-blank. Asserting the
  line's text requires the doc to carry the expected symbol; the gate's rule is
  that the symbol name in the same row is the authority, and the re-key pass
  enforced symbol identity at authoring time (mechanically: every anchored line
  was checked to contain its row's symbol). Making content-identity mechanical
  is a follow-up, not part of this fix.

  **Measured whether that follow-up is viable before deferring it** (throwaway
  positional-pairing probe over the explicit anchors: the Nth symbol token on a
  row pairs with the Nth anchor). Result: only **37 of 47** pairable rows
  actually contain the row's symbol name at the anchored line — a mandatory
  content check would therefore be RED on ~10 sites the day it lands, e.g.
  `GOPI_TASK_NOTIFY_EVENTS` anchored at `tasknotify.go:63`
  (`taskNotifyEventsDefault = "…"` — the env NAME's value constant, both correct)
  and `GOPI_HOME` → `const SessionsDirName = "sessions"`. These are *legitimate*
  anchor styles (name → its value const; call site → its call), so the predicate
  needs a pairing model richer than "contains the row's symbol" before it can be
  a gate. Deferring it is the correct call, and the number is recorded here so
  the follow-up starts from evidence rather than rediscovering this.
- 3 bare anchors remain out of row/table/heading scope (prose with no `.go`
  token anywhere above in reach); they are reported, not verified.
- The re-key was applied with throwaway tooling under
  `/home/user/workspace/_tmp/docanchor/` (not committed, but its output is
  preserved as `devdoc/docanchor-beforeafter.txt`). The durable artifacts
  are the two re-keyed docs, the gate, and `beforeafter.txt`.

## Files

- `internal/agent/docanchor_test.go` — the gate (regexes, 3-candidate
  resolution, range/blank checks, raised floor) + `TestDocAnchorBareLineRules`.
- `devdoc/hippocampus.md` — anchors re-keyed (§1.1–§1.5, §2–§4, §6).
- `devdoc/agent-five-organs-ontology.md` — §4.1/§4.2/§4.5 anchors re-keyed.
- `devdoc/docanchor-beforeafter.txt` — per-anchor before/after (the durable copy
  of `/home/user/workspace/_tmp/docanchor/beforeafter.txt`).

## Verification (raw)

```
$ go test ./internal/agent/ -run TestHippocampusDocAnchorsResolve -count=1 -v
    docanchor_test.go:125: doc anchors verified: 271 across 2 spec files (114 explicit + 157 bare continuations; 3 bare anchors out of row/table scope)
--- PASS: TestHippocampusDocAnchorsResolve (0.10s)

$ go test ./internal/agent/ -run TestDocAnchorBareLineRules -count=1 -v
--- PASS: TestDocAnchorBareLineRules (0.00s)   [7/7 subtests PASS]

$ CGO_ENABLED=0 go build ./...   # exit 0
```
