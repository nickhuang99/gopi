<!-- doc-class: normative -->
# One chat turn, one assistant answer — the duplicated chat answer (`pty-okpty-ok`)

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Status**: fix landed on `fix/dup-assistant-record` (linked worktree
`~/workspace/gopi-dup-assistant-record`, base `e8ee5156`).

## Completed

- **Root cause.** The duplicate was NOT the "streaming SSE path + non-streaming
  fallback both appending" hypothesis from the briefing. It is
  `internal/agent/chat_ops.go`'s **degenerate-round guard**: a no-tool round
  whose answer was shorter than `minFinalAnswerChars` (40) was *re-asked*, and
  because the answer is streamed to the carrier the moment the model produces
  it, the re-asked round delivered the same text a second time — both on the
  live stream (what the TUI renders) and in the session JSONL (a second
  assistant record). The repro prompt's answer is `pty-ok` — 6 chars.
- **Every producer that appends a session record for one turn** (`ChatOps`):
  | what | site | per |
  |---|---|---|
  | assistant answer | `internal/agent/chat_ops.go:433` (`LogAssistantText`) | turn — **was** round |
  | assistant tool_use | `internal/agent/chat_ops.go:343` (`LogAssistantToolUse`) | round |
  | request stats | `internal/agent/chat_ops.go:354` (`LogRequest`) | round |
  | user turn | `internal/agent/chat_ops.go:244` (`LogUser`) | turn |
  | tool result | `internal/agent/chat_ops.go:411` (`LogToolResult`) | round |
  | session_end | `internal/agent/chat_ops.go:458` (`LogSessionEnd`) | session |
- **Fix (two halves, both needed).**
  1. `chat_ops.go` no-tool branch: a **non-empty** round is now accepted as the
     answer, however terse. The re-ask enforced nothing (the second round's text
     was accepted at *any* length), so its only effect was the duplicate. The
     one-shot continuation is kept for the genuinely degenerate round — the
     model ended its turn having said **nothing** — where nothing was streamed,
     so asking again duplicates nothing.
  2. `chat_ops.go`: the assistant's TEXT is logged **once per turn**, after the
     loop converges (`:433`), instead of once per round. `tool_use`/`request`
     stay per-round (each round really happened on the wire); only the answer is
     the turn's answer.
- **Regression tests** (Rule 10.5), one per carrier layer:
  - `internal/agent/chat_ops_test.go` `TestChatOps_TerseAnswerAnsweredOnce` —
    a 6-char answer: exactly 1 API call, the stream carries the text once, the
    session JSONL holds 1 user + 1 assistant text record.
  - `testsuite/chat_short_answer_single_record_test.go`
    `TestChat_TerseAnswer_OneAssistantRecordPerTurn` — the **real carrier**: the
    `gopi web --tools` server (same engine the Bubble Tea TUI drives via
    `POST /api/chat`), one turn with the repro prompt; asserts 1 assistant
    record and that the concatenated SSE deltas — what the TUI renders — equal
    the answer once.
- **Guard test retargeted.** `TestChatOps_DegenerateRoundGetsOneContinuation`
  asserted the buggy shape (a *terse* round re-asked); it now drives the truly
  degenerate **silent** round, which is the case the continuation still covers.

### Why the guard existed, and why narrowing it is the fix

The guard was introduced with `ChatOps` (`b94b0120`) to catch "the model
concluded on reflex without doing the work". Measured against its own tests: it
fired on a **short** answer and accepted whatever the second round said — it
enforced no substance, only a second round-trip. On a display carrier that
second round-trip IS the visible defect. Narrowing it to the silent round keeps
the one case where the continuation has a real effect (the model said nothing
at all) and removes the only case where it produced a duplicate.

## Not Done (honest)

- The **TUI-render** half of the acceptance is asserted at the SSE-delta layer
  (the frames the TUI's `DeltaText` handler appends), not by driving HP's
  `gopi ui` PTY: that binary lives on `hp/feat/gopi-cli-bubbletea`, which
  vendors Bubble Tea into `go.mod` (a new dependency this branch deliberately
  does not take). The deleted second round is the fix at the source the TUI
  consumes — nothing is left to render twice.
- `Agent.Chat` (the plain, tool-free carrier) keeps its own one-record-per-turn
  logging (`internal/agent/loop.go:3723`); it has no round loop and was never
  affected. Left untouched.
- The task-loop carriers (`loop.go:3230`, `loop.go:3392`) still use
  `minFinalAnswerChars` for the task-end double-confirm. That is a different
  contract (a dispatched task, with a task list to confirm) and is out of scope
  here — `cmd/agent/cli_term_once.go`'s warning about it remains accurate.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests (branch-scoped, the packages the diff touches):
  - `go test ./internal/agent/ -run 'TestChatOps' -count=1` → 7/7 PASS
    (incl. `TestChatOps_TerseAnswerAnsweredOnce`, and the retargeted
    `TestChatOps_DegenerateRoundGetsOneContinuation`)
  - `go test ./testsuite/ -run 'TestChat|TestWeb' -count=1` → PASS except
    `TestChatPTY_EditorRendersAndSubmits`, **pre-existing** (see `## Evidence`)
  - `go test ./internal/agent/ -count=1` → FAILs ONLY on
    `TestDocAnchorsResolveInNormativeDocs`, **pre-existing** (see
    `## Evidence`)
  - `go test ./cmd/agent/ -run 'Chat|Web' -count=1` (the surface this diff
    touches, branch-scoped per Rule 27) → `ok github.com/the operator/gopi/cmd/agent
    20.372s`. The FULL `cmd/agent` package was not run to completion: the
    composite whole-package run exceeded the budget on this host, so **no
    whole-package claim is made** — only the scoped command above was observed
    green.
  - `go test ./internal/standards/ -count=1` → the devdoc-manifest coverage gate
    is red on 3 unclassified devdoc files, **pre-existing** (3 at base too,
    overlap not identity); the two gates my row must satisfy
    (`TestDocManifestRowsAreWellFormed`, `TestDocClassBannerMatchesManifest`)
    PASS
  - `go test ./internal/stylegate/ -count=1` → all five rule lines identical at
    base and on the branch (pre-existing); **zero** hits in the files this diff
    touches
- Commit: the tip of `fix/dup-assistant-record` in
  `~/workspace/gopi-dup-assistant-record` (recovered with
  `git show fix/dup-assistant-record:devdoc/chat-duplicate-assistant-record.md`)
- devdoc: this file (+ a normative row in `devdoc/DOC-INDEX.tsv`)

## Evidence (self-filed)

The command below is ONE command run in two worktrees: the base commit
`e8ee5156` (detached scratch worktree, `bin/gopi` built from base) and the fix
branch. Same test file, same repro prompt (`"Reply with exactly: pty-ok."`).

- **BEFORE — base `e8ee5156`** —
  `go test ./testsuite/ -run TestChat_TerseAnswer_OneAssistantRecordPerTurn -count=1`:
  ```
  --- FAIL: TestChat_TerseAnswer_OneAssistantRecordPerTurn (6.50s)
      chat_short_answer_single_record_test.go:53: streamed (rendered) text = "pty-okpty-ok", want "pty-ok" — the answer streamed more than once
      chat_short_answer_single_record_test.go:59: session: assistant events = 2, want 1 (types: [session_start user assistant request assistant request])
      chat_short_answer_single_record_test.go:73: session assistant records = [pty-ok pty-ok], want exactly one carrying "pty-ok"
  FAIL
  ```
  Record counts, one user turn: **assistant = 2**, user = 1; the render-visible
  stream was literally `pty-okpty-ok`.
- **AFTER — fix branch** — same command:
  ```
  --- PASS: TestChat_TerseAnswer_OneAssistantRecordPerTurn (1.81s)
  PASS
  ok  	github.com/the operator/gopi/testsuite	4.622s
  ```
  Record counts, same turn: **assistant = 1**, user = 1; the concatenated SSE
  deltas equal `pty-ok` once.
- **Unit layer, same repro** —
  `go test ./internal/agent/ -run TestChatOps_TerseAnswerAnsweredOnce -count=1 -v`:
  ```
  --- PASS: TestChatOps_TerseAnswerAnsweredOnce (0.12s)
  PASS
  ```
  (1 API call for the terse answer — before the fix the guard made 2.)
- **Pre-existing red, not mine** —
  `TestChatPTY_EditorRendersAndSubmits` fails identically in a detached
  worktree at base `e8ee5156` (no diff applied):
  ```
  chat_pty_test.go:272: the editor did not announce itself on a TTY; output so far:
      gopi chat — Ctrl+D or /quit to exit.
      [?2004h[1;12r[1;1H[13;1H[2Kyou>
  --- FAIL: TestChatPTY_EditorRendersAndSubmits (0.92s)
  ```
  Same assertion and same captured output on the branch and at base — a
  pre-existing failure of the raw-mode editor path, unrelated to chat records.
  Likewise `internal/standards`' devdoc manifest coverage (3 devdoc files on the
  branch, 3 at base — overlap, not the same 3) and `internal/agent`'s doc-anchor
  gate (base `6 new, 10 stale permits` = branch `6 new, 10 stale permits`; the
  new normative row contributes 0 anchors and 0 drift).
