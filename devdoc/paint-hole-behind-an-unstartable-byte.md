<!-- doc-class: historical -->
# The paint hole behind an un-startable byte: `Screen.Feed` held what no read could complete

**Class:** defect post-mortem + the criterion that fixes the class · **Scope:**
`internal/lineedit` · **Commit:** `14cde7c6` (fix + regression test) · **Task:**
`01a0e839-a400-7641-8487-9fe1aa12e408`

## Symptom

A frame written to the terminal never appeared on the grid the dump reads back:
the same byte stream painted `ok` in one chunking and an **empty screen** in
another, with the parser still holding 9 bytes "for the next read" that would
never come. No pty, no timing, no load — deterministic, and reproducible from
eight bytes.

## Root cause (5 why)

1. *Why was the paint gone?* `Feed` held the whole remaining buffer instead of
   interpreting it.
2. *Why did it hold?* The `default` arm read `utf8.DecodeRune`'s tuple and treated
   ANY `(RuneError, size<=1)` as "a multi-byte rune whose continuation has not
   arrived".
3. *Why is that wrong?* That tuple has **two** causes, and the arm could only
   name one. `(RuneError,1)` with `utf8.FullRune == false` is a *truncated valid
   prefix* — the next read completes it. `(RuneError,1)` with
   `utf8.FullRune == true` is a *byte that can never begin a sequence* — a lone
   continuation byte `0x80..0xBF`, an over-long lead `0xC0/0xC1`, `0xF5..0xFF` —
   and the decoder's verdict is **final**. (Confirmed against the stdlib's own
   table: `DecodeRune(80)=(U+FFFD,1) FullRune=true`; `DecodeRune(E4)=(U+FFFD,1)
   FullRune=false`; `DecodeRune(E4 41)=(U+FFFD,1) FullRune=true`.)
4. *Why does that cause a hole rather than one lost rune?* The held tail is
   re-offered verbatim on the next `Feed`, so its **head** is the same byte, the
   same branch is taken, and the tail is re-held — including every genuine paint
   byte appended behind it (that frame's cursor moves, text, SGR).
5. *Why up to ~512 bytes and no more?* `holdPending`'s bound eventually drops the
   whole tail, so the livelock ends by **discarding the state**, and the paint
   behind it is lost outright. The hole is bounded by the bound, not by the
   defect. (Why the bound drops rather than truncates is sibling `01a0e81b`'s
   subject — `e06a2aed` — and is deliberately **not** touched here.)

## The criterion, not a byte offset

Two readings of one tuple must be told apart by a **property of the bytes**, not
by a length guess:

- **Hold** iff the buffer is an *incomplete prefix that could still become
  valid* — `!utf8.FullRune(buf)`.
- **Skip** iff the byte begins no rune at all — `!canBeginRune(b)` — because no
  amount of waiting makes it one. Advancing past it is exactly what a real
  terminal does (draw a replacement glyph, continue with the next byte).

`canBeginRune` states the RFC 3629 lead ranges once, with named bounds (Rule 1):
`0x00..0x7F` yes, `0x80..0xBF` no, `0xC0/0xC1` no (over-long), `0xC2..0xF4` yes,
`0xF5..0xFF` no (past U+10FFFF).

## Verification recipe (reusable, zero-dep, no pty — Rule 27)

```bash
# RED: restore the pre-fix blob and watch the new tests fail
git show <parent>:internal/lineedit/screen.go > internal/lineedit/screen.go
go test ./internal/lineedit/ -run 'TestScreenFeed'

# GREEN
cp <fixed> internal/lineedit/screen.go && go test ./internal/lineedit/ -run 'TestScreenFeed'
```

Two traps this exercise produced, both worth keeping:

1. **`string(0x80)` is not `0x80`.** An int→string conversion is a *rune*
   conversion, so the fixture became U+0080's two bytes and the "defect" vanished
   (`painted "\u0080hello"`). A byte fixture must be built as
   `string([]byte{b})` — see `rawByte` in the test file.
2. **A harness template can cost bytes you did not mean to spend.** An early
   fixture glued a fixed paint to a *plain-text* case, where the paint needed a
   leading `\n` to land on its own row; the cell contents then differed for a
   window-layout reason rather than the defect. Each case now derives its own
   reference by removing the invalid bytes from the same stream.

## Boundary (what this fix is not)

`internal/lineedit/keys.go:134` has the same shape
(`b&utf8LeadMask != 0 && !utf8.FullRune(d.buf)` → wait) and is **correct** there:
the decoder only holds once a LEAD byte has arrived, so an un-startable byte is
not reachable at the head. A different surface with the same words — checked, not
assumed, and left alone.

## Files

- `internal/lineedit/screen.go` — `Feed`'s `default` arm; `canBeginRune`+range
  constants.
- `internal/lineedit/screen_invalid_utf8_resync_test.go` — 7 tests: lone `0x80`
  (same chunk / separate feeds), `0xBF`, `0xF5`, `0xFF`, a run of noise, the
  mixed-stream chunking equivalence, `0xE4`+`A`, and the guard that a genuinely
  truncated VALID prefix is **still held**.
