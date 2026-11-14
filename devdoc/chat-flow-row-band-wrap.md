<!-- doc-class: historical -->
# Task Report: an over-wide agent flow row wraps at the band's own width

Branch: `fix/chat-flow-row-band-wrap` (worktree `~/workspace/gopi-flowwrap`)

## Completed

- **Root cause, measured.** `0fc25400` routed the agent's flow output to the
  reserved layout's band (`chatFlowSeam` → `chatInput.writeBandRow` →
  `Editor.WriteBandRow`), but the seam handed the band a row of UNBOUNDED width.
  The band's own line-writing path then re-cut it at `region.Cols()` through
  `cutCols` — and that loop was not escape-aware, so it measured a control
  sequence's bytes as display cells. Two distinct defects came out of the same
  missing width:
  1. the split column belonged to the LAYOUT, not the producer, and came up
     short by exactly the escape bytes it had counted (measured on the 100×30
     grid: the first band row of a 2×100-cell reasoning row was **96** cells
     wide, not 100);
  2. the cut landed mid-sequence, so the sequence's own parameters were printed
     as literal TEXT (measured: `\x1b[31m` cut after `\x1b[` rendered the band's
     next row as `31mbbbb…`; on a 100-column grid a cut after `\x1b[` printed
     `38;2;18;52;86`).
- **Fix 1 — one escape-aware measure/split policy** (`Rule 14`, one definition,
  no second cutter):
  - `internal/sgr/sgr.go`: new `sgr.EscapeSeqLen` — the ONE definition of "which
    bytes draw no cell" — plus the constants it needs (`Esc`, `EscTwoByteMax`,
    `ParamIntermediateMin/Max`). A truncated sequence reports its length to the
    end of the text, so a caller treating the span as atomic keeps it whole.
  - `internal/lineedit/render.go`: `displayWidth` (and its exported face
    `DisplayWidth`) skips whole escape sequences instead of counting their bytes.
  - `internal/lineedit/answer.go`: `cutCols`, `wrapCols`, `truncateCols` walk by
    byte index and carry an escape sequence whole onto the row it started on.
- **Fix 2 — the split column belongs to the producer.** New `Editor.BandCols()`
  publishes the band's drawing width, `chatInput.bandCols()` exposes it to the
  seam, and `chatFlowSeam.commit` wraps each row with the ONE policy
  (`lineedit.WrapToCols`) at that width before the band ever sees it. The band's
  own row loop keeps its safety split for callers that do not wrap (Rule 15: a
  line is never dropped), but a producer that wrapped its own text owns the
  break. The ONE-writer rule is untouched: the seam still writes only through
  `WriteBandRow`; there is no second fd and no second writer.
- **Regression test, RED then GREEN** (`cmd/agent/chat_flow_wrap_test.go`,
  asserted on the RENDERED CELL GRID, not the byte log):
  - `TestChatFlow_OverWideDeltaWrapsAtTheBandsOwnWidth` — no band row exceeds the
    band; each wrapped segment but the last FILLS it; the whole body survives in
    order; the editing row keeps its prompt and the status row keeps its model
    text.
  - `TestChatFlow_CutNeverLandsInsideAnEscapeSequence` — the wrap boundary is put
    exactly on a long sequence (`pad = band − len(CSI)`); the sequence's
    parameters must appear nowhere on screen, and the text either side must be
    adjacent (so the sequence cost zero cells).
- **Before/after cell-grid evidence pair** under `devdoc/evidence/sgr-grid/`,
  differing ONLY in the binary: `before-*` from a binary built at the branch
  base (`master` @ `ccdd5f1e`), `after-*` from the branch. The capture is a new,
  fully offline instrument that streams a known-length reasoning row
  (`capture-flowwrap.sh` + `flowwrapmock.py`, an Anthropic-SSE stub — no network,
  no API spend), and `flowwrapdiff.py` states the verdict.

## Not Done

- **The byte-identity of the non-reserved path is argued, not captured.** I did
  not run the `--no-editor` / piped capture; the argument is structural and
  stated: with no layout `BandCols()` is 0, `WrapToCols(row, 0)` returns the row
  unchanged as ONE element (its `width <= 0` guard), and `commit` then performs
  exactly the single historic write it performed before. The branch-scoped
  `./cmd/agent/` suite (which covers the non-reserved paths) is green.
- **The instrument's row offset is not eliminated, only measured and isolated.**
  A shot's band begins at whatever blank row the scroll position was on; two runs
  of the SAME binary differ by one leading blank row (control run `before2`). The
  comparator therefore normalizes it and reports the width claim, which is
  identical across the two same-binary runs. Making the shot byte-identical would
  need a deterministic scroll position in the harness — a harness change, not
  this defect.
- **No whole-tree suite run.** Per Rule 27 / `internal/smokegate`, the
  merge/verify path is branch-scoped smoke; the packages the diff touches are the
  ones run.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → **BUILD_OK**
- Tests: `go test -count=1 ./cmd/agent/` → see `## Evidence`; plus
  `./internal/lineedit/`, `./internal/sgr/`, `./internal/config/` (the packages
  the diff touches).
- Ran:
  - `bash devdoc/evidence/sgr-grid/capture-flowwrap.sh before  ~/workspace/_tmp/gopi-base`
  - `bash devdoc/evidence/sgr-grid/capture-flowwrap.sh before2 ~/workspace/_tmp/gopi-base` (control: same binary)
  - `bash devdoc/evidence/sgr-grid/capture-flowwrap.sh after   ~/workspace/_tmp/gopi-flowwrap`
  - `python3 devdoc/evidence/sgr-grid/flowwrapdiff.py before-… after-… before2-…`
  - `go test -count=1 -run 'TestChatFlow' ./cmd/agent/` (with the production
    files reverted to `master` and the new test kept — the RED proof)
- Result: the RED proof prints

  ```
  --- FAIL: TestChatFlow_OverWideDeltaWrapsAtTheBandsOwnWidth
      wrapped segment 0 (band row 8) is 68 cells wide, want the band's full 72
  --- FAIL: TestChatFlow_CutNeverLandsInsideAnEscapeSequence
      the control sequence's parameters ("38;2;18;52;86") were printed as literal text
  ```

  and the same two tests pass once the fix is restored. The cell-grid comparator
  prints `[96, 100, 71]` before and `[100, 100, 67]` after, with the flow TEXT
  byte-identical (299 cells) and the same-binary control showing the row offset
  the raw diff reports.

## Evidence (self-filed)

- `bash capture-flowwrap.sh before … | tail -4` →
  `rendered rows (screen row: cells): 3:100, 4:100, 5:88, 6:96, 7:100, 8:71, 9:4, 10:32 …` /
  `full-width rows (== 100 cells): 3`
- `bash capture-flowwrap.sh after … | tail -4` →
  `rendered rows (screen row: cells): 3:100, 4:100, 5:88, 6:100, 7:100, 8:67, 9:4, 10:32 …` /
  `full-width rows (== 100 cells): 4`
- `python3 flowwrapdiff.py …` → `before: [96, 100, 71]`, `after: [100, 100, 67]`,
  `flow TEXT byte-identical: YES (299 cells)`, control `before vs before2
  -> identical: NO (row counts 27 vs 25)`.
- RED proof (production reverted to `master`, new test kept): the two FAIL lines
  quoted above; restored: `ok github.com/nick/gopi/cmd/agent`.

---

## Rework (2026-09-23, relanded onto master 56f665a5)

The original branch (`fix/chat-flow-row-band-wrap`, tip `3377b7af`) could not
merge: master had moved `internal/lineedit/render.go` after the branch's base
(commit `7fed257c` — the palette lane put `moveCursor` back on the one escape
vocabulary `sgr.MoveCursorSeq` and reordered the import block), so the archived
hunk's context (`moveCursor`'s body + the import list) no longer existed.

**What was done.** The archived diff was applied to a fresh worktree
(`feat/chat-flow-band-wrap-rework` off master `56f665a5`): 20 of its 21 files
applied cleanly, `internal/lineedit/render.go` was hand-resolved — the branch's
escape-aware `displayWidth` and its `unicode/utf8` + `internal/sgr` imports, with
**master's own call path kept** (`moveCursor` still delegates to
`sgr.MoveCursorSeq`; the branch's `strconv`/`escCSI` variant is master's older
form and was not restored).

**Bit-identity check against the archived record:** for each file, the archived
`index <base>..<post>` line was resolved against the actual blobs. Every file
whose post-image exists lands byte-identical (`git diff --quiet` between the
working file and the archived post-image) — including all 15 new files, the
evidence rig, and `cmd/agent/{chat_flow_seam,lineedit_wire}.go`. The three files
whose post-image legitimately differs are the three where **master had moved
past the branch's base**, and in every one of them the resolved file is
`master's newer content + the branch's change`:

| file | why not bit-identical |
|---|---|
| `internal/lineedit/render.go` | master's `moveCursor`→`sgr.MoveCursorSeq` + `sgr` import kept, branch's `displayWidth` landed |
| `internal/sgr/sgr.go` | only master's extra constant blocks (`FinalPrivateModeOn/Off`, erase modes) had to be kept; the branch's `EscapeSeqLen` block landed verbatim |
| `internal/lineedit/answer.go` | only master's newer turn-structure constants (the branch's base predates them); the branch's escape-aware `truncateCols`/`cutCols`/`wrapCols` landed verbatim |

`sgr width helpers as the branch wrote them`: `EscapeSeqLen` and its constants
are byte-identical to the archived post-image (`c8b16b91`) modulo master's own
additions elsewhere in the file.

**Merge probe (the acceptance command):**

```
$ cd /home/user/work/GoPi && git merge --no-commit --no-ff feat/chat-flow-band-wrap-rework
Automatic merge went well; stopped before committing as requested
```

**Re-run capture (the archived instrument, re-executed):** `capture-flowwrap.sh`
was run twice on the SAME harness with only the binary varying —
`rework-before` from a binary built at master `56f665a5`, `rework-after` from the
rework branch:

```
before  [96, 100, 83, 34]
after   [100, 100, 79, 34]
flow TEXT (whitespace-normalized) IDENTICAL: True (272 cells)
```

The claim reproduces: the over-wide reasoning row's first segment is 96 cells on
master (the layout's re-cut, four cells short — the `[thinking] ` dim marker's
bytes counted as cells), 100 cells (a full band) with the rework.

**RED proof, all production files reverted:** with the six production files read
back from `master` (keeping the new tests), both new tests fail with the real
messages —

```
chat_flow_wrap_test.go:98: wrapped segment 0 (band row 14) is 68 cells wide, want the band's full 72
chat_flow_wrap_test.go:151: the control sequence's parameters ("38;2;18;52;86") were printed as literal text
```

— and pass with the rework restored. (Reverting only the seam is NOT a red proof:
the escape-aware cutters alone let the assertion pass, because the layout's
re-cut measured the sequence correctly once `cutCols` was fixed. The red proof
must revert the measure/split policy too.)

**Doc manifest:** the 13 new `devdoc/` files (the report + 12 evidence artifacts)
were added as `historical` rows to `devdoc/DOC-INDEX.tsv` (union — master's rows
are untouched; the manifest is sorted by path and rebuilt that way).

**Pre-existing failure, PROVEN pre-existing:** `go test ./internal/standards/`
is red on bare master too — `TestDocManifestCoversEveryDevdocFile` (3 unclassified
bubbletea docs) and `TestStandardGates` (Rule 17 `internal/tools/tasks_test.go:1109`).
A master-vs-branch diff of the gate output is empty (`diff /tmp/master_std.txt
/tmp/branch_std.txt` → no lines): the rework adds no new standards violation.
