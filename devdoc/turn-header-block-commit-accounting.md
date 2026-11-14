<!-- doc-class: normative -->
# The turn-header block's commit accounting — the expectation was one leg, the ledger counts two

**Scope**: one test file, `internal/lineedit/turn_header_block_test.go`
(`TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`). No production code
changed: the implementation was right and the expectation was stale.

## The red, reproduced first

```
$ go test ./internal/lineedit/ -count=1 -run TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow
--- FAIL: TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow (0.00s)
    turn_header_block_test.go:364: BeginTurn committed 8 row(s), want 5 (one per block row)
FAIL	github.com/nick/gopi/internal/lineedit	0.546s
```

Reproduced in this run's own worktree at cols=40 (the harness width), before
believing any archived measurement.

## Why 8, and why 5 was never the whole turn's opening

The case was written by `fe51f47e` (*feat(chat): the turn header becomes a
multi-line block with a counted ending*), where `beginTurnNow` wrote exactly the
header block and nothing else. `54c5212d` (*the four-layer format table —
request/thinking/response/code-tool*) landed **52 minutes later** and added one
line to the same function:

```go
r.drawRequestRow()   // beginTurnNow, internal/lineedit/answer.go:4465
```

That row is the operator's own line (`▸ <prompt>`, `RequestGutter` + `pal.Prompt`),
committed as the turn's FIRST body row so that a tool call — which is announced
before the answer streams — can never open a block whose request header arrives
afterwards. It commits through the ordinary path (`writeRow` +
`finalizeLiveRow`, `layers.go`), i.e. **through the same `Committed()` counter**,
wrapped at the same width and capped at `requestRowMaxRows` (3).

Measured at the fixture's width (temporary probe, deleted before commit):

```
PROBE header=5 requestRows=3
PROBE hdr[0]="──── turn 1 · 从日志里找出失败… ────────"
PROBE hdr[4]="... (+3 more lines)"
PROBE committedAfterBeginTurn=8
```

5 + 3 = 8. **No row is double-counted, no row is lost, and no block row wrapped**
— the block's own 5 rows are exactly the block's 5 rows. So the ledger is
correct and the assertion's scope was wrong: it measured the turn's *opening*
total while naming the *block*.

The counter is package-wide by design — `layers_appendonly_test.go` asserts
"committed rows == visible rows", and a second counter for the header leg would
be the second definition Rule 14 forbids. The repository's other cases already
read the opening as the SUM: `turn_layout_test.go`
(`got != 3 { // header + request + answer }`). This case was the one that never
caught up.

## The fix

The expectation names the leg it is about, and reads the other leg from the ONE
composer that draws it, so two spellings of the same arithmetic cannot drift:

```go
reqRows := requestRowRows(prompt, turnHarnessCols)   // layers.go's own composer
if len(reqRows) == 0 { t.Fatalf(...) }               // the case must exercise both legs
h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: prompt})
if got := h.answer.Committed(); got != len(want)+len(reqRows) { ... }
```

The guard on `len(reqRows) == 0` is load-bearing: a fixture change that stopped
producing a request row would otherwise make the case pass while silently no
longer exercising the sum it asserts.

The rest of the case is untouched and still holds the accounting the epic cares
about: every block row is on the grid, in order, un-wrapped, and the block ends
in the counted marker row.

## Not a licence to rename the red away

The **implementation** arm of this epic — "BeginTurn commits one row per block
row" — is verified true by the same run: the grid leg of the case compares the
band's rows against `TurnHeaderLines` row by row, so a block row that failed to
commit (or wrapped into two) still fails. What changed is only where the
assertion stops measuring the other leg's rows as if they were the block's.

## Verification

`Ran:` — the surface changed is a test, so it was executed directly, plus the
package that owns it.

- Red first, then green, same command:
  `go test ./internal/lineedit/ -count=1 -run TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`
  → before: `FAIL ... BeginTurn committed 8 row(s), want 5`; after: `ok`.
- Package: `go test ./internal/lineedit/ -count=1` (all green).
- Build: `go build ./...`.
