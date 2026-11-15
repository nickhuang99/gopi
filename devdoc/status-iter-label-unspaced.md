<!-- doc-class: normative -->
# The iteration label carries no space — and the withdrawn "different defect"

Task `01a10dd0`. The status row's iteration field printed `iter 9` (a space
between the word and the count). `statusIterCol` reserves 7 cells, so the form
"iter " (5 cells) left the number 2 — and the FIRST three-digit count a session
reached was cut by `lineedit`'s own column cut (`padStatusColumn`) into `iter 1…`.
A truncated NUMBER is the one thing this row must not print, so the defect was not
a shortage of cells but a formatter that spent one on a space.

## The change

```go
// before
func iterLabel(iter, live int) string {
	if live > 0 { return fmt.Sprintf("iter %d", live) }
	return fmt.Sprintf("iter %d", iter)
}
// after — both branches
	return fmt.Sprintf("iter%d", …)
```

`statusIterCol` stays 7 and the pin does not move
(`statusPinCols == statusColumnsAtCols == statusLayoutBudgetCols == 100`,
`statusPinSlackCells = 0`, guard compiles). The reservation is now ARITHMETIC on
the formatter (Rule 1/13) rather than a measured string:

```
statusIterWordCells 4 ("iter") + statusIterDigitsMax 3 ("999") = statusIterCol 7
```

## The withdrawn reading

`statusIterCol`'s comment used to say what the 7 does not cover — *"a five-digit
iterator … but that is a different defect (the class, not this cell)"* — while
`padStatusColumn` was already cutting the first three-digit count. That sentence is
removed rather than reworded: the defect WAS this cell and the label's shape, and a
suspended explanation is what let the cut survive a suite that pins the field's
start cell (`TestChatStatus_FieldsStartOnTheirReservedCells` never rendered a
count wide enough to meet the cut).

`devdoc/status-row-way-breadth-funding.md` audited the geometry's air and read the
same term as *"short, not spare: `statusIterCol` is 7 while `"iter 999"` measures
8"* — true at the time, and corrected there in the same commit: the term is now
drawn exactly.

## Checked

```bash
go build ./...                                   # OK
go test ./cmd/agent/ -run 'TestChatStatus|TestStatusRow|TestStatus' -count=1
go test ./testsuite/ -run 'TestChatPTY_ContextDenominatorIsQualifiedOnScreen|TestChatPTY_EditorRendersAndSubmits|TestChatPTYCtxQualifierExpectedCellsFollowTheRowsLadder'
```

`TestChatStatus_WidestIterFitsItsColumn` (new, `chat_status_degradation_test.go`)
pins the fact on the COMPOSED row: the widest count the reservation admits
(`statusIterDigitsMax` digits, built from the constant) appears WHOLE at
`statusColumnsAtCols` on both `iterLabel` branches, and the row then measures the
pin exactly. Evidence rows (`artifacts/iter-render-probe.txt` in this task's
session directory, rendered to `iter-render-probe.png`):

```
cols=100 iter= 100 live=0 width=100 label="iter100" row="deepseek-flash ·  iter100 ·  ⚙ keyword_search i… ·  ctx 4k/64k≡ ·↑12k↓4k hit 74.5%     4m00s · ready"
cols=100 iter= 999 live=0 width=100 label="iter999" row="deepseek-flash ·  iter999 ·  ⚙ keyword_search i… ·  ctx 4k/64k≡ ·↑12k↓4k hit 74.5%     4m00s · ready"
cols=100 iter=1000 live=0 width=100 label="iter1000" row="deepseek-flash ·  iter10… ·  ⚙ keyword_search i… ·  ctx 4k/64k≡ ·↑12k↓4k hit 74.5%     4m00s · ready"
```

**The bound is stated, not universal** (Rule 15): the reservation holds three
digits; a four-digit count is cut inside the column and reads `iter10…` — visible,
not silent. Fixing THAT is a reservation question (a wider `statusIterCol`, paid
from somewhere in the 100-cell pin), which this task deliberately did not touch.

## The row as the SCREEN holds it, not only as the composer returns it

The rows above come from the composer (`status.Line(cols)`) — the formatter's own
output, but not proof that the value survives the editor path that draws it. The
screen half was measured separately, through the REAL chat editor (a real
`chatInput` + its read loop + `lineedit.Screen`) with `publishChatStatus` — the
call `cli_chat`'s REPL makes — and the status row read back OFF THE GRID at the
operator's 117 columns (`artifacts/iter-screen-probe.txt`):

```
iter=100  screen row 23  screen-cells=92
  screen  : "glm-5.3-flash… ·  iter100 ·  ⚙ keyword_search i… ·  ctx 12k≡                           ready"
iter=999  screen row 23  screen-cells=92
  screen  : "glm-5.3-flash… ·  iter999 ·  ⚙ keyword_search i… ·  ctx 12k≡                           ready"
```

and the same probe run against the PRE-FIX form (restored in the worktree for one
run, then reverted — `artifacts/iter-screen-probe-prefix-red.txt`) shows what the
fix removed: the grid carries **`iter 1…`** where `iter100` belongs.

## Not touched

The suite's live assertions of the label were updated with the formatter
(`"iter 12"` → `"iter12"`, and the same in the height/turn-layout waits), and every
measured row quoted in prose was updated to the form the current producer draws.
Historical devdocs that quote PAST runs' rows were left alone: they are records of
what was on screen then, and rewriting them would fabricate evidence.
