<!-- doc-class: historical -->
# Second-Opinion Review: `gopi log` subcommand (GLM / HP node, 2026-09-02)

Independent re-review of the `gopi log` CLI surface. Fresh pass — the earlier
peerbox report (`devdoc/review-cli-log.md`, branch `review/cli-log`) was NOT
consulted; all findings below were re-derived from source reading, git history,
live CLI runs on 997 real sessions, and synthetic JSONL session fixtures in an
isolated `GOPI_HOME`.

Scope: `cmd/agent/cli_log.go` (700 lines), `internal/gopilog/` (reader.go,
types.go, formatter.go, rawlog.go — 5,619 lines incl. tests), and the session
log *writer* `internal/agent/session.go` + raw-log writer `internal/agent/rawlog.go`.

Method: `gopi log -h` enumeration → source walk (dispatch → each view handler →
engine) → per-view fidelity testing (live + synthetic corrupt/partial/CJK logs
via isolated `GOPI_HOME=/tmp/logrev2`) → GOPI.md/devdoc cross-check → git
log/blame feature-accumulation audit → regression test run.

Test state at review time: `go test ./internal/gopilog/` PASS (78 tests);
`go test ./cmd/agent/ -run TestLog` PASS; `go vet` clean. All CLI evidence from
a fresh `go build -o bin/gopi ./cmd/agent/` in the review worktree.

---

## Architecture summary

The surface is cleanly split:

| Layer | Files | Responsibility |
|---|---|---|
| CLI dispatch | `cmd/agent/cli_log.go` | arg parsing (`parseLogArgs`, hand-rolled `memoryFlagSet`), view routing, session discovery |
| Engine | `internal/gopilog/reader.go` | `ParseFile`, `FindSession`, `ListSessionFiles`, `LoadAllSummaries`, context_trim extraction |
| Engine | `internal/gopilog/types.go` | `RawEvent` — the single JSONL line schema the reader knows |
| Engine | `internal/gopilog/formatter.go` | All human views (session/chat/todo/response/thinking/filter/grep/context-trim/summary) |
| Engine | `internal/gopilog/rawlog.go` | raw HTTP log pairing + `--request`/`--raw` backing |
| Writer | `internal/agent/session.go` | JSONL session logger (schema *author*) |
| Writer | `internal/agent/rawlog.go` | raw HTTP log writer (marker *author*) |

**Good design properties (verified, worth preserving):**
- No per-view JSONL re-parsing: every view consumes the single parsed `Session`
  produced once by `ParseFile` (formatter.go views are pure `Session → string`).
  The only raw-file views (`--json`, `--request`, `--raw`) read the file
  directly and are correctly routed *before* `ParseFile` in `runRead`.
- `toolFamilies` registry (cli_log.go:244-256) is a single source of truth for
  family→tool-name expansion, with `familyOrder` keeping display order stable.
- `truncateRunes` rune-safe helper + named display caps (formatter.go:32-63)
  after the P1 rune fix — discipline is good *except* one missed site (P1-1).
- Raw-marker matching is whole-line anchored and raw log pairing has an
  explicit ambiguity rejection (rawlog.go:171-183) — no silent wrong pairing.
- `renderSessionView` shares dispatch with `runRead` for `--last N <view>` —
  no duplicated view logic at the CLI layer.
- Constants are named and commented throughout (Rule 1 largely respected in
  gopilog; the writer files do the same).

---

## Per-view findings

### 1. Default session view (`gopi log <id>`) — *faithful*
Design purpose: full transcript grouped into API iterations with cost/exit
summary. Renders `Session/Model/Started/Workdir`, TASK, per-iteration tool
calls with elapsed, COST, EXIT. Text blocks capped at `formatTextMaxLen`=300
runes (formatter.go:105). Consistent, matches help ("Read specific session").

Issues:
- **P2 — no-session_start / empty files render a garbage header with exit 0**
  (default human path). Verified: empty file and a file lacking `session_start`
  both print `Session:` (blank) / `Model:` / `Started: 0001-01-01 00:00:00`
  and exit 0. `ValidateSessionFile` (formatter.go:877-909) exists but is only
  called by `FormatJSONL` (the `--json` path) — the human views never validate.
  `--json` on the same files correctly errors (exit 1). Asymmetric: a leftover
  empty file (`dddd4444…`) or crash-mid-write file is silently shown as a
  "session from year 1". Suggest routing all views through `ValidateSessionFile`
  (or having `ParseFile` detect missing `session_start`).
- Partial/crash session (has `session_start`, no `session_end`) renders fine
  with exit 0 but gives **no orphan/“no session_end” indicator** — a reader
  cannot tell a still-running session from a crashed one. P2: one line
  `(no session_end — crash or still running)` would make `--last` triage honest.

### 2. `--chat` — *faithful, one residual rune bug*
Design purpose: human conversation transcript (USER/THINKING/TOOLS/RESPONSE per
iteration + cost summary). Faithful to help.

Issues:
- **P1-1 — `formatChatToolCall` search-family preview still byte-slices**
  (formatter.go:953-956): `first := strings.SplitN(tc.Result, "\n", 2)[0]`,
  `if len(first) > 120 { first = first[:117] + "..." }`. `len()` is bytes and
  `[:117]` slices mid-rune on a CJK-heavy first result line → emits `U+FFFD`
  replacement char. **Verified**: synthetic session with a 200-CJK-char first
  result line renders `…中中中�...` (one U+FFFD) in `gopi log <id> --chat`.
  This is the exact bug class the P1 batch (8f9e9b4) claims to have fixed —
  its commit message lists "5 byte-cut display sites (text[:300], summary[:77],
  oldStr[:57]/newStr[:57], combined[:500])" and formatter.go:34's comment
  echoes it, but `first[:117]` at :956 was missed. Magic literals 120/117
  violate Rule 1 in the very file that now has named constants for this.
  Fix: `truncateRunes(first, chatPreviewMaxLen)` with a named const, plus a
  regression test on a CJK first line.
- Iteration numbering: a session whose first content precedes any `request`
  event shows "Iteration 0" (chat) / "iteration 1" is only stamped from the
  `request` event's `Iteration` field. Cosmetic, matches the log format.

### 3. `--todo` — *faithful + resilient*
Design purpose: task-list status overview across ALL snapshots
(`extractAllTaskLists`, not just the last — the deprecated single-list path was
superseded). Faithful. Notably well-engineered:
- BEGIN-without-END recovery ported from agent `ParseTaskListBalancedPrefix`
  (formatter.go:1057-1075) — a P0 silent-drop fix with a real-session citation.
- Task markers/parse now alias `agent.TaskItem` (single source of truth) after
  the cross-package duplication merge (043cd07) — good Rule 2 hygiene.
- `[TASK_LIST_BEGIN]`/`[TASK_LIST_END]`/`completed` string constants
  (formatter.go:984-987) still duplicated with tasklist.go — see P2-3.

### 4. `--response` / `--thinking` / `--text` — *faithful*
Response/thinking views iterate `TextBlocks`/`ThinkingBlocks`; `--text` strips
headers for piping. Faithful to help. CJK-safe via `truncateRunes` where capped
(thinking in chat). No issues beyond the shared iteration-number cosmetic.

### 5. Tool-family filter views (`--memory/--search/--web/--job/--context/--vision/--text2img/--git` + `--filter`)
Design purpose: per-family tool-call audit with full input/result.
`--memory` also expands to `memory_delete` (CLI-only tool, correct call — the
help comment documents why). Header lists the resolved filter set
(`resolveFilterSet`). Empty state is a helpful tool-name hint. Faithful.

Issues:
- **P2 — help does not document that the family flags are *filter* views that
  share one switch with `--chat/--todo/...`, nor the precedence when combined.**
  Help lists `--memory`/`--search`/… under "Tool family shortcuts" and says
  "Only memory tool calls", which reads like a filter on the default view —
  true — but combining e.g. `--chat --memory` silently renders the *filter*
  view (dispatch order in runRead: chat → todo → contextTrim → response →
  thinking → else family via runFilter… actually family filters route through
  the earlier `filterActive` branch in `runLogSubcommand`, so `--chat --memory`
  renders the memory filter, ignoring `--chat`). Combining view flags with
  filter flags should either be rejected or documented.
- `--filter` accepts arbitrary tool names (no validation against the tool
  inventory) — by design (forward-compat), the empty state's hint compensates.

### 6. `--grep` — *one contract deviation*
Design purpose: content search across a session, scopeable by family flags.
Faithful to help *except*:
- **P2 — matches tool *names*, not just content** (formatter.go:464):
  `strings.Contains(strings.ToLower(tc.Name), q)`. Help says "Only entries
  whose content contains TEXT". `gopi log <id> --grep read` therefore matches
  every `read` tool call whose *name* contains "read" even when neither args
  nor result mention it. Name matching is arguably a feature (find the tool
  you meant) but it violates the documented contract and makes result counts
  surprising. Either drop the name match or document it in help.

### 7. `--context-trim` — *faithful, one dead case*
Design purpose: context_trim audit table (call id, target, outcome class,
before/after bytes). Rich extraction in reader.go:262-337 (`collectContextTrims`)
with outcome regexes anchored mid-string (survives `[t+NNNs]` and
`[tool_use id: …]` prefixes), byte-accurate before/after, and an "unknown"
fallback outcome so older binaries' phrasing is not silently dropped. Good.

Issues:
- **P2 — dead `case opts.contextTrim` in `runRead`** (cli_log.go:498).
  `runLogSubcommand` dispatch (cli_log.go:336) routes `--context-trim` to
  `runContextTrim` before `runRead` is ever reached, so runRead's case is
  unreachable (a copy-paste leftover). Harmless today, but it is exactly the
  kind of duplicated-branch drift the review epic worries about — remove it or
  add a comment. (The `renderSessionView` case at :627 IS reachable via
  `--last N --context-trim` and must stay.)

### 8. `--request` / `--request --json` / `--raw` — *faithful*
Design purpose: expose the session's authoritative system prompt / full first
request body from the paired raw HTTP log; `--raw` dumps the raw log verbatim.
Correctly routed ahead of `--json` in runRead (cli_log.go:424-447) so
`--request --json` = "full request body as JSON" and `--raw --json` = "verbatim
raw log" (documented in the comments). Pairing prefers the authoritative
`rawLogPath` recorded in `session_start` (rawlog.go:231-245, resume-era), falls
back to timestamp proximity with explicit ambiguity/tolerance errors
(rawlog.go:166-183). Raw view capped at `maxRawViewBytes`=64MiB with an
explicit truncation marker. Good error messages when the raw logger was off.
Verbatim passthrough verified byte-exact on live raw logs.

Issues:
- **P2 — `--request` on a session whose raw log is missing/before the field
  errors with "no raw logs found … (the raw HTTP logger may not be enabled)"
  even when the *session* log is fine** — correct but cold; a pointer to
  `gopi log <id> --raw`/GOPI_RAW_LOG_DIR would help (minor).
- `--request` output is not JSON-validated for machine use except `--json` —
  fine, text is the default contract.

### 9. `--json` — *machine-parseable, one corruption gap*
Design purpose: raw JSONL passthrough for piping. `--last N --json` verified to
emit a clean concatenated JSONL stream (blank separators suppressed) — every
line parses. Empty/no-session_start files are rejected with clear errors
(exit 1). Good.

Issues:
- **P1-2 — corrupt mid-file JSONL passes `ValidateSessionFile` and prints
  garbage with exit 0.** `ValidateSessionFile` only checks (a) non-empty and
  (b) presence of *some* `session_start` line (formatter.go:877-909); it never
  parses the remaining lines. Verified: `--json` on a file whose second line is
  `{bad json` prints the raw bytes verbatim (including the corrupt line) and
  exits 0 — the "machine-parseable" contract is broken (a downstream JSONL
  consumer chokes). The human default view correctly errors (exit 1, parse
  error). Suggest: make `ValidateSessionFile` (or `--json`) full-line-parse and
  error out naming the first bad line, matching the human path's exit code.
  P2-adjacent: the *human* default view gives a raw parse error with no line
  number — a partial line-number hint would help crash forensics.

### 10. Session discovery: `--last [N]`, `--all --summary`, short-ID, listing
Design purpose: recency-based discovery + summary table.
- `ListSessionFiles` sorts by `session_start` timestamp (reader.go:128-155),
  filename tiebreak — stable across writes (mtime flip-flop fixed in 8f9e9b4;
  comment documents the review provenance). Good.
- `FindSession` exact-then-prefix with **ambiguity rejection** (reader.go:
  196-235): a shared prefix errors listing all candidates. Good (verified the
  error format on a synthetic shared-prefix pair).
- `--last N` honors view flags across each of N sessions (6298305 fix +
  renderSessionView) — verified renders N distinct sessions.
- **P2 — `--last N` parse-failure asymmetry remains**: with one corrupt file in
  the last N, the summary path (`runLast` n>1) and the view path both print the
  error to stderr and `continue`, but exit code stays 0 — the command reports
  success while silently dropping sessions. Verified: `--last 3` with a corrupt
  middle file printed 2 summaries + stderr error, exit 0. The 8f9e9b4 fix made
  the loop resilient but the exit code still lies. A corrupt tail should exit
  non-zero (or print a summary-line "(1 session unreadable)").
- **P2 — `--all` / `--summary` used alone print "missing <session-id> argument"
  + full usage** (cli_log.go:348-353) — misleading: the only valid form is
  `--all --summary`. Suggest an explicit "`--all` requires `--summary`" error.
  (The error is not wrong for other combos, but for these two flags it sends
  the user down the wrong path.)
- **P2 — view precedence is silent.** `--chat --todo` renders chat;
  `--response --json` renders JSON; `--memory --chat` renders the memory
  filter — each is deterministic (runRead dispatch order + runLogSubcommand
  pre-routing) but undocumented. Conflicting view flags should error or the
  precedence should be in the help (one line).

### 11. Error handling on corrupt/partial logs — *mostly good*
Single-session corrupt file: clear "Error parsing … JSON parse error" exit 1.
`--json` gap = P1-2. `--last N` silent-success = P2-9. Empty-file human gap =
P2-1. Raw-log pairing errors are the best in the file (ambiguity + tolerance +
cause hint).

---

## Cross-cutting: Rule 2 (single source of truth) findings

- **Good**: `RawEvent` (types.go) is the one reader-side schema; views share
  one `Session`. Task items aliased to `agent.TaskItem` after the duplication
  merge. `config.GopiDir` routes all 8 data-root call sites (cdab7fa).
- **P2 — raw-log marker strings duplicated across four packages**
  (`internal/agent/resume.go:21-23`, `internal/gopilog/rawlog.go:25-27` —
  identical names+values; `internal/deepseek/client.go:45,47` embeds the
  direction strings in comments/doc; `internal/checkpoint/parse.go` has its own
  `dirRequest = "→ REQUEST\n"` and the writer `internal/agent/rawlog.go` has
  *no* constants at all — the direction strings come from callers
  (`deepseek.Client.RawLog` callbacks in loop.go / resume.go pass literals).
  A format change (e.g. adding a timestamp prefix) silently desynchronizes the
  writer, the readers, and the checkpoint parser. Markers should live in ONE
  exported package (e.g. `internal/rawlog` or exported from `gopilog`) that
  writer + readers + checkpoint import.
- **P2 — task-list markers duplicated** (`[TASK_LIST_BEGIN]/[TASK_LIST_END]`,
  formatter.go:984-986 vs agent tasklist.go) — same cross-package drift risk
  the flexibleString merge already fixed for TaskItem. Export from one place.
- **P2 — `contextTrim` outcome regexes** (reader.go:283-288) are the *third*
  copy of the context_trim result phrasing (agent loop writes it, memory hit
  #1232 noted the claude_client.go 8-site literals). Anchored-match comments
  mitigate, but a shared exported constant for the three outcome shapes would
  be the real single source.
- **Schema author ≠ reader**: `internal/agent/session.go` writes events as
  ad-hoc `map[string]interface{}` literals; the reader models them in
  `RawEvent`. There is no compile-time check that writer fields (`sessionId`,
  `rawLogPath`, `promptId`, `exit_code`, `elapsed_sec`, …) match the reader's
  json tags — typos/drift surface only at runtime. The `session_end` "error"
  block (session.go:166-172) has **no field in RawEvent** — the reader drops it
  silently (a failed session's crash-dump pointer, category, http_status are
  invisible to every `gopi log` view). P2: add the error block to RawEvent and
  surface it (even one EXIT/error line) in the default + summary views — this
  is the crash-dump epic's own data, currently unreadable through `gopi log`.

## History / design-reversal audit (git log + blame on cli_log.go, gopilog/*)

Feature accumulation is linear and mostly monotonic (23ebc22 integrate →
41c8619 memory family → 209f97f family shortcuts+grep → 284393a context-trim →
6fb876f vision → f6c6be6 text2img → b6f6cea git → 8f9e9b4 P1 batch). Two
genuine reversals/duplications visible in the log:
1. **`--context-trim` vs `--trim` (284393a vs dc2d3a4)** — two parallel
   implementations of the same view (AB arm vs master), merged with master's
   version kept (de47a8e "keep master's context-trim view"). The `--trim` arm
   is gone from the log but the commit history shows the design back-and-forth
   the epic warns about. No live dead code remains from it.
2. **ListSessionFiles sort reversal**: mtime ordering → session_start ordering
   (8f9e9b4) after a P1 review finding. Now correct, comment documents why.
3. `extractTaskList` (last-list only) is deprecated but still present alongside
   `extractAllTaskLists` (formatter.go:1090-1096) — dead-ish backward-compat
   API; harmless but removable.

No P0. The surface is in good shape: shared dispatch, no per-view reparsing,
good error discipline in raw pairing, rune-safe truncation (one miss), stable
ordering, ambiguity-rejecting discovery.

---

## Prioritized findings

### P0
None.

### P1
1. **`formatChatToolCall` search preview byte-slices CJK → U+FFFD**
   (`internal/gopilog/formatter.go:953-956`). `first[:117]` + `len(first)>120`
   are byte-based; verified U+FFFD on a CJK first result line in `--chat`. The
   P1 rune-fix batch (8f9e9b4) fixed 5 sites and its own comment (formatter.go:
   34) lists them, but missed this one — the fix is incomplete for the class it
   claims to close. Fix: named const + `truncateRunes`; regression test with a
   CJK first line (synthetic fixture: web_search/web_fetch result whose first
   line is >117 CJK runes).
2. **`--json` prints corrupt lines verbatim with exit 0**
   (`internal/gopilog/formatter.go:877-909` ValidateSessionFile + cli_log.go
   `--json` branch). Validation checks only non-empty + a session_start line,
   never full-line parse. Verified: corrupt second line passes, garbage is
   emitted, exit 0 — breaks the machine-parseable contract (human view on the
   same file errors, exit 1). Fix: full-line JSON validation naming the first
   bad line, same exit-code semantics as the human path.

### P2
1. Human views on empty / no-session_start files print a garbage header
   (`Session:` blank, `Started: 0001-01-01`) with exit 0 — ValidateSessionFile
   guards only `--json`. (formatter.go:877-909; reader.go ParseFile path)
2. No orphan/no-session_end indicator: a crashed session is indistinguishable
   from a live one in every view. One warning line would fix `--last` triage.
3. `--last N` with a corrupt file: loop continues (good) but exit code stays 0
   while sessions are silently dropped — success is reported on partial data.
   (cli_log.go runLast)
4. `--grep` matches tool names, not only content, contradicting the help text
   "Only entries whose content contains TEXT". (formatter.go:464)
5. Dead `case opts.contextTrim` in `runRead` (cli_log.go:498) — unreachable
   after dispatch pre-routing; duplicate-branch drift risk.
6. `--all` / `--summary` alone report "missing <session-id> argument" — the
   only valid form is `--all --summary`; the error should say so.
7. View-flag precedence is silent and undocumented (`--chat --todo` → chat,
   `--response --json` → json, `--memory --chat` → memory filter). Reject
   conflicting view flags or document one line in help.
8. Raw-log marker strings duplicated across 4+ packages / absent as constants
   in the writer (`internal/agent/rawlog.go`); task-list markers duplicated
   (formatter.go:984-986 vs agent/tasklist.go); context_trim outcome regexes
   are a third copy of loop phrasing. One exported source each (Rule 2).
9. Reader drops the `session_end` error block: `RawEvent` (types.go) has no
   error field, so crash-dump category/http_status/dump_path — written by
   session.go:166-172 — are invisible to every `gopi log` view. Add + surface.
10. `--request` raw-log-missing error is cold (no pointer to enabling raw
    logging / `--raw`).
11. `extractTaskList` deprecated last-only API still present (formatter.go:
    1090-1096) — removable.

## Verification evidence
- `go build -o bin/gopi ./cmd/agent/` — OK.
- `go test ./internal/gopilog/` — PASS (78 tests); `go test ./cmd/agent/ -run TestLog` — PASS; `go vet` clean.
- Live CLI on 997 real sessions + synthetic fixtures in isolated GOPI_HOME:
  all 17 views dispatch; `--last N --json` = parse-clean JSONL stream; shared
  prefix rejected with candidate list; corrupt single session → exit 1;
  CJK chat preview → U+FFFD (P1-1 repro); corrupt-line `--json` → exit 0
  garbage (P1-2 repro); empty/no-session_start default view → year-1 header
  (P2-1 repro); `--last 3` corrupt-middle → exit 0 partial (P2-3 repro).
- Synthetic fixtures were written to and removed from `/tmp` (GOPI_HOME
  isolated; nothing touched the real `~/.gopi`).

## Not Done / deferred
- No code changes made (docs-only review per epic).
- No check of `cli_resume.go`/`cli_checkpoint.go` raw-log consumption beyond
  the marker-constant duplication (they are sibling surfaces of a different
  review item).
- Real-session CJK hunt limited to synthetic fixtures + the one live
  `--chat` sample; P1-1's real-world frequency not quantified.
