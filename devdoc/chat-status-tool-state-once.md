<!-- doc-class: historical -->
# Task Report: operator bug #5 — the chat status row states the tool state ONCE, in one language

**Task**: `01a0e4c7-9ac5-7e79-9e88-7648600b3ebe` · branch `task/01a0e4c7-9ac5-7e79-9e88-7648600b3ebe`
**Worktree**: `/home/user/workspace/gopi-01a0e4c79ac57e799e887648600b3ebe`
**Operator's defect, verbatim**: `tools · no tool 语义重复且英文混中文`

## Completed

### 1. The tool state is stated ONCE — the duplicate column is retired, not renamed

The row used to answer the tool question from **two columns**, one apart:

```
fake-model     ·  iter 1  ·  tools ·  no tool          ·  ctx 51/200.0k≡   6s · ready
                              ^^^^^^   ^^^^^^^^^
                              the SAME fact, twice, nine cells apart
```

`statusColMode` (whose entire content was the string `"tools"`) and `statusColTool`
(whose idle content was `"no tool"`) were one bit of information in two opposite
signs, and in left-to-right reading order the pair says *"tools … no tool"* — which
is how the operator's row read as self-contradiction.

The mode column is **deleted** (`statusColMode`, `statusColumnNameMode`,
`statusModeCol`, `statusToolsHint` are all gone from production — the tools fact is
now the boolean `chatStatusSource.toolsOn`), and the ONE tool column states the fact
in all three of its states:

| state | column content |
|---|---|
| a tool has run | `⚙ bash echo tool-state-p…` (name + digest) |
| tools on, nothing called yet | `no tool` |
| no toolset at all (`chat` without `--tools`) | `tools off` |

The third arm is why the second is honest: `no tool` is a statement *about tool use*
(there are tools and none ran), so a surface with no toolset must not claim it —
that distinction is exactly what the retired column's blank-on-one-path destroyed.

**The retired column's cells fund the survivor, and the pin does not move.** The
arithmetic is spelled, not hand-totalled:

```go
statusToolCol = statusToolColOwn(16) + statusModeColRetired(5) + statusToolColWayBreadth(4)  = 25
statusColumnSepTotalN: 4 → 3   // the retired column's top-level boundary goes with it
statusPinCols = 14 + 7 + 25 + 27 + 3*4 + 2 + 13 = 100  = statusLayoutBudgetCols   (slack 0)
```

`statusPinCols` stays 100 and `statusPinSlackCells` stays 0 — every cell the row
had, it still spends, now on ONE statement of the fact. The fifth absorbed cell is
**returned** to the row rather than spent: reading the absorption as "the retired
separator's 4 went in too" puts the pin at 101 — over `statusLayoutBudgetCols`,
which the compile-time guard refuses — and 102 wraps the row. `statusToolColWayBreadth`
is a second, measured correction: the 16-cell column was sized before the two-segment
way marker the row learned in task 01a0e3db ("⚙ bash · retrieval: …"), and at 16 cells
the aligned row cut the marker away entirely — the column was reserving cells for a
field it could not draw as designed.

**Three existing tests encoded the retired geometry and were corrected, not deleted**
(each is a genuine follow-through, not a loosened assertion):
- `chat_status_ctx_provenance_test.go` — a hardcoded `4*lineedit.StatusColumnSepCells`
  in the group-start arithmetic now reads `statusColumnSepTotalN` (the bare literal was
  the very defect Rule 13 names: a number no column change can update); and its
  "the pair is not CUT" check was **scoped to the metrics group's own cells** — a
  row-wide `"…"` test would read the tool column's *by-design* digest cut as a
  truncated denominator.
- `chat_status_cache_bound_test.go` — the same scoping, same reason.
- `chat_turn_layout_test.go` — the tool field's position contract was "between the
  mode field and the token field"; the mode field no longer exists, so it is
  "between the iter field and the token field".
- `chat_status_width_test.go` — the provenance map's column-count check now compares
  against the map's NON-NEGATIVE entries (the `-1` rows are read-outs no column
  carries, by design), and the tool anchor is the glyph the row actually draws
  (`toolGlyph`) now that the row's under test tool is a real one.

### 2. The language decision: the row speaks ONE language, and it is English

The operator's complaint names both halves of the defect. The decision is recorded
and **asserted as a property** (`assertRowSpeaksOneLanguage`: no CJK rune may appear
on the row), and the reason is in the test's own comment:

- The row is a read-out of **machine vocabulary** (a model identifier, `iter`, tool
  names, arrow-marked cache counters) where the only translatable parts are the
  connective words. Translating those alone produces the mix **in the opposite
  direction** — Chinese connectives between English counters and English tool names
  — i.e. the very defect reported.
- Every other line of this surface's chrome is English today (the same screen prints
  `[gopi] w context window: …`, `tools ENABLED`, and the whole `/help` legend).
- A fully localised surface is a **different decision with its own cost** (it needs
  a locale channel, which does not exist). The defect here is that the row *MIXES*;
  that is what is fixed and asserted.

Measured on the branch: `cmd/agent`'s non-test sources carry exactly **one** CJK
string literal (`cli_vision.go:29`, a vision prompt), and the chat surface carries
none.

### 3. The blank-but-present segment is gone

The operator's row in one state was `iter 0  ·        ·  no tool` — the mode column
BLANK but still joined by two separators (`dump-input.txt`, `dump-dup.txt` row 025).
The column and its separator are gone with it, and the zero-count case is **stated**
(`no tool` / `tools off`). Asserted as `assertNoEmptySegment` — no segment between
two of the row's own separators may be empty.

### 4. The reader-visible legend documents the three states

`chatStatusRowLegend` (what `/help` prints about the row) now defines the tool
field's three states and says why the row spends ONE column on it. It is composed
from the same constants the row draws from (`statusNoToolLabel`, `statusNoToolsLabel`,
`chatFlagTools`, `toolGlyph`), so the legend cannot drift from the row.

## Files

| file | change |
|---|---|
| `cmd/agent/chat_stream.go` | `statusColMode`/`statusColumnNameMode`/`statusModeCol`/`statusToolsHint` retired; `chatStatusSource.modeHint string` → `toolsOn bool` (+ `setTools`); `statusToolField(toolName, toolSummary, toolsOn)` with the three arms; `statusToolColumnFieldFor` (the aligned arm); `statusToolCol` re-derived via `statusToolColOwn` + `statusModeColRetired` + separator; `statusColumnSepTotalN` 4 → 3; ladder/keep-always/column-name map/`statusRowColumns` updated |
| `cmd/agent/cli_chat.go` | `newChatStatusSource(ag.Model, sess.toolsEnabled)` (the fact comes from the same flag that selects the runner); `/tools` toggle calls `status.setTools`; `chatStatusRowLegend` documents the three states |
| `cmd/agent/chat_status_cache_bound.go` | the donor-claim comment updated (the mode column is void as a donor twice over) |
| `cmd/agent/chat_status_tool_state_once_test.go` | **NEW** — the minimal repro/regression test (below) |
| 13 existing `cmd/agent/*_test.go` | mechanical migration to the retired column and the boolean field |

## The regression test

`cmd/agent/chat_status_tool_state_once_test.go` — drives the **production wiring**
(the source + the row's own `status().Line(statusColumnsAtCols)`) in all three states
and asserts three PROPERTIES, never the fix's own spelling:

- **`assertToolStateStatedOnce`** — exactly ONE of the row's columns is named `tool`.
  The count is taken over the row's real geometry, not over a substring count on the
  drawn text: the defect was a *second column*, and a column that stated the same
  thing in different words (`tools` beside `no tool`) is invisible to any counting of
  one label's own spelling.
- **`assertNoEmptySegment`** — no segment between two of the row's own separators is
  empty.
- **`assertRowSpeaksOneLanguage`** — no CJK rune on the row.

Plus **`TestStatusRowStatesToolStateOnce_RedProof`**: the same predicates are run
against the PRE-FIX row shape (built from the layout's own separator constant) and
must refuse it — the two-segment shape must be recognised as "states it twice" and
the blank-segment shape as "carries an empty segment", with the fixed row passing
both as the negative control. Without it, a predicate that had stopped seeing the row
would still report green.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK
- **Tests**: `go test ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 389.707s` (see `## Evidence`)
- **Ran**: the real surface, on a real pty, at 117x26, twice — the exact invocations
  and their grid rows are in `## Evidence` and in the session evidence dir
  `/home/user/.gopi/sessions/01a0e4c7-9ac5-7e79-9e88-7648600b3ebe/evidence/`
- **Result**: the three states, read from the pty grid (`row 025`, the status row),
  each stating the tool fact exactly once with no empty segment and no language mix
- **Commit**: see the close-out (the run's branch tip)

## Evidence (self-filed)

### State A — tools enabled, ZERO calls (the operator's own state)

Real pty at 117x26, scripted stub (no API spend):

```
GOPI_HOME=$S/home-zero ANTHROPIC_BASE_URL=http://127.0.0.1:8917 \
ANTHROPIC_AUTH_TOKEN=sk-fake ANTHROPIC_MODEL=fake-model \
PTY_SCRIPT=$S/evidence/script-zero.txt \
python3 $S/evidence/pty_multi.py $S/evidence/raw-zero.log 117 26 \
  /tmp/gopi-bug5-gopi chat --tools --dump-screen=$S/evidence/dump-zero.txt
```

Grid row 025 of `dump-zero.txt` (verbatim, re-captured against the FINAL build):

```
025|fake-model     ·  iter 1  ·  no tool                   ·  ctx 14/200.0k≡ ·↑0↓10        6s · ready
```

One tool statement, one language, no empty segment. The provenance line names the
surface: `#gopi-chat-dump … target=self:chat-tools tools=on terminal=117x26 grid=117x26 …`.

### State B — a tool HAS been called (captured mid-turn; the row is cleared at turn end by design)

The dump sink writes its file atomically (`writeFileAtomic`), so a copy taken at a
fixed instant is a complete, parseable frame. `capture-called.sh` copied it every 10s;
`dump-called-live-t30.txt` row 025, verbatim (FINAL build):

```
025|fake-model     ·  iter 1  ·  ⚙ bash echo tool-state-p… ·  ctx 25/200.0k≡ ·↑0↓10        4s · sent
```

The tool is named ONCE, in the column that also states its absence in state A — no
second column restating it. (`⚙` is `lineedit.ToolGlyph`.)

### The pre-fix row, for comparison (the run's own prior evidence, task 01a0e498)

```
025|fake-model     ·  iter 1  ·  tools ·  no tool          ·  ctx 51/200.0k≡               6s · ready
025|fake-model     ·  iter 0  ·        ·  no tool          ·                               0.1s · ready
                                  ^^^^^^  <- blank but present, two adjacent separators
```

### The regression test, on the real wiring

```
$ go test ./cmd/agent/ -run TestStatusRowStatesToolStateOnce -v
    chat_status_tool_state_once_test.go:156: tools on, zero calls: "deepseek-flash ·  iter 0  ·  no tool                   ·  ctx 15.4k≡                   ready"
    chat_status_tool_state_once_test.go:183: tool called: "deepseek-flash ·  iter 1  ·  ⚙ keyword_search status … ·  ctx 15.4k≡                   sent "
    chat_status_tool_state_once_test.go:208: no tools configured: "deepseek-flash ·  iter 0  ·  tools off                 ·  ctx 15.4k≡                   ready"
--- PASS: TestStatusRowStatesToolStateOnce_ToolsOnNoCall (0.00s)
--- PASS: TestStatusRowStatesToolStateOnce_ToolAlreadyCalled (0.00s)
--- PASS: TestStatusRowStatesToolStateOnce_NoToolsAtAll (0.00s)
--- PASS: TestStatusRowStatesToolStateOnce_RedProof (0.00s)
ok  	github.com/nick/gopi/cmd/agent	0.233s
```

### Package suite

```
$ go test ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	389.707s
```

(One earlier full-suite run hit the harness's 10-minute package timeout with
`TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn` at the top of the panic
trace. It was **not** this diff: this task's own pty-harness stub processes
(`fakellm_state.py`) had been left running and starved the machine's process
budget. After killing them the test passes alone in 68.25s and the full suite is
green — an environmental hang, named here rather than quietly re-run.)

## Not Done (honest)

**A SECOND blank-but-present segment exists on the row in a different state, and it is
NOT fixed here — it is a sibling finding, deliberately kept out of this change.**

`dump-called-live-t20.txt` row 025, measured during this task, verbatim:

```
025|fake-model     ·  iter 1  ·  no tool                   ·                               3s · sent
                                                       ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
                                                       the METRICS group is blank but present
```

When NOTHING has been measured yet (no context count, no cache counters) while the
clock is known, the metrics group renders as an empty-but-reserved segment. It is the
SAME defect class this epic names ("a blank-but-present segment"), but it is **not the
reported defect**: the operator's blank segment was the *mode* column (evidence: their
own dumps — `iter 0  ·        ·  no tool`, i.e. blank between `iter` and `no tool`),
and the metrics arm is *deliberate and test-pinned today* — `statusMetricsField`'s
`last < 0` branch returns `""` with the comment "the column keeps its cells as padding
— an empty field, never an invented zero (Rule 15b)", and
`TestStatusMetricsFieldsAreHonest` asserts `statusMetricsField(statusOps{}, 0) == ""`.

So closing it means choosing a WORD for an unmeasured group and **reversing a pinned
honesty contract** — a decision with its own blast radius (`statusMetricsField` and its
group-seam arithmetic are covered by the metrics test family). It is a separate task,
not a quiet extension of this one. Suggested shape: a dedicated field label (e.g.
`metrics off` / `—`) composed from a named constant, `TestStatusMetricsFieldsAreHonest`'s
empty-group clause updated in the same commit, and the same pty evidence recipe this
report carries.

Everything else the epic asked for is done and verified above.
