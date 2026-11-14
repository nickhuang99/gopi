<!-- doc-class: historical -->
# Task Report: the smoke fixture stages the entry point's whole script closure

Task `01a0b539-4ace-75c2-9154-f84c7c4efc19` · branch `fix/smoke-fixture-stage-scripts-lib`

## Completed
- **Root cause fixed (fixture, not product).** `stageEntryPoint`
  (`internal/smoke/e2e_test.go`) staged a hand-listed two-path closure:
  `Command` (`scripts/test-budget.sh`) plus `scripts/lib/tmp-ban.sh`.
  `scripts/test-budget.sh` later grew a second source —
  `. "$root/scripts/lib/heavy-threshold.sh"` (line 93) — where
  `heavy_inventory_rel` lives (Rule 14 single source). The stale list meant the
  fixture executed a script whose line 93 failed, `set -u` made
  `heavy_inventory_rel` fatal at line 103, the entry point never printed its
  `raw json: ` line, and the e2e assertions reported the WRONG thing:
  *"its artifact contract changed"*.
- **Fix**: `stageScriptTree` replaces `stageEntryPoint` — it mirrors the repo's
  **entire `scripts/` tree** (`filepath.WalkDir`), preserving each file's mode
  and the directory shape, with three non-vacuity checks: the walk must stage
  >0 files, the staged tree must carry `Command`, and a non-regular entry (a
  symlink) is a loud failure rather than an unverifiable closure. `set -u` in
  `scripts/test-budget.sh` is untouched, and `heavy_inventory_rel` is **not**
  inlined — it still has exactly one definition, in
  `scripts/lib/heavy-threshold.sh`.
- **Regression guard** (`internal/smoke/e2e_staged_scripts_test.go`):
  `TestStagedScriptTreeCarriesTheSourceClosure` checks (a) every repo file under
  `scripts/lib/` is present in the staged fixture tree (a non-vacuity floor
  fails if the repo carries no lib files), and (b) **every source directive the
  staged scripts carry resolves inside the staged tree** — the directive set is
  derived from the scripts' TEXT (`"$root/scripts/lib/x.sh"` → repo-relative
  path), never from a list, so a source line added tomorrow is covered without
  editing the guard.
- **Red-proof by mutation**
  (`TestStagedScriptTreeCheckFailsWhenTheSourceClosureIsLost`): over a scratch
  copy of the repo's script tree with `scripts/lib/` **removed**, the same
  predicate must report unresolvable sources, must flag the entry point itself,
  and every flagged path must be under the removed directory. A predicate that
  silently matched nothing cannot read green.

## Not Done
- Nothing deferred. The guard is deliberately **fixture-closure-scoped**, not a
  repo-wide tree diff: an unrelated new script (or a new `scripts/lib/` file no
  staged consumer sources) must not redden a gate whose subject is the e2e
  fixture. Completeness of the mirror is enforced by construction (the walk
  copies everything), so a repo-wide diff would only duplicate that.
- No change to `scripts/test-budget.sh`, `scripts/lib/*`, or any product code —
  the defect was entirely in the fixture.

## Verification
- Build: `go build ./...` → OK; `gofmt -l internal/smoke/` → no output; `go vet ./internal/smoke/` → OK.
- Tests: `go test ./internal/smoke/ -count=1` → `ok github.com/nick/gopi/internal/smoke 2.994s`.
- Acceptance test alone: `-run '^TestBranchScopedSmokeRunIsScopedAndSkipsHeavy$'`
  → `--- PASS`, subtests `scoped` (exit=0, stream packages = the fixture's `internal/foo` only)
  and `whole-tree-control` (exit=1, stream packages = `internal/bar` + `internal/foo`).
- Commit: on `fix/smoke-fixture-stage-scripts-lib` (see the ledger row).

## Evidence (self-filed)

**RED, on the pre-fix branch state (master `a9a96cd7` + branch, before any edit):**
`go test ./internal/smoke/ -count=1 -run '^TestBranchScopedSmokeRunIsScopedAndSkipsHeavy$'`
→ `exit=1`; both subtests failed, each preceded in the same output by

```
.../scripts/test-budget.sh: line 93: .../scripts/lib/heavy-threshold.sh: No such file or directory
.../scripts/test-budget.sh: line 103: heavy_inventory_rel: unbound variable
    e2e_test.go:133: the entry point printed no "raw json: " line — its artifact contract changed
    e2e_test.go:169: the entry point printed no "raw json: " line — its artifact contract changed
```

— i.e. the exact failure the epic names (line 93 source → `set -u` fatal → no
raw-json line → 133/169).

**GREEN, after the fix:**
`go test ./internal/smoke/ -count=1 -run '^TestBranchScopedSmokeRunIsScopedAndSkipsHeavy$' -v` →
`--- PASS: TestBranchScopedSmokeRunIsScopedAndSkipsHeavy (1.79s)`, `ok ... 2.340s`, with the two
recorded run lines `e2e_test.go:136: scoped run: exit=0 packages-in-stream=[... /internal/foo]` and
`e2e_test.go:172: whole-module control: exit=1 packages-in-stream=[... /internal/bar ... /internal/foo]`.
Whole package: `go test ./internal/smoke/ -count=1` → `ok github.com/nick/gopi/internal/smoke 2.994s`.

**Guard red-proof (the mutation gate):**
`go test ./internal/smoke/ -count=1 -run TestStagedScriptTree -v` →
`--- PASS: TestStagedScriptTreeCarriesTheSourceClosure (0.14s)` and
`--- PASS: TestStagedScriptTreeCheckFailsWhenTheSourceClosureIsLost (0.01s)` whose own recorded
observation is

```
e2e_staged_scripts_test.go:125: red-proof: removing scripts/lib yields 16 unresolvable source
directive(s): [... scripts/test-budget.sh sources scripts/lib/heavy-threshold.sh ...]
```

— the entry point's own source line among them, and each flagged path under the removed directory.
The mutation is the removal of `scripts/lib/`; the predicate's answer flips from "0 unresolvable"
(the guard's green) to "16 unresolvable" (the red-proof). Nothing was deleted from the repo, the
working tree, or any worktree: the mutation was applied to a `t.TempDir()` copy of the tree.

**Closing state:** `git status --porcelain` in the worktree printed nothing after the commit.

**Guard red-proof #2 — the mutation the epic names (staging loses `scripts/lib/`):**
`stageScriptTree` was temporarily mutated (`os.RemoveAll(<tree>/scripts/lib)` right
after the walk) and the guard re-run:
`go test ./internal/smoke/ -count=1 -run TestStagedScriptTreeCarriesTheSourceClosure`
→ `--- FAIL: TestStagedScriptTreeCarriesTheSourceClosure (0.20s)` with four
`e2e_staged_scripts_test.go:75` errors ("the repo carries `scripts/lib/<f>` but the
fixture's staged scripts tree does not") plus the `:89` unresolvable-source errors,
`scripts/test-budget.sh sources scripts/lib/heavy-threshold.sh` among them. The
mutation was reverted (`git checkout -- internal/smoke/e2e_test.go`); the worktree
was clean afterwards. So the guard's red is demonstrated against the REAL fix site,
not only against the scratch-tree copy.

## Files
- `internal/smoke/e2e_test.go` — `stageEntryPoint` → `stageScriptTree` (whole `scripts/` tree, mode-preserving, non-vacuity checked).
- `internal/smoke/e2e_staged_scripts_test.go` — the closure guard + its mutation red-proof.
- `devdoc/smoke-fixture-script-closure.md` — this report.
