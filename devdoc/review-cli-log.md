<!-- doc-class: historical -->
# Code Review: `gopi log` subcommand

Branch `review/cli-log` · Base `783ab6d` · Review date 2026-09-02
Scope: `cmd/agent/cli_log.go` (684 lines) + engine `internal/gopilog/`
(reader.go 501, rawlog.go 461, formatter.go 1118, types.go 141, tests ~1410).
This is a **docs-only review** — no code changed.

## 0. Executive summary

`gopi log` is a well-designed CLI surface: the engine (`internal/gopilog`) is
cleanly separated from the dispatcher (`cmd/agent/cli_log.go`), the JSONL
schema is defined **once** on the writer side (`internal/agent/session.go`)
and mirrored by one reader struct (`RawEvent`, types.go) — not re-parsed per
view; the tool-family shortcut design (a registry map + one-line additions
per agent toolcall) is a genuine single source of truth that has scaled
across 13 feature commits with zero duplication of the family list. Named
constants for all scanner buffers / size caps (Rule 1) are exemplary.

Findings cluster into three themes:

1. **Byte-vs-rune truncation bugs** (P1) — three display truncations slice
   UTF-8 mid-rune producing U+FFFD replacement chars in output. Proven with
   synthetic CJK sessions.
2. **A bare prefix-match hazard** (P1) — `FindSession` prefix match
   (short-ID support) silently selects an arbitrary file when the prefix is
   not unique (13 real sessions share 4-char prefixes). Live `--last` output
   also shows truncated session IDs in the summary line.
3. **Inconsistent corruption/partial-session handling** (P1) — one corrupt
   file kills `--last N --view`, is skipped gracefully in the summary path;
   `--last` ordering is mtime-based (flip-flops while sessions are being
   written); `--json` prints the raw file even when it is empty / lacks a
   session_start.

Strengths worth preserving: `toolFamilies` registry, `renderSessionView`
sharing runRead's dispatch (no view duplication), timestamp-pairing + exact
`rawLogPath` resolution for raw logs, context_trim regex anchoring, the
balanced-prefix task-list recovery.

---

## 1. View-by-view findings

### `--chat` — human-friendly conversation view
- **Design purpose**: full transcript grouped by iteration (user / thinking /
  tools / response + cost summary).
- **Verified**: faithful to help. Iterations numbered from the `request`
  event's `iteration` field. Search-family tools get a compact query + first
  line summary via `searchToolQuery` (fallback `%v` of Args — correct).
- **Issues**:
  - Thinking blocks truncated at 500 bytes (`combined[:500]`) — **byte cut,
    mid-rune on CJK** (P1, same class as §7).
  - `write` renders `→ N bytes written` from the **input** content length,
    not the tool_result; `edit` shows `→ N bytes result`. Minor: inconsistent
    with `read` which shows result length. Cosmetic (P2).
  - Iteration 0 renders as `── Iteration 0 (11,520 hit / 918 miss) ──` with
    empty USER line (the tool_result-only iteration the agent logs before
    the first numbered request). Cosmetic (P2).

### `--todo` — task-list status overview
- **Design purpose**: render `[TASK_LIST_BEGIN]...[TASK_LIST_END]` snapshots
  (all of them, showing evolution).
- **Verified**: `extractAllTaskLists` scans ALL text blocks for ALL blocks;
  balanced-prefix recovery for BEGIN-without-END (P0 port of the agent-side
  fix, devdoc `tasklist-p0-evaluation.md`) is present and first-class.
- **Issues**:
  - Marker constants **duplicated** between `internal/agent/tasklist.go`
    (exported `TaskListBeginMarker`) and `internal/gopilog/formatter.go`
    (private `taskListBeginMarker`) — Rule 2 violation, same strings in two
    places (P2). The comment even documents the copy (`"must match
    GoPi's tasklist.go"`).
  - Status counting: `done`/`prog` treats every non-`"completed"` status as
    progressing — with no statuses counted as progressing (ok for the
    current 2-status world, brittle if a third status appears). P2.
  - Iteration numbering: iteration 0 snapshots render without the iteration
    suffix (`TASK LIST #N` vs `#N — Iteration M`) — inconsistent line
    format between the two cases (P2, cosmetic).

### `--response` / `--thinking` — text / reasoning blocks
- **Verified**: faithful. `--text` mode emits the raw block(s) pipe-friendly
  (no headers). Response works (12 RESPONSE lines on a live session).
- **Issues**: none functional. Note `--response --json` resolves to `--json`
  (raw passthrough) — `--json` wins over response/thinking (silent
  precedence, documented nowhere in help). P2 (document or reject).

### `--request` / `--request --json` — system prompt / full first request
- **Verified**: pairs session → raw log (exact `rawLogPath` first, then
  timestamp pairing), extracts FIRST `→ REQUEST` marker's body
  (whole-line marker match — correct, prevents marker-in-body false hits).
  Handles modern (`system` array) and legacy (`messages[0]` role=system)
  shapes. `--request --json` emits the **verbatim** request body indented —
  key order preserved (verified: `max_tokens` first, matches raw body).
- **Issues**:
  - `--request` on a session whose raw log predates the `rawLogPath` field
    and whose timestamp misses (>2 min) fails with an explicit, accurate
    message (verified: `27h29m away, more than 2m0s`). Good error handling.
  - `maxRawLogMatchDelta = 2 * time.Minute` is a named constant with a
    comment citing verification across 190 sessions — exemplary Rule 1/2.
  - ParseFirstRequest skips blank lines after the marker but NOT stray
    non-JSON lines; a corrupt raw log yields a clear error. OK.

### `--raw` — verbatim raw HTTP log
- **Verified**: byte-exact passthrough via `io.Copy`, capped at
  `maxRawViewBytes` (64 MiB named constant) with an explicit truncation
  marker naming the file. Resolution: exact `rawLogPath` then timestamp
  pairing (memory #875 fix — custom GOPI_RAW_LOG_DIR no longer pairs
  wrongly). `--raw --json` = same verbatim output (precedence documented in
  code comment; `--json` passthrough bypassed first). Faithful.
- **Issues**:
  - The marker strings `→ REQUEST` etc. are defined in `internal/gopilog/
    rawlog.go` but **written** as literals in `internal/deepseek/
    claude_client.go` (`c.RawLog("→ REQUEST\n", reqBody)` at 8 sites, plus
    `(1214-guard)` / `(1214-guard, stream)` variants). The reader's
    whole-line match means the guard-suffixed markers are simply never
    matched as REQUEST markers — `--request`/`--raw` still work (guard
    bodies are extra lines), but the marker contract lives in two places
    with **no shared constant** and **no test asserting the reader matches
    what the writer emits**. P2 (Rule 2 — export the markers from the
    writer; add a cross-package test).
  - 64 MiB cap on `--raw` means the tail (usually what's wanted) is cut —
    the truncation note mitigates; acceptable design, noted for awareness.

### `--json` — raw JSONL passthrough
- **Verified**: byte-exact file passthrough (machine-parseable — each line
  is valid JSON; verified).
- **Issues**:
  - `--json` prints an **empty file with no output at all** and prints a
    file with no session_start (e.g. crash mid-write) verbatim — no error,
    no warning, zero exit. For a "machine-parseable" contract this is
    acceptable (garbage in = garbage out) but the human path (`default`
    view) prints an ugly `Session: (empty) Model: (empty) Started:
    0001-01-01` header instead of an error. P2 (validate at least a
    session_start presence for the human views).
  - `--json` silently beats `--response/--thinking/--chat` (dispatch order).
    P2 (document or reject).

### `--last [N]` / `--last N <view>` — session discovery
- **Verified**: `--last N` without views → N=1 full session, N>1 summaries
  (documented, sane). With view flags → applies the view to EACH of the N
  sessions (fix 6298305 — earlier bug returned only files[0]). `--last N
  --json` concatenates raw JSONL of N sessions — **not a single JSON
  document** (no separator/array wrapping) — machine-parseable only if the
  consumer splits lines and knows N sessions. P2.
- **Issues**:
  - **Ordering is mtime-based** (`ListSessionFiles` sorts by ModTime) —
    while a session is being actively written (or a stale session's file is
    touched), `--last N` flip-flops: verified live (735e66fe moved from
    #1 to #2 as its file was re-synced). Sorting by session_start timestamp
    from the JSONL would be stable. P1/P2 (P2 for correctness of "most
    recent" semantics — it is *usually* right; P1 for the flip-flop).
  - **One corrupt file aborts the whole `--last N --view` run** (os.Exit(1)
    in renderSessionView) while the summary path (`ParseFile` error →
    `continue`) skips gracefully with exit 0 — asymmetric. Verified:
    `--last 2000 --memory` exits 1 on a single corrupt file. P1.
  - `--last` with a session-id positional is silently ignored (runLast wins)
    — `gopi log <id> --last` reads the LATEST session, not the given one.
    Surprising, undocumented. P2 (reject the combination).
  - `--all` without `--summary` falls to the `missing <session-id>` error
    with usage — confusing for a documented flag pair. P2.
  - `--summary` without `--all`/`--last` likewise. P2.

### `--all --summary` / `--last N --summary` — summary table / lines
- **Verified**: `--all --summary` loads summaries per file without full
  parsing (`LoadAllSummaries` single pass, skips malformed lines — verified
  951 rows in 2.3s). Table column alignment consistent. `--last 1 --summary`
  **renders the full session** instead of a summary (view flags beat
  summary) — `--summary` is documented only in the `--all --summary` combo;
  the `--last N --summary` split behavior is undocumented (P2, verified).
- **Issues**: summary line truncates the session ID to 8 chars
  (`shortID`) — on the live machine `--last 3` shows `735e66fe`,
  `c6b13d45`, `78d40ad1` which are **NOT unique enough to re-lookup**
  (verified: 13 sessions share 4-char prefixes; FindSession prefix match
  picks an arbitrary one). `--last` output cannot be piped back into
  `gopi log <short-id>` reliably. P1.
  - `--last N --summary --json` prints raw JSONL (json beats summary) —
  precedence again undocumented. P2.

### `--filter TOOL` / `--memory/--search/--web/--job/--context/--vision/--text2img/--git`
- **Design purpose**: family shortcuts = registry `toolFamilies`
  (cli_log.go) mirroring the agent tool inventory; `--filter` unioned in via
  `resolveFilterSet`. The registry + `familyOrder` are single source of
  truth; adding a toolcall = one line (proven: 13 commits, zero drift).
  `--context-trim` is a *view*, correctly excluded from the filter families.
- **Verified**: `--memory` renders full input via `InputRaw` (key order
  preserved, HTML-escaping off), tool_result indented. `--search`, `--git`,
  `--context` all render headers + matches. `--last N --memory` applies per
  session.
- **Issues**:
  - `memory_delete` is in the `--memory` family though it's a CLI-only
    maintenance command (agent sessions may still log it — documented,
    reasonable). OK.
  - `--filter bash` on a session with bash calls whose tool_result landed
    in a different iteration renders correctly (result matched across
    flushed iterations — verified with synthetic duplicate/late results;
    first result wins). Good.
  - **The `--filter=<tool>` form**: `collectLogFilters` handles it; the
    shared `memoryFlagSet` keeps only the last value per name, so
    repeatable `--filter` needed its own collector pass (documented
    pitfall). Works.
  - Family + `--grep` scope composition is well-designed (grep restricted to
    the family set). Verified `--grep timeout --job` semantics via code
    reading (FormatGrep scoped branch). OK.

### `--context-trim` — context_trim activity table
- **Design purpose**: audit trail of context_trim calls: call_id, target
  call, outcome (replaced/removed/noop/unknown), before/after byte sizes.
- **Verified**: table renders correctly; regexes anchored on the
  `context_trim:` outcome line (tick prefix + stamped [tool_use id] header
  immune — the anchor comment is precise); replace_with conclusion shown in
  full. Sized columns via named `contextTrimIDCol` constant. Faithful.
- **Issues**:
  - Regexes duplicate the loop's outcome phrasing (`executeTool` in
    internal/agent/loop.go) as string literals — if the loop's phrasing
    changes, the view silently reports "unknown". P2 (Rule 2: export the
    outcome strings or add a cross-package test).
  - `collectContextTrims` only inspects `s.Iterations` tool calls — a trim
    targeting a call whose tool_use event is in the same iteration works;
    verified the size comparison (BeforeBytes = full logged result length)
    on live session data. OK.

### `--grep TEXT`
- **Design purpose**: content search across tool calls / text / thinking
  (case-insensitive), scoped by family/filter flags.
- **Verified**: faithful. Header shows scoping. `--grep` on `--last N`
  applies per session (renderSessionView). Verified `--grep GOAL` matches.
- **Issues**: none functional. Note grep lowercases the whole content for
  the search (fine); matches print full tool calls — large outputs for
  common terms (by design).

### `--text`
- **Verified**: pure modifier on response/thinking/request; `--raw --text`
  accepted (no-op). No standalone meaning; correct.

---

## 2. Cross-cutting findings (prioritized)

### P0 — none found
The command is in good health; no crash / data-loss / wrong-answer class
bug surfaced. (The `FindSession` ambiguity and truncation bugs are real but
P1.)

### P1

1. **Rune-split truncation in `formatter.go`** (3 sites, verified with
   synthetic CJK sessions):
   - `FormatSession` text blocks: `text[:300]` — 100 CJK chars shown from
     250 (bytes cut), and mid-rune splits possible at byte 300.
   - `formatToolCall` bash result first line: `summary[:77]` — a first line
     whose 78th byte splits a multi-byte rune yields U+FFFD (verified:
     replacement char appears).
   - `edit` old/new string preview: `oldStr[:57]` / `newStr[:57]` —
     **verified U+FFFD** in output when the cut lands inside a CJK rune.
   Fix: rune-safe truncation helper (e.g. slice by runes or
   `strings.ToValidUTF8`), shared. Chat view thinking `combined[:500]` is
   the same class. This is a Rule-1-adjacent discipline issue: length caps
   are inlined (300/57/77/500/80) rather than named constants.

2. **`FindSession` prefix-match ambiguity** (`reader.go`): short-ID support
   matches the FIRST file whose basename has the prefix — with 13 sessions
   sharing 4-char prefixes on this machine, `gopi log 735e` can resolve to
   an arbitrary session. No ambiguity detection (compare with the raw-log
   matcher which explicitly rejects ties!). Fix: on multiple prefix matches,
   error with the candidates (mirroring `FindRawLogForSession`'s tie
   handling), or require full UUID when ambiguous.

3. **`--last N <view>` aborts on one corrupt file** (`cli_log.go`
   `renderSessionView` → `os.Exit(1)` on ParseFile error) while the
   summary path (`runLast` loop) skips and continues (exit 0). Verified:
   `--last 2000 --memory` dies on a single bad file; `--last 2000` (no
   view) survives it. Align: error+continue with a stderr warning for both
   paths.

4. **`--last` ordering is mtime-based and flips live** (`ListSessionFiles`
   sorts by file ModTime). Verified: a session whose file is re-synced
   while another is written changes `--last 3` order between two runs
   seconds apart. Sort by session_start timestamp read from the JSONL
   (already read for summaries) or by filename when timestamps tie.

### P2

5. **Duplicated marker/outcome constants** (Rule 2):
   - `[TASK_LIST_BEGIN]/[TASK_LIST_END]` duplicated in `gopilog/formatter.go`
     vs exported `agent.TaskListBeginMarker`.
   - Raw markers `→ REQUEST/← CHUNK/← RESPONSE` written as literals in
     `deepseek/claude_client.go` (8+ sites, incl. `(1214-guard)` variants),
     matched as literals in `gopilog/rawlog.go`; no shared constant, no
     writer↔reader conformance test.
   - context_trim outcome strings duplicated between `loop.go` and the
     `gopilog` regexes.
   Each should be exported from the writer side with a cross-package test.

6. **Flag-precedence silent wins** (verified): `--json` beats
   `--response/--thinking/--chat/--summary`; `--last` beats a positional
   session id; `--raw --json` = raw (documented in code, not help);
   `--last 1 --summary` renders a full session. Help text doesn't state
   precedence; either document or reject conflicting combinations.

7. **Empty/no-session_start file handling**: human views print a
   `Session: (empty) Started: 0001-01-01` header with no warning; `--json`
   prints nothing for an empty file (exit 0). Validate session_start
   presence for human views and warn for `--json`.

8. **`--all` / `--summary` without their partner** falls through to
   `missing <session-id>` + usage — confusing error. Suggest explicit
   "`--summary` requires `--all` or `--last`".

9. **Byte-cut display caps should be named constants** (Rule 1): 300, 57,
   77, 80, 500, 120, 100, 60 in formatter.go — the summary-length limits
   ARE named (`summaryTaskMaxLen` etc.) but the display truncations are
   inlined.

10. **`--last N --json` / `--last N --summary --json`** concatenates N raw
    JSONL streams without a framing boundary — consumers must know N.
    Consider a single JSON array or a documented line-stream contract.

11. **Chat view** `write`/`edit` byte counts come from input args, not the
    tool_result — cosmetic inconsistency with `read`.

12. **Iteration-0 display** (`Iteration 0 (… hit / … miss)` with empty USER)
    — cosmetic; skip or label as tool-result round.

13. **`--todo` status counting** treats missing status as progressing;
    iteration-0 snapshots render without the iteration suffix.

## 3. Architecture notes (strengths)

- **Schema single source of truth**: the JSONL schema is written in ONE
  place (`internal/agent/session.go` — session_start/user/assistant/
  tool_use/tool_result/request/session_end/crash_dump), read by ONE struct
  (`RawEvent`, types.go) that all views share. Views never re-parse JSONL;
  `ParseFile` is the single parser (trajectory.go has its own minimal
  `sessionEvent` for telemetry — acceptable, different concern).
- **No view duplication**: `renderSessionView` shares runRead's dispatch
  order, so `--last N <view>` can't drift from `<id> <view>`. Verified the
  two switch blocks are in the same order.
- **Tool-family registry** (`toolFamilies` + `familyOrder`) is the model for
  extensibility: 13 feature commits added families with one-line changes
  and no duplication (blame shows each family added by its own commit).
- **Error handling on corrupt raw/session pairing is exemplary**: the
  timestamp matcher rejects ties and >2-min gaps with precise messages
  (verified on a real stale session).
- **Rule 1 discipline**: all scanner buffers (1 MiB/10 MiB/64 MiB), the raw
  view cap, the match delta, and column widths are named constants with
  justification comments — including a comment citing the 190-session
  verification behind `maxRawLogMatchDelta`.
- Tests: 78 tests in internal/gopilog (formatter 32, rawlog 25, reader 9,
  todoitem 9, context-trim 3) — all pass; `go vet` clean.

## 4. git history / feature accumulation

13 commits touching cli_log.go since integration (23ebc22, 2026-08-25):
- 23ebc22 integrate gopi-log-reader (441 lines)
- 41c8619 `--memory` full family → 6298305 `--last N <view>` fix →
  284393a `--context-trim` view → 209f97f family shortcuts + `--grep`
  (582) → dc2d3a4 `--trim` (AB arm) → d3d0656 `--raw` (664) → 6fb876f
  `--vision` → f6c6be6 `--text2img` → b6f6cea `--git` → 2dabbfb gofmt
  (684).
- **Design reversal observed**: `--trim` (dc2d3a4, AB-arm branch) vs
  `--context-trim` (284393a, master) were two competing implementations of
  the same view; merge de47a8e kept master's `--context-trim` and dropped
  the branch's `trim_activity.go`. Resolution was correct (master's naming
  matches the tool name `context_trim`) — but this is exactly the
  "designs varying back and forth" pattern the epic warns about: the same
  feature was implemented twice on sibling branches.
- No dead views/flags remain: every `logFlagMember` flag is dispatched and
  tested (verified the flag list against dispatch cases).
- The `--context` family flag (context_trim filter) coexists with the
  `--context-trim` view — both live, distinct, documented. Fine, but a
  naming trap for users (`--context` vs `--context-trim`).

## 5. Verification notes

- `go build ./...` OK; `go vet ./internal/gopilog/ ./cmd/agent/` clean;
  `go test ./internal/gopilog/` OK (0.34s, 78 tests).
- Every view exercised against live sessions (735e66fe…, 78d40ad1…) and
  synthetic sessions (CJK truncation, corrupt files, duplicate/late
  tool_results, missing session_start).
- Summary table over 1175 sessions: 2.3s.
- Real-session anomalies consistent with findings: response/thinking empty
  on a `echo hello` session (no assistant events logged — the agent's
  LogText/LogThinking are skipped when there is no text); partial-session
  COST shows `Iterations: 0` (no session_end yet) while iterations render.

## 6. Suggested fix order

1. (P1) Rune-safe truncation helper + replace the 5 byte-cut sites.
2. (P1) FindSession ambiguity → error with candidates.
3. (P1) renderSessionView error path → continue (align with summary path).
4. (P1) ListSessionFiles sort by session_start timestamp.
5. (P2) Export marker/outcome constants from writers + conformance tests.
6. (P2) Document/reject flag-precedence combos; fix `--all`/`--summary`
   misuse errors; validate session_start for human views.
