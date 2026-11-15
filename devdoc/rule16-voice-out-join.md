<!-- doc-class: normative -->
# Rule 16 repair: `voiceWriteCaptureFile` cleanup error was rendered with `%v`

Task `01a0a417-9996-710b-8a11-eff9b6b2b4c5` · branch `fix/rule16-voice-out-join` (off `master` = `eb802d64`)

## The defect

`internal/standards` Rule 16 (checklist 2.2) was red on master at
`cmd/agent/cli_voice_client.go:811` — introduced by `0e566c03` (feat/voice-input-out, merged
`4cb499c6`), and **not** covered by a baseline entry (the permit that later covered a neighbour
was for `cmd/agent/main.go:954`, a different site):

```go
if rmErr := os.Remove(path); rmErr != nil {
    return nil, fmt.Errorf("--%s %s: %w (and the incomplete file could not be removed: %v)",
        voiceFlagOut, path, err, rmErr)   // ← rmErr unreachable via errors.Is/As
}
```

The `%v` is not cosmetic. The function's whole contract is *"the partial capture must not be
mistaken for a complete recording"*: the caller could reach the write error through the chain
but **never** the cleanup error, so "a partial WAV is still on this disk" was undetectable —
the exact silent-failure shape Rule 15 names. This is a real defect, not a lint nit: the site
gets fixed in code, and no permit is added for it.

Two candidate fixes satisfy both `errors.Is/As` reachability halves:

| option | chain | operator message |
|---|---|---|
| (A) two `%w` verbs in one `fmt.Errorf` | write err **and** cleanup err reachable | one sentence, names both |
| (B) `fmt.Errorf("…: %w", errors.Join(err, rmErr))` | both reachable (joined message) | joined message is the tail |

**Chosen: (A).** It keeps the single operator-facing sentence that already named both failures
and the README-style hint, and it matches the repo's own precedent — `internal/scratch/gc.go:619`
(`fmt.Errorf("%w: %s: %w", ErrNoScratchHome, home, err)`), plus seven sites in
`internal/cgroupscope`. `errors.Join` keeps the same chain but flattens the message; it is
already the right tool where **no message is wanted** — the sibling `voiceDiscardSpool`
(`voiceDiscardSpool`, `cmd/agent/cli_voice_server.go:347`) does exactly that, and still does.
(The line number moved from :255 when the child-reaper series inserted the
`voiceServeShutdownCh` seam above it — the symbol, and the `errors.Join` shape this
sentence cites, are unchanged.) Option (A) wins on
message fidelity, (B) would have won on "no sentence wanted"; the comment at the site records
this choice so the next reader does not re-open it.

## The change

The cleanup had to actually RUN to produce a reachable error, so the branch that failed to
remove a file is unreachable from a test without an `os` seam (this package has none — no
package-level `= os.*` var exists in `cmd/agent`). The fix therefore separates the decision
from the I/O:

```go
if err != nil {
    return nil, voiceCaptureFailure(path, err, os.Remove(path))
}
return wav, nil
```

`voiceCaptureFailure(path, writeErr, rmErr)` owns the sentence and the chain: `rmErr == nil`
produces the "(incomplete file removed)" form and no joined error; `rmErr != nil` produces the
"(and the incomplete file could not be removed: %w)" form with **both** errors in the chain.
`os.Remove` keeps its best-effort role — its error is still reported, never propagated as a new
failure, and the guard-clause shape means no line carries `%v`-on-error any more.

### Why not the alternatives

- **`// no-wrap:` inline escape** — the error IS part of the chain's story; an escape would hide a
  real broken chain (and Rule 16's escape is reserved for "deliberately not inspectable").
- **Swallow the cleanup error** (`_ = os.Remove(path)`) — that is Rule 15's swallowed-error
  defect, and it deletes operator evidence.
- **A baseline permit** — forbidden by the task, and the wrong shape: the entry list is keyed by
  `file:line`, so every later insertion above it goes STALE. The fix removes a hit instead
  (10 → 9), which is the ratchet moving in its only legal direction.

## Regression guard

`TestVoiceCaptureFailureKeepsBothErrorsReachable` (`cmd/agent/cli_voice_out_test.go`) is the
minimal repro/ship test: `errors.Is` must reach **both** sentinels, the message must name both
failures, and the clean path must not claim a removal that did not happen. It fails on the old
`%v` code by construction (that is the shape it pins) and passes on the helper.

## Line drift (Rule 23 context)

The briefing's `:811` was re-verified by grep on my branch tip, not trusted: the site still sits at
`:811` in the edited file (the guard-clause rewrite kept the same line count, so the call now reads
`return nil, voiceCaptureFailure(path, err, os.Remove(path))`), and `voiceCaptureFailure` is defined
at `:828`. Two other tasks are live in `cmd/agent` (`01a0a40b`, `01a0a3f9`), so any later reader must
re-grep `rmErr` / `voiceCaptureFailure` rather than use a line number.

## Verification

See `tasks/01a0a417-9996-710b-8a11-eff9b6b2b4c5-report.md` for the before/after transcripts.
