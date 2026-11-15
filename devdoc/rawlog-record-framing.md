<!-- doc-class: normative -->
# The raw HTTP log's record framing — length-prefixed, with the legacy format still readable

## Why this exists (the operator's requirement)

> rawlog 它必须保证原始，就是说不加任何的修饰，不能为了这个观察观看的方便，就是
> user friendly 这个概念去加任何的修饰。因为我们花了大量的力气去做这个 session
> Replay 以及这个 chat Replay 就是为了解决这个人类用户观看的这个困难。那么就要求
> 说这个原始的这个 log 绝对不能有任何的这种多余的修饰的。

The raw HTTP log exists to be **the bytes that went on the wire**. Every byte of
decoration in it is a byte that is not original, and the cost is not aesthetic:

- A reader that wants a payload must first strip a marker word and one appended
  newline — so "the request body" is a derived value, not a span of the file.
- A payload that *contains* marker text (a prompt quoting the format, a tool
  result pasting a log) is indistinguishable from structure. The legacy readers
  defend by comparing whole lines and by knowing every annotated variant
  (`→ REQUEST (1214-guard)`, `(1214-guard, stream)`), and that defence is
  **partial by construction**: it can only fail less often.
- The marker vocabulary must be re-spelled in every package that reads the file.

## The format

A record is a header line, then the payload verbatim, then ONE newline:

```
R 412\n<exactly 412 bytes>\n
P 210 (1214-guard, stream)\n<exactly 210 bytes>\n
C 91\n<exactly 91 bytes>\n
```

- **header** = one tag byte, one space, the payload length in decimal, an
  optional ` (annotation)`, one newline.
- **payload** = exactly `length` bytes, unmodified. Nothing is escaped, nothing
  is scanned, nothing is added.
- **trailer** = one newline that belongs to the framing, not to the payload — the
  same convention the legacy writer used, so a flattening reader sees the same
  lines it always saw.

Tags name a record's DIRECTION and nothing else:

| tag | legacy marker | meaning |
|---|---|---|
| `R` | `→ REQUEST` | request body handed to the provider |
| `P` | `→ REQUEST (1214-guard…)` | a request the guard/fail-fast ladder sent |
| `S` | `← RESPONSE` | complete (non-streamed) response body |
| `C` | `← CHUNK` | one streamed SSE event |
| `E` | `HTTP-ERROR` | a failed attempt's diagnostics |

The annotation (`(1214-guard, stream)`) rides the **header**, not the payload. It
is metadata the writer knows and the reader renders (`--raw` shows it), and
because it is not in the payload a payload cannot forge it.

## Two framings, one discriminator, and no mixed files

`internal/rawlogfmt` owns everything: the encoder, the decoder (`Reader`), the
line-oriented view (`LineScanner`), the flattening rule, and `Detect`.

**`Detect` is total**, and that property is the whole design. A framed header is
*one ASCII byte, one space, digits*. A legacy marker line is *a non-ASCII arrow
(U+2192 / U+2190) or the word `HTTP-ERROR`* — which carries letters and a space
its first byte can never be, since a tag is one of five letters that are not
`H`. So no line can match both, and a bounded read of the head answers which
framing the file is in. `DetectExact` adds the third answer a *writer* needs:
"no record yet".

**A file's framing is fixed at creation.** A raw log is created by one run and
appended to by later runs of the same row (a resume). Mixing two framings inside
one file would force every reader into a mid-stream mode switch — exactly the
kind of "the reader must know more" that this format exists to remove. So one
function decides, at open, for all three creation doors
(`rawLogContinueFormat`, `internal/agent/rawlog.go`):

| the file on disk | the framing this run writes |
|---|---|
| already carries a record | that record's framing (never mix) |
| carries only `#` header lines | **framed** — nothing on disk contradicts it |
| head unreadable | **error** — never a guess |

The middle row is the ordinary case, not an edge: every raw log is created with
its header, and the first record lands at the first HTTP round, so a run that
dies before that round leaves a header-only file.

## What it costs (measured, not asserted)

- **Discriminating is not free but is bounded**: `Detect` reads the head only.
- **Decoding costs one length parse per record**, and the payload is returned as
  a byte span — for the framed format the payload is *literally* the file's bytes,
  which is stronger than the legacy reader can offer (it must trim).
- **The file is slightly larger**: `R 412\n` ≈ 7 bytes of header per record where
  legacy wrote `→ REQUEST\n` = 11 bytes. The framed format is **smaller**, since
  the arrow is 3 bytes of UTF-8 plus a word.
- **Every existing log stays readable**: 1.79 GB of legacy logs on this host
  still parse — `session raw <id> --rounds` output on that file was diffed
  against the pre-change binary and is byte-identical (3385 lines).

## What this changed in the readers (all format-independent now)

| surface | before | after |
|---|---|---|
| `gopilog.ParseHTTPRounds` | scanned for marker TEXT | dispatches on a record's TAG; one parser, both framings |
| `gopilog` line view (`--lines A-B`) | `bufio.Scanner` over the file | `rawlogfmt.LineScanner` — the file's own lines, either framing |
| `checkpoint.LastRequest` / `CountRequests` | reversed over lines for a marker | `Detect` once, then decode; the framed window accepts a candidate boundary ONLY if it decodes to EOF (the framing is self-validating) |
| `agent.resume` (raw-log checkpoint) | scanner + cursor | the last request record's payload |
| `scratch` raw-log trim | cut at an arbitrary offset, back off to a newline | back off to a **decodable record boundary**, or REFUSE (`SessionsRawLogTrimNoRecordBoundaryReason`) — never write a file the readers cannot read |

None of those consumers branches on the format. That is the point of putting the
discriminator in `rawlogfmt` and the switch in the decoder: a consumer written
against "records" keeps working, and the number of places that must know there
are two framings is **one**.

## The ambiguity proof (the defect this format removes, demonstrated)

`internal/rawlogfmt/cmd/proveambiguity` builds ONE payload carrying every marker
the legacy reader recognizes — `→ REQUEST`, `← CHUNK`, `← RESPONSE`,
`HTTP-ERROR`, `R 5`, `C 0`, `S 12`, `R 999999`, `E 1`, `# gopi session gc —
TRIMMED`, `← CHUNK (1214-guard, stream)` — and writes it twice:

- **framed**: the payload is 203 bytes, declared once, and reads back as **1
  round**;
- **legacy**: the same bytes are scanned as text and read back as **2 rounds**.

The payload never changed. The legacy format's structure is a *textual
coincidence* that any payload can satisfy; the framed format's structure is a
length.

## Pitfalls measured while landing this (each one has a regression test)

1. **The annotation tag must stay a request tag.** `P` is the annotated `→
   REQUEST`. Dispatching "is this a request?" on `R` alone makes every
   guard/ladder round disappear (7 tests went red) — `isRequestTag` accepts both.
2. **A comment line does not inherit a record's tag.** `LineScanner` keeps a
   parallel `recs[]` slice, one entry per line, because a single "current record"
   field made the file's first `#` line claim tag `R`.
3. **The line counter must count a framed payload's lines.** Otherwise the
   second record's line number is always 2 and `--lines A-B` shifts.
4. **The request body is the RECORD's bytes, not the line under the header.**
   A framed payload containing newlines was truncated to its first line: a
   146-byte body read back as 7 bytes (`prefix`). Both sides now read the
   record's payload span.
5. **Response bytes are counted once.** The record branch added `len(payload)+1`
   and the line branch added again (116 for a 58-byte body). The line branch now
   only consumes lines; the record branch is the one counter, and `+1` is right —
   it is the framing newline, exactly what the legacy counter always reported, so
   the two formats agree byte-for-byte on the same logical body.
6. **Legacy payloads are TRIMMED, and the decoder says so.** The legacy decoder
   cuts the record's own terminating newline with `TrimSuffix` and used to leave
   `Terminated` unset — so `FlattenRecord` treated every legacy record as a
   truncated framed tail, adding a newline back and then splitting a
   newline-terminated string (one extra empty element). An empty-payload record
   therefore invented a line: the last line reported as 7 in a 6-line file.
   `legacyPayload()` now reports what it removed, and `FlattenRecord` was unified
   to "rebuild the physical text, consume one trailing newline, split".

## Files

- `internal/rawlogfmt/rawlogfmt.go` — tags, `Encode`/`EncodeAnnotated`/
  `EncodeLegacy`, `ParseHeaderLine`, `Detect`/`DetectExact`, `LegacyTag`,
  `LegacyMarker`.
- `internal/rawlogfmt/reader.go` — `Reader` (`Next`, `NextHeader`, `Decode`,
  `Skip`), streaming, no whole-file buffering.
- `internal/rawlogfmt/flatten.go` — `FlattenRecord`: the ONE rule for turning a
  record into the lines it occupies (shared by both framings).
- `internal/rawlogfmt/lines.go` — `LineScanner`: the line view every line-based
  consumer uses.
- `internal/rawlogfmt/cmd/proveambiguity/main.go` — the ambiguity proof above.
- `internal/agent/rawlog.go` — `RawLogger.format` + `rawLogContinueFormat` (the
  one decision point).
- `internal/deepseek/` — `RawLog` takes a `(tag, annotation)` pair; every call
  site names a `rawlogfmt` constant.

## How to verify a change here

```bash
go test ./internal/rawlogfmt/ ./internal/gopilog/ ./internal/checkpoint/ ./internal/deepseek/ ./internal/agent/ ./internal/scratch/

# the ambiguity proof (framed = 1 round, the same bytes in legacy = 2)
go run ./internal/rawlogfmt/cmd/proveambiguity

# a real log end to end
gopi session raw <session-id> --rounds | head
gopi session raw <session-id> 1 request | python3 -m json.tool | head
```

A change that touches the framing must keep the **legacy** reading byte-identical
on a real, large log; the diff against a pre-change binary is the cheapest test
of that and it caught two real regressions during this work.
