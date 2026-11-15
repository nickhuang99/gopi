<!-- doc-class: normative -->
# Branch-scoped smoke gate — `internal/smoke`

**Task:** epic split 1/4 — the missing primitive + the branch-scoped smoke gate.
**Branch:** `feat/branch-scoped-smoke-gate` (worktree `~/workspace/gopi-smoke-scope`).
**Date:** 2026-09-18. Base: `master` @ `f01e3b91`-era tip (BDD below records the exact base).

## The gap this closes

The merge/verify path had two options and no middle:

- run `go test ./...` — the whole tree: every fixture, every package, the exact
  double-work the heavy-test epic complains about (and the reason
  `internal/tools/verify_gate.go` was still prescribing the full suite for its
  children), or
- run nothing behavioural (take-over runs only build + vet).

Nothing in the repo could answer the question that decides between them:
*which packages did this branch actually touch?* Recon before writing code:
`grep -rn "name-only|TouchedPackages|scopedSmoke" internal/ cmd/ scripts/`
returned no touched-package selection anywhere — the primitive was genuinely
missing, not merely unwired.

## What landed

### 1. The primitive — `internal/smoke`

```go
sel, err := smoke.TouchedPackages(repoDir, "master", "feat/topic")  // Selection{Packages, ChangedGoFiles}
sel.Packages   // ["./internal/foo/..."]   — never "./..."
sel.Empty()    // true for a docs-only branch (plus ErrNoPackages from Run/Execute)
```

Two git queries, both read-only and bounded (`smoke.gitCommandTimeout = 60s`,
`GIT_OPTIONAL_LOCKS=0`, `git -C <repo>` so no process-wide `chdir`):

1. `git diff --name-only master...branch` → the branch's changed paths; filtered
   to `.go`. **Deleted paths stay in the list** — the diff names a path, not a
   package.
2. `git ls-tree -r --name-only branch` → the branch **tip**, parsed into
   `goDirs` (directories that still hold a Go source) and `nestedModules`
   (directories that are their own module).

`PackagesIn(files, tree)` then applies three pure rules:

| rule | why |
|---|---|
| a directory is selected only if the **tip** still holds a Go source there | a deletion inside a surviving package keeps that package (it still must compile); a directory emptied by the branch disappears |
| a directory inside a **nested module** is dropped | `devdoc/voice-zerodep-poc/…` belongs to another module; the root module's patterns cannot address it |
| the **module root** is addressed as `./`, never `./...` | appending the ellipsis addresses the whole module — literally re-creating the run this exists to replace |

`Execute`/`Run` then run the repo's declared entry point on exactly those
patterns and return the child's exit code (0 green, 1 assertion/build, 2 usage,
3 budget trip) — a non-zero code is a *report*, not an error.

### 2. The command constant — Rule 14

`smoke.Command = "scripts/test-budget.sh"` is the ONE exported name for the
repo's budget-declaring entry point. A consumer references it instead of
re-inlining `go test`; `TestSelectionCommandIsTheDeclaredEntryPoint` pins it.
`internal/heavytest` gained `SkipMarkerPrefix` for the same reason (below).

### 3. The heavy class self-skips inside a selected package

Selecting a package must not buy back its heavy cost. Nothing in `internal/smoke`
knows about heaviness — `heavytest.Require` already handles it — but the
acceptance asks for that to be **proven**, so `internal/smoke/e2e_test.go` runs a
real fixture whose selected package contains a heavy test and asserts, from the
run's own `go test -json` stream, that its final action is `skip` and that the
`heavytest.SkipMarkerPrefix` line is present (exported so the consumer matches
the same text the helper writes).

## How it is verified (and why this way)

`internal/smoke/smoke_test.go` — pure + real-git layers:

- `PackagesIn`/`PackagePattern`/`Args` unit rules (deletion, nested module,
  dedup, root-is-never-recursive);
- a **throwaway real git repo** (`fixtureRepo`) whose `feat/topic` branch edits
  `internal/foo`, deletes `internal/bar/bar.go` outright, edits a docs file and
  edits a file inside a nested module — asserting the selection is
  `./ ./internal/foo/...` and that `./...` never appears;
- `TestEmptySelectionIsReportedNotSilentlyGreen` (Rule 15): a docs-only branch
  yields an empty selection + `ErrNoPackages`, never a silent green.

`internal/smoke/e2e_test.go` — the **executed** gate. Real git repo + real
`scripts/test-budget.sh` as the child process, with the fixture module named
`github.com/the operator/gopi/smokefixture` and hot-linked to the repo by a `replace`
directive. That module path is deliberate: Go's internal-visibility rule is a
**path** rule, so only a module under `github.com/the operator/gopi/` may import
`internal/heavytest` — proven necessary by measurement (see "Pitfall 1").

Two sub-tests:

- `scoped` — packages in the stream == exactly the fixture's `internal/foo`;
  selected test `pass`; heavy test `skip` (+ marker); untouched package's test
  **absent**; exit 0;
- `whole-tree-control` — the SAME fixture under `./...` **does** run the
  untouched package to a failure and exits non-zero. The counterfactual is a
  measurement, not a claim: it is the pre-change behaviour made visible.

Evidence is read from the entry point's **declared artifact** (the `raw json:`
line + the `stream=` stamp field name the per-run copy), because the script
deliberately prints nothing about passing tests — grepping human output cannot
work. Reading the per-run copy rather than the shared `last.json` matters:
another suite's run on this host overwrites the shared name.

No fixture is checked in and nothing is written into the repo's working tree, so
the repo's own scans (heavy inventory, the `go test`-budget gate, Rule 27's
working-tree scan) never see a `heavytest.Require` call site that is not real.

## Pitfalls found (worth the next agent's time)

1. **`internal` visibility is a PATH rule, not a module rule.** A fixture module
   named `example.com/smokefix` cannot import `github.com/the operator/gopi/internal/…`
   (`use of internal package … not allowed`). Naming the fixture module
   `github.com/the operator/gopi/smokefixture` and `replace`-ing the repo fixes it and
   needs no `go.sum`.
2. **The entry point's summary hides passing tests.** `scripts/test-budget.sh`
   streams `go test -json` into its artifact and prints only a summary; only the
   `stream=<file>` stamp field names the **per-run** copy (the fixed `last.json`
   is shared and gets overwritten).
3. **A branch-scoped test must not use the repo's own package** as its fixture:
   running `scripts/test-budget.sh ./internal/...` from a test that lives in
   `internal/smoke` recursively re-runs itself. Hence a throwaway module in
   `t.TempDir()` with the script tree **staged** into it.
4. **`t.TempDir()` needs the suite pin.** The e2e fixture module lives on a temp
   root; on a default host that is `/tmp` — the boot device the 2026-09-11
   exhaustion filled. `TestMain` calls `testutil.PinSuiteForTestMain("smoke")`
   (Rule 22), and the child inherits the pinned `TMPDIR`.
5. **The entry point's artifact directory is SHARED HOST-WIDE, and that is a
   correctness trap for any reader.** `scripts/test-budget.sh` defaults its
   artifacts to `$GOPI_TEST_SCRATCH_HOME/queue/test-budget/` and writes
   `last.json` with `>`, which **truncates** a concurrent run's file. Measured
   2026-09-18 on this shared host: a scoped run of ONE fixture package came back
   carrying `internal/tools`' tests from a neighbour's suite — the interleaved
   file made another branch's packages look like this branch's. The test now
   points `GOPI_TEST_SCRATCH_HOME` (the script's documented knob) at its own
   directory AND refuses any stream carrying a package outside the fixture
   module, so a shared artifact fails loudly instead of misattributing scope.
   Anyone else parsing `last.json` should isolate the same way.
6. **The heavy opt-in must be pinned when asserting it is off.** `make test-heavy`
   runs the suite with `GOPI_HEAVY_TESTS=1`; the e2e assertion "the heavy test
   skipped" pins `heavytest.EnvName` to empty so the claim is about the smoke
   DEFAULT rather than about whatever the ambient shell exported.

## The seam with the sibling split (2/4) — read before unifying

Split 2/4 (`feat/wire-smoke-gate-merge-path`) adds **`internal/smokegate`**, a
leaf package with its own exported command constants:

```go
smokegate.Command       // "make test"            — the merge-path smoke command
smokegate.ScopedCommand // a SLOT form for briefing TEXT (never executed)
```

That branch's own doc says it leaves the **branch→packages mapping** to this
package, and its gate reports a whole-tree prescription by quoting
`smokegate.Command`/`ScopedCommand` into `internal/standards`.

So the epic's two splits meet at exactly one point: **who owns the command
string.** Both branches export one, and neither can import the other while only
one is on master (`internal/smokegate` does not exist on master; this branch is
based on master). The values differ for a reason — `smoke.Command` is EXECUTED
with package arguments (`scripts/test-budget.sh ./internal/foo/...`), while
`make test` is a fixed whole-module target that cannot take them, so
`smokegate.ScopedCommand` is text for a briefing rather than a runnable argv.

**The unification (one line, once both are on master):** `internal/smoke` should
stop defining the value and take the entry point from `smokegate.Command`
(`Execute`/`ExecuteTo` already receive it through `smoke.Command`), leaving
exactly ONE definition — Rule 13's requirement that a constant is referenced
rather than mirrored. Until then this constant is the runnable one and is
documented as such at its declaration.

## Honest limits

- The selector answers *which packages the branch touched*; it does NOT decide
  whether a docs-only branch should be smoke-tested at all. That policy stays
  with the caller (an empty selection is reported, never guessed).
- A branch that touches only a file under a nested module yields an empty
  selection for the root module — correct, and worth saying out loud at the call
  site rather than silently widening.
- It is a **library** (this split, 1/4). Wiring it into a merge lane, and
  replacing `internal/tools/verify_gate.go`'s whole-tree instruction, is the
  sibling task's job (`feat/wire-smoke-gate-merge-path`).
