<!-- doc-class: historical -->
# Review #3 (HP/GLM): `gopi log` surface — third-party review

Date: 2026-09-03 · Node: hostb-Laptop · Branch: `review2/hp` (based on 92d7386)
Scope: `cmd/agent/cli_log.go` (700 lines) + `internal/gopilog/` (reader.go 526,
formatter.go 1162, rawlog.go 461, types.go 141 + ~2030 test lines).
Method: full source read, live CLI verification against real session logs
(f0bab300…, 0c2a44eb…), git history of the feature line, docs cross-check.
This review is independent of the peerbox review (devdoc/review2-log.md) and of
the 2026-08-28/08-31 reviews; every finding below was reproduced or verified
against code+live output on this machine.

## Verdict

The log surface is in good shape: help text matches behavior on every view I
exercised, error paths are honest, Rule-1/Rule-2 hygiene is far above the rest
of cmd/agent, and the earlier review rounds (rune-safe truncation, ambiguous
short-ID, BEGIN-without-END todo recovery, corrupt-file resilience) all hold
up under live verification. The findings below are mostly P2 polish plus two
P1 correctness edges: per-event JSON fragility and two dispatch semantics
traps.

## Findings — prioritized

### P1-1 · ParseFile hard-fails on ONE malformed JSONL line (all-or-nothing)
`internal/gopilog/reader.go:46-48`: any single unparseable line makes
`ParseFile` return an error, so every view (`--chat`, `--memory`, `--todo`, …)
dies with `Error parsing …: JSON parse error` — even though `ValidateSessionFile`
(formatter.go:780) and `LoadAllSummaries` (reader.go:326) deliberately
`continue` past bad lines. The resilience is inconsistent *inside one package*:
summaries tolerate corruption, detail views don't.
Live: a truncated tail line (crash mid-write, like the real
`f0bab300….dump.json`) is recoverable content-wise — the session_start and
all earlier events are intact and parseable, but every view refuses them.
Risk: gopi itself (agent loop) writes these files; one torn write (the exact
crash class `session-raw-log-verification-20260803.md` describes) bricks the
log reader until manual surgery.
Suggestion: skip-and-count unparseable lines in ParseFile, expose the count in
Session (e.g. `SkippedLines`), and print `Note: skipped N corrupt lines` to
stderr once per view. Keep the current hard error only for the empty-file /
no-session_start cases where nothing can be shown.

### P1-2 · `--last N` silently drops sessions whose parse fails — including N=1
`cmd/agent/cli_log.go:620-626` (`runLast`): for `n == 1` a ParseFile error
prints to stderr and `os.Exit(1)`; for `n > 1` it `continue`s — so
`gopi log --last 3` can show 2 sessions with no indication that 3 were
requested (and `--last 1` exits 1 while `--last 2` on the same data exits 0).
Live-verified: mixed good/bad tails are common (every crash-aborted session,
e.g. 0c2a44eb which is the newest file, has no session_end and shows
`0 iter / 0 tok`).
Suggestion: on error print `Error parsing <id> — skipped (1 of 3)` to stderr
and keep going; surface the skipped count in the exit code (e.g. exit 0 with
data shown, exit 1 only if nothing could be shown).

### P1-3 · `sessionID` positional shadows the `--last`/`--all` modes
`cmd/agent/cli_log.go:363-393` dispatch order:
1. `opts.all && opts.summary` — but `gopi log <id> --all --summary` renders
   the FULL single-session view (falls to runRead) and `gopi log <id> --all`
   errors "missing <session-id>… " only after `--all` was accepted, i.e.
   `--all` is a no-op/mis-lead flag with a positional.
2. `opts.contextTrim && sessionID != ""` beats the generic filter branch —
   correct — but `--last 3 --context-trim` + a positional ID silently ignores
   `--last 3` and renders just that session: `case opts.lastN > 0` is only
   reached when sessionID == "".
Live: `gopi log 0c2a44eb… --last 2 --memory` renders one session, no warning.
This is the same class as the review-#2 "flag accepted but ignored" family.
Suggestion: either reject positional+--last/--all as a parse error (they're
mutually exclusive modes), or make --last win over the positional and say so
in help. Silent precedence between sibling modes is the worst of the options.

### P2-1 · Two live decode paths for the same schema (single-source drift)
`parseContentBlocks`/`parseToolResultBlocks` (reader.go:163-179) decode
`[]ContentBlock`/`[]ToolResultBlock`; `extractContent` (reader.go:133) decodes
either string or block-array for the user message; `ValidateSessionFile`
(formatter.go:780) re-implements the RawEvent scan with its OWN buffer
constants (`1<<20, 10<<20` inline at formatter.go:790 instead of
`scannerInitBufSize/scannerMaxBufSize`). Same schema, four call sites — the
formatter.go:790 inline constants are the only Rule-1 violation left on the
surface. Suggestion: one `scanJSONL(path, func(line []byte) error)` helper
with the named constants; all four call sites share it.

### P2-2 · User message array-content is reduced to the FIRST text block
`extractContent` (reader.go:133-158) returns on the first non-empty
`b.Text`. Real logs put the EPIC GOAL and the [system] checkpoint into one
user event as a block array (verified in 0c2a44eb lines 2-3) — fine — but a
user event carrying `[image, text, text]` (vision) or a doc-attachment array
would show only block 2. Only visible in `s.Task` / chat `▸ USER` / summary
TASK column. Low impact today (user content is currently always a string);
flag for the vision era.

### P2-3 · `--grep` tool-scope ignores text/thinking by design but the help
doesn't say which content is searched
Help says "combines with the family flags above to scope the match" — the
actual behavior (formatter.go FormatGrep): with a scope, text/thinking blocks
are skipped entirely (the `continue` at the scoped branch), so
`--grep timeout --job` = "job tool calls mentioning timeout", but
`--grep timeout` (no scope) also searches Name/Args/InputRaw of EVERY tool,
which surprises (match on `--filter`-unrelated tools). Behavior is coherent,
docs under-specify it. Suggestion: one help line — "no scope: searches every
tool call, response and thinking block; scoped: only matching tool calls".

### P2-4 · `formatChatToolCall` bash has a "TIMEOUT" dead-end claim
formatter.go:1053: a bash call with no result renders `→ TIMEOUT` — but an
absent tool_result is equally produced by a still-running pair, a dropped
pair (context_trim "removed"), or a parse miss. "TIMEOUT" asserts a cause the
log doesn't carry. Suggestion: `→ (no tool_result)` — same neutral wording
the filtered view already uses (formatter.go:603).

### P2-5 · Iteration Number 0 is cosmetic noise (cosmetic)
`request` events start at iteration 0, so chat shows `── Iteration 0 …` and
`--response` labels blocks `[iteration 0]` (live-verified on f0bab300).
Suggestion: display `iter.Number+1` (or a 1-based renumber at parse) — the
"iteration 379" vs 380 total mismatch today invites miscounting.

### P2-6 · `collectLogFilters` uses `!strings.HasPrefix(args[i+1], "--")` to
grab the value — a filter name can never start with `--`, fine, but a flag
VALUE that itself looks like a flag (`--grep --job`) is consumed as the grep
query by memoryFlagSet (grep swallows "--job" as its value → family scoping
silently lost). Live: `gopi log <id> --grep timeout --job` works only because
the value comes first; `--grep --job timeout` treats "--job" as the query.
The hand-rolled parser has no value/flag disambiguation. Suggestion: at least
warn when a flag's captured value starts with `--`.

### P2-7 · Duplication between runRead and renderSessionView
cli_log.go:435-470 vs 530-585: the same view dispatch (request→raw→grep→
filter→json→chat→todo→contextTrim→response→thinking) is written twice; the
--last path's json-precedence differs (renderSessionView checks `opts.json`
AFTER filter/grep but BEFORE the chat switch — runRead checks json BEFORE
ParseFile). Today's flag combinations make both orderings equivalent, but the
next view added must be threaded through two dispatchers with subtly
different precedence — the exact divergence class that produced the review-#2
"checkpointUsage stale" finding. Suggestion: renderSessionView is already the
single engine — make runRead call it (runRead keeps only FindSession + the
session-not-found listing).

### P2-8 · `--context-trim` and `--context` are two flags for one family
`--context-trim` (view) vs `--context` (filter) both select context_trim
activity; the only difference is the output format (table vs filtered-view).
The help explains it ("(--todo is a view, not a filter)" line covers todo but
not this pair), and `--context --text` falls through to FormatToolFilter
scrolled past as a filter view. Suggestion: help one-liner under the shortcuts
table: "--context-trim is the table form of --context".

### P2-9 · Summary token numbers are fictional for crash-aborted sessions
Live: newest session shows `0 iter / 0 tok` in `--all --summary` (no
session_end yet) while it has 58 real request events. The table has no way to
say "in progress / no session_end". Suggestion: render `…` for missing
session_end fields, or add `running` in the EXIT column when a session_end
event is absent (LoadAllSummaries already knows; it just doesn't record it).

### P2-10 · `.dump.json` sibling files pollute session discovery
FindSession matched both `f0bab300….jsonl` and its `.dump.json` twin
(live: ambiguous-prefix error naming both). ParseFile then dies on the dump
file's truncated JSON. Two options: (a) accept only `<uuid>.jsonl` (UUID
regex) in ListSessionFiles/FindSession — dumps stop shadowing; (b) keep as-is
and let the ambiguity error guide. (a) is safer: today a typo'd short prefix
can land on the dump twin.

### Confirmed-good (checked, no finding)
- Help ↔ behavior: every view/flag in `log -h` reproduced live (chat, todo,
  response, thinking, request±json±text, raw±json, memory, search, web, job,
  context, vision, text2img, git, filter×2, grep±scope, json, text, last 1/N,
  last N+view, all+summary). No dead flags found on this surface (unlike the
  08-28 `--keep-thinking` finding elsewhere).
- Short-ID ambiguity fix (8f9e9b4) verified live: `f0bab300` now errors with
  candidates instead of picking one.
- Rune-safe truncation verified: no U+FFFD in any truncated view on this run.
- BEGIN-without-END todo recovery verified live on f0bab300: 2 recovered
  task lists render as first-class tables.
- Rule 1/2: timeout/buffer constants named (scanner*, rawScanner*,
  maxRawViewBytes, summary caps, contextTrimIDCol); only formatter.go:790
  inline scan buffer constants escape (P2-1).
- Zero external deps: stdlib only (encoding/json, bufio, regexp) — GOPI.md
  zero-dep rule holds; no testify anywhere in the package tests.
- Views share one parse+engine (ParseFile→format*) — no per-view re-parsing;
  per-event decode is the only seam (P1-1).
- Error messages are actionable (session list on not-found, "use the full
  session id", raw-log-not-enabled hint on --request/--raw).

## Comparison with prior reviews (what changed since)

- 08-28 buddy review: read-tool false-binary on cli_log.go — the file is now
  clean UTF-8 with emoji arrows only in rawlog markers (read works fine).
- 08-31 feat-sweep: `--last N` per-session rendering (6298305) and family
  shortcuts (209f97f) landed; both verified working.
- peerbox review2 (92d7386): their P1s (dispatch precedence, summary
  consistency) overlap with my P1-3/P2-9 — independently reproduced here.
- Feature-accumulation check (git log --follow cli_log.go, 17 feature
  commits): flags were ADDED (vision→text2img→git→memory_list), none
  REMOVED and none renamed (the dc2d3a4 `--trim`→`--context-trim` rename was
  the only reversal, resolved before master). No dead views.

## Verification

- Build: `go build -buildvcs=false -o /tmp/gopi-r2 ./cmd/agent` — OK
- Tests: `go test ./internal/gopilog/` — ok 0.532s; `go test ./cmd/agent/`
  — 2 FAILs, both environmental (TestParseTasksArgsSubmit{Repo,Model}Flag:
  `getwd: no such file or directory` — cwd deleted mid-run by another
  agent's worktree cleanup; re-run from a stable cwd passes; unrelated to
  the log surface).
- Live: 40+ invocations against real sessions f0bab300… (380 iter, 1.1MB)
  and 0c2a44eb… (crash-aborted tail), plus error paths (bogus flag,
  missing values, bad --last, not-found, ambiguous prefix).
- Reviewer note: my worktree was deleted twice mid-session by a competing
  cleanup job (`~/work/gopi-review2-log` + branch `review2/hp` recreated
  from 92d7386 each time; gopi-review2b sandbox has NO .git — it is a
  contents-only shell). Deliverable therefore also exists as a plain file
  at `~/work/gopi-review2b/devdoc/review2-log-hp.md` as a copy-paste
  fallback until this commit is merged.
