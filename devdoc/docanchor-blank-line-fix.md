<!-- doc-class: normative -->
# Task Report: re-point the drifted §4.1 hippocampus doc anchors

## Completed
- Reproduced the red on master (worktree `~/workspace/gopi-docanchor` @ f6c54702,
  fresh branch `fix/docanchor-blank-line`) exactly as briefed:
  `docanchor_test.go:127: … anchor internal/tools/memory.go:1244 points at a BLANK line`.
- Fixed the reported anchor AND the rest of the same table (§4.1 海马体 Memory):
  the table was authored once (61aae3a9, 2026-09-10, "docs(hippocampus): author the
  missing spec") and never re-derived, while `internal/tools/memory.go` grew
  3.5k→3516 lines — so **every** row was stale, not just the one the gate caught.
- Verified SYMBOL IDENTITY, not non-blankness: for each of the 21 anchors I
  asserted the anchored line actually contains the named symbol. 21/21 OK
  (script output in §Verification).
- Committed. Docs-only; no product code touched.

## The one-line root cause
A `{file}:{line}` anchor is a line-addressed reference in a file no edit
re-derives. `memory.go` gained lines above the anchored symbol, the symbol moved
1253→1483, and line 1244 became the blank separator that used to sit *above*
`MemorySearchHits`.

## Not Done (honest) — and why
1. **The gate is weaker than it reads.** It asserts only *in-range + non-blank*
   (docanchor_test.go §"WHAT it asserts", item 4). A wrong-but-non-blank number
   passes silently — §4.1's `MemorySearch :1015` pointed at a `for _, c := range
   append(...)` line for 7 months. Non-blank is a weaker proxy than
   symbol identity; I applied identity by hand, the gate still cannot.
2. **The same drift class is live and UNGATED one table below.** §4.2 前额叶
   Decision and §4.5 心跳/节律 use bare `:NNN` continuations (no file in
   backticks), which `docAnchorRe` cannot match — so the gate reports green
   while those rows are stale by tens-to-hundreds of lines. Measured on this
   tree: `Run :2386→2519`, `Resume :2397→2530`, `runEngine :2406→2539`,
   ReAct `for :2685→2844`, `TakeJobNotices :2729→2892`, `promptSections
   :760→807`, `PromptSection :750→797`, `assembleSystemPrompt :776→823`,
   `claudeToolDefs :802→849`, `maybeInjectTaskStartSearch :3432→3618`,
   `hotResidentMessagePrefix :3468→3654`, `maybeEnforceCompletionStore
   :3472→3658`, `taskStartSearchMessage :170→172` (const line),
   `completionCheckpointMessage :158→160`, `resumeContinuationMessage
   :146→148`, `NoEnforceMemory :1578→1679`, `GOPI_NO_ENFORCE_MEMORY
   :2305→2427`, `case toolnames.ToolMemory: :4485→4839`, `Interrupt
   :2341→2474`, `interruptRoot :2368→2501`, `maybeNudgeStaleTaskList
   :4146→4491`; §4.5 `OpenTaskHeartbeatConn :65→66`, `DetectStaleTasks
   :140→142`, `NotifySyntheticEvent :39→40`, `notifyTaskTransition
   :271→280`.
   Two of these are **blank** (tasknotify.go:173 — the measured `taskNotifyPayload`
   is :~180+). Fixing this is a separate deliverable (new row, dispatched) —
   it changes the gate's *reach*, which is exactly the class this row exists to
   close, so leaving it silently green here would be the same defect one level up.
3. The sibling `gofmt -l .` 7 pre-existing paths named in the briefing are
   untouched — explicitly out of scope (operator's call).

## Verification
- Repro (before): `go test ./internal/agent/ -run TestHippocampusDocAnchorsResolve -count=1 -v`
  → `docanchor_test.go:127: … memory.go:1244 points at a BLANK line` / FAIL.
- After: same command → `doc anchors verified: 72 across 2 spec files` / PASS.
- Symbol identity: python audit of all 21 re-keyed anchors → `symbol-identity failures: 0`.
- Full package: `go test ./internal/agent/ -count=1` → `ok github.com/the operator/gopi/internal/agent 22.870s`.
- Build: `CGO_ENABLED=0 go build ./...` → OK.
- gofmt: `gofmt -l $(git diff --name-only)` → empty (clean).
- Commit: `8867f88d` on `fix/docanchor-blank-line` (worktree-shared ref).

## Deliverable declaration
- Branch: `fix/docanchor-blank-line` (in the shared main repo — worktree, no push needed)
- Recovery: `git show fix/docanchor-blank-line:devdoc/docanchor-blank-line-fix.md`
- Commit `8867f88d` is in `/home/user/work/GoPi`'s object store (linked worktree).
