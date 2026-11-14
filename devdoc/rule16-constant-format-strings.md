<!-- doc-class: normative -->
# Rule 16: the gate now sees a constant format string — and counts what it cannot see

**Task**: `01a163e4-9f74-7385-aac8-a7ea93e93088` (feature work that repairs the
detector blind spot recorded by `01a1638e-…`; base `master` = `2ae7305`).
**What this closes**: `internal/standards`' Rule-16 predicate accepted only a
string LITERAL as `fmt.Errorf`'s first argument, so a `const` format string was
skipped in silence — not reported, not counted, not listed as undecidable. That
is PROJECT.md Rule 15 (no silent failure) violated inside the gate itself, and it
is why this gate read GREEN while `internal/stylegate` reported violations in the
same tree (`devdoc/rule16-two-gates-disagree-20261022.md` has that measurement).

## 1. The predicate, before and after

Before (`internal/standards/gate.go`, `errorWrapFind`):

```go
lit, ok := call.Args[0].(*ast.BasicLit)
if !ok || lit.Kind != token.STRING {
    return true          // ← a named constant lands here and vanishes
}
fmtStr, err := strconv.Unquote(lit.Value)
if err != nil || !percentV.MatchString(fmtStr) {
    return true
}
```

After — the format expression is RESOLVED, and an unresolvable one is RECORDED:

```go
fmtStr, ok := formatText(p, call.Args[0], 0)
if !ok {
    noteUndecidableFormat(p, line, call.Args[0])   // Rule 15: counted, never dropped
    return true
}
if !percentV.MatchString(fmtStr) {
    return true
}
```

## 2. How far the resolution reaches — and where it stops

`internal/standards/gate1_fmtformat.go`, AST only (`go/parser` + `go/ast`; NO
`go/types`, so no new dependency — the constraint that made this a surgical
predicate change rather than "use stylegate's approach").

| format expression | resolved? | how |
|---|---|---|
| string literal, incl. a raw string | yes | `strconv.Unquote` |
| `(` x `)` | yes | transparent |
| `"a" + b + "c"` | yes | `token.ADD` walked on both sides; the depth does **not** advance (a concatenation is a finite tree; a dozen parts is an ordinary long refusal message) |
| an identifier naming a same-FILE const | yes | the probe source is in memory and in no directory index, so the file is consulted first |
| an identifier naming a same-PACKAGE const | yes | every non-test `.go` of the file's directory, parsed once per directory per run (`constIndexCache`) |
| `pkg.Name` / `alias.Name` where pkg is imported from THIS module | yes | the import path is mapped to a directory through `go.mod`'s module path; an alias is honoured |
| a variable, a parameter, a struct field, a call | **no — recorded undecidable** | `noteUndecidableFormat` |
| `pkg.Name` imported from OUTSIDE this module | **no — recorded undecidable** | the gate does not walk past the module boundary |
| a const-reference chain deeper than `maxFormatResolveDepth` (4) | **no — recorded undecidable** | the bound makes a cycle/unreasonably deep chain terminate; the site is named, not guessed |

The undecidable channel is DESTRUCTIVE on read (`UndecidableFormats()` drains) so
no consumer accumulates stale sites across runs, and a non-destructive
`CountUndecidableFormats()` exists for the census arm that only wants the number.
`TestStandardGateDataReport` prints the count AND the `rel:line  expr` of every
recorded site, so a reader sees what the gate could not decide instead of a green
that quietly covers it.

## 3. What the widened predicate found on this tree (measured)

At the cut point the predicate was widened, before any site was repaired:

```
Rule 16 (%w error wrapping) (checklist 2.2): hits=12 baselined=0 escaped=1 NEW=11
```

eleven production sites, every one a NAMED CONSTANT carrying `%v` that the old
predicate had never seen. `internal/stylegate` reported **1** errorf-verb hit on
the same tree and counted **599** undecidable — the eleven were inside that mass,
which is exactly why the two gates disagreed for months.

All eleven are now REPAIRED at the site (message bytes unchanged — `%w` and `%v`
render a non-nil error identically; only the chain changes), not baselined:

| site | fix |
|---|---|
| `cmd/agent/cli_daemon_scope.go` `daemonUnitQueryFailedNote` | `%v`→`%w` |
| `cmd/agent/cli_notify.go` `notifyBatchJSONRefusal` (2 call sites) | `%v`→`%w` |
| `cmd/agent/cli_notify.go` `notifySpillRefusalFormat` | `%v`→`%w`; its SIBLING arm (the nil-store refusal, whose tail value is a plain string, not an error) got its own `notifySpillNoDirFormat` — one verb cannot be both a wrap and a plain render |
| `cmd/agent/cli_uistate_compare.go` `uiStateCompareDerivedSessionUnreadable` | `%v`→`%w` |
| `internal/scratch/rootgocache.go` `goCacheUnmeasurableReasonFmt` (3 call sites) | `%v`→`%w` (each caller's value is an `error`) |
| `internal/tools/task_merge.go` `taskMergeCheckoutHeadUnreadableFmt` | `%v`→`%w` |
| `internal/tools/writetaskarea.go` insert arm | the budget stays `%v` (a `time.Duration` is not an error), the error became `%w`; documented in place |
| `internal/tools/writetaskarea.go` bootstrap arm | **kept `%v` deliberately** — `errors.Is(reported, openErr)` must stay FALSE on the budget-expiry arm (pinned by `TestWriteTaskAreaBootstrapExpiryIsReportedAsABudget`), so the error is intentionally outside the chain; the reason now lives in a `// no-wrap:` block directly above the return |

The two in-place `// no-wrap:` declarations are the ONLY remaining Rule-16 hits in
the tree (`worktree_sweep.go:2479` from `01a1638e`, plus the bootstrap arm above):

```
Rule 16 (%w error wrapping) (checklist 2.2): hits=3 baselined=0 escaped=3 NEW=0
CENSUS: 3214 fmt.Errorf call(s) with arguments — 3214 resolved (of which 208 are
        CONSTANT format strings, the shape the old predicate skipped), 0 undecidable
```

`errorWrapBaseline` is EMPTY on this branch (it was already empty at the cut
point); `maxBaselineEntries` and every stylegate ceiling are untouched.

## 4. How the two gates now relate (the alignment claim, and its limit)

`internal/stylegate` remains the only one of the two that type-checks, so it is
still the authority on "is this argument actually an `error`" and on `%[n]v`
index positions. What changed is the POPULATION: every `fmt.Errorf` on this tree
whose format string is a compile-time constant is now visible to BOTH, so the
finding sets agree on the constant-format-string sites:

- stylegate `errorf-verb` = 1 (`worktree_sweep.go:2479`, the reviewed case).
- standards Rule 16 = 3 hits, all escaped, and its escaped list CONTAINS that
  same site — so standards' reported set ⊇ stylegate's, on the repaired tree.

What is still NOT equal, stated so a future reader does not mistake this for
parity:

1. **`%+v` / `%#v`.** stylegate deliberately does not report a flagged verb (the
   output would change); this gate reports any `%v` after an error-shaped value.
   The gate is the stricter of the two here — a documented, not accidental,
   difference.
2. **Indexed verbs (`%[2]v`).** stylegate matches verb position to argument
   position under go/types; this predicate is argument-shape based and pairs the
   format with "some argument is error-shaped". A site where the error-shaped
   argument is consumed by `%s` while a DIFFERENT argument takes `%v` is a
   false-positive shape for this gate and a non-finding for stylegate
   (`cmd/agent/main.go`'s historic `payloadFilePrefix` permit was exactly this
   shape, which is why `mentionsError` stays deliberately conservative).
3. **Argument type.** Without `go/types` a non-error value formatted with `%v`
   whose NAME looks error-like (`errText`, `xErr`) is still reported here, and
   must be answered with the inline `// no-wrap:` reason — the gate's reviewed
   escape. Widening the predicate's resolution (this change) did not widen that
   false-positive surface; it is unchanged.
4. **Cross-module format constants.** A format string declared in a package this
   module does not own resolves to neither side's advantage: here it is recorded
   undecidable; stylegate's importer resolves it when export data is present.

## 5. Probes (what keeps this from rotting back)

`internal/standards/gate1_fmtformat_test.go`:

- `TestGate1ConstantFormatRedProof` — a `const` format with `%v` MUST be reported
  (the arm that could not fail before this change).
- `TestGate1ConstantFormatConcatRedProof` — the `+`-spelled format.
- `TestGate1ConstantFormatNoFalsePositive` — a `const` `%w`, a non-error `%v`, and
  a verb-less format stay silent.
- `TestGate1VariableFormatIsUndecidable` — a parameter, a field and a call are
  recorded EXACTLY (three sites, named), the channel drains on read, and the
  second read returns nothing.
- `TestFormatTextResolution` / `TestFormatTextDepthBound` /
  `TestImportDirOfResolvesWithinTheModule` — the resolver's own table: every shape
  it claims to follow, and every shape it must refuse.
- `TestGate1EveryFmtErrorfIsJudgedOrCounted` — the tree-wide census, with a
  VACUITY FLOOR: it fails if the tree holds zero non-literal format strings
  (nothing would be proving the widening works) or if any site is unresolvable.

Red proof executed against the real tree: restoring the literal-only predicate
(mutating `formatText` to return early on a `*ast.BasicLit`) turns
`TestGate1ConstantFormatRedProof` and `TestGate1ConstantFormatConcatRedProof` red;
the restore was verified by content identity
(`git hash-object` = the recorded blob `a9e8d3155b…`), not by a diff against the
index.
