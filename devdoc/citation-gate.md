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

# The citation gate — a cited witness path must resolve at HEAD

**Gate:** `internal/standards/citation_gate.go`
**Generator:** `internal/standards/testdata/citation-census.go`
**Table:** `internal/standards/testdata/citation-baseline.tsv` (527 frozen rows)
**Selector:** `go test ./internal/standards/ -run 'Citation' -v`

## The defect this exists for

PROJECT.md Rule 42 named `internal/testutil/rule42_destructive_signal_gate_test.go`
and `devdoc/operator-apps-killed-during-suite-runs.md`. The first had **never been
added by any commit** — `git log --all --diff-filter=A -- <path>` is empty — and the
second did not exist either. The rule's own check line therefore ran
`go test -run Rule42`, printed `no tests to run` and exited **0**. A green that is
indistinguishable from a clean tree is the failure shape PROJECT.md Rule 38 names,
and it was found by hand-grep, not by any check.

Task 01a16d08 fixed those two instances and added no prevention. This is the
prevention.

## What it judges, and why the population was chosen by measurement

A gate cannot decide whether a cited path is the *right* witness. It decides one
thing: **does the cited path resolve at HEAD** (i.e. is it in the git index?).

Measured before the gate was written, census at `b9592a4`:

| figure | value |
|---|---|
| distinct repo-relative citations across `*.md` + `*.go` | 4856 |
| unresolved | 827 |
| — internal | 343 |
| — devdoc | 169 |
| — cmd | 165 |
| — tasks | 111 |
| — scripts | 35 |
| — briefings | 4 |
| devdoc corpus | 1645 historical, 800 normative, 13 roadmap |

827 rows of red is not a gate, it is a wall nobody reads to the 828th line. So the
deliverable is **census + baselined table** (PROJECT.md Rule 39's shape), not a
naked red gate.

The one **honest-exempt genre** is the `historical` devdoc class: a record of a run
("the gate named `internal/docclass/evidence.go`") is a true sentence about a file
deleted on purpose, and PROJECT.md Rule 36's teardown section *requires* such pages
to keep quoting what was typed that day. Their citations are exempted **and
counted** — an exemption that hides its own volume is the silent pass this gate is
about. `normative` and `roadmap` pages make a standing or forward claim, so their
citations are judged.

Measured split of missing occurrences by citing genre:

| citing genre | missing occurrences |
|---|---|
| devdoc/historical (exempt) | 1299 |
| devdoc/normative (judged) | 599 |
| Go source (judged) | 296 |
| other markdown (judged) | 122 |
| devdoc/roadmap (judged) | 5 |
| **judged total** | **1022 occurrences over 488 distinct paths** |

## Readout, on the real tree at this revision

Measured at `6f5b698` — the rebased tip the clone channel publishes, re-taken
after the delivery-point sync moved this branch off `b9592a4`:

```
citation gate: files walked 6203 (judged 4558, exempt-genre 1645)
citation gate: cited 34738 resolved 15711 exempted 19027 missing 0
citation gate:   exempt breakdown — genre 17314, table 1712, inline 1
```

The three figures moved by the seven commits the sync pulled in (6200→6203 walked,
4555→4558 judged, 34733→34738 cited, 15706→15711 resolved); the table and the
exempted count did not move, which is the property the table exists to have. A
readout is a reading of a REVISION, so it is re-taken whenever the branch moves —
a figure quoted from a revision the branch no longer stands on is the stale-citation
defect this gate is about, one level up.

`cited` is a partition: `resolved + exempted + missing == cited`. That identity is
asserted by the shape of the code — the counter is incremented once, before the
exempt branches — because the first draft printed `cited 17322` beside
`exempted 18997`, two figures from different populations that a reader could not
add up. It was caught by the real-tree run, which is the reason that run prints its
numbers.

## The table, and its four classes

One row per distinct pre-existing miss, `path<TAB>class<TAB>reason`. Classes:

| class | meaning | rows |
|---|---|---|
| `fixture` | a fabricated literal only a test or generator plants (`devdoc/x.md`, `internal/foo/foo_test.go`); a fixture that existed could not be told from a real file | 108 |
| `removed` | tracked once and deleted by a commit; the citing page still makes a standing claim | 112 |
| `unresolvable` | no commit ever tracked it — typically an abandoned report path | 303 |
| `retired` | the citing site is Rule 36's retired-mechanism exemption: PROJECT.md/CONFIG.md naming a mechanism this repository has since removed, *in order to say it is gone* | 4 |

`class` is **derived from evidence on every run** (does a commit delete it? is every
citing site a `_test.go`?) — with exactly one curated exception, `retired`, because
"this document names it in order to say it is gone" is a judgement no machine can
make.

`reason` has **one authority per row, decided by its AUTHOR** — and the two kinds
are not the same field:

- a sentence the **generator** writes (the three frames above, recognised by
  `reasonDerived`) states the **KIND of miss** and carries **no per-row figure**;
- a sentence a **human** wrote is preserved **byte for byte**, because it is the
  review the table exists to carry.

It took two wrong shapes to get there, and both are recorded because each is the
obvious next thing to try.

**Shape 1 — a machine figure frozen in the column.** The generator used to preserve
every reason verbatim, on the theory that "the reason is a human's sentence". True of
the four `retired` rows and the curated permits, false of the ~521 rows it wrote
itself. On those the preserved column held the site count, which no run re-derived,
so the number froze and then drifted as documents were fixed:
`internal/tools/memory_store_trigger.go` read `12 standing citation site(s)` at one
tree and `11` at the next, while `--check` stayed green. Measured on a COPY of the
shipped table whose count was mutated to 99, the pre-fix generator printed

```
citation-census: CHECK OK at d11b937 — 527 frozen misses, 6677 tracked paths, cited 34695 resolved 15587 exempted 19108
exit=0
```

**Shape 2 — re-deriving that count on every run.** This is the fix one reaches for
first, and it was measured before it shipped: it is worse. This column is compared
byte for byte, so making the row a function of the current tree makes every
unrelated edit rewrite it.

```
$ git commit -m "probe: unrelated doc mentions internal/x.go"
$ go run internal/standards/testdata/citation-census.go . internal/standards/testdata/citation-baseline.tsv --check
citation-census: 1 row(s) should be ADDED/CHANGED, 0 should be REMOVED
exit=1
```

One unrelated sentence in one document turned the gate red, and the only way back to
green would be to regenerate — the "red means regenerate" degeneration this table
exists to prevent. **A live figure inside a compared artifact is churn, however
honestly it was measured.**

**The shape that holds.** The row states the evidence (which kind of miss this is)
and the RUN states the counts. `--check`, `--print` and the write leg all print the
live per-path site census on their output:

```
citation-census: site counts (live, never stored — the row states the KIND of miss):
  internal/tools/memory_store_trigger.go	17 citing site(s) (2 in tests, 15 in documents)
```

Nothing compares that line, so nothing goes red when it moves — and a reader still
gets the figure on every run. Same tree, same probe, count-free shape: `CHECK OK`,
exit 0.

A column with two authorities stops being a reading and becomes a rumour
(PROJECT.md Rule 38). Three arms in
`internal/standards/citation_reason_authority_test.go` pin the result:
`TestCitationCensusReasonCarriesNoLiveMeasurement` (no stored reason may match a
count-bearing sentence, and the generator's own sentence carries no digit — red on
both earlier shapes), `TestCitationCensusMigratesACountBearingReason` (a table written
by the count-bearing revision is re-derived, not preserved forever — red when the
legacy recognition is removed) and `TestCitationCensusPreservesACuratedReason` (the
floor that stops all of this from becoming "re-derive the whole column").

The table **ratchets down only**: a row whose path no longer appears as a miss is
an ERROR, not a harmless leftover (Rule 39's precedent, `citationJudge`'s `Stale`).

## Non-vacuity (Rule 38)

Three floors, each of which can independently refuse a green:

- `citationMinFilesWalked` (500) — a walk that read 12 files did not read the repo;
- `citationMinCitations` (1000) — a walk that read every file and extracted nothing
  is broken, not clean;
- `citationMinTableRows` (1) — an emptied freeze makes every historical miss look
  fresh.

`TestCitationGateRefusesToPassBlind` disables each dimension in turn and requires
the refusal, plus a **clean control** that must NOT be refused — otherwise the arms
would pass merely because the floor function always fails.

## Generator

```bash
# regenerate (writes the table)
go run internal/standards/testdata/citation-census.go . \
    internal/standards/testdata/citation-baseline.tsv

# re-derive WITHOUT writing: exit 0 on the committed table, 1 on any delta
go run internal/standards/testdata/citation-census.go . \
    internal/standards/testdata/citation-baseline.tsv --check

# print the census and write NOTHING (the reading to take when the worktree
# may not be written to); combinable with --at REV to read the OBJECT STORE
go run internal/standards/testdata/citation-census.go . --print
go run internal/standards/testdata/citation-census.go . --print --at REV
```

**The generator's argv is a CLOSED vocabulary** —
`<repo-root> [<table.tsv>] [--check | --print] [--at REV] [-h|--help] [--]`, the
same spellings `config-anchor-census.go` accepts. Any other `-`-leading word is
refused BY NAME before anything runs, and so is a positional path whose base name
begins with `-`. That is not tidiness: before the vocabulary existed the second
POSITIONAL was the table path, so `--print --at <rev>` — the spelling the sibling
generator documents, and the remedy refusal text elsewhere names — became the
table FILENAME and the program wrote a 66 KB file literally named `--print` into
the caller's worktree, exit 0 (Rule 30 residue whose removal needs
`rm -- ./--print`). `--at REV` also had no meaning: it is real now, reading the
index with `git ls-tree REV` and the bytes with `git show REV:path`, never the
checkout (Rule 29). The mechanism is `devdoc/citation-census-argv.md`; the
regression arms are `TestCitationCensus{RefusesAFlagItDoesNotKnow,
RefusesAMisspelledTablePath, RefusesAValueFlagWithNoOperand, HelpWritesNothing,
PrintLegWritesNothing, AtReadsTheRevisionNotTheWorktree}` in
`internal/standards/citation_census_args_test.go`.

`--check` exits 0 against the committed table and 1 after an artificial delta,
proved both ways by `TestCitationCensusReDerivesTheTable` and
`TestCitationCensusCheckFailsOnADelta` (the latter appends a fabricated row to a
*copy* and requires `CHECK FAILED` to name that row).

### The `--check --at REV` contract: ONE TABLE DESCRIBES ONE TREE

A `--check` is a claim about a **pair**: the rows a tree yields, and the frozen
table that says which misses that tree tolerates. `--at REV` decides BOTH halves
or the pair describes two trees at once:

| spelling | the tree | the frozen table |
|---|---|---|
| `--check` | this worktree | this checkout's copy of the table |
| `--check --at REV` | REV (`git ls-tree REV`, `git show REV:path`) | **REV's own copy** (`git show REV:<table>`) |

Before that contract the rows came from REV and the expectations from the
checkout, so `--check --at <rev>` was red for every rev whose tree differed from
the checkout's — a newer integration tip, an older release, a peer's branch. That
red carried no work: it said "these are different trees", which the caller already
knew by passing `--at`. Measured on this repository at a revision 400 commits back
(`--check --at 86b3eba`): `11 row(s) should be ADDED/CHANGED, 50 should be
REMOVED` — pure cross-tree noise, on a table that described its own tree exactly.

Three consequences, and each is its own exit status:

- **`--at REV` never writes** (exit 2, a run that cannot happen). A reading taken
  at REV describes REV's tree; writing it here would freeze another tree's row
  population under this tree's name — green for REV, red for the worktree the
  table is committed in. `--print --at REV` is the read-only leg.
- **A rev that carries no table is exit 2**, never exit 1: a missing subject is
  not a row delta, and reporting it as drift would blame a tree for a missing
  file.
- **The provenance ancestry anchor is REV** (`merge-base --is-ancestor <cited>
  <REV>`), not this checkout's HEAD — with `--at REV` the tree, the table and the
  reading are all REV's, so anchoring at HEAD would refuse a re-derivable table
  because a DIFFERENT branch happens to be checked out.

A zero-row delta now prints what actually differed (the header lines), because the
ordinary shape of `--check --at <older rev>` is "the tree is fine, the table
committed there was written by an earlier revision of this generator". It stays
exit 1 — a regeneration would rewrite the file — but the reader is told which half
disagreed instead of hunting a tree defect that does not exist.

**"Self-consistent" is not "always green", and the difference must be stated or
this contract will be read as a promise it does not make.** The verdict still
depends on TWO things: the revision, and the version of the generator taking the
reading. What the contract removes is the THIRD input — which branch happens to be
checked out. So:

| the reading can be red because | yes? |
|---|---|
| the checkout moved to another branch | **no** — the contract removes this input |
| the generation rules changed since the table was written | yes — re-derive |
| the table in that revision genuinely does not describe that tree | yes — real drift |

Measured on this repository, `--check --at` a revision 400 commits back is exit 1
with `519 row(s) should be ADDED/CHANGED` and rows diffing only in the
REASON sentence (`fabricated literal planted by 1 test site(s)` → `fabricated
literal planted by a test or a generator`, the rule-13 wording migration). That is
the middle row of the table and it is a true verdict: today's generator would
rewrite that table. It is not cross-tree noise — it is a statement about that
revision alone.

Pinned by `internal/standards/citation_census_args_test.go`:
`TestCitationCensusCheckAtComparesTheRevisionsOwnTable` (green at the base AND the
tip of ONE fixture — on the pre-contract generator the base arm is red:
`--check --at <base> exited 1`),
`TestCitationCensusRefusesToWriteFromAnotherRevisionsReading` (exit 2, tree
untouched) and `TestCitationCensusRefusesACheckOfARevisionCarryingNoTable`
(exit 2, not 1).

## A defect this gate found while being written

The gate's first real-tree run reported a malformed doc-class banner:

```
briefings/brief-capture-list-windows-from-master.md: "brief"
```

The page is fine — `briefings/` carries its own header vocabulary, and PROJECT.md
Rule 36's class list belongs to the **devdoc corpus and nothing else**. The bug was
the gate's: judging a non-corpus page against the corpus's closed vocabulary reports
another directory's convention as this rule's defect. The banner check is now scoped
to `devdoc/` (`citationCorpusPrefix`), and the fix is pinned by the fixture's
`devdoc/typo-banner.md`, which requires a *misspelled* banner to fall back to
**judged** rather than to exempt — a typo in line 1 must never be the quiet way to
switch a page out of the population.

## What this does not do

- It does not judge whether a citation is the *right* witness — only that it exists.
- It does not clean the 488 frozen paths. That is the epic's later scope, and the
  table is how it stays visible instead of buried.
- It does not scan `briefings/`' own conventions as corpus classes, nor vendor /
  node_modules trees (third-party source is not this repository's claim).
