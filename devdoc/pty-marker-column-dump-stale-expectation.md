<!-- doc-class: historical -->
# Task Report: TestPtyMarkerColumnDump — a stale expectation, not a code defect

`devdoc/pty-marker-column-dump-stale-expectation.md`

## Verdict

**The CODE was right; the TEST was stale.** `cmd/agent/pty_marker_column_dump_test.go`
was never updated when `7e92b038` deliberately stopped drawing the turn gutter on a
fenced block's rows, so it still demanded the block column of every body row.
The fix is in the TEST, and it is STRONGER than the expectation it replaces — see
"The new claim" below. **No production code was changed.**

## The deciding evidence

### 1. `7e92b038` is the breaking change, and the test was GREEN before it

The worktree's own history says the test file was last touched by `6f001cb0`
(reading the layer table instead of spelling two markers) — three commits and some
weeks BEFORE `7e92b038`.

Measured, verbatim (Rule 31 — the surface run, not a claim about it). The parent
tree was produced with `git archive 7e92b038^ | tar -x` into the session directory
(Rule 29: never point the work at a foreign worktree), the test file was copied in
unchanged, and the test ran exactly as it stands on master:

```
$ cd <session>/parent-tree && go test ./cmd/agent/ -run TestPtyMarkerColumnDump -count=1
ok  	github.com/nick/gopi/cmd/agent	2.549s
```

**GREEN at `7e92b038^`, RED at `7e92b038`.** The commit that moved the behaviour is
the one that broke the test.

### 2. `7e92b038` says in its own words that this is exactly what it did

```
$ git log -1 --format=%B 7e92b038
fix(lineedit): a code block's rows carry no turn gutter, so a copy of it is the command
…
The code/tool LAYER's rows now open at column 0 (answerRow.copied,
answerRow.gutterGlyph): the row's text IS the answer's line, byte for byte.
```

and the diff itself, `internal/lineedit/answer.go`:

```go
+	// copied marks a row that is COPIED OUT OF THE SCREEN to be run — a line of
+	// the answer's code/tool layer (a fenced or diff line), whose reader is as
+	// often a paste as an eye. Its turn gutter is NOT drawn: no `▌ ` glyph, and
+	// not its cells either, because a cell is a character and a character is
+	// what a copy carries …
+	copied bool
```

The commit is a deliberate product decision with a measured motivation (the operator
selected a fenced command, pasted it into a shell, and the first line died with the
word `▌`). Nothing in it is accidental, and nothing about it was reverted later —
`7e92b038` is an ancestor of this branch.

### 3. The sibling test in `internal/lineedit` was updated in the SAME commit; this one was not

`7e92b038` carried the same fix into the sibling that makes the same class of claim,
`internal/lineedit/marker_column_narrow_test.go`, adding `markerProbe.fencedRows()`
and exempting the block's rows from the column claim:

```go
+		// The rows of a FENCED BLOCK belong to the code/tool layer, which opens at
+		// column 0 so a reader can copy them out whole (answerRow.gutterGlyph):
+		// this claim is about the PROSE body's column, and the block is the one
+		// shape exempt from it — by design, not by accident, and asserted instead
+		// by TestCopiedText_CodeBlockRowsArePasteable.
+		if r >= block[0] && r <= block[1] {
+			continue
+		}
```

So the repo already had ONE place where this claim is stated correctly at the fix's
own commit. `cmd/agent/pty_marker_column_dump_test.go` is the file that was missed —
the same claim, driven through the real pty, left demanding the old shape. This
report's change makes the two files say the same thing.

### 4. The screen on this branch, verbatim (the state the test reads)

```
035|▸ 窄列中文
036|▌ 这一段中文答案用来检查换行之后的列是否对齐，中间夹着 …
037|  ASCII token 和 code 片段，然后再来一段中文以确保换行发
038|  生多次，最后以句号结束。
039|```
040|这是一行很长的中文代码内容，用来在窄列里强制换行，看看续
041|行有没有标记。
042|echo ok
043|```
```

The PROSE rows keep the block's column (`▌ `, and 2 blank cells on the rows a line
wrapped onto). The block's rows open at cell 0 with no cell of the band's chrome in
front — which is the whole point of `7e92b038`.

## The new claim (stronger, not weaker)

The old loop asserted ONE thing about every non-layer row: `indent == 2`. The new
code asserts TWO, each on the shape it is about:

1. **the prose rows** still open at the block's column — the old assertion, unchanged,
   now applied to the rows it was always about (a row of the fenced block is skipped,
   exactly as the lineedit sibling skips it);
2. **the block's rows ARE the answer's block, byte for byte** — the rows the block
   occupies are read off the screen IN ORDER, joined, and compared against the block
   the test itself sent (`markerDumpBlock()`, built from the same constants the test
   streams, so there is no second spelling of it):

```go
rows := make([]string, 0, blockTo-blockFrom+1)
for i := blockFrom; i <= blockTo; i++ {
    rows = append(rows, strings.TrimRight(lines[i], " "))
}
joined := strings.Join(rows, "")
if joined != markerDumpBlock() { … }
```

That is a **strictly stronger** claim than the one it replaces: the old test could
never have caught the defect `7e92b038` fixed (it asserted the glyph was there, which
WAS the paste defect), and it also cannot be satisfied by a row that merely happens to
start at column 0 — the text must be the answer's own. A wrapped code row that opens
with `▌ `, or with the block's two blank cells, now fails by name:

> `the block read off the screen = "▌ ```▌ 这是一行很长…" , want the answer's block verbatim …`

Three further guards were added where the old test would have silently degraded:

- the turn must contain a fenced block at all (a `t.Fatalf` naming that);
- the block must occupy MORE than its four source rows, i.e. it must actually WRAP —
  otherwise the claim would only ever be read on rows that open their own line, and
  the row the defect reported was exactly a WRAPPED one;
- `continuations < 2` (kept from the original) still fails if nothing wraps.

**Red proof of the new test** (Rule 12's shape: the new assertion must fail on the
tree the fix was about). The same test file, copied onto the parent tree — where the
block's rows still wear the gutter — fails on the paste assertion, not on a missing
block:

```
$ cd <session>/parent-tree && go test ./cmd/agent/ -run TestPtyMarkerColumnDump -count=1
    pty_marker_column_dump_test.go:353: the block read off the screen =
      "▌ ```▌ 这是一行很长的中文代码内容…▌ echo ok▌ ```", want the answer's block
      verbatim "```这是一行很长的中文代码内容…echo ok```" — a code row that
      carries ink or cells of the band's own cannot be pasted:
--- FAIL: TestPtyMarkerColumnDump (0.00s)
```

(the identical assertion also fails on `7e92b038` itself and passes from `7e92b038`
onward — the direction of the check is: RED where the code draws the gutter, GREEN
where it does not.)

## Completed

- `cmd/agent/pty_marker_column_dump_test.go`: the body-row loop now separates the two
  shapes; the block's rows are asserted byte-exact against the answer's own block;
  the turn's block and its wrap are required to exist before either claim is read.
  Literal answer text moved to named constants (Rule 1) and shared between what the
  test streams and what it asserts.
- `devdoc/pty-marker-column-dump-stale-expectation.md` (this report).
- No production file was touched: `git status --porcelain` at close lists only the
  test and this report.

## Not Done (honest)

- **`internal/lineedit/TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` is RED on
  this branch, and it is RED ON MASTER TOO — it is not mine.** Measured, identically
  from a pristine `git archive master` tree and from `7e92b038^`:

  ```
  --- FAIL: TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow (0.00s)
      turn_header_block_test.go:364: BeginTurn committed 8 row(s), want 5 (one per block row)
  ```

  **Attributed** rather than left as a shrug. The test compares
  `len(TurnHeaderLines(...))` against `Committed()`. Measured along the history of
  that file:

  | tree | `len(want)` | `Committed()` | verdict |
  |---|---|---|---|
  | `fe51f47e` (where the file was ADDED) | 5 | 5 | ok |
  | `51935bb2` | 5 | 5 | ok |
  | `1a7ebcda` | 5 | 5 | ok |
  | `40caf6fe` | 5 | 5 | ok |
  | `f475dde6` | 5 | **8** | **RED from here on** |

  and at `f475dde6` — `Merge branch 'task/01a0e8341340736da5f3948dad0b68c3'` — the
  diff against its first parent is:

  ```
  $ git diff --stat f475dde6^1 f475dde6 -- internal/lineedit/turn_header_block_test.go internal/lineedit/answer.go
   internal/lineedit/answer.go | 44 ++++++++++++++++++++++++++++++++++++++++----
   1 file changed, 40 insertions(+), 4 deletions(-)
  ```

  So the merge took that commit's `answer.go` (the REQUEST row, `BeginTurn` now also
  calling `drawRequestRow()` — which commits the request row's own band rows) and
  **did not take the test-file change that went with it**. `TurnHeaderLines` still
  returns 5; `Committed()` now counts 5 + the request row's 3 rows = 8. It is a
  merge-regression, not a code defect, and it is on master.

  It is filed here rather than fixed: this task's scope is `TestPtyMarkerColumnDump`,
  and fixing a second, unrelated RED in the same package would have made this
  report's own acceptance unreadable. The fix belongs to whoever owns the merge —
  `f475dde6^1..f475dde6` is the range to read. **A new row should be dispatched for
  it (Rule 26): the epic's own `make test-budget PKGS='./cmd/agent ./internal/lineedit'`
  cannot go green while it stands.**
- The judge-side channel (`uiclient.Compare`) has no knowledge of the gutterless
  code-row shape; whether it should is a question for whoever owns that file
  (`internal/lineedit/marker_column_narrow_test.go` does not cover it either).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (no output, exit 0)
- Tests:
  - `CGO_ENABLED=0 go test ./cmd/agent/ -run TestPtyMarkerColumnDump -count=1` →
    `ok  github.com/nick/gopi/cmd/agent` (see `Ran:`/`Result:` below)
  - `CGO_ENABLED=0 go test ./internal/lineedit/ -run TestMarkerColumn_ -count=1` →
    `ok  github.com/nick/gopi/internal/lineedit`
  - `make test-budget PKGS='./cmd/agent ./internal/lineedit'` — run in an
  in-session background job (a foreground duplicate was killed: two `go test` sets is
  the documented failure). Its package verdicts and the complete set of failed tests
  came out of its own `-json` stream:
  ```
  lineedit -> fail (71.576s)   cmd/agent -> fail (921.486s)
  FAILED TESTS: (agent, TestPrefetchGoroutineSettleGapDetectsALiveLeak)
                (lineedit, TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow)
                (agent, TestSyncBarePeersTestCoversEveryChangedFile)
  ```
  `TestPtyMarkerColumnDump` is not among them — it PASSES inside that same run.
  **The two `cmd/agent` rows have a named cause the run itself printed**: the worktree
  had no `bin/gopi`, and the suite says so on stderr —
  ```
  test-budget: NOTE: <worktree>/bin/gopi is absent — the CLI E2E guards (cmd/agent, testutil)
  test-budget:       fail rather than skip; run `make build` first
  ```
  Both are CLI-E2E guards that spawn the real binary; without it they FAIL by design.
  They are green when run one at a time on this branch (`TestPrefetch…` ok 1.575s,
  `TestSyncBarePeers…` ok 2.560s) and green on a pristine `master` tree
  (`ok github.com/nick/gopi/cmd/agent 1.824s`). With `make build` done, the declared
  scope was re-run (see `## Evidence`).
- Ran: (verbatim, from the worktree `/home/user/workspace/gopi-01a0fa9c1013727888799b4e4e55606e`,
  branch `task/01a0fa9c1013727888799b4e4e55606e`)
  ```
  MARKER_DUMP_DIR=<session>/dump-after CGO_ENABLED=0 go test ./cmd/agent/ \
      -run TestPtyMarkerColumnDump -count=1 -v
  ```
- Result:
  ```
  pty_marker_column_dump_test.go:339: COPIED BLOCK: "```这是一行很长的中文代码内容，用来在窄列里强制换行，看看续行有没有标记。echo ok```" (83 cells on 5 row(s))
  pty_marker_column_dump_test.go:345: READ BACK: 9 body rows, 2 continuation rows, every one at the block's column
  --- PASS: TestPtyMarkerColumnDump (0.22s)
  ```
  The block read off the screen is 83 cells on 5 rows (4 source lines, one of them
  wrapped) and equal to the answer's own block — the copyable property, measured on
  the real pty at 56x48 through the same sink and reader `gopi chat --dump-screen`
  uses.
- Re-run after the commit (`CGO_ENABLED=0 go test ./cmd/agent/ -run TestPtyMarkerColumnDump -count=1`)
  → `ok  github.com/nick/gopi/cmd/agent	2.396s`; the same three names run one at a
  time on the branch: `TestPrefetch…` ok 1.575s, `TestSyncBarePeers…` ok 2.560s,
  `TestPtyMarkerColumnDump` ok 2.396s. The one 1.636s `FAIL` seen between those runs
  named no test and printed no message (the suite's child-sweep close-out, not a test
  verdict) and did not reproduce on the two runs around it.

## Evidence (self-filed)

- `make test-budget PKGS='./cmd/agent ./internal/lineedit'` → exit=1,
  `packages: 2 finished, 2 with a failure`, the three names above; the run's own note
  names the missing `bin/gopi` as the cause of the two `cmd/agent` rows. Stream:
  `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1790912780-3900956.json`.
- The two `cmd/agent` rows run alone on this branch: `TestPrefetch…` ok 1.575s,
  `TestSyncBarePeers…` ok 2.560s. On a pristine `master` tree together:
  `ok github.com/nick/gopi/cmd/agent 1.824s`.
- `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` run alone: FAIL (`committed 8
  row(s), want 5`), and FAIL on `master`'s tree — master's own red, attributed in
  `## Not Done`.
- `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, `bin/gopi` 29763263
  bytes. `bin/` is gitignored: `git status --porcelain` stays empty.
- Parent-commit green run (the stale-expectation proof):
  `cd <session>/parent-tree && CGO_ENABLED=0 go test ./cmd/agent/ -run TestPtyMarkerColumnDump -count=1`
  → `ok  github.com/nick/gopi/cmd/agent	2.549s`
- Red proof of the NEW assertion on the parent tree:
  `cd <session>/parent-tree && CGO_ENABLED=0 go test ./cmd/agent/ -run TestPtyMarkerColumnDump -count=1`
  → `--- FAIL: TestPtyMarkerColumnDump (0.00s)` with `the block read off the screen =
  "▌ ```▌ 这是一行很长…" , want the answer's block verbatim …`
- The failing test on master's tree, verbatim:
  `cd <session>/master-tree && CGO_ENABLED=0 go test ./internal/lineedit/ -count=1`
  → `--- FAIL: TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` (the unrelated,
  pre-existing red named in `## Not Done`)
- `gofmt -l cmd/agent/pty_marker_column_dump_test.go` → no output (exit 0)
- The dumps this report reads are kept OUT of the checkout, at
  `<session>/dump-before/chat-screen.dump{,.json}` and
  `<session>/dump-after/chat-screen.dump{,.json}` (Rule 37: a chat/verification
  artifact belongs in the session directory, never in a git-tracked path).

## Files

- `cmd/agent/pty_marker_column_dump_test.go` (changed)
- `devdoc/pty-marker-column-dump-stale-expectation.md` (this report, new)
