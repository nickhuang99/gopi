<!-- doc-class: historical -->
# Task Report: merge `feat/chat-turn-layout` (94f5289b) onto peerbox master, SGR unified onto `internal/sgr`

Row: `01a0bedc-6d0e-7547-8d18-19c1587616a9` · Verdict recorded by reframe: **DOABLE**

## Completed

- **Branch reconstructed and merged.** `feat/chat-turn-layout` survived on peerbox only
  as archive tag `archive/feat-chat-turn-layout-20260921` (tip `94f5289b`). The row's
  own instruction ("re-implement from peerbox/master off the live V1 work") means the
  archive tag is the SOURCE, not a branch to check out: `git merge 94f5289b` in a fresh
  worktree off `master` is that re-implementation, with the ancestry preserved.
- **Real trial merge, not `git merge-tree`** (the epic states merge-tree is unreliable
  here). Two files conflicted — `cmd/agent/cli_chat.go`, `internal/agent/loop.go` —
  reproduced and recorded in `conflict-hunks.txt`; the remaining 10 branch files
  auto-merged including all of `internal/lineedit`. Resolved hunk-by-hunk so BOTH
  intents survive (before/after below).
- **SGR single source of truth.** The branch's pre-V1 family (`internal/lineedit/attr.go`
  line, re-landed on master as `internal/sgr` + `internal/lineedit/screenattr.go` by
  `4f56ccd2`) is retired: every terminal sequence the branch composed by hand now comes
  from `internal/sgr`. `internal/sgr` grew the builders a caller needs
  (`CursorSeq`, `MoveCursorSeq`, `ScrollRegionSeq`, `ResetScrollRegionSeq`,
  `EraseLineSeq`, `EraseDisplaySeq`, `PrivateModeSeq`, `BracketedPasteSeq`,
  `CursorShowSeq`); `internal/lineedit` (`raw.go`, `region.go`, `render.go`, `screen.go`)
  and `internal/agent` (`ansiDim`/`ansiReset`) now derive from it.
- **A grep gate, not a paragraph** (Rule 13/14):
  `internal/standards/sgr_single_source_gate_test.go` — `TestEscapeLiteralsLiveOnlyInSGR`
  scans every production `.go` under `internal/` + `cmd/`, flagging a raw `\x1b` byte or
  any `\x1b`/`\033`/`\u001b` escape inside a STRING LITERAL, with `internal/sgr/sgr.go`
  as the ONE reasoned permit; `TestSGRPackageOwnsTheSequenceBuilders` pins the builders
  the rule prescribes; `TestEscapeLiteralsGateHasARedProof` pins red shapes and green ones.
- **Intent A and intent B both proved by an observation** (see Verification §4-§5): the
  `region_yield` suite proves the editing line is never sacrificed, the `turn_layout`
  suite proves turn structure + aligned columns, and both were also observed in a live
  PTY capture at a real terminal geometry.

## Not Done

- **The merge to `master` itself.** The objective says review/PA owns that; the
  deliverable is the branch. `master` is untouched by this run.
- **Whole-tree suite.** Deliberately not run: the epic states it is not the merge path
  (28+ min). Scoped smoke only. For the record, one UN-SCOPED `go test ./cmd/agent` was
  attempted and hit Go's 10-minute package budget (`FAIL github.com/nick/gopi/cmd/agent
  600.469s`) inside `TestMergeUnionLaneReportsTheTransitionOnTheOrdinaryRun`
  (`cmd/agent/merge_union_test.go:345` → `mergeAcquireLock` → `LockEvidences`): merger-lock
  machinery, and `git diff --stat ccdd5f1e..HEAD` over `merge_union_test.go`,
  `merger_work_lock.go`, `merge_run_test.go`, `internal/tools/merger_status.go` is **empty** —
  the merge does not touch it. Run alone it passes in 1.9s
  (`go test ./cmd/agent/ -run TestMergeUnionLaneReportsTheTransitionOnTheOrdinaryRun` →
  `ok … 1.950s`), so the 600s figure is package-wide budget contention. This is the known
  `cmd/agent` package-budget condition (Rule 27), not a regression from this branch; the
  scoped run of the tests this diff actually implicates is green (§3).
- **`internal/lineedit/attr.go` is not deleted**, because it does not exist on master or
  in the merge result — the V1 re-land was `internal/sgr` + `screenattr.go`, so there is
  no file to remove. What the task names as the duplication was the branch's LITERALS;
  those are gone. (Recorded rather than silently "fixed".)
- **`cmd/agent` e2e chat capture** is a live-PTY instrument, not part of `go test`; it was
  run by hand (see `## Ran`) and its artifacts live outside the worktree under
  `~/workspace/_tmp/turn-layout-evidence/`, so they are not deliverables in the branch.

## Conflict hunks — before / after

Trial merge in a throwaway detached worktree at `ccdd5f1e` + `git merge 94f5289b`:
`CONFLICT (content): Merge conflict in cmd/agent/cli_chat.go`, `internal/agent/loop.go`;
12 files total, 10 auto-merged. `HEAD` side = master, `94f5289b` side = the branch.

### Hunk 1 — `cmd/agent/cli_chat.go` (screen ownership: seam vs console.log redirect)

```
++<<<<<<< HEAD
 +	flowSeam, restoreFlowSeam := installChatFlowSeam(ag, input)
 +	defer restoreFlowSeam()
++=======
+ 	if input.Answer() != nil {
+ 		path, rerr := routeChatDiagnostics(ag)
+ 		defer closeChatDiagnostics(ag)
+ 		switch { case rerr != nil: ... case path != "" && verbose: ... }
+ 	}
++>>>>>>> 94f5289b
```

**Resolution — composed, not chosen.** Both are installed, into ONE destination:

```go
flowSeam, restoreFlowSeam := installChatFlowSeam(ag, input)
defer restoreFlowSeam()

if input.Answer() != nil {
    if path, rerr := routeChatDiagnostics(ag, flowSeam); rerr != nil { ... } else if path != "" && verbose { ... }
    defer closeChatDiagnostics(ag)
}
```

`routeChatDiagnostics` was widened from the branch's `(ag)` to master's seam-aware
`(ag, screen io.Writer)` and now builds a single `io.MultiWriter(screen, file)`. The
branch's shape installed TWO competing redirects of `ag.SetDiagnosticWriter` — whichever
ran last won, so a notice was EITHER visible OR recorded. Master's seam is not a
markerless display: it is a writer that commits a whole row through the layout's single
writer, so feeding it the notices keeps them on screen *inside* the reserved layout (the
comment at the install site says exactly this) while the file keeps the durable record.
"Where did the notice go" now has one answer.

### Hunk 2 — `cmd/agent/cli_chat.go` (the turn renderer)

```
++<<<<<<< HEAD
 +		renderer := newChatTurnRenderer(input)
++=======
+ 		renderer := newChatTurnRenderer(input.Answer(), os.Stdout)
+ 		renderer.BeginTurn(iterCount+1, msg)
++>>>>>>> 94f5289b
```

**Resolution — master's constructor + the branch's turn chrome.**

```go
renderer := newChatTurnRenderer(input)
renderer.BeginTurn(iterCount+1, msg)
```

Master had already moved the renderer onto the input layer (`input`, not
`input.Answer(), os.Stdout`), which is what keeps `Reserved()` truthful — the band is
selected from the layout the editor installed. The branch's `BeginTurn` is orthogonal:
it paints the numbered header rule and the marker column. Taking the branch's constructor
would have bypassed the input layer and lost the reserved-band selection.

### Hunk 3 — `cmd/agent/cli_chat.go` (turn close)

```
++<<<<<<< HEAD
 +		flowSeam.Flush()
++=======
+ 		renderer.EndTurn()
++>>>>>>> 94f5289b
```

**Resolution — both, in order.** `renderer.EndTurn()` (the closing rule goes AFTER the
tail) then `flowSeam.Flush()` (the seam holds an unfinished reasoning line; a turn
boundary is where it must be committed). Master's yield-order fix and the branch's turn
structure are not alternatives here — they are two different streams' terminators.

### Hunks 4-6 — `internal/agent/loop.go` (the thinking stream's destination)

```
++<<<<<<< HEAD
 +			m := newThinkingMarkers(stderrIsTTY())
 +			flow := a.flowWriterFor()
 +			fmt.Fprint(flow, m.open) ... m.close
++=======
+ 			if a.OnThinking != nil {
+ 				a.OnThinking("\n" + thinkingPrefix + thinking + "\n")
+ 			} else { ...os.Stderr... }
++>>>>>>> 94f5289b
```

```
++<<<<<<< HEAD
 +	thinkingOut := a.flowWriterFor()
 +	defer func() { if thinkingStarted { fmt.Fprint(thinkingOut, markers.close) } }()
++=======
+ 	owned := a.OnThinking != nil
+ 	closeThinking := func() { if thinkingStarted && !owned { fmt.Fprint(os.Stderr, markers.close) } ... }
++>>>>>>> 94f5289b
```

```
++<<<<<<< HEAD
 +				if thinkingStarted { fmt.Fprint(thinkingOut, markers.close); thinkingStarted = false }
++=======
+ 				closeThinking()
++>>>>>>> 94f5289b
```

**Resolution — master's single destination; the branch's `owned` suppression DROPPED, its
structuring kept.** `thinkingOut := a.flowWriterFor()` decides the destination in ONE
place (it already returns `a.OnThinking` when a carrier seam is installed, else
os.Stderr — see its comment at `internal/agent/loop.go:5608`), and the markers are
emitted through it. The branch's `owned` flag suppressed the SGR markers for a carrier
because *its* carrier was a markerless display that labelled the block itself; the merged
seam is a WRITER that renders what it receives, so suppressing here would drop the
thinking block's delimiters from BOTH paths. The branch's `closeThinking` closure is kept
as the structuring (it collapses three duplicated close sites into one), and its dead
`emitThinking` helper — which wrote to a bare `os.Stdout`, bypassing the seam — is retired
with it (`grep -rn emitThinking` → no hits, so the branch's dead path is gone, not left
stale).

### The one semantic delta taken from the branch side (recorded, not silent)

`internal/lineedit/region.go` is NOT a conflict file, so the auto-merge blended it
without asking — and the branch had **removed master's `regionRowsMax` band cap**
(`regionRowsMax = 12`), a constant both sides inherited from the base `dc75ea64`.
The branch *deliberately* deleted it (`git diff dc75ea64 94f5289b -- internal/lineedit/region.go`
shows the deletion; it is not a merge artifact), and it deleted the `if bandRows > regionRowsMax`
clamp with it, so on a tall terminal the band now takes every residual row.

I checked whether this loses master's editing-line fix, and it does **not**: the removals in
the master→HEAD diff of this file are master deleting BASE-line code, and master's own
step-2 fix is present and intact in the merge —
`if budget := editorRowsBudget(rows); editingRows > budget { editingRows = budget }`
(`editorRowsBudget` = `rows - statusRows - regionRowsMin` clamped to the cosmetic ceiling
`editorRowsCap`, which 45531378 raised 4 → 12). The discarded step-2 line
`if avail := free - regionRowsMin; editingRows > avail` is exactly the arithmetic that fix
replaced. `grep -rn regionRowsMax` over the merged tree returns **no hits**.

Master's 133-line invariant test from 45531378
(`cmd/agent/lineedit_height_test.go`) survived the merge **byte-identical**
(`git diff ccdd5f1e..HEAD -- cmd/agent/lineedit_height_test.go` is empty) and passes —
it asserts a band **floor** (`got < minBand`), not the removed cap, so it does not
contradict the branch's decision. The cap's removal IS a behaviour change on a tall
terminal (a bigger answer band); it is **the branch's intent, and the work's whole point**,
so it is preserved and named here rather than reverted — reverting it would undo the
aligned-column layout the task exists to land. Recorded so review sees it.

## Verification

- **Build:** `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` exit 0;
  `bin/gopi` rebuilt in the run worktree (25,137,886 bytes, 2026-09-23 06:08).
- **Tests:** scoped smoke —
  `internal/lineedit`: `TestRegionYield_*` 5/5 PASS, `TestTurnLayout_*` + `TestStatusSeparatorCellWidths` + `TestStatusAlignedLayoutFitsARealTerminalWidth` + `TestTurnHeader_IsTotalInCols` 9/9 PASS;
  `cmd/agent`: master's six height/layout tests 6/6 PASS
  (`TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights`, `TestChatStatus_StaysOneScreenRowWithALongToolSummary`,
  `TestChatStatus_ToolRowRegression`, `TestChatStatus_LongLeadingFieldIsCutWithTheEllipsis`,
  `TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow`, `TestChatPublishDoesNotOvertakeQueuedInput`);
  `internal/standards`: the three SGR gate tests 3/3 PASS.
  `go vet ./...` exit 0.
- **Ran:** the built `bin/gopi` was executed for real, in a PTY at a real geometry, by
  the exact command (verbatim, from the evidence dir):

  ```
  cd /home/user/workspace/gopi-turn-layout
  EV=/home/user/workspace/_tmp/turn-layout-evidence
  COLS=100 ROWS=30 GRID_PORT=8891 SCREEN_PORT=8892 DISPLAY_NUM=:77 \
    WORK=/home/user/workspace/_tmp/sgr-grid-capture-after \
    bash "$EV/capture.sh" after /home/user/workspace/gopi-turn-layout
  ```

  which builds the checkout under test
  (`cd $REPO && CGO_ENABLED=0 go build -o "$WORK/gopi" ./cmd/agent/`) and then execs
  `python3 "$EV/termgrid.py" "$WORK/gopi" 100 30 "$EV/script-ui.txt"` — i.e.
  `gopi chat --tools` on a PTY, with a mock endpoint
  (`ANTHROPIC_BASE_URL=http://127.0.0.1:8891`, `ANTHROPIC_AUTH_TOKEN=capture-key`,
  `GOPI_HUMAN_TERMINAL=1`, `TERM=xterm-256color`, `LANG/LC_ALL=C.utf8`), driven by the
  typed script and dumped as a VT100 **cell grid** (`*.grid.txt` + `*.grid.attr.txt`) per shot.
  Its log: `~/workspace/_tmp/turn-layout-evidence/capture-after.log`.
  Re-run at the then-current tip (only this report file changed since `bcd8720a`):

  ```
  COLS=100 ROWS=30 GRID_PORT=8893 SCREEN_PORT=8894 DISPLAY_NUM=:78 \
    WORK=/home/user/workspace/_tmp/turn-layout-evidence-final \
    bash "$EV/capture.sh" final /home/user/workspace/gopi-turn-layout
  ```

  → `built …/gopi (a2f4ce99)`, and shots 2 and 3 came back **byte-identical** to the first
  run (`sha256=5115b7401fde289e` and `sha256=245dec79ebd7b863`), i.e. the captured layout is
  deterministic across two builds of the same tree. Its log:
  `~/workspace/_tmp/turn-layout-evidence/capture-final.log`. The screenshot leg degraded in
  both runs (`screen capture failed (non-fatal): the grid is the exactness instrument`) — the
  PNG leg is not the instrument quoted here; the VT100 cell grid is.
  The in-worktree `bin/gopi` from `make build` is 25,137,886 bytes, mtime 2026-09-23 06:13;
  the final acceptance re-run rebuilt it again at 06:17 from the same tree (identical byte
  count, so the mtime is the only difference between the two observations).
- **Result:** all four scripted states observed in BOTH runs (`waitfor 'gopi chat'`,
  `'Here is the answer'`, `'Done.'`, `exit`), each shot distinct within a run, and shots 2+3
  identical BETWEEN the two runs (`5115b740…`, `245dec79…`) — the turn layout renders the
  same grid from two independently built binaries of the same tree. In the final run's grid:
  row 9 = the header rule `──── turn 1 · what is in devdoc/chat-mode-report.md? briefly ────`,
  rows 14 + 17-26 open at the `▌` marker column, row 28 = `you>` (the editor line, never
  overwritten), row 29 = the status row, `·` separators at cells `[17, 28, 37, 61]` in both
  shot 1 and shot 3.
- **Commit:** `bcd8720a` `Merge chat-turn-layout (94f5289b) into master: turn structure + aligned status columns, SGR unified onto internal/sgr`
  on branch **`feat/turn-layout-master-off-v1`**, worktree `~/workspace/gopi-turn-layout`.
  Parents `94f5289b` + `ccdd5f1e`.

### Acceptance, clause by clause

| # | Clause | Observation |
|---|---|---|
| 1 | `git diff --name-only --diff-filter=U` empty | printed nothing; `git status --porcelain` printed nothing |
| 2 | `go vet ./...` + `make build` green | `vet_exit=0`; `build_exit=0`, `bin/gopi` 25 MB rebuilt |
| 3 | scoped smoke over `./internal/lineedit ./cmd/agent` green | `internal/lineedit` 14/14 PASS (`TestRegionYield_*` 5, `TestTurnLayout_*`+3 status/header tests 9); `cmd/agent` 6/6 PASS on master's own height/layout tests (the `45531378` invariant suite); `internal/standards` SGR gate 3/3 PASS |
| 4 | master's editing-line invariant still holds | **master's own test, byte-identical to master, PASS**: `cmd/agent/lineedit_height_test.go` is unchanged by the merge (`git diff ccdd5f1e..HEAD --` → empty) and `TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights` PASSes — it sweeps small heights streaming a 30-row tool output and asserts the `you>` line is still there. Reinforced by `TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow` PASS (printed `you> abcdabcd…` with the status row directly beneath) and `internal/lineedit` `TestRegionYield_*` 5/5. Live: shot `after-ui-1-boot.grid.txt` row 28 = `you>` with row 29 the status row. |
| 5 | turn structure + aligned columns exist | `TestTurnLayout_*` + `TestStatusSeparatorCellWidths` + `TestStatusAlignedLayoutFitsARealTerminalWidth` + `TestTurnHeader_IsTotalInCols` 9/9 PASS. The strongest single one is `TestTurnLayout_EditingLineAndStatusRowSurviveEveryTurnChromeRow`: it drives a full turn (header rule wrapping over TWO rows + guttered thinking + 12 body rows past the band) and then asserts, per row, that NO chrome row (`TurnRuleGlyph` or `TurnMarker`) landed between `p.EditorTop` and `p.StatusRow` and that the status row still holds the label — i.e. the chrome cannot be displaced by the band and vice versa. `TestTurnLayout_WrappedRowsKeepTheBlockColumn` pins the wrapped row continuing at the `▌` column. Live: shot `after-ui-3-answer-done.grid.txt` row 9 = the header rule, rows 14-16 = body rows opening at the `▌` marker column with the WRAPPED row 15 continuing at that same column, and the status row's `·` separators sit at cells **17/28/37/61 in BOTH shot 1 and shot 3** — reproducible with `python3 -c '…for ch=="·": enumerate(ln…)'` over the two `.grid.txt` files, which prints `[17, 28, 37, 61]` for each. |
| 6 | no SGR constant duplicated in `internal/lineedit` | gate `TestEscapeLiteralsLiveOnlyInSGR` PASS; independent scan for `"\x1b`, `"\033`, `"\u001b` in production `internal/lineedit/*.go` + `cmd/agent/*.go` returned **no hits**; only survivors are `keys.go` (prose comment) and `screen.go:171 const escByte = 0x1b` (the input byte, not a sequence) |
| 7 | worktree `git status --porcelain` empty | printed nothing (`bin/` is gitignored, `.gitignore:2`) |

### Evidence (self-filed)

- **Gate is red on the real tree, not just on synthetic strings.** Wrote
  `internal/sgr/PROBE_TMP.go` containing `const probeDim = "\x1b[2m"`, ran the gate:
  `sgr_single_source_gate_test.go:90: internal/sgr/PROBE_TMP.go:4 carries the escape spelling "\"\\x1b[2m\"" in a string literal` → **FAIL**; deleted the probe → `ok github.com/nick/gopi/internal/standards 0.886s` **PASS**.
- **Master's fix is in the merge's ancestry, not merely in a diff I read.**
  `git merge-base --is-ancestor <c> HEAD` → `45531378` YES ("fix(chat): the editing line is never sacrificed — explicit compositor yield order"), `5bd8573c` YES, `4f56ccd2` YES, `dc75ea64` YES, `ccdd5f1e` YES. Merge-base of `94f5289b` and `ccdd5f1e` is `dc75ea64`, single (`merge-base --all | wc -l` = 1), as the objective states.
- **No SGR literal survives outside `internal/sgr`.**
  `grep -rn '"\x1b|"\033|"\u001b' --include='*.go' internal/lineedit/ cmd/agent/ | grep -v _test.go` → empty.
- **Attribute probe reached the screen** (so the grid is not colour-blind):
  `after-ui-3-answer-done.grid.attr.txt` line 54 → `~ 0-1 default | 2-15 fg=1 | 16-20 default | 21-28 bold dim | 29-99 default`.

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/turn-layout-master-off-v1`, repo `/home/user/work/GoPi`.
  The MERGE commit is **`bcd8720a`** (parents `94f5289b` + `ccdd5f1e`); the branch tip is the
  branch head one commit after it — `git rev-parse feat/turn-layout-master-off-v1`, the
  report sits at the tip, so its own hash advances with each report commit. Merge
  `bcd8720a` if you want the merge alone.
  — the worktree shares the main repo's object store, so the commit is already in the main repo.
- **Recovery:** `git log --oneline feat/turn-layout-master-off-v1`, or
  `git show feat/turn-layout-master-off-v1:devdoc/turn-layout-merge-report.md`.
- **Worktree:** `~/workspace/gopi-turn-layout` (kept: the merge branch is the deliverable
  and review/PA merges it; not removed by this run).
- **Untouched:** `master`, `archive/feat-chat-turn-layout-20260921` (no force/reset/delete;
  no rebase of the branch).
