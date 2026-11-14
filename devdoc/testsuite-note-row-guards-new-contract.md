<!-- doc-class: historical -->
# Task Report: the testsuite's note-row guards now speak the LOSSLESS contract

**Task**: `01a1267b-13b4-7351-ae53-04851160b8b5` · branch `task/01a1267b13b47351ae5304851160b8b5`
**Devdoc class**: historical
**Supersedes the premise of**: `devdoc/note-row-cut-is-lossless.md` (task 01a12669, commit `e58009f65`)

## Completed

### 1. `testsuite/chat_render_verdict_pty_test.go` — the NOTE-WRAP guard, rewritten

The harness asked the **pre-`e58009f65`** question: *"does every mid-token cut carry the
continuation ellipsis?"* — so it reported the **fixed** shape as a failure (measured on this
branch before the change: `RENDER-VERDICT NOTE-WRAP: FAIL` against a screen that was correct).
Three pieces moved:

- **`chatRenderVerdictNote`** — the criterion is now the note path's real guarantee: (1) the
  rows reassembled (gutter stripped) are the call's command, **byte for byte** when the wrap
  broke inside a token; (2) every continuation row opens with `NoteContinuation` (`↳ `); (3)
  **no row draws the cut glyph** — a `…` inside the command's own cells is a character the
  reader pastes into a shell that cannot run it (`-ma…xdepth`). A note that did **not** wrap at
  this geometry is `COULD NOT DETERMINE` (the cut was never exercised), never a pass.
- **`chatRenderNoteRebuild`** — its second return flipped from `unmarked` (the old defect:
  a cut with no glyph) to `marked` (the new defect: a row that draws the glyph). It no longer
  *strips* the glyph before rebuilding: stripping it would delete the very cell the finding is
  about.
- **`chatRenderRowCutMidToken`** **deleted** — it existed only to demand a glyph for the
  mid-token shape, i.e. it encoded the pre-fix ontology and had no consumer afterwards (a
  comment marks the spot).
- **The canned pair `TestChatRenderComparatorSanity` is FLIPPED** (the epic's核心):
  lossless+unmarked → **PASS**, the same rows with one content cell replaced by `…` → **FAIL**
  (the marked leg also drops that character from the cells, which is what a reader's paste
  actually yields). The two previous directions were the ones that were wrong together.
- `chatRenderShapes[NOTE-WRAP].Pass` / `.ClaimedFix` rewritten to name 1bfd07eb **as superseded
  by `e58009f65`** for the glyph half, so a non-PASS verdict is a finding about the right claim.
- New helper `chatRenderNoteMatches(rebuilt, want, boundaries)`: reports whether the
  reassembly is byte-exact or differs only by the spaces a **boundary** break legitimately
  consumed, with the skip count bounded by the number of row boundaries. The verdict sentence
  distinguishes the two, so a byte-exact reading is visible as a byte-exact reading.

### 2. `testsuite/chat_cut_pty_test.go` — WHICH path it guards: decided, measured, documented

**The determination**: this case guards the **ANSWER path's right-edge label**, not the note
path's losslessness. The measurement is in the fixture itself: the mock answer is one 400-rune
token rendered through the **answer** renderer and every collected row opens with the answer
band's own lead (`▌ ` on the turn's first row, two blanks on a continuation) — a note row opens
`⚙ ` and continues `↳ `, so **no row this test reads can be a note row**. Its assertions are the
answer path's contract: (1) a row ends with the right-edge label, (2) the displaced character
opens the row below, (3) folding the label away, the characters are the answer once, in order.

**Changes made**: `cutLabel` now reads `lineedit.StatusEllipsis` (the **answer/status**
vocabulary) instead of `lineedit.NoteCutMarker` — the same glyph spelled from the note path's
vocabulary, which since `e58009f65` names a glyph the note path never draws; the file head now
states which of the two contracts it guards and why; and the collected band rows are `t.Logf`'d
so the report quotes the test's own evidence rather than a paraphrase. **No assertion was
weakened** — (1)/(2)/(3) are unchanged and the case still passes.

### 3. A follow-up is *not* needed for the sibling file

`chat_render_redzone_pty_test.go` consumes `chatRenderStripGutter` only; it carries no note-cut
assertion and needs no change (checked by grep, not assumed).

## Not Done

- The **q** for the answer path's label was not re-litigated: whether an answer row should draw
  `…` at its right edge (the status row's vocabulary) is a **separate contract** with its own
  tests (`internal/lineedit/answer_cut_marker_test.go`) and its own operator sighting. This task
  decided only *which* path `TestChatPTYCut…` guards, and left that contract alone (the epic's
  constraint: do not touch the status row or `statusEllipsis` semantics).
- `chatRenderNoteWords` / `chatRenderRowsByPrefix` / `chatRenderBoldHead` are left in place:
  they are small, documented helpers with no consumer in this file, but deleting them is a
  separate tidy-up with no bearing on the contract under test.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0, no output (`BUILD_OK`).
- Tests: `go test ./testsuite/ -run 'TestChatPTYCut|TestChatRenderVerdicts_OnRealPTY' -count=1`
  → `ok  	github.com/nick/gopi/testsuite	29.557s` (exit 0).
- `go vet ./testsuite/` → exit 0; `gofmt -l` on both files → no output.

### Ran: the real surfaces (Rule 31)

```
$ go test ./testsuite/ -run 'TestChatRenderVerdicts_OnRealPTY' -count=1 -v
```

The harness builds `bin/gopi` from **this worktree**, starts a real `gopi chat --tools` on a pty
forced to **64×34** via `TIOCSWINSZ`, lets a mock model call `bash` with the long command, and
reads the surface's own `--dump-screen` artifact back. Observed, verbatim:

```
dump row 004|⚙ call bash bash -
dump row 005|↳ c 'echo 0123456789abcdef0123456789abcdef0123456789abcdef012345
dump row 006|↳ 6789abcdefghij'
RENDER-VERDICT NOTE-WRAP: PASS — the call's command reaches the screen in full — the rows
  joined, gutter stripped, are the command BYTE FOR BYTE — every continuation row carries the
  gutter, and no row draws a cut glyph
RENDER-VERDICT NOTE-WRAP evidence: note reassembled: "bash -c 'echo 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdefghij'"
RENDER-VERDICT NOTE-WRAP evidence: command:          "bash -c 'echo 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdefghij'"
RENDER-VERDICT NOTE-WRAP evidence: reassembled vs command: byte-exact=true (spaces the row boundaries consumed: 0 of at most 2)
```

The dump geometry and provenance of that run: `cols=64 rows=34 attrs_recorded=true`.

```
$ go test ./testsuite/ -run 'TestChatPTYCut' -count=1 -v
```

```
answer-path band rows (5), as the terminal shows them:
  band row 0|fix/pushback-rule31-evid:alpha-beta_gamma.deltafix/…-rule31-…
  band row 1|evid:alpha-beta_gamma.deltafix/…-evid:alpha-beta_gamma
  band row 2|.deltafix/…-rule31-evid:alpha-beta_gamma.deltafix/…-rule31-…
  band row 3|ule31-evid:alpha-beta_gamma.deltafix/pushback-rule31-evid
  band row 4|-cut-end-
--- PASS: TestChatPTYCut_MidWordCutIsMarkedOnARealTerminal (13.97s)
```

**The shell flattens nothing here**: this case reads the pty's own bytes through
`lineedit.NewScreen`, i.e. the answer band's rows as the terminal holds them.

### Red → green (the epic's acceptance gate)

The new canned assertions must be **red under the old semantics**. Measured in this worktree:

1. Old contract restored on the **lossless** leg only
   (`v != verdictPass` → `v != verdictFail`):
   ```
   chat_render_verdict_pty_test.go:404: a lossless, unmarked mid-token cut was reported PASS
     (the call's command reaches the screen in full — the rows joined, gutter stripped, are the
     command BYTE FOR BYTE …) — the note half is measuring the OLD contract
   --- FAIL: TestChatRenderComparatorSanity (0.00s)
   ```
2. Old contract restored on the **marked** leg only
   (`v != verdictFail` → `v != verdictPass`):
   ```
   chat_render_verdict_pty_test.go:416: a mid-token cut carrying the cut glyph "…" in the
     command's OWN cells was reported FAIL — the harness is green over the defect this contract
     exists to catch
   --- FAIL: TestChatRenderComparatorSanity (0.00s)
   ```
3. Both legs restored (the file as it shipped) → green:
   `ok  github.com/nick/gopi/testsuite  0.529s`.

The **second** red is the one that matters: it is the old CANNED PAIR (an unmarked mid-token
cut → FAIL, the same row with `…` → PASS) — under it the harness is green over exactly the
defect `e58009f65` removed.

Second, independent red→green on the real surface: before the harness change this same run
printed `RENDER-VERDICT NOTE-WRAP: FAIL — a note row ended in the middle of a token with no
continuation ellipsis` against a correct screen; after it prints PASS with the byte-exact
reassembly quoted above.

### Files / workspace

```
$ git status --porcelain
 M testsuite/chat_cut_pty_test.go
 M testsuite/chat_render_verdict_pty_test.go
```

plus this devdoc and its `DOC-INDEX.tsv` row, committed together (Rule 36). No untracked
residue: the worktree carries only committed files (Rule 30) — the red-proof runs were made by
patching the tracked file in place and restoring it from a copy held **outside** the worktree
(`/dev/shm/…`, off the `/` partition by device id: `stat -c %d /` → `66308`, the scratch copy →
`27`).

## Evidence (self-filed)

- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0, no output.
- `go test ./testsuite/ -run 'TestChatPTYCut|TestChatRenderVerdicts_OnRealPTY' -count=1`
  → `ok  	github.com/nick/gopi/testsuite	29.557s`, exit 0.
- `go test ./testsuite/ -run 'TestChatRenderComparatorSanity' -count=1` → `ok … 0.529s` (green);
  the two red runs above are the same command with one assertion each flipped.
- `go vet ./testsuite/` → exit 0. `gofmt -l testsuite/chat_cut_pty_test.go
  testsuite/chat_render_verdict_pty_test.go` → no output.
- `gopi tool doccheck` → see the commit's own run; the new devdoc carries its row in the same
  commit (Rule 36).
- `stat -c '%d %n' / /dev/shm/gopi-01a1267b13b47351ae5304851160b8b5-redproof`
  → `66308 /` and `27 /dev/shm/…` (the red-proof scratch is off the `/` partition, Rule 22).

## Why the flip is the fix and not a loosening

An assertion can be wrong in two ways: too weak (it passes a defect) or **pointed at the wrong
contract** (it fails a correct screen and passes the shape that was removed). This pair was the
second kind, and its own report said so — `devdoc/note-row-cut-is-lossless.md` `## Not Done`
records that the two testsuite guards "define the defect as master did" and "would report the
FIXED shape as a failure". The flip therefore *strengthens* the guard in the direction that
matters: a note row that draws `…` inside the command's cells is now a FAIL, and the row's
losslessness is asserted **byte for byte** rather than through a whitespace-erasing
normalization.

**Commit**: see the branch tip — branch = the deliverable, worktree share, no push needed.
