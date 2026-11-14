<!-- doc-class: normative -->
# The whole-repo style ratchet joins the landing path (mergeset, `stylegate-ratchet` leg)

**What changed:** `internal/tools/mergeset_stylegate.go` — one new LEG in the
merged-result measurement (`RebuildAfterMergeMergeset`, `rebuild_vet_mergeset.go`).
It runs

```
go test -count 1 ./internal/stylegate
```

over the landing's merged revision whenever the landing's own diff brought ANY
`.go` file. Nothing else was added: no binary, no script, no cron line, no
resident tick, no new config key. The ratchet's own data —
`internal/stylegate/gate_test.go` (the ceiling table) and
`internal/stylegate/reasons/*.json` — is untouched by this change.

## The defect this closes, measured

`internal/stylegate`'s `TestStyleGate` is a WHOLE-REPO invariant: it walks up to
`go.mod` and scans every package, failing when any of the ceiling-0 rules
(`test-sleep`, `test-skip`, `ignored-error`, `unchecked-close`, …) gains a hit.
Its scope is the whole tree, so the branch that introduces a hit can be ANY
branch, and nothing about the hit points at `internal/stylegate`.

This step already ran the package — but only through
`mergesetGateTouchedGatePackages`, a fixed list of three gate directories
(`internal/standards`, `internal/stylegate`, `internal/testutil`), consulted
against the landing's OWN diff. So a branch that adds

```go
time.Sleep(50 * time.Millisecond)   // cmd/agent/x_test.go
```

touches `cmd/agent`, not any gate directory. The leg never fired; the landing
was green on every measurement the close loop owned; master went red for the
next person to run the suite. That was the FIFTH recurrence of the same decay
(`038211066`, `c77c4b1f5`, `1863dbfb3`, `0de16060e`, `bf75add69`), each closed
by a hand-run tree-wide sweep — which is exactly a repair that cannot hold,
because it re-opens on the next unrelated landing.

Baseline re-measured on this ground's base, `7ecfa12` (the previous task's
annotation branch merged):

```
$ go test ./internal/stylegate/ -count=1
ok  	github.com/nick/gopi/internal/stylegate	12.858s
```

Green. So the mode left in the tree is not "master is red" — it is "the next
new hit arrives silently", and that is what a landing-time trigger answers.

## Why "any `.go` file" is the right trigger

The invariant is not keyed to a directory, so a directory list cannot express it.
A Go AST scan can only be moved by Go source, so the honest scope is the landing
that brings Go source at all: that is every landing which can possibly change
the answer, and it abstains (with the diff it read, named) on the landings that
cannot. `Rule 27` is satisfied on both axes: SMOKE — one package, the one whose
suite IS the invariant; BRANCH-SCOPED — the trigger is the landing's own
`<mergeRev>^..<mergeRev>` diff, and no whole-tree command span is introduced
anywhere (`TestSmokePathPrescribesNoWholeTreeSuite` stays green).

Two stand-downs keep the leg from being wasted work:

- **no Go in the diff** → the leg reports "the landing brought no Go source
  (`<repo> <rev>^..<rev>`), so the whole-repo style ratchet cannot have moved —
  not run". A skip that cannot be told from a broken leg is a hole, not a saving,
  so the reason is logged.
- **the landing itself touched `internal/stylegate`** → the gate-package leg is
  already asking the toolchain for that exact package on this same measurement;
  running it twice would double the leg's wall time to learn the same answer. The
  predicate is delegated to the sibling's own mapping
  (`mergesetGateTouchedGatePackages`), so the two legs cannot disagree about what
  "already asked" means.

An unreadable diff base ABSTAINS with its own reason — never silently read as
"no Go source", which would stand the ratchet down on a landing nobody measured.

## Attribution is the existing arm, not a second policy

The step already materialises the merge's FIRST PARENT and measures every run in
the set on it. A red the base ALSO had is downgraded to an abstention by
`mergesetGateAttribute`, with the reason naming the base's own reference. So a
legitimately red base does not fire on every Go-touching landing; only a red the
landing introduced is reported — and it is then reported through the channels
the close loop already owns (a `VetFailedEvent` notify plus a deduped `bug` row).

## Executed witnesses

Both were run through the REAL entry point, not a stubbed one. Reproduction:

1. Clone this repo, check out the base, copy in `internal/tools/mergeset_stylegate.go`
   and its test, and build a small driver that calls
   `tools.RebuildAfterMergeMergeset(<repo>, <rev>)`.
2. Two landings on top of it, each a real `git merge --no-ff`:
   `plant` adds `cmd/agent/zz_mergeset_redproof_test.go` containing a bare
   `time.Sleep(time.Millisecond)`; `clean` adds only a test that does nothing.

RED (the merged revision carrying the sleep) — the real suite over the real
repo:

```
GATE=stylegate-ratchet STATUS=FAIL PACKAGES=[./internal/stylegate]
  REASON="" REF=cmd/agent/zz_mergeset_redproof_test.go:81
```

and the verdict line the close loop logs:

```
post-merge gate RED on the MERGED result: gate=stylegate-ratchet repo=… rev=3f4ed0276da8… packages=./internal/stylegate
  at cmd/agent/zz_mergeset_redproof_test.go:81 (1 finding(s)) [attributed to this landing: the base revision was green]
```

The `[attributed …]` suffix is the attribution arm's own output: the base
revision was measured on the same run and was green there.

GREEN (the same landing without the sleep):

```
post-merge gate green on the MERGED result: gate=stylegate-ratchet repo=… rev=9c9cbaf3eefa… packages=./internal/stylegate
```

So the verdict is a fact about the plant, not about the gate being stuck.

## Honest limits

- **The leg's cost is the suite's own cost** (measured warm on this host: ~13 s).
  It runs on every landing that brings Go source, which is most landings. That is
  the price of an invariant whose scope is the whole tree; the alternative the
  briefing rejected — a resident tick — is a persistent dependency, and Rule 35
  says the thing that already ticks owns it.
- **A landing that brings no Go source is not measured**, by construction: it
  cannot move a Go AST scan. This is stated in the abstention, not assumed.
- **This leg reports; it never repairs.** It cannot re-point a `reasons` entry or
  move a ceiling, and neither path would be legitimate from a landing gate: a gate
  that can excuse its own hit is not a ratchet.
- **`go vet ./...` on the same step still compiles the module's test trees**, so a
  non-Go landing is not entirely unmeasured — but nothing about it speaks to the
  ceiling table, which is the point.

## Cross-references

- `internal/tools/rebuild_vet_mergeset.go` — the step; the leg joins its run set.
- `internal/tools/mergeset_stylegate.go` — the leg.
- `internal/tools/mergeset_stylegate_test.go` — the scope arms and the red proof.
- `devdoc/stylegate-ceiling0-recurrence-20261016.md` — the recurrence analysis
  that measured the decay and named the missing landing path.
- `devdoc/style-sweep-20260912.md` — the sweep the ceilings come from.
