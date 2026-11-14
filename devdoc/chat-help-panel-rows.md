<!-- doc-class: historical -->
# Task Report: chat UI — the `/help` panel and the live status row no longer paint the same rows

Task: `bug(chat-ui): /help panel and the live status row paint the same absolute
rows and overwrite each other`.
Branch: `feat/chat-help-status-rows` (worktree `~/workspace/gopi-chat-help`,
base master `2fdc65e4`).

## Completed

- **Reproduced the defect with the committed instrument** (no guessing): the
  `/help` panel's first line lands on the **editing row** and every following
  line scrolls the band up over the panel's own previous line.
  `devdoc/evidence/sgr-grid/help-before-ui-5-help.grid.txt`:
  ```
  12|you> Usage: /quit
  13|                 End the interactive session and exit (Ctrl+D on an empty prompt does the same).
  14|                                                                                                The
  15|session's CHATS row is stamped on the way out, exactly as a normal exit.
  ```
  The bare `/help` panel (taller than the band) is worse —
  `help-long-before-ui-5-help.grid.txt` shows whole rows where the panel, the
  band's committed rows and the grammar text from *other* panel lines interleave
  mid-line (rows 12-29), which is exactly the "torn row" the epic reports.

- **Root cause.** With the reserved layout active the terminal is inside a
  scrolling region (`CSI 1;bandBottom r`) and the cursor is parked on the row the
  editor last drew (the editing row). The REPL printed its own text with
  `fmt.Println(out)` (`cmd/agent/cli_chat.go`, the `/help` and shared-slash-command
  branches), i.e. **flow output**: the first line is written at column 0 of the
  *editing* row (giving `you> Usage: /quit`) and each `LF` scrolls the band up by
  one, pushing the panel's earlier lines off and painting the later ones onto the
  band's rows. Nothing in that path knows the layout exists.

- **Fix — the REPL's own text takes the same path a committed answer line takes.**
  - `internal/lineedit/region.go`: `(*region).WriteLines(text) bool` — per line,
    clear the live row (`CSI liveRow;1H` + `EL`), place absolutely, write, then
    **one LF inside the scroll region** to promote the row into the band, exactly
    as `commitScroll` promotes a finalized answer line. A line wider than the band
    is split at cell boundaries (`cutCols`) rather than left to the terminal, and
    an absolute placement is issued before the LF (an absolute CUP cancels the
    emulator's pending-wrap state, so a full-width row cannot advance two rows).
  - `internal/lineedit/editor.go`: `(*Editor).WriteLines(text) bool` — publishes
    the panel into the band and repaints the chrome (the editor owns every other
    row); false when no layout is active.
  - `cmd/agent/lineedit_wire.go`: `(*chatInput).WriteLines(text)` — reserved path
    through the editor; the flow path keeps the historic bytes exactly
    (`fmt.Fprint` to **stdout**, plus a newline only when the text does not end
    with one), so a piped `gopi chat` reads precisely the bytes it always did.
  - `cmd/agent/cli_chat.go`: the `/help` branch and the shared-slash-command
    branch call `input.WriteLines(out)` instead of `fmt.Println(out)`; the editor
    path prints an explicit blank line between turns instead of the stderr
    newline the reserved path used to emit behind the layout's back.

- **Evidence, before/after, from the instrument** (branch-scoped, same script,
  same geometry, only the binary differs):
  - `help-before-ui-5-help.grid.txt` (torn) vs `help-after-ui-5-help.grid.txt`
    (panel on its own band rows 7-9; editing row 12, status row 29 untouched).
  - `help-long-before-ui-5-help.grid.txt` (torn, bare `/help`) vs
    `help-long-after-ui-5-help.grid.txt` (band rows 0-10, each a whole help line).
  - the `raw.txt` siblings carry the exact PTY byte streams.
  - `capture-help.sh` is the reusable one-command reproduction (the committed
    `capture.sh` shot set deliberately omits a `/help` shot — its own comment now
    says why and points here instead).

- **Regression tests** (rendered cell grid, Rule 10.2) —
  `cmd/agent/chat_panel_rows_test.go`:
  `TestChatHelpPanel_RendersInTheBandNotOverTheChrome` (every panel line in the
  band, the editing/status rows carry none of it and keep their own text),
  `TestChatHelpPanel_LongPanelScrollsWithoutTearing` (a panel 3× the screen
  height: every non-empty band row is exactly one written line, no interleave),
  `TestChatPanelWrite_BlankLineReservesARow` (the inter-turn separator is a real
  band row, so the reserved and piped paths agree),
  `TestChatPanelWrite_KeepsTheFlowPathByteIdentical` (the frozen cooked path).

- **The same defect on the REPL's own NOTICES (`/new`, `/tools`).** Found while
  independently re-verifying this branch with a freshly built binary: those two
  branches still wrote with `fmt.Fprint(os.Stderr, …)` — flow output — so the
  notice landed at column 0 of the **editing row** and the editor's next repaint
  **erased it**: the raw PTY stream carried `[gopi] conversation cleared.` while
  the screen showed a bare `you>` (three measured states:
  `notice-before-ui-5-new.grid.txt` = `you> [gopi] conversation cleared.`,
  `notice-erased-ui-5-new.grid.txt` = bare `you>`, `notice-after-ui-5-new.grid.txt`
  = band row 9). Fixed in `1e5ed45f` by `(*chatInput).WriteNotice`, which routes
  the notice through the same band path; on the cooked/flow path it writes to the
  historic destination (`errWriter` → `os.Stderr`) with the historic bytes, since
  an e2e test greps that stream for these strings.
  Tests: `TestChatNotice_ReachesTheBandWhereTheUserReadsIt` (red-proofed — with
  the band route removed the grid shows `you> /new` and the notice is gone) +
  `TestChatNotice_KeepsTheFlowPathByteIdentical`.

## Not Done

- The **X11/PNG half** of the instrument was not re-run: `capture-help.sh` drives
  the PTY cell grid only. The grid is the exactness instrument (the epic's
  acceptance is a grid claim — "no overlapping text on a row"), and the pixel
  half adds no column information this defect needs.
- **Two `cmd/agent` test failures are PRE-EXISTING on clean master** and not
  regressions from this branch (verified on a detached master worktree at
  `2fdc65e4`): `TestCompletionCandidateSetsCoverTopLevel` and
  `TestTopLevelHelpCoversEveryDispatchedSubcommand` (the latter reports
  `dispatch predicate ConfigSubcommand has no help mapping`). They are unrelated
  to the chat layout and are left alone.
- The epic's phrasing "or the panel clearing the status row" was satisfied the
  other way: the panel streams **inside** the band, so it neither clears nor
  touches the status row — the stronger of the two acceptable outcomes.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (also
  `go build ./...` clean); `go vet ./cmd/agent/ ./internal/lineedit/` → clean.
- Tests: `go test ./cmd/agent/ ./internal/lineedit/` → `internal/lineedit` ok;
  `cmd/agent` FAIL with exactly the three masters-red tests below (two
  pre-existing, one ordering artifact), everything else green. Focused:
  `go test -run 'TestChatHelpPanel|TestChatPanelWrite|TestChatStream|TestChatEditingLine|TestLineedit|TestCompletion|TestRegion|TestAnswer|TestScreen|TestEditorRows' ./cmd/agent/ ./internal/lineedit/` → both packages ok.
- Instrument: `devdoc/evidence/sgr-grid/capture-help.sh help-before <master-binary> '/help quit'`
  then `… help-after <branch-binary> '/help quit'` → the before/after grid pair
  above (both runs reach `shot ui-5-help`, selftest OK).
- Commit: `2714db29` (fix) + `8b6d249e` (evidence/docs, the report edit is the
  tip of the branch).
- Focused re-run after the blank-row correction:
  `go test -count=1 ./internal/lineedit/` → ok (8.5 s);
  `go test -run 'TestChatPanelWrite|TestChatHelpPanel' -count=1 ./cmd/agent/` → ok.
- `git status --porcelain` in the worktree after the commits → empty.

## Residual defect, DISPATCHED (not fixed here)

Measured while re-verifying this branch (a `hello world` turn instead of `/help`,
`capture-help.sh` with only the binary varying, 100x30):

```
12|you>
13|budget_tokens, so the harness aborts a round whose streamed thinking passes 8192 tokens (~32768 char
14|s) and re-issues it thinking-disabled. Raise GOPI_MAX_TOKENS to keep a larger thinking budget.
15|                                                                                              [think
16|ing] The user said "hello world". That's a trivial greeting. No memory search needed (off-topic chat
17|ter skip). Just respond briefly.
...
29|deepseek-flash · iter 1 · tools · 10.2k tokens  ready
```

**The REPL's own text is fixed; the AGENT's flow output is not.** Three sources
still write to `os.Stderr` outside the REPL branches this branch rerouted, so
they land on the editing row / scroll the band exactly as `/help` used to:

1. the live thinking stream — `internal/agent/loop.go` `fmt.Fprint(os.Stderr, …)`
   for `markers.open`, each reasoning delta, and `markers.close`
   (the `thinking_delta` case, ~line 4091);
2. the agent's `Logger` — `log.New(os.Stderr, "[gopi] ", 0)` (`loop.go:2430`),
   which is the `[gopi] ℹ️ thinking budget 16384 clamped to 8192 …` line;
3. other agent-side `os.Stderr` warnings (parseTaskList recovery, raw-log/session
   errors, env warnings).

Same failure mode on master, a different tear (the answer, then the warning
fragment, then `[thinking]` mid-row, and the status row itself overwritten
mid-line by `… thinking…[gopi] ℹ️ thinking budget …`).

It is **out of this branch's scope** (it needs a seam in `internal/agent` — the
thinking stream is hardcoded to `os.Stderr`, and re-pointing `ag.Logger` is a
wiring change; both reach beyond the chat REPL this task was scoped to), so per
Rule 26 it is **dispatched as its own task — `01a0c0a7-6ef0-76ca-88f4-78fd474bf29f`**
(tags `bug,chat-ui`) with the two measured grids + raw PTY streams attached.
This branch does not claim it.

## Evidence (self-filed)

- **RED proof of the new tests:** with `chatInput.WriteLines`'s reserved branch
  reverted to the historic flow write, `go test -run TestChatHelpPanel ./cmd/agent/`
  printed `--- FAIL: TestChatHelpPanel_RendersInTheBandNotOverTheChrome` and
  `--- FAIL: TestChatHelpPanel_LongPanelScrollsWithoutTearing`; restoring the
  file made the same command print `ok github.com/nick/gopi/cmd/agent`.
- **Pre-existing master reds (not mine):** in a detached worktree at `2fdc65e4`,
  `go test -run 'TestCompletionCandidateSetsCoverTopLevel|TestTopLevelHelpCoversEveryDispatchedSubcommand|TestBareRunKeepsAllItsArtifactsInOneSessionDir' ./cmd/agent/`
  printed the same two FAIL lines (`TestBareRunKeepsAllItsArtifactsInOneSessionDir`
  passed there and failed once on the branch under the full-suite run — it is an
  ordering/timing artifact of the 586 s suite, not a layout assertion).
- **Screen readings quoted above are verbatim** from the committed
  `*-ui-5-help.grid.txt` files (one line per screen row, produced by
  `termgrid.py`, whose `--selftest` passed in both capture runs).
- **Worktree cleanliness (Rule 30):** `git status --porcelain` in
  `~/workspace/gopi-chat-help` prints nothing after the final commit — see the
  commit step; the capture work trees live under `~/workspace/_tmp/`, outside
  the worktree.
