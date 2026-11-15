<!-- doc-class: normative -->
# Web M1+M2 rework on today's master — design + verification (feat/web-m2-rework)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch `feat/web-m2-rework` (worktree `~/workspace/gopi-web-m2-rework`), off
master `ba53da3d`. Scope: B1, B2, B3, B5, B8, B9, B12 re-developed on today's
tree, with the archived reference `feat/web-m2-b5b8b9b12` read as GUIDANCE only
(no merge/rebase/commit on it — Rule 23).

## 1. Why a rework was needed at all (what master actually had)

Master was NOT missing all of M1/M2. Verified before writing any code:

| increment | state on master `ba53da3d` | action |
|---|---|---|
| B1 reset | **present** — `handleChatReset` + `webChatResetPath`, `webResetBodyMessage` | kept, re-verified |
| B2 queued frame | **present** — `webEventQueued`, `webBusyWaitNotice` | kept, re-verified |
| B3 follow threshold | **present** — `webFollowThreshold`, page `FOLLOW_THRESHOLD` | kept, re-verified + page affordance |
| B4 / B6 / B7 / B10 / B11 | **present** — `cmd/agent/cli_web_m1_test.go` is byte-identical to the reference branch's copy | untouched |
| B5 transcript fidelity | **missing** — `/api/transcript` marshalled `deepseek.Message` raw, whose `ToolCall` fields are all `json:"-"` | **built** |
| B8 markdown | **missing** — the page wrote deltas with `textContent` only | **built** |
| B9 input-trigger menus | **missing** — no trigger detection, no candidate endpoint | **built** |
| B12 per-turn timeout | **missing** — `handleChat` never set a turn budget | **built** |
| B12 visible turn queue | **missing** — the page disabled the composer while busy, so a mid-turn message was lost | **built** |

Master's `cli_web.go` and the reference tip also DIFFER substantially (949/234
lines) because master gained `feat/web-local-trust` (token gate, 31 sites) in
branches the reference never contained. That is why this is a rework, not a
cherry-pick.

## 2. Design decisions (and where they deviate from the reference)

- **B5 digest shared, not duplicated.** The reference kept a summary helper
  unexported. Here the digest is a WIRE value: `agent.ToolCallSummary` and
  `agent.ToolResultDigest` live in `internal/agent/chat_ops.go` and the live
  `event: tool` frames call them too, so a replayed row and a streamed card
  cannot drift. `ToolResultDigest` also skips the harness's
  `[tool_use id: …]` header line — otherwise every replayed digest would render
  harness metadata instead of the tool's output (a defect the reference's
  first-line rule had).
- **B9 candidates come from the EXISTING shared table.** `detectSlashMenu` reads
  `slashCommandNames()` — the same registry `gopi chat` completes from — so a
  command added to the table appears in the browser menu with no second edit.
  The reference carried its own list.
- **B9 bounds its walk three ways** (entries, depth, skip-dirs) and returns
  every match the bounded walk saw. The caller caps what it SHIPS
  (`capMenuList`) while reporting an honest `total`, which is what makes the
  page's "+more" flag truthful; a walk that stopped at the first 8 matches
  could never know more existed.
- **B12 budget is applied, not merely parsed.** `webTurnTimeout()` is the ONE
  env reader (Rule 2) and `handleChat` sets `ag.Timeout` for the turn, restoring
  it in a defer. A knob read from the environment but never reaching the turn
  context would bound nothing (Rule 21's dead-knob class).
- **B12 steering sentinel stripped server-side.** The page prefixes an
  Alt+Enter message with `!steer `; `handleChat` strips it BEFORE routing, and
  a sentinel-only message is refused as empty — so carrier framing can never
  become content the model sees.
- **B12 queue is strictly sequential.** The page delivers queued entries one at
  a time, each as its own turn; no second agent, no second lock, so the
  single-session invariants hold by construction.
- **B8 renders through `textContent` only.** `markdownNodes` is a pure
  tokenizer (testable without a DOM); the renderer builds text nodes. The page's
  only two `innerHTML` uses are the deliberate clears. An over-long fence body
  is deliberately NOT formatted (a bounded token cap) so pathological input
  stays inert text.
- **The M1 test's B1 assertion was re-pointed at BEHAVIOUR** (`clearNode(logEl)`
  *or* the old direct clear) instead of one spelling: the rework routes the
  clear through the shared helper, and a test that pins a spelling rather than
  the behaviour blocks a correct refactor.

## 3. Per-increment verification

### B1 — conversation reset
- Server: live `POST /api/chat {"message":"/clear"}` →
  `event: command` `{"cleared":14,…,"output":"conversation reset — …"}` and
  `/api/transcript` → `{"messages":[]}`.
- Page: `RESET_NOTICE` must equal the server's `webResetBodyMessage` verbatim
  (`TestWebM2B_ResetAckAndQueuedNoticeAreSingleSourced`); the harness scenario
  drives the command frame and asserts `#log` is emptied.

### B2 — queued-turn feedback
- Live: two concurrent POSTs → the second stream began with
  `event: queued {"notice":"waiting for the in-flight turn to finish…","placement":"queued"}`.
- Page: the waiting bubble appears on the frame and is taken down once the turn
  starts (`cli_web_page_scenarios.js`).

### B3 — scroll pinning
- Page: follow-bottom is conditional on the named `FOLLOW_THRESHOLD` (24, the
  same value `webFollowThreshold` holds server-side); scrolling up suppresses
  auto-scroll and reveals `#jump`; clicking it returns to the bottom and hides
  itself. All driven in the harness.

### B5 — transcript fidelity
- Server: `TestWebM2B5_TranscriptCarriesToolNameAndSummaryForReplay` drives a
  real tool turn through the mock model and asserts the transcript carries the
  tool NAME, the call digest (equal to the live frame's summary, from one
  definition) and the result digest — and that the digest is the tool's payload,
  never the harness header. Minimal-sample checks cover a stamped result, an
  unstamped one, an empty one, and the rune cap on CJK text.
- Live: the transcript rows above.
- Page: the harness replays a transcript and asserts the tool card is rebuilt
  and collapsed.

### B8 — markdown, safely
- Harness: fenced block → `pre.mdpre`, inline span → `code.mdcode`, an inert
  `<script>` stays TEXT (no element created), an unterminated fence stays text,
  a body past `MD_MAX_FENCE_LINES` stays text, and `doc.markupWrites` is empty
  (no `innerHTML` interpolation anywhere on the render path).

### B9 — input-trigger menus
- Server: `TestWebM2B9_MenuEndpointServesTheSharedTable` (candidates ARE the
  shared table; prefix filter; non-matching prefix falls back to the whole
  table; one budget; 405 on a wrong method; unknown trigger = empty) and
  `TestWebM2B9_MenuHonoursItsOneBudgetAndItsWalkRoot` (walks
  `GOPI_CODE_SEARCH_ROOT`; skips `node_modules`/`.git`; basename vs path
  fragment; honest `total` above the shipped cap).
- Page: the pure detect core (start-of-text `/`, whitespace-introduced `@`,
  caret inside the first word, no menu inside a fence), the endpoint fetch,
  arrow navigation with wrap, **Enter completes and does NOT send**, Escape
  closes, and Enter sends only when the menu is closed.

### B12 — per-turn timeout + visible turn queue
- Server: `TestWebM2B12_PerTurnTimeoutAppliedFromOneEnvReadSite` pins ONE env
  reader, the resolver's contract (override wins; malformed/zero/negative fall
  back), the budget reaching the agent DURING the turn, and the pre-turn value
  being restored after it. `TestWebM2B12_PerTurnBudgetRestoresAndStopsCleanly`
  proves the restore AND that a turn against a dead upstream still emits a
  terminal frame (otherwise the page's busy gate would wedge).
- **RED/GREEN pair** (both runs in §5).
- Page: a mid-turn message becomes a VISIBLE `queued` entry with its placement
  badge, is delivered as its own turn (strictly sequential), the composer and
  Send stay usable, Alt+Enter posts the sentinel and is typed `steering`.

### Roadmap §5 invariants
`TestWebM2_InvariantsSingleSessionAndSerializedTurns` runs 5 concurrent turns
and measures (a) one session id, one JSONL, one raw log with 5/5 user and 5/5
assistant events, and (b) an atomic in-flight counter never above 1 — the
per-turn mutex actually serializes. Complementary live evidence: the run's
session dir held `session_start: 1` with 4 user / 9 assistant / 7 request
events while turns were issued concurrently.

## 4. Verification commands

```
make build                                   # OK
go vet ./...                                 # OK (no output)
gofmt -l cmd/agent/cli_web.go cmd/agent/cli_web_m2_test.go \
         cmd/agent/cli_web_m2b_test.go cmd/agent/cli_web_m1_test.go \
         internal/agent/chat_ops.go          # clean
go test ./cmd/agent/ -run 'TestWebM2|TestWebM1' -count=1     # ok
node cmd/agent/cli_web_page_harness.js                       # PASS: 95/95
```

Full-suite baseline/delta (the touched packages plus the known-red set) is in
`tasks/web-m2-rework-report.md`.
