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

# The census gate is NOT heavy — the epic's premise is false, and the real cliff is the package

**Status:** no test was reclassified and no gate was weakened. The claim this task
arrived with — 13 arms shelling a 102 KB generator 37 times at 21–178 s per arm, so
a bare `go test ./internal/tools/ -run TestMergeConfigAnchorGate` panics at go's 10
minute timeout — **could not be reproduced on any axis** (per arm, per run, cold GOCACHE,
or generator-only). What the measurements did find is real, and it is a different
defect with a different owner.

**Measured at:** `153c41b4398aaab5012c5bbc6af3250326bee9c7` (this ground's branch
`task/01a1a76a16907df89627a62d28f67e91`, cut from the peer's master), clean tree,
`git status --porcelain` empty, 32 cores, load average `1.18` for §1's default-path
run and `6.72` for the cold-cache and whole-package runs. Everything below is a
`go test` / `go run` invocation with its own output — nothing here is estimated.

## 1. The gate on the DEFAULT path: 13 PASS, 0 FAIL, 9.8 s

```
$ go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1
ok  	github.com/the operator/gopi/internal/tools	9.790s                     # exit 0, wall 11 s
$ GOPI_HEAVY_TESTS=1 go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1 -v
13 PASS, 0 FAIL, 0 SKIP                                            # exit 0, wall 16 s
```

Re-run as the acceptance check, from the CLEAN tree of the commit that carries this
file (load 4.28 and 2.67 respectively — the box's own ambient work, not load this run
generated):

```
$ go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1
ok  	github.com/the operator/gopi/internal/tools	85.510s                    # exit 0, no FAIL
$ GOPI_HEAVY_TESTS=1 go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1 -timeout 45m
ok  	github.com/the operator/gopi/internal/tools	86.795s                    # exit 0, 13 PASS / 0 FAIL
```

85.5 s is 14 % of the 10 minute default. The spread between `9.790 s` and `85.510 s`
for the identical tree is §2's point: this gate's wall time tracks the host's
ambient load, not its own work.

Per-arm wall times from that `-v` run (`--- PASS:` lines, slowest first):

| arm | wall |
|---|---|
| `ACommittedCitationOfNoObjectIsRefused` | 1.67 s |
| `AFailureCarriesTheRepairCommand` | 1.61 s |
| `TheRefusalReportsWhatTheRevisionHolds` | 1.26 s |
| `MeasuredAtProvenanceIsAStateNotASentence` | 1.22 s |
| `AnAbsentRevisionIsUndecided` | 0.88 s |
| `AnUnreferencedCitationIsRefused` | 0.60 s |
| `ARePointedCitationIsAStaleRecord` | 0.60 s |
| `ReportsTheOffendingPathAndLine` | 0.57 s |
| `DiagnosesAreWellFormed` | 0.45 s |
| `RealTreeIsClean` | 0.34 s |
| `WriteLegRefusesAReadingThatIsNotACommit` | 0.17 s |
| `RepairAloneIsRefusedAndWritesNothing` | 0.17 s |
| `ShippedTableParsesAndClearsTheFloor` | 0.00 s |

**"It is fast because the build cache was warm" is not the explanation, and it was
measured rather than assumed.** An isolated, empty `GOCACHE` — so every `go run`
pays a full compile of the 102 KB generator, which is what a fresh CI box pays:

```
$ GOCACHE=<empty dir> go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1
ok  	github.com/the operator/gopi/internal/tools	15.174s                     # exit 0, wall 35 s
```

`15.174 s` against a 10 minute cap. Both runs are in this run's evidence directory
(`acc-default.log`, `full-v.log`, `coldcache-run.log`).

## 2. The generator's own cost, measured directly

The premise's per-arm number implies the census run itself costs tens of seconds.
It costs a third of a second:

```
$ go run internal/standards/testdata/config-anchor-census.go --check . internal/standards/testdata/config-anchor-census.tsv
  five consecutive warm runs: 386ms 393ms 380ms 383ms 408ms
$ GOCACHE=<empty dir> go run …  (same arguments)
  2864ms                                                           # cold compile + run
```

The generator is not a tree walk. It reads `CONFIG.md` and the files that document
cites (`os.ReadFile` at `internal/standards/testdata/config-anchor-census.go:526`,
`:563`, `:1525`) and it has never contained a directory walk; `--at <rev>` reads a
revision out of the object store and measured `0.22–0.25 s` at every historical
revision tried (`rev#1`, `#10`, `#20`, `#30`, `#40`). The test file shells it 29
times in total across all 13 arms, not 37 times into one arm, and most of those
shells are on temporary mutated copies of a table a few rows long.

So the only way to reach "21–178 s per arm" on this gate is a host under ambient
contention — and that is measurable without manufacturing it. The same 13 arms, the
same clean tree, re-run at load 2.67 (nothing of this run's doing: Rule 27 forbids
generating load to prove a duration, so the figures are whatever the host was
already doing):

```
$ GOPI_HEAVY_TESTS=1 go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1 -v -timeout 45m
13 PASS, 0 FAIL                                            # exit 0, `ok … 76.487s`
single slowest arm: MeasuredAtProvenanceIsAStateNotASentence 15.72s   (was 1.22s)
             next: ACommittedCitationOfNoObjectIsRefused    10.77s   (was 1.67s)
```

**Per-arm cost is ambient-sensitive by up to 13×** (`1.22 s → 15.72 s` for the same
arm), so `21–178 s` is a plausible reading of THIS gate on a box carrying several
other agents' suites — and it is a duration, never a verdict: the arms were 13 PASS
at every load measured here. What the contention changes is which REMEDY is right:
under it the whole package is already past the cap (the header of
`scripts/test-budget.sh` records `internal/tools 600.3xx s` at load 42-63), so the
answer is the declared budget and a timeout that names itself — not a reclassification
that would also remove the gate from the merge/verify path (§4).

## 3. What IS real: the 10 minute cliff belongs to the whole `internal/tools` PACKAGE

This is the defect the epic felt, and it is not this gate's:

```
$ go test ./internal/tools/ -count=1                                # load average 6.72, 32 cores
ok  	github.com/the operator/gopi/internal/tools	397.080s                    # exit 0, wall 404 s
```

**397 s at a modest load is 66 % of go's 10 minute default**, with zero headroom
left for the case `scripts/test-budget.sh` was written about — its own header
records the measurement:

> the same tree that finishes internal/tools in ~189-291 s on a quiet box takes
> >600 s when other agents' suites are running (measured: FAIL
> `internal/tools 600.3xx s` at load 42-63)

A whole-package run crossing 10 minutes produces `panic: test timed out` and
`FAIL github.com/the operator/gopi/internal/tools 600.3xx s`, which reads exactly like a
failing assertion while proving nothing about the code.

**That defect already has its owner, and the owner is not heavytest**: it is the
script that owns the budget (`scripts/test-budget.sh`), which is why the lane's
documented command for this package is `make test-budget PKGS='./internal/tools/...'`
— the same command `CONFIG.md:1222` already publishes for this gate's package, and
the same one `devdoc/config-anchor-census-gate-scope.md:125` calls "the lane's own
command". Classifying 12 arms of this gate as heavy would have removed ~13 s of
`-run`-filtered work from a package whose cost is elsewhere, and left the 397 s
untouched.

## 4. Why classifying these arms would have been worse than doing nothing

`internal/heavytest`'s criteria are five named constants
(`internal/heavytest/heavy.go:78-96`) and a test is classified heavy when one of
them is TRUE — not when someone wants it out of the default path. For a 1.5 s arm
that shells a cached binary reading one markdown file, the only candidate is
`CriterionDeviceExec` ("drives a real external process end to end"), which is true
in form and false in cost: the `disk-fixture` criterion says it in so many words
— *minutes* of real device work. Rule 27's own text is that heavy tests are
**classified, not tolerated**, and the classification has a cost the epic did not
price: a `heavytest.Require` arm is excluded from the merge/verify path, so the
`internal/standards` inventory gate would then guarantee that **the only
default-path check over a 228-anchor census never runs on a default run**. A drift
in `CONFIG.md` would be invisible to every branch-scoped and smoke run, which is
precisely the hole `finishCleanupBranchCensusCheck` was added to close
(`CONFIG.md:1218`).

That is the shape this repository has already named: a change that moves a test off
a path without changing what is measured is motion, not a fix.

## 5. What a checker should run

| purpose | invocation | measured |
|---|---|---|
| the arms alone, default path | `go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1` | exit 0, `9.790s` |
| the arms with full coverage and headroom | `GOPI_HEAVY_TESTS=1 go test ./internal/tools/ -run TestMergeConfigAnchorGate -count=1 -timeout 45m` | 13 PASS, 0 FAIL, 16 s |
| the package, through the declared budget | `make test-budget PKGS='./internal/tools/...'` | the lane's own command; budget declared once in `scripts/test-budget.sh` |
| the gate alone, no test framework | `go run internal/standards/testdata/config-anchor-census.go . internal/standards/testdata/config-anchor-census.tsv --check` | exit 0, 0.39 s |

`CONFIG.md` still needed no edit for deliverable (c): **it publishes no bare
`go test` for this gate at all** — `grep -n 'go test' CONFIG.md` matches two table
rows about other knobs, and `CONFIG.md:1214` names the arms while `CONFIG.md:1222`
gives the budget invocation. The bare form appears only in `devdoc/` as dated
evidence of past reds, each inside a page whose own text is a record
(`devdoc/delivery-target-name-space.md:1` still instructs readers, and its command
still works — measured above, 9.790 s).

## 6. The acceptance criterion carried a vacuum, and that part IS a defect

`go test ./internal/tools/ -run 'HeavyTestInventory'` — one of the four commands
this task's acceptance names — matches **no test at all**:

```
$ go test ./internal/tools/ -run 'HeavyTestInventory' -count=1
ok  	github.com/the operator/gopi/internal/tools	3.959s [no tests to run]      # exit 0
```

It exits 0 having executed nothing, which is the green Rule 36 and Rule 38 both
warn about: a named check that does not exist cannot be told apart from a clean
tree. The inventory gate is one test, in the other package:

```
$ go test ./internal/standards/ -run HeavyTest -count=1 -v
=== RUN   TestHeavyTestInventoryMatchesItsCallSites
--- PASS: TestHeavyTestInventoryMatchesItsCallSites (0.63s)
```

It is green in both directions today because **no row was added and none was
removed** — the inventory is unchanged, which is the correct state for a task that
reclassified nothing.

## 7. Not done, and why

- **No `heavytest.Require` call was added to any arm** (§4) and **no row was added
  to `testdata/heavy-tests.tsv`** — a row for a 1.5 s test would fail the inventory
  gate's own criterion check, and it would be a false classification rather than a
  skip.
- **No arm was deleted or filtered out**, so coverage is exactly what it was.
- **The whole-package budget was not changed** (§3): it is `scripts/test-budget.sh`'s
  single-source declaration, and re-measuring it is a separate task with its own
  evidence — `devdoc/tools-25m-budget-remeasure.md` and
  `devdoc/cmd-agent-package-budget.md` are where that work lives.
