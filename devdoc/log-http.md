<!-- doc-class: normative -->
# gopi log --http — per-round raw HTTP request/response view

## Motivation (Epic)
The session JSONL log (`~/.gopi/sessions/<id>.jsonl`) is a *filtered* record:
it logs event summaries (user / assistant / tool_result / request), never the
exact bytes of each LLM API request nor the full streamed response. When you
need to study **cache-hit/miss, context-size growth, or exactly what each
round sent to the server**, the JSONL cannot help — the `request` events only
carry the token totals, not the payload. The raw HTTP log
(`~/.gopi/raw/gopi-raw-*.log`) has every byte but is far too bulky (hundreds of
MB, 415k+ lines for one session) to eyeball directly; even an LLM wastes
tokens re-reading it.

This feature adds a **per-round raw-log-based view** to `gopi log` that shows
each `→ REQUEST … (next) → REQUEST` round as a digest line (summary) or a
structured request/response detail (per-round) — a compact middle ground
between the filtered JSONL and the verbatim raw dump.

## Usage
```
gopi log <session-id> --http             one-line summary per round
gopi log <session-id> --http N           detail of round N (0 / last / >count → last round)
gopi log <session-id> --http last        detail of the last round
gopi log --last N --http                 summary of the last N sessions' rounds
```

## Data source
The view pairs the session to its **raw HTTP log** via the existing
`ResolveRawLogForSession` resolution (exact `rawLogPath` recorded in
session_start when present, timestamp proximity otherwise) — the SAME
resolution `--raw` / `--request` use. The JSONL is never consulted for bytes.
The whole raw log is scanned once with the existing raw scanner buffer sizes.

## Raw log grammar — TWO framings, and a file is always exactly one of them

A raw log's record framing is decided **once, at file creation**, and every
reader dispatches on it (`rawlogfmt.Detect`, a bounded read of the head). The
parser below is ONE implementation serving both: it dispatches on a record's
TAG, never on marker text, so no consumer branches on the format.

**FRAMED** — every log created from this version on:

```
R 412\n<exactly 412 bytes>\n
C 91\n<exactly 91 bytes>\n
```

The header is one tag byte, one space, the payload's length in decimal, one
newline. The payload follows VERBATIM, then ONE newline that belongs to the
framing and not to the payload. **No byte is decoration and no payload byte is
ever scanned for text** — the length decides where a record ends, so a payload
that quotes this grammar (or any grammar) is simply payload. The tags name a
record's DIRECTION and nothing else: `R` request body · `P` a request the
guard/fail-fast ladder sent (the annotated `→ REQUEST (1214-guard…)`) · `S` a
complete non-streamed response body · `C` one streamed SSE event · `E` a failed
attempt's diagnostics (`HTTP-ERROR`). An optional annotation rides the header
(`P 210 (1214-guard, stream)`) as metadata: it never enters a payload, so a
payload cannot forge it, and it is preserved because `--raw` renders it.

A tag is ASCII on purpose: a legacy marker line begins with a non-ASCII arrow
(U+2192 / U+2190) or with `HTTP-ERROR`, none of which a frame header (one ASCII
byte, one space, digits) can be — which is what makes the discriminator total.

**LEGACY** — every log written before that, and any later run that CONTINUES an
existing file (a resume writes its file's own framing; no file is ever mixed):

```
→ REQUEST\n<single JSON line>\n
← CHUNK\n<one SSE event, blank-line terminated>\n
← RESPONSE\n<single JSON line, possibly annotated>\n
HTTP-ERROR\n<diagnostics until the next marker>\n
```

Each round = one request record to the next. Marker lines (whole-line or
marker+" (" annotated) that the legacy grammar recognizes:

| Marker | Meaning | Payload framing |
|---|---|---|
| `→ REQUEST` | HTTP POST body (Anthropic Messages API) | single JSON line after marker |
| `→ REQUEST (1214-guard, stream)` | ladder/guard probe, annotated | single JSON line |
| `← CHUNK` | one SSE event | **multi-line** `event:`/`data:` lines, blank-line terminated (Anthropic); OR **single JSON line, no blank** (older deepseek `chat.completion.chunk`) |
| `← RESPONSE` | non-stream response body | single JSON line (possibly annotated) |
| `HTTP-ERROR` | non-200 diagnostic (`non200RawLog`) | header + body lines until next marker |

Two wire shapes for `← CHUNK` payloads are digested: Anthropic multi-line SSE
(`data:` concatenation parsed into message_start / content_block_start /
content_block_delta / message_delta events) and single-line Deepseek
`chat.completion.chunk` JSON (finish_reason / delta content / reasoning_content).

## Per-round digest fields (RoundResponse)
- request: model, max_tokens, stream flag, system-block count, message count,
  **exact request byte size** (the context-growth signal the JSONL hides).
- response kind: stream / response / http-error / none.
- chunks, total payload bytes, text chars, thinking chars.
- **usage**: input / cache-read / cache-write / output tokens when the
  provider reported them in the stream.
- stop_reason, tool_use names (from content_block_start).

Guard/ladder probes (annotated `→ REQUEST`) are visible as their own rounds —
exactly the extra sends the JSONL hides (1214-guard, minimal-core degrade).

## View formats
Summary mode prints one line per round:
```
R01 req 50.2 KB   2sys/1msg   resp 160 chunk(s) 20.4 KB  tools [memory_search,bash] stop=tool_use · in 513 cr 12544 out 204
R02 req 66.0 KB   2sys/3msg   resp 639 chunk(s) 80.1 KB   tools [memory_feedback,bash] stop=tool_use · in 4497 cr 13056 out 754
```
Detail mode prints the round's request (per-message block digest + text
previews, rune-safe truncation) and response digest, plus the raw-log line
span for locating the exact bytes.

## Files
- `internal/rawlogfmt/` — **the ONE definition of the record framing**: the
  encoder, the decoder (`Reader`), the line-oriented view (`LineScanner`, which
  keeps every line-reading consumer working unchanged), the flattening rule, and
  the total discriminator `Detect`. Writers and readers both import it; no
  package re-spells a tag, a header shape or a legacy marker (Rules 13/14).
- `internal/gopilog/rounds.go` — `HTTPRound` / `RoundResponse` types,
  `ParseHTTPRounds` (dispatches on a record's TAG, so ONE parser serves both
  framings), `digestChunk` SSE digest.
- `internal/gopilog/httpview.go` — `FormatHTTPRounds` summary + detail.
- `internal/gopilog/rounds_test.go` + `rounds_framed_test.go` +
  `rounds_bytes_test.go` — monolithic round parser tests, the two-framing
  equivalence test, and the response-byte accounting test.
- `cmd/agent/cli_log.go` — `--http` flag, `runHTTP`, dispatch before
  `--raw`/`--request`/`--json`, `--last N --http` support.

## Design decisions / pitfalls
1. **Rounds count ALL request records**, including guard/ladder probes (`P`).
   Filtering them would silently hide the very requests the JSONL already hides.
2. **No payload byte is ever scanned for text in the framed framing.** A legacy
   reader must match whole lines because a payload can quote a marker; the
   framed reader does not have that hazard at all, because the length decides
   where a record ends (`internal/rawlogfmt/cmd/proveambiguity` builds a payload
   carrying every marker and shows the framed read stays one round while the
   legacy read splits it in two).
3. **In LEGACY logs, marker matching is whole-line (or marker+" (")**, never
   substring — marker text quoted inside a request body must not split rounds.
   This is the property the framed framing replaces rather than fixes: the
   legacy hazard is inherent to scanning text for structure.
4. **Chunk framing is ambiguous between the two wire shapes**: Anthropic
   events are blank-line terminated; old deepseek chunks are not. The parser
   digests the chunk buffer on BOTH the blank line AND the next marker line,
   so both shapes work on the same scanner.
5. **HTTP-ERROR bodies end at the next marker**, not at a blank line; blank
   lines inside an error body are data.
6. **A record's own framing newline is not the payload's.** The legacy decoder
   removes exactly the one newline the legacy writer appended (and reports
   that it did), the framed decoder removes exactly the framing newline after
   the declared bytes. Line numbering, `--lines A-B` and blank payloads all
   depend on this being exact — measured: treating a record as "truncated"
   invented one extra empty line at the end of a file.
7. Text/thinking *char* counts are a proxy for output volume, NOT tokens —
   the usage fields (when the provider reports them) are the token truth.
8. The session JSONL `request` events are NOT needed for this view — but they
   remain the only place with cache-hit/miss numbers for the OLD non-stream
   wire shape (new Anthropic streams report cache_read in message_start).
9. **A raw log is never MIXED.** The framing is fixed at creation; a run that
   continues an existing file writes that file's own framing (detected, or the
   write is refused — never guessed). Every byte of every file therefore decodes
   in one mode, which is what makes a bounded head read a complete answer.


## Verification
- Round parser unit tests (`rounds_test.go`): synthetic raw logs covering
  blank-line Anthropic SSE + legacy single-line chunk shapes, annotated
  guard/ladder markers as their own rounds, tool_use capture, marker-text
  quoted inside a request body, HTTP-ERROR folding, request-byte growth.
- Formatter tests (`httpview_test.go`): summary/detail render the expected
  fields; round selector 0/last/out-of-range falls back to the last round.
- CLI arg test: `--http` bare / N / last / `--last N --http` parse.
- E2E against real raw logs (Anthropic-style + Deepseek-style sessions):
  - `gopi log <id> --http`: request size growth 50.2KB → 372.4KB across 60
    rounds; cache-read growth 12,544 → 40,448; tools and stop_reason per
    round.
  - `gopi log <id> --http 2`: full per-message request digest + response
    usage/tools/stop.
  - non-stream RESPONSE rounds and `--last 1 --http` verified.
- `go build ./...` OK; `go vet` clean; `go test ./internal/gopilog/` green;
  `go test ./cmd/agent/ -run 'Log|Parse'` green. Full `go test ./...` suite
  not run to completion (known sibling CPU contention stalls network E2E);
  the changed packages are green.
- Commits: `208b2d2` (feature), `38e10fa` (arg tests) on branch
  `feat/log-http` (shared-repo worktree — the branch IS in the main repo).
