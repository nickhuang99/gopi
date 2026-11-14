<!-- doc-class: historical -->
# The prose table that looked like a resolver — TestGopiBinaryResolverCensus, red on `./bin/gopi`

**Task**: 01a12dc4-1d51-7ef0-a5cc-e6bd629f472a (2026-10-12, host=hosta)
**Filed by**: row 01a12d67's bug report (`bug-gopibin-census.md`, evidence
`evidence-5-testutil-today.txt`).
**Symptom**: `TestGopiBinaryResolverCensus` (the stale-binary SHAPE gate,
`internal/testutil/gopibin_resolver_gate_test.go`) failed on master with two
UNPERMITTED hits of kind `literal-bin-gopi` — `internal/taskreport/delegated.go:149`
and `internal/taskreport/delegated_test.go:171`.

## Root cause — measured, not inferred

`git log -S'./bin/gopi' -- internal/taskreport/` returns exactly one commit, and
`git grep` says which side of it the literals live on:

```
$ git grep -n '"\./bin/gopi"' 4a535c2ab^ -- cmd internal
4a535c2ab^:internal/testutil/gopibin_resolver_gate_test.go:40:	// … ("../../bin/gopi", "./bin/gopi").
$ git grep -n '"\./bin/gopi"' 4a535c2ab -- cmd internal
4a535c2ab:internal/taskreport/delegated.go:149:	"./bin/gopi",
4a535c2ab:internal/taskreport/delegated_test.go:171:		"./bin/gopi",
4a535c2ab:internal/testutil/gopibin_resolver_gate_test.go:40:	// …
```

At the offending commit's **parent** the only occurrence is inside the gate's own
doc comment — which the AST parser does not read, so it was never a hit. The gate
file itself is present at that parent (`git ls-tree 4a535c2ab^ -- …/gopibin_resolver_gate_test.go`
→ blob `5306937b`) and dates to **2026-10-04 03:39** (`433a122df`), while the
literals date to **2026-10-12 08:46** (`4a535c2ab`, `feat(taskreport): record a
report that delegates its gate conclusion to a path`). `merge-base --is-ancestor
433a122df 4a535c2ab` → **true**.

**First offending commit: `4a535c2ab`.** It introduced both literals.

## Why the gate only went red that day

The census is a **tree-wide** scan — `gopiResolverScanRoot` walks `cmd/` and
`internal/` in full and floors the file count (`gopiResolverScanFloor`) so a
partial read cannot pass as a clean tree. It therefore lives in, and is only
executed by, the package that owns it: **`internal/testutil`**.

`4a535c2ab` touched `internal/taskreport`, `internal/tools` and `cmd/agent` —
never `./internal/testutil`. A branch-scoped smoke run on that diff (`PKGS=`
covering the packages the diff touched, which is exactly what Rule 27/`smokegate`
prescribes for the merge/verify path) therefore **could not execute the census**
and reported green. The defect landed with nothing red anywhere; it surfaced the
next time anyone ran `PKGS='./internal/testutil'`. This is the structural blind
spot of a tree-wide census whose own package is outside the changed set — not a
mistake in that commit's testing, and not fixable by asking a future author to
"remember to run testutil too".

## The decision — a permit with a reason, not a refactor

Both surviving sites are in the **result-delegation** family
(`devdoc/report-result-delegation.md`): a check that reads report PROSE.

1. `internal/taskreport/delegated.go:149` — an entry of `delegatedInstruments`,
   the vocabulary `sentenceDelegatesResult` matches a sentence against
   (`hasAny(low, delegatedInstruments)`, `delegated.go:362`). Below that,
   `delegatedProse` **strips** every path token before the result test judges the
   same sentence. The token is a word in a list; nothing is opened, stat-ed or
   executed. The package's whole import set is
   `fmt os path/filepath strconv strings unicode` — it owns no resolution.
2. `internal/taskreport/delegated_test.go:171` — the token is the **subject** of
   `TestDelegatedPathShapeRejectsNonArtifacts`, whose assertion is that this argv
   spelling is *not* a delegated artifact path (no extension in its last
   segment). Removing the token would delete the shape the case exists to pin.

**Why not change the code.** Sourcing the token is not merely awkward, it is
banned by the rule this gate serves:

- `BinDirName`/`BinFileName` live in `internal/testutil`, the package that owns
  the resolver (`run.go`). `internal/taskreport` is a **production** package with
  **zero** dependencies (`go list -f '{{join .Imports "\n"}}'` → stdlib only), so
  importing the test harness from it to borrow two spellings would be a strictly
  worse change than the literal.
- Assembling it from those constants inside taskreport would mean **re-typing**
  `"bin"` / `"gopi"` — the same string, one indirection out.
- Assembling it here from `internal/testutil`'s constants is precisely what
  **Rule 13's reverse** forbids: "不能为了躲检测而把常量拼成字符串". The gate then
  would still have to be taught to see the assembled form, and each new spelling
  is a new hole.

So the literal stays where it is, with a reason, and the permit is keyed
`(file, kind)` — it exempts the FILE's `literal-bin-gopi` hits, never a kind
globally, and its own staleness is already an error in the census.

## Regression protection

The ratchet existed (`STALE PERMIT`), but nothing pinned it, and a permit is the
one place this gate can be quietened — a table that exempted everything would
look exactly like a green census. The census's verdict is now factored into
`gopiResolverVerdict(hits, permits)`, and `TestGopiBinaryResolverPermitsAreNarrowAndRatcheted`
asserts three properties plus a reason-length floor:

1. **RATCHET** — with the site gone, every row is reported `STALE PERMIT`.
2. **FILE-NARROW** — the same shape in another file is still UNPERMITTED.
3. **KIND-NARROW** — `join-bin-gopi` in the permitted file is still UNPERMITTED.

Measured, three runs on this tree:

```
# GREEN (the delivered tree)
$ go test ./internal/testutil/ -run 'TestGopiBinaryResolver' -count=1 -v
=== RUN   TestGopiBinaryResolverGateRedProof
--- PASS: TestGopiBinaryResolverGateRedProof (0.00s)
=== RUN   TestGopiBinaryResolverPermitsAreNarrowAndRatcheted
--- PASS: TestGopiBinaryResolverPermitsAreNarrowAndRatcheted (0.00s)
=== RUN   TestGopiBinaryResolverCensus
    gopibin_resolver_gate_test.go:516: permitted  internal/taskreport/delegated.go:149 [literal-bin-gopi] ./bin/gopi
    gopibin_resolver_gate_test.go:516: permitted  internal/taskreport/delegated_test.go:171 [literal-bin-gopi] ./bin/gopi
    gopibin_resolver_gate_test.go:519: census: 2 hit(s) over 2420 Go files, 2 permitted
--- PASS: TestGopiBinaryResolverCensus (0.90s)
ok  	github.com/nick/gopi/internal/testutil	1.267s

# RED — mutant A: a permit row for a shape that does not exist in that file
$ go test ./internal/testutil/ -run TestGopiBinaryResolverCensus -count=1
    gopibin_resolver_gate_test.go:524: census: 2 hit(s) over 2420 Go files, 3 permitted
    … STALE PERMIT internal/taskreport/delegated.go [join-bin-gopi]: MUTANT ROW: a permit on a shape
      that does not exist in that file (the stale-permit arm).
FAIL	github.com/nick/gopi/internal/testutil	1.663s

# RED — mutant B: the permit keyed on the FILE alone (kind dropped)
$ go test ./internal/testutil/ -run TestGopiBinaryResolverPermitsAreNarrowAndRatcheted -count=1
    gopibin_resolver_gate_test.go:460: the table does not exempt its own sites, so the arms below
      would prove nothing: [STALE PERMIT …]
FAIL	github.com/nick/gopi/internal/testutil	0.534s
```

Both mutants are the regression the permit could have introduced; both are red.
The mutant edits were reverted (`git diff` on a clean tree prints nothing).

## Evidence

- RED before the change (verbatim, this worktree, HEAD `6ff8aeee5`):
  `~/.gopi/sessions/01a12dc4-1d51-7ef0-a5cc-e6bd629f472a/artifacts/01-red-before.txt`
  — `census: 2 hit(s) over 2420 Go files, 0 permitted`, both hits named.
- GREEN after (same command): `artifacts/03-green-after.txt`, census line quoted above.
- Root cause: the three `git grep` outputs and the `ls-tree`/`merge-base` lines above.
- Scope measurement: `make test-budget PKGS='./internal/taskreport ./internal/testutil'`
  before the change — `internal/taskreport` **green**, `internal/testutil` red on the
  census alone (stream `run-1791772053-3446403.json`), so nothing but the census
  was wrong.

## Not claimed

- **The census sees only `cmd/` and `internal/`.** A hand-rolled resolution under
  `scripts/`, `testsuite/` or `devdoc/` is out of the gate's reach by
  construction. Not a defect this task introduces and not one it fixes.
- **`TestRealWorkerIncarnationMatchesProcStat` also failed** in the filed evidence
  (`cannot read the cmdline of the probe process 2974687`). It is a separate
  defect — an unreadable `/proc/<pid>/cmdline`, a Rule 15 shape (unknown reported
  as failure) — and it did **not** reproduce in this run's scope. It is not
  covered here and deserves its own row (Rule 41).
- The permit does not make the taskreport token a "good" spelling; it records
  that no binary is resolved there. If a future change makes that package open a
  gopi binary, this row is a lie and must be deleted in the same commit.
