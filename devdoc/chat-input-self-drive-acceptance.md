<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The self-driven chat loop, end to end: the operator's act reproduced, and the bound that renews only on their line

Class: normative (an acceptance record with the two defects it found).

## What this epic delivered

`chat_surface` could already reach the chat input line, but nothing proved the
OPERATOR-VISIBLE half:

1. **The operator's own act, driven by the model** — inject `/h`, then Arrow-Down,
   then Enter, with zero operator keystrokes, judged on the painted cell grid.
2. **A renewable bound** — the self-driven loop stops at an absolute ceiling, and
   only a line the OPERATOR submitted opens a fresh period (the ruling on task
   `01a0e44a`: reset-per-user-input, never by the model itself).

## Defect 1 — the renewal ran on the WRONG ARM (found by the full-stack case)

`internal/agent.ChatSurfaceUserTurn()` is the one reset site, and the unit gate
(`internal/agent/chat_surface_renewal_test.go`) drove it directly and passed. What
that gate cannot see is the WIRING, and the wiring was wrong: the carrier called
`ChatSurfaceUserTurn()` after EVERY line, on both arms — so a line the MODEL had
just typed renewed its own budget.

Measured on the first end-to-end run: the mock asked for one more self-driven turn
on every round, and the session never reached its bound. The console read
`self-driven turn 1/8 of this period` and `budget RENEWED by your own submitted
line` forever, one pair per turn, with the counter pinned at 1. That is exactly the
self-renewal the ruling forbids, wearing the operator's clothes.

**Fix** (`cmd/agent/cli_chat.go`): the seam's own branch sets `selfDriven = true`,
and the renewal is gated on **which arm produced the line** — never on the text
(the two are indistinguishable as strings, so a text-keyed renewal IS the defect).
The `ChatSurfaceTakePending()` arm does not renew; the `lineCh` arm does.

Red proof: with `if selfDriven || true {`, the bound acceptance fails at
`timed out waiting for the loop's LAST turn to announce the bound on the console` —
the loop never closes.

## Defect 2 — the console notice is longer than the terminal is wide

Every notice is asserted against the PAINTED GRID, and the grid wraps: the renewal
notice reaches a 100-column pty as `…available again` / `) — it is reset only this
way…`, so the wrap point and the constant disagree about whether a space belongs
there and a naive `strings.Contains` fails on a notice that is present and
complete. Two helpers carry the fix, and the distinction between them is the point:

- `flattened` removes ALL whitespace from both sides — the comparison claims "this
  wording reached the console", and no claim about where the terminal broke the line.
- `notice` reads the LIVE grid ("the operator can read it right now");
  `sawNotice` reads the DURABLE stream (`streamANSIFree`, the recorded bytes with
  ANSI stripped). The renewed period's "turn 1/8" is announced and then SCROLLS OFF
  a 30-row pty while the mock races to the next bound — so only the durable stream
  can honestly answer "did the console say it".

## The two acceptance cases

| Case | File | What it proves |
|---|---|---|
| The operator's act | `cmd/agent/chat_surface_help_menu_e2e_test.go` | `/h` opens a menu whose sole row is `/help` (asserted as `1 + CompletionNoteRow()`), Down CLAMPS and leaves the interaction rows byte-identical, Enter ACCEPTS and leaves `/help` on the editing row — every claim read off the cells, with the arrival of the injected key proven separately by new bytes on the wire (a clamped arrow's effect is invisible, so "nothing changed" alone would also be true of a key that never arrived) |
| The bound | `cmd/agent/chat_surface_bound_e2e_test.go` | the loop accepts the bound's worth of turns and refuses the next (8 accepted, 1 refused = 9 asks), the console announces the last turn LIVE; the operator's own line on the pty master renews it, the console says so, the next turn is numbered `1/8` again, and the renewed period terminates too |

Both run a real `gopi chat` REPL on a real pty with a mock model as the ONLY actor
(`chat_surface action=key` / `action=type`), and the operator supplies exactly one
line per case — the bootstrap, because the REPL runs no round before a line arrives.

### One key per model round, and the test holds the gate

The keys are injected by the model, but the acts must be OBSERVED one at a time: a
model that fires all three keys in one round leaves the test racing its own
fixture, and "the grid was intact after the Down" is unobservable once the Enter may
already have landed. Each round after the first BLOCKS until the test releases it,
having asserted the previous act. The SSE body is the gate; the mock stays the only
actor.

### The self-driven turn must END for the bound to be reachable

The bound is charged at the TURN BOUNDARY (`ChatSurfaceTakePending`), so a mock
that keeps calling `type` inside one turn replaces its own pending line forever and
never reaches the seam — measured: the turn died at the agent's own 40-tool-round
ceiling with the counter still at zero. The fixture therefore types and then
ANSWERS, which lets the typed line become the next turn.

### The mock decides from the LAST message, never the whole body

The conversation replays, so an earlier tool result stays in the history. A
whole-body search for "at its bound" made every round after the first refusal
answer the closure marker (read as "period 2 never started" while the console was
running it). Every fixture decision reads the tail after the last `"role":"user"`.

## Files

- `cmd/agent/chat_surface_help_menu_e2e_test.go` (new) — the operator's act.
- `cmd/agent/chat_surface_bound_e2e_test.go` (new) — the bound, the renewal, the notices.
- `cmd/agent/cli_chat.go` — the arm gate (`selfDriven`).
- `internal/lineedit/menu.go` — `MenuMarkerActive()` exported (Rule 1b: the acceptance
  reads the marker the editor paints rather than retyping the glyph).

## Verification

- Build: `go build ./...` — OK (exit 0); `CGO_ENABLED=0 go build -o /tmp/gopi-01a0e442 ./cmd/agent/` — OK.
- Tests:
  - `go test ./cmd/agent/ -run TestChatSurface -count=1` — ok, 3 consecutive runs.
  - `go test ./internal/lineedit/ -count=1` — ok (34.6s).
  - `go test ./internal/agent/ -run ChatSurface -count=1` — ok.
  - `go vet ./cmd/agent/ ./internal/agent/ ./internal/lineedit/` — clean.
  - Flake sweep: `TestChatSurfaceKey_OperatorHelpMenuAct` alone 12/12 PASS.
- Ran: `go test ./cmd/agent/ -run 'TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator' -count=1 -v`
  — a real `gopi chat --tools` REPL on a real pty (100x30), a mock deepseek SSE
  server, the only actor being `chat_surface`. Result, verbatim from the run:
  - `PHASE 1: the model asked for a self-driven turn 9 times; the tool refused at the bound and the console announced "[gopi] this is the last self-driven turn of this period (bound 8): the loop closes when it ends."`
  - `PHASE 2: the operator's line "carry on" renewed the budget — the console carried "[gopi] the self-driven loop's budget was RENEWED by your own submitted line (8 turns available again) — it is reset only this way, never by the model itself." and the next self-driven turn was numbered 1 of 8`
  - `BOUND: 36 model round(s); the self-driven loop stopped at the absolute ceiling TWICE (once before the operator's line, once after it)`
- Ran: `go test ./cmd/agent/ -run 'TestChatSurfaceKey_OperatorHelpMenuAct' -count=1 -v` — the
  operator's act end to end: the REAL `NewSlashCompleter()` offers exactly one candidate for
  `/h` (asserted as a premise before anything is injected), the menu reserves
  `1 + CompletionNoteRow()` rows, Down is proven ARRIVED by new bytes on the wire while the
  interaction rows come back byte-identical, Enter leaves `/help` on the editing row, and the
  final request's tool results carry exactly 3 `injected=` reports and 0 refusals.
- Red proofs (temporary patches, all reverted — `grep -c "RED PROOF"` on both files now 0):
  - arm gate removed (`if selfDriven || true {`) → the bound case FAILS:
    `timed out waiting for the loop's LAST turn to announce the bound on the console`.
  - text path reverted in `internal/lineedit/inject.go` → the act case FAILS:
    `timed out waiting for the completion menu to open`.
  - pacing reverted (`injectKeyInterval() == 0`) → same wait fails (unpaced keys break framing).
  - `injector.wake()` reverted → PASSES: recorded honestly, that seam is not load-bearing
    for this path (it was briefly patched as a candidate; the case does not distinguish it).
- Commit: `e749a04a` fix(chat): renew the self-driven budget only on the operator's own arm

## Evidence (self-filed)

- `go build ./...` printed nothing, exit 0 — BUILD_OK echoed.
- `go test ./cmd/agent/ -run TestChatSurface -count=1` printed `ok github.com/nick/gopi/cmd/agent`
  three times in a row.
- Red-proof observation: the arm-gate revert produced `--- FAIL: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (10.47s)`
  at `chat_surface_bound_e2e_test.go:304`, then the patch was restored and
  `git diff --stat cmd/agent/cli_chat.go` showed only the 39 inserted lines of the fix.
- `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`.

## Not Done

- `injector.wake()` is exercised but NOT distinguished by these cases (documented above).
- `go test ./internal/agent/` as a WHOLE package is RED, but it is red on master too and for
  an unrelated reason: `TestDocAnchorsResolveInNormativeDocs` fails reading
  `internal/agent/testdata/docanchor-baseline.tsv:266: duplicate entry
  "devdoc/chat-answer-gold-is-not-a-regression.md|blank|palette.go:500"` — verified by running
  the same test in the main repo at master, which fails identically. Not touched here.
- The teammate case `TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty` (the sibling
  branch's own case) failed 1-in-8 when run TOGETHER with the new act case; alone it is 12/12
  and the new case is 12/12 alone. Both cases use a per-test `t.TempDir()` GOPI_HOME and their
  own pty/server, so no shared state was identified — this is recorded as an unresolved
  intermittent rather than fixed.

