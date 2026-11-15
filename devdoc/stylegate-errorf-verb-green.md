<!-- doc-class: normative -->
# stylegate `errorf-verb` green on master — root cause, not a ceiling bump

**Task**: `internal/stylegate` was RED on master (`TestStyleGate`: errorf-verb 10 hits vs ceiling 9), blocking
the merger/verify merge gate. Branch `feat/stylegate-green`, commit `50413d89`, base master tip `807dda98`.

## What the gate actually said (measured, not assumed)

`internal/stylegate` keeps a global ceiling per rule. On master the verbose output listed **10**
`errorf-verb` hits against a ceiling of **9** — i.e. three net-new sites had accumulated since the ceiling
was last touched, all from post-sweep commits: `internal/signalrec/signalrec.go:749` (`9e482ea8`),
`cmd/agent/cli_voice_client.go:811` (`0e566c03`), `internal/worktree/worktree_sweep.go:1325` (`1ed5167f`).

The briefing's second half (4 failing self-tests) did not reproduce — see the report's "Not Done"; those
tests pass on pristine master under every env/shuffle variant tried.

## Why the fix is a fix and not an escape

Two of the three net-new sites are **real bugs**, so the right move was to repair the code and let the
ceiling *fall*:

### 1. `internal/signalrec/signalrec.go` — nil-error `%w` **and** a nil dereference behind one guard

```go
// before: ONE guard, TWO different failure arms
if serr != nil || !fi.IsDir() {
    return fmt.Errorf("... %w", serr)   // !IsDir arm: serr == nil -> "%!w(<nil>)"
}
```

The `!IsDir` arm entered the same body with `serr == nil`. `%w` of a nil error does not panic, it renders
`%!w(<nil>)` — a corrupted message — and the body also dereferenced `fi`, which is nil on that arm. Splitting
the guard fixes both: the `!IsDir` path now has no error to wrap at all, and the `serr != nil` path wraps a
real error. It also de-indents the branch (Rule 18's else-after-return shape disappears there for free).

### 2. `cmd/agent/cli_voice_client.go` — `%v` where the chain matters

```go
// rmErr is provably non-nil under its own guard
return fmt.Errorf("...: %v", rmErr)   // -> %w
```

With the error provably non-nil, `%v` is only a broken chain: `errors.Is/As` from a caller can never match.
`%w` it is. Multi-`%w` in one `Errorf` is byte-identical output to multi-`%v` and is legal since Go 1.20
(`go.mod`'s directive satisfies that).

### 3. `internal/worktree/worktree_sweep.go:1325` — already reviewed, left alone

This site already carries the reviewed inline escape `// no-wrap: <reason>`, and `internal/standards`
verifiably honours it (the site does not appear in that gate's NEW list). **Note the trap**: stylegate's
`errorf-verb` rule accepts **no** inline escape — verified experimentally by appending one and re-running,
the hit stayed listed. So the reviewed position for deliberately-unwrapped sites is the `baseline.go` permit,
which is exactly where the remaining 7 `%v` sites already live (all four in
`internal/memory/memory_procedural.go`, `internal/tools/memory_sync_claim.go:912`,
`internal/tools/memory_sync_procedural.go:245`, `internal/tools/task_merge.go:362`). Those were re-read and
left untouched: their `%v` is a deliberate baselined permit, not drift.

## Ceiling: ratcheted DOWN (9 → 8), never up

`internal/stylegate/gate_test.go` — the `errorf-verb` ceiling literal moved 9 → 8, with the measured reason
in the comment. Fixing two real defects *earns* the tighter ceiling; the rule forbids raising without a
written reason, and nothing here raised anything.

## Second, unstated red: `internal/standards` Rule 16 (stale permit)

Acceptance requires `go test ./internal/standards/` green, and it was red on master too: hits 10, baselined 7,
escaped 1, **NEW 2**, **stale 1**. Of the two NEW, one is the `cmd/agent/cli_voice_client.go` site above; the
other is `cmd/agent/main.go:965`, the `payloadFilePrefix` **string-constant** site. The stale entry was its
permit at `cmd/agent/main.go:954`.

Root cause of the false "new": a pure line shift. Commit `4b00b0bd` is exactly **11 insertions / 0 deletions**
above line 954, so the permitted site slid to 965 while the permit stayed pinned at 954 — and 954 today is a
different statement (`if err != nil {`). A pinned `file:line` permit is a *position*, so any insertion above
it re-points it; the repair is to re-key the permit (954 → 965), not to weaken the gate. The escape/baseline
mechanism is deliberately position-exact (a stale permit is a silent permission), which is why it errors
instead of sliding silently — that behaviour is correct and was left as-is.

## Traps worth carrying forward

1. **`%w` on a nil error is silent corruption, not a crash** — `%!w(<nil>)`. A combined guard that admits a
   nil error into a wrap is the shape to hunt; `%w`-safety needs non-nil *provability*, not just intent.
2. **stylegate `errorf-verb` has NO inline escape.** Adding `//stylegate:ignore`-style comments does nothing
   (proved). Deliberate `%v` sites live in `baseline.go` permits.
3. **Pinned `file:line` permits rot on any insertion above them.** Expect a stale-permit error after upstream
   commits land — re-key, don't relax. (This is the `internal/standards` Rule-16 family.)
4. **A briefing's "N failing tests" is a claim, not a measurement.** Re-measure with the gate's own verbose
   output before designing a fix; here 4 of the 5 claimed failures were green on pristine master.

## Verification

```
go build ./...                               : OK
go test ./internal/stylegate/ -count=1       : ok  10.5s   (FAIL on master)
go test ./internal/standards/ -count=1       : ok  13.9s   (FAIL on master)
go test ./internal/signalrec/... -count=1    : ok
go test ./cmd/agent/ -run TestVoice -count=1 : ok
commit 50413d89                              : 4 files, 58 insertions(+), 5 deletions(-)
```
