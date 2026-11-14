<!-- doc-class: historical -->

> **Retired mechanism (documentation-citation teardown, 2026-11-05).** 本页记录的机制在树里
> **已不存在**。`78bdf05a`（operator ruling）与 `0ee5745c` / `1e3b0976` 删掉了整个
> documentation-citation 家族：三个生成器 `internal/standards/testdata/config-anchor-census.go`、
> `citation-census.go`、`citation-selector-census.go`，它们写的三张 `.tsv` 表，驱动它们的
> 两道门 `internal/standards/citation_gate.go` / `citation_selector_gate.go`（其表已不存在，
> 这两道门本体由 `1e3b0976` 删除；`78bdf05a` 删的是它们的测试），
> 以及交付路径上的 census-citation 腿（`internal/devworkspace/census_gate.go`、
> `census_redrive.go`，见 `0ee5745c`）。
>
> **下面没有任何一条命令还能跑**：`go run …` 得到 `stat … no such file or directory`，
> `go test -run TestMergeConfigAnchorGate` 打印 `[no tests to run]` 并 exit 0（**空绿，不是通过**）。
> 本页保留为**历史记录**：它记的是当时那个缺陷与堵住它的推理。测量方法本身已随生成器消失。
>
> 今天仍能跑、也仍该跑的，是 Rule 36 的语料门：
>
> ```bash
> go test ./internal/standards/ -run RetiredPipeline -count=1
> go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader -count=1
> ```

# The census gate was WIRED and never SELECTED

**Status:** the defect is closed on this ground. The `--check` leg exists, runs, and
now runs on **every** delivery rather than only when the branch happened to touch
the package that owns it.

**How cheaply it runs, measured** (`153c41b`): all 13 arms pass on the DEFAULT path
in `9.790 s` and 13 PASS / 0 FAIL with `-v` in `16 s`; a whole-`internal/tools`
package run is `397.080 s`. The bare `go test ./internal/tools/ -run
'TestMergeConfigAnchorGate'` this page reproduces below is a **dated record of the
red at `7734b63`**, not the command to run as acceptance — the command is the lane's
own `make test-budget PKGS='./internal/tools/...'` (see the table at the end of this
page). The measurements and the reason no arm is classified heavy are
`devdoc/config-anchor-gate-is-not-heavy.md`.

## The premise the task arrived with was false, and measuring it was the first work

The briefing said `config-anchor-census --check` has *no runner in the tree* and
that the fix was to write one. Measured on this ground (HEAD `7734b63`, 2026-10-27),
that is not so:

```
$ grep -n "func runCensusCheck" internal/tools/merge_config_anchor_gate_test.go
86:func runCensusCheck(t *testing.T, root, tablePath string) censusCheckResult {
```

`runCensusCheck` shells the shipped generator's own `--check` entry point, and
`TestMergeConfigAnchorGate_RealTreeIsClean` drives it. The gate was restored under
its documented names by `f5b79b0` after `5a35f61` deleted the merge-time leg. So
the tree already had a runner, and writing a second one would have been the
"second mechanism" Rule 35 forbids.

## What was actually broken: the SCOPE of the branch-scoped check

The table WAS drifted at HEAD — five rows — and the wired gate WAS red at HEAD:

```
$ go run internal/standards/testdata/config-anchor-census.go . \
    internal/standards/testdata/config-anchor-census.tsv --check
… 5 anchor row(s) differ (first 5 shown)      # exit 1
$ go test ./internal/tools/ -run 'TestMergeConfigAnchorGate' -count=1 -v
--- FAIL: TestMergeConfigAnchorGate_RealTreeIsClean
--- FAIL: TestMergeConfigAnchorGate_ReportsTheOffendingPathAndLine
--- FAIL: TestMergeConfigAnchorGate_ARePointedCitationIsAStaleRecord
```

The commit that drifted it names its own cause:

```
$ for f in cmd/agent/main.go cmd/agent/cli_ui.go; do git log --oneline -1 66a2a43..HEAD -- $f; done
7734b63 feat(session): 一个 turn 数，四个面 —— log/stats/bar 也印 ledger 的 counts
7734b63 feat(session): 一个 turn 数，四个面 —— log/stats/bar 也印 ledger 的 counts
```

`7734b63` touched `cmd/agent/`. The close-out's branch-scoped check asks
`smoke.TouchedPackages` which packages the branch's diff can have regressed, and for
that commit the answer is `./cmd/agent/...`. **`./internal/tools/...` was never
selected**, so the gate that would have caught the drift never ran in the round that
introduced it. The red then appeared on the integration branch — for whoever looked,
on a branch that did nothing wrong. That is exactly the shape the epic set out to
remove.

**The general lesson:** *a gate is only as good as the selection that reaches it.*
"Wired" answers "does it run when its package is tested"; it does not answer "is its
package tested". Those are different questions and only the second one explains a
drift that lives on master while every gate is green on the branch that caused it.

## The smallest change (zero new persistent dependency)

`finishCleanupBranchCheckAt` already builds a **list** of checks and runs them on the
ground through one runner:

```go
checks := []mergeCheck{check}                       // the touched-package smoke run
… anchor, findings, anchorWhy, anchorOK := finishCleanupBranchAnchorCheck(dir, base, branch)
```

The close is one more member of that same list, built from the TREE alone — no
branch, no base, no diff, so **no diff can decide whether it runs**:

```go
if census, ok := finishCleanupBranchCensusCheck(dir); ok {
    checks = append(checks, census)
} else {
    censusNote = censusGateAbstention()
}
```

`internal/tools/task_finish_premerge_census.go` holds the arm. Its command is
rendered by the same declared entry point every other check uses —
`mergeCheckSmokeCommand` → `smokegate.ScopedForPackages` — and its scope comes from
`smoke.PackagePattern(censusGatePackageDir)`, the ONE composer, so the arm cannot
re-spell `./` + `/...`:

```
ARM_BUILT=true NAME="config-anchor-census" CMD="make test-budget PKGS='./internal/tools/...'" TIMEOUT=1800
```

No new script, no Makefile target, no cron, no resident, no hook. Nothing ticks;
the arm rides the lane that already runs before every push.

**A tree without `internal/tools/` abstains with its own sentence** rather than
inventing a pattern (`censusGateAbstention`), because a fixture, a nested module or
another project is not this repo's shape and an invented pattern addresses nothing
while reading as a pass (Rule 15).

### Why not "run it in `internal/standards`, which every branch could be made to include"

That was the obvious alternative and it is measured here: `internal/standards` is
**red at HEAD for reasons unrelated to the census**.

```
$ scripts/test-budget.sh ./internal/standards/   # exit 1, elapsed 112s
  NEW violation (checklist 4.3) at internal/tools/memory_sync_child_cwd.go:369
  NEW violation (checklist 1b/git-env) at internal/tools/task_briefing_authoring_test.go:457
  package verdict: FAIL: github.com/nick/gopi/internal/standards
```

Hanging an always-run arm on a package that is already red would make every
delivery refuse for a defect the delivery did not cause — the false-refusal family
`merge_anchor_gate.go`'s own header warns about ("a target that does not PASS the
same measurement has established nothing about this candidate"). `internal/tools` is
green, holds the gate, and is where the lane already runs.

## The red proof, and the restore

The probe mutated ONE row's line column in place (a real drift shape) on a copy
seeded first, then both halves had to go red:

| measurement | clean | planted |
|---|---|---|
| `go run … --check` exit | 0 | **1** (2 rows differ) |
| `go test ./internal/tools/ -run TestMergeConfigAnchorGate_RealTreeIsClean` | PASS | **FAIL** |
| the lane's own command `make test-budget PKGS='./internal/tools/...'` | PASS | **FAIL** |

Restore is by **content identity**, not `git diff HEAD` (HEAD held the drifted
bytes, so a correct restore keeps that diff non-empty):

```
$ test "$(git hash-object internal/standards/testdata/config-anchor-census.tsv)" = \
       "$(git hash-object <pre-probe-copy>)" && echo RESTORED
```

The table itself is regenerated through its generator, never hand-edited (Rule 38),
and the regenerate is idempotent (sha256 equal across two runs).

## What this does NOT close

- **`GOPI_TASK_REPORT_PATH`-style runs are not the only push route.** The arm closes
  the close-out lane (which every `gopi` delivery runs). A commit pushed by a route
  that skips that lane is still unchecked; the branch-scoped push guard and the merge
  lane remain the other two halves.
- **`internal/standards`'s own red at HEAD is untouched here** — it is a different
  defect with a different remedy (baseline entries for two new violations), and
  fixing it inside this change would have widened the diff past its subject.
