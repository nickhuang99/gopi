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

# A cited `-run` selector is a CHECK only if the test exists

**Status:** gate landed (`internal/standards/citation_selector_gate.go`), freeze
table + generator (`internal/standards/testdata/citation-selector-baseline.tsv` /
`citation-selector-census.go`), 14 test arms in
`internal/standards/citation_selector_gate_test.go`.

## The defect, measured

`go test ./pkg -run 'TestX'` **exits 0 when no test in `pkg` matches `TestX`**. It
prints `ok <pkg> 0.5s [no tests to run]` and every consumer of the exit status
sees a pass. So a citation used as an acceptance criterion can be a permanent
green that proves nothing — and the more authoritative the page, the longer it
lives.

Measured 2026-10-31 (task `01a19081`), verbatim:

```
$ go test ./internal/standards/ -run 'TestAssertionTotalityAuditGate'  # selector-ok: THIS LINE IS THE MEASUREMENT — the quoted command is the defect, quoted verbatim; the real gate lives in internal/tools
ok  	github.com/nick/gopi/internal/standards	0.318s [no tests to run]
$ echo $?
0
```

The real gate lives in `internal/tools`. The acceptance command named the wrong
package, and nothing in this repository noticed.

This is the third recorded shape of the same failure. PROJECT.md Rule 38 names a
`-run` citation that matched nothing **for a month** (`TestCensusGate`, deleted
with its implementation in `29882b3`), and Rule 42 names a check citing a file
**no commit ever added**. Both were repaired by hand. Neither repair prevented the
next one, because nothing checked the one fact that makes a `-run` citation a
check at all: **does the cited package contain a test that matches the selector?**

`internal/standards/citation_gate.go` mechanised the sibling question for cited
**paths** (does the text name a file that exists at HEAD). This file mechanises
it for cited **selectors**.

## What the gate decides, and what it cannot

Three standing outcomes, plus by-design skips that are **counted, never passed
silently**:

| outcome | meaning |
|---|---|
| `resolved` | some test function in the cited package matches the pattern |
| `vacuum` | the package parses and declares tests, but none matches → the citation is a permanent green |
| `unresolvable-package` | the pattern names no parseable package (`./...`, an out-of-repo path) — nothing was learned, so nothing is claimed |
| `no-test-files` | the package exists and has no `_test.go` at all — nothing was learned either, and it must not be confused with "the test is missing" |
| `prose` / `deliberate` | not a selector (`<anchor gate>`, `the four`, `^$` — `go test -run '^$'` is the documented run-nothing invocation) |

Only `vacuum` fails, unless the shipped table freezes it with a reviewed reason.

**A gate can judge existence. It cannot judge correctness**: whether that test is
the right witness, whether it passes, or what it proves. The report never implies
otherwise, and `subtest-tail` is printed separately for exactly that reason (see
below).

## Three modelling decisions, each paid for by a red test

**1. No subprocess per citation.** The population is thousands of citations
(measured on this tree: 3389 `go test … -run …` occurrences across 2863 tracked
markdown files). Shelling `go test` per citation would be thousands of
compilations — and the device load PROJECT.md Rule 27 bans. So the verdict is
derived by **parsing the cited package's `_test.go` with `go/ast`** and matching
the selector against the declared test-function names. One arm,
`TestCitationSelectorGateAgreesWithRealGoTest`, then **executes** `go test -run <a
name that matches nothing>` for real and asserts the parse agrees with Go's own
`[no tests to run]` — the model is anchored to the surface it models, not trusted.

**2. The markdown backtick is not a shell quote.** First red run: the gate
extracted **zero** citations from a page carrying one, because the tokenizer
treated ``` ` ``` as a shell quote and collapsed
`` `go test ./internal/tools/ -run 'TestRealGate'` `` into one token. In markdown  <!-- selector-ok: TestRealGate belongs to the throwaway fixture module in the test file, not to this tree -->
a backtick **wraps a whole command**; only `'` and `"` group one argument,
because a selector may contain a space. Measured on the real tree: 1157 citations
before the fix, 3389 after — the defect was hiding two thirds of the population,
i.e. a gate that read a third of the tree and called it clean.

**3. Anchors are Go's, not ours.** The first version stripped `^`/`$` from each
alternative of a `-run` pattern before matching. That is wrong in the direction
that HIDES defects: `^TestFoo$` would then match `TestFooBar` (a false
**resolve**), and `^$` — the run-nothing idiom — became a pattern that matched
every test name. `^A|B$` compiles as one regex and splitting it on `|` yields two
equivalent regexes for the only question asked ("does anything match"), so
splitting is safe and stripping is not.

**4. A subtest selector resolves on its HEAD, and says so.**
`-run 'TestStandardGates/TestGate3ElseAfterReturnRedProof'` is judged by
`TestStandardGates`. Subtest names are string literals inside `t.Run` calls and
are frequently built from table data; matching the tail would red correct
citations, which is the one failure a new gate cannot afford. The reading prints
`subtest-tail N` so the report cannot imply coverage it did not have. (16 such
citations on this tree.)

## Non-vacuity: floors in BOTH directions

The printed report is the gate's own census, every run:

```
citation-selector gate: citations walked 3389 across 2863 files (judged 952, exempt-genre 2049, inline 0)
citation-selector gate: skipped — prose/deliberate 137, wildcard-package 119, truncated 132
citation-selector gate: pairs judged 952: resolved 772, vacuum 167 (167 baselined), unresolvable-package 4, no-test-files 9
citation-selector gate: baseline rows 121 (fired 167, stale 0), subtest-tail 16
```

`selectorFloorVerdict` refuses a reading that is too small to be a reading, and
it refuses it from both sides:

- **absolute floors** — files walked, pairs judged, pairs resolved. A walk that
  read three files, or a scan that extracted nothing, fails.
- **a SHARE floor** — `resolved / judged ≥ 50%`. An absolute floor alone lets a
  resolver that stopped finding test names pass on a huge population: resolved
  100 of 20000 satisfies "resolved ≥ 100" while every citation reports as
  vacuum. The share floor catches the parser collapsing, which is a different
  defect from a corpus of dead citations, and the two must not report as one.
- **a table floor** — a 0-row baseline means pre-existing vacuums read as fresh.

Every floor has a red and a green arm in
`TestCitationSelectorGateRefusesToPassBlind`, including the healthy case, because
a floor that reddens a good reading is a gate nobody keeps.

## The freeze table, and why it is 121 rows and not a wall of red

Measured when the gate first ran on the real tree: **167 vacuum citations**, and
they cluster by genre, not by accident —

| directory | vacuum citations |
|---|---|
| `devdoc/` (113 rows, almost all `historical`) | 163 |
| `tasks/` (filed reports) | 4 |
| `briefings/` | 3 |
| `COMMANDS.md` | 1 |

A `historical` devdoc is a **record of a run**: `TestZZProbeSubPredicates` was
real when it ran, and rewriting the page would erase the trail Rule 36 requires
it to keep. So those pages are **exempted by genre and COUNTED**
(`exempt-genre 2049`), which is why 167 live vacuums freeze into 121 distinct
rows: one row per `(citing-file, package, selector)`, and a page often cites the
same dead selector twice.

The rows are **generator-produced**, never hand-written:

```bash
# write the table
go run internal/standards/testdata/citation-selector-census.go . \
    internal/standards/testdata/citation-selector-baseline.tsv

# re-derive without writing (exit 1 on any delta)
go run internal/standards/testdata/citation-selector-census.go . \
    internal/standards/testdata/citation-selector-baseline.tsv --check

# print the derived table, write nothing
go run internal/standards/testdata/citation-selector-census.go . --print
```

`TestCitationSelectorCensusReDerivesTheTable` runs the generator as a subprocess
and requires `--check` to exit 0 against the committed table;
`TestCitationSelectorCensusCheckFailsOnADelta` proves `--check` is not a rubber
stamp by adding a row to a COPY and requiring exit 1 plus the row's name.

**The ratchet runs down only.** A frozen row whose citation no longer fires is an
error naming the row — the citation was fixed, so the permit must go in the same
change — and a fresh vacuum is a failure naming citing file, line, package and
selector. `TestCitationSelectorGateStalePermitIsAnError` and
`TestCitationSelectorGateBaselineAcceptsAFrozenVacuum` are the two arms.

## Red proof, on the real tree

```
$ printf '\nPROBE (temporary): `go test ./internal/standards/ -run %s -count=1`\n' "'TestZZCitationSelectorProbeGate'" >> devdoc/standards-gate.md  # selector-ok: the probe that was planted and removed, quoted verbatim as evidence
$ go test ./internal/standards/ -run 'TestCitationSelectorGateRealTreeIsBaselined' -count=1
        citation-selector gate: pairs judged 953: resolved 772, vacuum 168 (167 baselined), unresolvable-package 4, no-test-files 9
        citation-selector gate: baseline rows 121 (fired 167, stale 0), subtest-tail 16
    citation_selector_gate_test.go:471: internal/standards/testdata/citation-selector-baseline.tsv
        devdoc/standards-gate.md:424 cites go test ./internal/standards -run 'TestZZCitationSelectorProbeGate' — no test in internal/standards matches; that command exits 0 with [no tests to run]  <!-- selector-ok: the gate's own output, pasted as evidence -->
FAIL

# restore, verified by CONTENT IDENTITY against the revision (not by `git diff`,
# which would compare against a HEAD that never held the probe):
$ test "$(git hash-object devdoc/standards-gate.md)" = "$(git rev-parse HEAD:devdoc/standards-gate.md)" && echo SAME_AS_HEAD
SAME_AS_HEAD
$ go test ./internal/standards/ -run 'TestCitationSelectorGateRealTreeIsBaselined' -count=1
ok  	github.com/nick/gopi/internal/standards	5.747s

# and the surface this gate models, executed for real:
$ go test ./internal/standards -run 'TestZZCitationSelectorProbeGate' -count=1  # selector-ok: the executed arm proving the vacuum, quoted verbatim
ok  	github.com/nick/gopi/internal/standards	0.512s [no tests to run]
$ echo $?
0
```

The planted row moved `pairs judged 952 → 953` and `vacuum 167 → 168` and named
the file and line. Nothing else changed.

## What this gate does NOT catch (stated, so it is not mistaken for coverage)

- **A selector that names a test which exists and does not test what the page
  claims.** Existence is not relevance; that is a reviewer's judgement.
- **A subtest tail.** Only the head component is verified (decision 4).
- **A `-run` value that is on another line.** Counted as `truncated 132` on this
  tree and refused rather than guessed — the count is printed so the refusal is
  visible.
- **A package pattern that is not a single repo-relative directory**
  (`./...`, `./a/...`): counted as `wildcard 119`, judged as nothing.
- **Citations in `*.go` / `*.sh`.** The walk reads markdown only: a briefing, a
  rule or a report is where an acceptance command lives. Source comments carry
  the same risk and are a later scope; naming it here is cheaper than pretending
  the walk is exhaustive.
- **A page whose text is not a `go test … -run …` command at all** — e.g. a
  citation written as `gopi task verify`. Not this defect class.

## Check

```bash
go test ./internal/standards/ -run 'CitationSelector|TestStandardGates' -count=1
go run internal/standards/testdata/citation-selector-census.go . \
    internal/standards/testdata/citation-selector-baseline.tsv --check
```

The `-run` selector above is itself subject to this gate: `CitationSelector`
matches 14 test functions in `internal/standards`, so it prints real `=== RUN`
lines. A citation that matches nothing exits 0 and proves nothing — which is the
whole point of the file you just read.
