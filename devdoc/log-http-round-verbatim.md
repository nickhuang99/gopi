<!-- doc-class: historical -->
# Task Report: gopi log --http round-scoped verbatim (--http N --request/--response)

## Context / Epic
The `gopi log --http` feature parse was mis-aligned with the ORIGINAL requirement:
the reporter wanted **verbatim** HTTP request content per exchange retrieved from the
**raw** log (not the JSONL), but what got built was a per-round digest *statistics*
view. The statistics are genuinely useful and the reporter explicitly wants them kept.

## Design decision
- Keep `--http` (bare) = one digest line per round (the stats — unchanged).
- Keep `--http N` = one round's full request/response **digest** detail (unchanged).
- ADD **round-scoped verbatim**: `--http N --request` prints round N's request body
  byte-for-byte from the raw log; `--http N --response` prints round N's response
  side (the ← CHUNK / ← RESPONSE / HTTP-ERROR markers + SSE payloads) **char-for-char**
  by re-reading the raw-log line span. `N` = 0/last/over-count → last round.
- This gives the "retrieve other parts from raw log verbatim with a filter param"
  idea the epic asked about, scoped to a round so it stays bounded on
  multi-hundred-MB / thousands-of-round logs.
- Naming reconcilation: `--http` stays the raw-log namespace; `--response`/`--request`
  change meaning *only* when composed with a round selector under `--http N`. At the
  session level (`gopi log <id> --response`) they keep their JSONL meaning unchanged.

## Completed
- `HTTPRound.RespLineStart` field: raw-log line of the first response-side marker in
  a round (← CHUNK / ← RESPONSE / HTTP-ERROR); stamped once per round by
  `ParseHTTPRounds`. 0 when no response marker was logged.
- New exported formatters in `internal/gopilog/httpview.go`:
  - `FormatHTTPRoundRequestBody(r, name)` — prints `RequestRaw` verbatim (no re-read).
  - `FormatHTTPRoundResponseBody(r, rawLogPath, rawLogName, HTTPBodyOptions)` — re-reads
    raw-log lines `RespLineStart..LineEnd` and prints them verbatim; `MaxLines` cap
    avoids multi-hundred-line dumps (with a truncation hint).
  - `readRawLogLineSpan` line-range re-reader; `HTTPBodyOptions`; shared
    `HTTPDetailIndex(rounds, round)` (0/last/over-range → last) refactored out of
    `formatHTTPRoundDetail`.
- CLI `runHTTP` dispatch (`cmd/agent/cli_log.go`): interprets `opts.request`/`opts.response`
  as round-scoped body selectors only when a **detail** round is selected. Guard:
  bare `--http` / `--http all` combined with `--request`/`--response` prints a clear
  error telling the user to pick a round via `--http N` (never a silent no-op).
- Help text (`logUsage`) documents the new combined flags.

## Not Done
- No `MaxLines` surfaced as a CLI flag (defaults to unlimited). Streamed rounds can be
  long; a future `--http N --response --max-lines K` would be the ergonomic toggle.
  Response re-read opens the file a second time (cheap); a seek-friendly index could
  avoid the re-scan for very large logs, but the per-round line span is small.
- Automatic line-end trimming: the last round includes the trailing blank separator
  lines the logger appends (they ARE in the logged bytes, so verbatim is faithful).

## Verification
- Build: `go build ./...` OK; `go vet ./internal/gopilog ./cmd/agent` clean.
- Tests: `go test ./internal/gopilog/` PASS; cmd/agent log-parse + gopilog HTTP/Round
  verbatim tests PASS:
    TestParseHTTPRoundsBaseStreaming (RespLineStart 1/3/14, 15/17/18)
    TestFormatHTTPRoundRequestBody / ResponseBody / ResponseBodyMaxLines / HTTPDetailIndex
    TestParseLogArgsHTTP (request/response parse alongside --http N)
- Commit: `a95e083 feat(log): round-scoped verbatim request/response under --http N
  (--http N --request/--response)` on branch `feat/log-http-verbatim`.
- Live verify (reporter's own environment) against `gopi-raw-20260905-064208.log`,
  session bbdc45b4-2c7e-4e79-9f9d-e3f977dcbad6:
  - `gopi log <id> --http 1 --request` → verbatim request body JSON (the request line).
  - `gopi log <id> --http 1 --response` → verbatim `← CHUNK` / SSE payloads (pristine,
    line 3..338) — the original verbatim requirement satisfied.
  - `gopi log <id> --http` stats listing still intact (digest lines per round).
  - `gopi log <id> --http --request` → clear "need a round" guard error.

## Deliverable
- Branch: `feat/log-http-verbatim` (commit a95e083, in the shared main repo via the
  linked worktree `/home/user/workspace/gopi-http-verbatim`).
- Recovery: `git show feat/log-http-verbatim:internal/gopilog/httpview.go` (and
  `rounds.go`, `cmd/agent/cli_log.go`).
