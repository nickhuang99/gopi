<!-- doc-class: normative -->
# Rule 16: two gates, one rule — which of them is telling the truth

**Task**: `01a1638e-3681-77e9-bc53-bc95ce50a1b3` (master tip `882c6457c`).
**Question asked**: `internal/stylegate` reports 8 production `errorf-verb` hits
(ceiling 6) and 3 `test-sleep` hits (ceiling 0), while `internal/standards`'
Rule-16 gate is GREEN. Both cannot be right about the same rule.

**Answer, measured**: **both are right about what they measure, and stylegate is
the one with the true system verdict.** They do not measure the same population.
`internal/standards`' Rule 16 cannot see the five sites that produced most of
the red, because of one line in its predicate; stylegate can. The three sites
Rule 16 DOES see are the three that use a string LITERAL, and two of those are
baselined permits and one is a reviewed inline escape — which is exactly why the
standards gate reads green while the tree still carried eight `%v` wraps.

Nothing about this is stylegate being generous or standards being lax: the two
detectors differ by a decidable, mechanical property, and the gaps are named
below with the line of source that decides each one.

## 1. The decisive difference: a LITERAL format string, vs a resolvable one

`internal/standards/gate.go` (`errorWrapFind`, the Rule-16 predicate):

```go
lit, ok := call.Args[0].(*ast.BasicLit)
if !ok || lit.Kind != token.STRING {
    return true          // ← NOT a string literal: the site is skipped, silently
}
```

It parses the FIRST argument as an AST literal node. A call whose format string
is a named constant — `fmt.Errorf(memorySyncTickCwdWhyUnmeasurable, resolved, err)` —
has `*ast.Ident` there, so `ok` is false and the site is never reported, never
counted, and never mentioned as undecidable. **Silence, not a pass** — Rule 15's
exact defect shape, inside a gate.

`internal/stylegate/stylegate.go` (`checkErrorf`):

```go
format, ok := constString(v.info, call.Args[0])
if !ok {
    *v.undecidable++     // dynamic format string: the verbs are not knowable statically
    return
}
```

It resolves the argument through `go/types`' constant information, so a named
`const` format string is seen (and a truly dynamic one is COUNTED as
`undecidable` — 598 on this tree — rather than dropped).

**That single asymmetry accounts for 5 of the 8 hits.** Measured, on the
pre-fix tree (`git stash` at `882c645`):

| # | site | format arg | Rule 16 sees it? | stylegate sees it? |
|---|---|---|---|---|
| 1 | `cmd/agent/memory_sync_tick.go:361` | named const | **no** | yes |
| 2 | `cmd/agent/memory_sync_tick.go:365` | named const | **no** | yes |
| 3 | `cmd/agent/memory_sync_tick.go:371` | named const | **no** | yes |
| 4 | `cmd/agent/worker_control_selector.go:340` | named const | **no** | yes |
| 5 | `cmd/agent/worker_control_selector.go:347` | named const | **no** | yes |
| 6 | `internal/tools/memory_procedural.go:241` | literal | yes → **baselined** | yes |
| 7 | `internal/tools/memory_sync_procedural.go:245` | literal | yes → **baselined** | yes |
| 8 | `internal/tools/worktree_sweep.go:2479` | literal | yes → **escaped** (`// no-wrap:`) | yes |

Raw counts either side of the fix, same tree, same invoke:

```
$ go run ./internal/stylegate/cmd/stylegate -rule errorf-verb   # BEFORE
errorf-verb          1
ignored-error       65
TOTAL               66
undecidable        598

$ go test ./internal/standards/ -run TestStandardGateDataReport -v   # BEFORE
Rule 16 (%w error wrapping): hits=3 baselined=2 escaped=1 NEW=0
```

8 against 3. The gap IS the constant-format population, and the 3 that overlap
are precisely the 2 permits + 1 escape.

## 2. The second difference: who declares a deliberate unwrap, and where that reason lives

The same site (`worktree_sweep.go:2479`) is green in one gate and red in the
other — permanently, by design:

* `internal/standards` honours the inline marker `// no-wrap: <reason>`
  (`errorWrapEscape`), so the site is `classEscaped` and the data report shows it
  by name. The reason's HOME is the comment block above the code.
* `internal/stylegate`'s `errorf-verb` honours **NO** inline escape — verified,
  not assumed: the marker vocabulary there is `AllowMarker` (`allow-sleep`) and
  `IgnoreMarker` (`ignore`), both consumed by other rules; `checkErrorf` calls
  `v.report(...)` with no reason lookup at all. A deliberate unwrap is declared
  in **`internal/stylegate/gate_test.go`'s ceiling + its comment block**.

**So: which of the two is the true value?** For "why is this `%v` correct here",
the code-side `// no-wrap:` is the true value — a reader of the statement needs
it there, and the gate that consumes it (standards) is the one whose contract
says so. For "is this rule satisfied across the tree", stylegate is the true
value, because its ceiling is a MEASURED COUNT of every decidable site, not a
per-site permission — and that is what makes it the double-check on every
`// no-wrap:` claim. The two are not contradictory once you see that they answer
different questions; what was wrong was the claim (in `gate_test.go`'s own
comment, and repeated in `PROJECT.md`) that the remaining 6 were "the
long-baselined Rule-16 sites" — they were not, they were constant-format sites
Rule 16 had never seen. That comment is corrected in this change.

## 3. Adjudication of the 8 production hits

Rule 16's substance: keep the cause in the chain unless a stated reason says
otherwise. `%v` → `%w` is safe only where the wrapped value is **provably
non-nil** (`%w` on nil renders `%!w(<nil>)`). Measured on this tree, the trap and
its boundary:

```
$ go test ./internal/tools/ -run TestEvidenceWrapMessageIdentical -v   # scratch probe, not committed
    %v -> "procedural_store: name dedup check: disk I/O error: read-only"
    %w -> "procedural_store: name dedup check: disk I/O error: read-only"
    %w on nil -> "x: %!w(<nil>)"
```

The text is IDENTICAL for a non-wrapping cause, so the conversion changes the
chain and never the message; and `errors.Is(a, cause)` was **false** before and
true after. All eight are adjudicated on that basis:

| # | site | verdict | why |
|---|---|---|---|
| 1–2 | `memory_sync_tick.go` measurement failures | **FIXED → `%w`** in the reason constant | both call sites sit under their own `if err != nil` (lines 359–365), so the value is non-nil by construction |
| 3 | `memory_sync_tick.go` `MkdirAll` failure | **FIXED → `%w`** in `memorySyncTickCwdWhyUncreatable` | that const is reached only under `if err := …; err != nil` |
| 4 | `worker_control_selector.go:340` | **FIXED → `%w`** in `workerControlSelectorUnreadableFmt` | under `if err != nil` |
| 5 | `worker_control_selector.go:347` | **FIXED**, same const | the arg is a `fmt.Errorf(...)`, an error by construction — never nil |
| 6 | `memory_procedural.go:241` | **FIXED → `%w` on the failure arm, guard SPLIT** | see below |
| 7 | `memory_sync_procedural.go:245` | **FIXED**, same split | same shape, and `err` is provably non-nil (the `err == nil` arm returned above) |
| 8 | `worktree_sweep.go:2479` | **KEPT `%v`**, reason in the code (`// no-wrap:`) | the unlock error is deliberately OUTSIDE the chain — the cause worth unwrapping is the lock-file failure, which is `%w` |

Note on #3: `memorySyncTickCwdWhyUncreatable` is a distinct constant from the two
measurement failures; it carries `%v` with the same "plain failure, no sentinel"
reason and was converted in the same edit. The `%v` in
`memorySyncTickCwdWhyNoRoot` was left alone on purpose — it is rendered by
`fmt.Sprintf`, not `fmt.Errorf`, and formats no error at all.

**Why #6/#7 are a GUARD SPLIT and not a `%v`→`%w` byte edit.** Both sites are one
line reached in three states: the query succeeded (`err == nil`, handled above),
the query found nothing (`sql.ErrNoRows` — whose ONLY meaning here is "no row
yet, go insert"), and a real failure. A mechanical `%w` would put `sql.ErrNoRows`
into the chain of an error, handing callers a sentinel they must treat as
SUCCESS through `errors.Is` — and `err` can be nil there in principle, so `%w`
would also render `%!w(<nil>)`. The guard became:
`if err != nil && err != sql.ErrNoRows { … %w … }`, which removes BOTH hazards
and drops the sentinel to the insert path as before. Behaviour is unchanged; only
the chain changed.

## 4. The 3 test-sleep hits

`internal/testutil/suiteroot_signal_test.go` — 3 sites, ceiling 0, and all three
are the fixture of a PROCESS-liveness test (a child of the same test binary is
parked, signalled with SIGTERM, and its pinned run root must be reaped):

* `:84` — a bounded poll for the run root to disappear; the `suiteRootSignalReapWait`
  deadline above it decides the outcome and the loop exits on the first match.
* `:124` — `time.Sleep(5 * time.Minute)`: the child must still be PARKED when the
  signal lands; returning normally would exercise a different (already covered)
  arm. There is nothing to poll for — the sleep IS the fixture.
* `:159` — a bounded poll for the child to report its root; `suiteRootSignalWait`
  owns the deadline.

**Disposition: `//stylegate:allow-sleep <reason>` on each**, which is this rule's
own accepted escape and keeps every site visible in the scan. Converting `:124`
to a poll is impossible (it waits for a signal, not a state) and converting `:84`
/`:159` to a bare `time.After` would only relocate the same fixed interval. The
test was run for real after the change and passes (`--- PASS:
TestSuiteRunRootIsReapedOnSignal (1.13s)`).

## 5. The ratchet, both directions

| gate | before | after | direction |
|---|---|---|---|
| stylegate `errorf-verb` production ceiling | 6 | **1** | down (the measured count is 1) |
| stylegate `test-sleep` ceiling | 0 | **0** | unchanged — the escape markers are the documented route; the ceiling already had no headroom |
| standards Rule-16 baseline entries | 3 | **1** | down (2 permits deleted because their sites were repaired) |

Nothing was raised and nothing was relaxed. `git diff` on
`internal/stylegate/gate_test.go` touches exactly one ceiling line (`6` → `1`)
plus its comment block; the standards change DELETES two baseline entries.

**Red proof** that the count still bites: setting the new ceiling to `0` makes
`TestStyleGate` fail and name the surviving site —

```
gate_test.go:210: stylegate errorf-verb: 1 production hit(s), ceiling 0
        internal/tools/worktree_sweep.go:2479 (%v wraps error (expr unlockErr))
```

— then it was restored byte-clean.

## 6. What remains open (honest)

* **The standards Rule-16 predicate is still blind to constant format strings.**
  This task REPAIRED the sites it could not see; it did not fix the blind spot,
  because widening a detector is a change to the check infrastructure itself
  (Rule 28: peerbox's layer) and because a wider predicate would immediately report
  every constant-format site in the tree, which is a sweep of its own. It is
  filed as its own row rather than smuggled in here.
* `cmd/agent/worker_control_selector.go`'s format const has a trailing `\n` and
  is now `%w`; `go vet`/`printf` treats `fmt.Errorf` with a trailing newline as
  errorf-with-newline, which is pre-existing shape and unchanged by this work.
