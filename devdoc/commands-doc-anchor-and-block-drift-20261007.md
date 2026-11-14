<!-- doc-class: historical -->
# COMMANDS.md: the generated block and its anchors drift together — regenerate, never repoint

**Date:** 2026-10-07 · **Task:** `01a1139d-1ac8-7552-98b2-dc1f462f5b7b` (REISSUE of `01a112ce`)
**Verdict on the entry state:** `go test ./internal/standards/ -count=1` was RED on a clean
master tip — three failures, all in the `TestCommandsDoc*` family.

```
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource
    first difference:
      got:  | `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:155` | ...
      want: | `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:167` | ...
--- FAIL: TestCommandsDocAnchorsResolve
    cmd/agent/cli_session.go:209 (points at a BLANK line)
--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock
    precondition: the committed document's block is already stale
```

## The root cause, and why it is one defect and not three

Every anchor in the block is `file.go:NNN` **of the routing predicate** — `cliindex` records
`PredLine`, the 1-based line of `func isXxxSubcommand(args []string) bool {`. Editing the
prose *above* a predicate therefore moves the anchor, and the document is then wrong in two
places at once:

| file | anchor published at | predicate now at | moved by |
|---|---|---|---|
| `cmd/agent/cli_checkpoint.go` | 155 | **167** | +12 lines of prose above the function |
| `cmd/agent/cli_session.go` | 209 | **213** | +4 lines of prose above the function |
| `cmd/agent/cli_config.go` | 83 | **86** | +3 lines of prose above the function |

The third failure is not an independent bug: it is a red-proof that **asserts its own
precondition** ("the committed document's block is already stale") before poisoning the
block. A correct document makes the precondition false, so the red-proof *fails* while the
defect it guards is gone. **Do not "fix" that test** — fix the document and it goes green.

Seventeen other anchors inside the block (`cli_chat.go:862`, `cli_scratch_gc.go:507`, …) were
byte-identical to what the renderer wanted, which is exactly the shape of the trap: a partial
repointing by hand leaves the block stale in a way that only the byte comparison catches.

## The fix

The block is **generated** — one command, and nothing else is a legal edit:

```bash
go run internal/standards/testdata/commands-census.go . COMMANDS.md           # rewrite
go run internal/standards/testdata/commands-census.go . COMMANDS.md --check   # re-derive, exit 0 = in step
```

It rewrote three anchor cells (`cli_checkpoint.go:155→167`, `cli_config.go:83→86`,
`cli_session.go:209→213`) and nothing else: `3 insertions(+), 3 deletions(-)`. That single
command also repaired the anchor gate, because the block's anchors **are** the document's
`cli_*.go` references — the two failures were one drift.

**No hand edit was made to the block, and no gate was weakened.** Repointing
`cli_session.go:209` by hand to `:210` (the first non-blank line after the `sessionUsage`
raw string) would have satisfied the anchor gate and left the block stale — the gate
verifies `file:NNN` resolves to a non-blank line, it does not verify the line is the
predicate.

## Verification recipe for the next reader

```bash
# RED, on a clean master tip (proof the defect was pre-existing, not introduced here):
git stash list                                  # empty; no local edits involved
go test ./internal/standards/ -count=1          # exit 1, the three failures quoted above

# GREEN, after regenerating:
go test ./internal/standards/ -count=1          # exit 0 — ok  github.com/nick/gopi/internal/standards  50.105s
go run internal/standards/testdata/commands-census.go . COMMANDS.md --check   # exit 0 (44 verbs)

# The anchor the gate only proves non-blank — print the line and read it:
sed -n '213p' cmd/agent/cli_session.go  # func isSessionSubcommand(args []string) bool {
```

The block's published anchor is `cli_session.go:213` while the document's *prose* still
cites `cli_session.go:126` for a different fact (`sessionUsage`'s own comment block) and
`cli_checkpoint.go:98` / `cli_config.go:94` for two more — those resolve to non-blank lines
that say what the prose claims, and the gate leaves them alone. `126`/`94`/`98` are
hand-written prose anchors pointing at prose; only the block's anchors are generated.

**A hand-written prose anchor is not covered by the generator.** If a future edit moves the
prose those point at, the anchor gate will only fire when the target becomes *blank* — a
one-line shift onto another non-blank line passes silently. That is a known, accepted limit
of the gate (`TestCommandsDocAnchorsResolve` proves resolvability, not meaning), and it is
why this file exists rather than a wider check: the only honest instrument for prose anchors
is printing the line and reading it.
