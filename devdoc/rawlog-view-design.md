<!-- doc-class: historical -->
# Design: `gopi log --raw` — raw HTTP log view + verification

## Problem

`gopi log` reads the session JSONL (~/.gopi/sessions/<id>.jsonl). The raw
HTTP log (~/.gopi/raw/gopi-raw-*.log) — the byte-exact transcript of every
`→ REQUEST` / `← CHUNK` / `← RESPONSE` — is only surfaced via `--request`
(which pairs session→raw log by timestamp and prints the *first* request's
system prompt). When debugging (thinking trim, memory_feedback absence,
context_trim outcomes), the user must know the raw log convention and locate
the file by hand. The Epic adds a first-class `--raw` view.

Raw log filename convention: `gopi-raw-YYYYMMDD-HHMMSS.log` (local wall
clock). Session JSONL carries `rawLogPath` in its `session_start` event
(recorded by the agent since the resume feature) — the exact link, preferred
over timestamp pairing (memory #875: custom GOPI_RAW_LOG_DIR sessions pair
with the WRONG adjacent raw log via timestamps).

## Design

### 1. `--raw` view

- `gopi log <id> --raw` — print the raw log paired with the session,
  verbatim, byte-exact (markers + payloads, as written by RawLogger).
- `gopi log <id> --raw --json` — `--json` currently means "raw session
  JSONL"; with `--raw` it means "raw HTTP log" (same precedence trick as
  `--request --json`).
- `gopi log <id> --raw --text` — plain text mode: same verbatim content
  (formatting is already minimal; flag accepted for symmetry, no headers).
- Resolution order: (1) `rawLogPath` from session_start (exact); (2)
  fallback `FindRawLogForSession(sessionStart)` (timestamp pairing, default
  dir only). Errors are explicit: session predates rawLogPath field / raw
  logging disabled → clear message, never a silent wrong file.
- `gopi log --last N --raw` — applies the view to each of the last N
  sessions (renderSessionView dispatch, same as --request/--chat).
- Family flag interplay: `--raw` is a *view*, not a tool-family filter;
  combining `--raw` with `--filter`/`--memory`/`--grep` is not supported
  (error or precedence — decide in implementation; default: reject with
  clear message, mirroring how --request ignores other view flags).
- New flag must be whitelisted in `logFlagMember` and parsed in
  `parseLogArgs`.

### 2. Implementation

- `internal/gopilog/rawlog.go`:
  - `FormatRawLog(path string) (string, error)` — reads the file and
    returns the bytes verbatim. Big-file safe: raw logs reach hundreds of
    MB — do NOT slurp into memory; stream via bufio.Scanner or chunked
    io.Copy to a buffer capped at a named constant (`maxRawViewBytes`,
    default 64 MiB) with an explicit truncation marker when exceeded.
  - `ResolveRawLogForSession(sessionPath string) (string, error)` — wraps
    `SessionRawLogPath` first, `FindRawLogForSession` fallback.
- `cmd/agent/cli_log.go`:
  - `logOptions.raw bool`; `logFlagMember("raw")`; parse in parseLogArgs.
  - Dispatch: `case opts.raw && opts.sessionID != ""` before the --json
    passthrough (so `--raw --json` = raw HTTP log verbatim).
  - runRaw(sessionPath, opts): resolve raw log → print verbatim.
- `cmd/agent/cli_log_test.go` + `internal/gopilog/rawlog_test.go`:
  - parse tests (flag accepted, combined with --json/--text, --last N).
  - resolver tests: rawLogPath preferred, fallback timestamp pairing,
    missing-log error path.
  - format test: verbatim round-trip, truncation marker at cap.

### 3. Verification scope (this Epic)

The Epic asks to *verify* three claims using the raw log — with `--raw` in
place the user can do it; this task also performs the checks now and records
findings:

1. **memory_feedback never called** — scan session JSONL for
   `"name":"memory_feedback"` tool_use entries across recent sessions; the
   system-prompt description may be unconvincing (draft a wording fix to
   internal/agent/loop.go memory_feedback description and/or the memory
   section of the system prompt).
2. **thinking trim** — `internal/agent/context.go` TrimToFit removes oldest
   messages; `internal/agent/loop.go` `thinkingForceDisabled` (gopi-race
   R5-R6) drops thinking. Verify a → REQUEST actually shows the trimmed
   history (request bodies contain the full context incl. thinking blocks —
   verified: `"type":"thinking"` appears 24× in a 405KB raw log; `--request
   --json` / `--raw` expose it).
3. **context_trim activity** — `--context-trim` view already exists; verify
   its outcome lines match what the raw log shows (call_id cross-check).

Findings → devdoc/rawlog-verification.md; any system-prompt wording fix is a
separate commit (feat: better memory_feedback description).

## Not in scope

- Raw-log resume (internal/checkpoint, already built).
- Editing/deleting raw logs.
- Raw log tail-follow mode.
