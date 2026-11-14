<!-- doc-class: historical -->
# Registering `gopi doccheck`: two defects behind one red package

Truth class: **dated record** — what was red on master, what was fixed, and what
the measurements were. It is not a standing claim about master; the standing
claims live in `devdoc/gopi-verb-census.md` and in the gates it names.

Task `01a0df40-55c3-7659-801b-bc82f5e3b92b`, commit `7ab03fc8`, 2026-09-27.

## The briefing was stale, and saying so was the first finding

The briefing said `doccheck` (commit `c804d68e`) was "registered in none of the
sibling vocabularies, leaving 8 test reds". A prior task
(`01a0def2-45cb-71ed-9d47-4f9eca55a350`, commit `c143c05b`) had ALREADY fixed
four of them — `PositionalVerbs`, the `allVerbs` token table, the completion
want-list, and the census row. Measured before touching anything:

```
git merge-base --is-ancestor c143c05b HEAD   # → is an ancestor
```

So the diagnosis was re-derived from the tree instead of the briefing: run the
gates and read which side lost the verb. Three real reds remained in
`cmd/agent`, plus one the briefing never named.

## Defect 1 — the verb was dispatched but not advertised (3 co-failing gates)

`doccheck` was in `topLevelCommandNames` and in the completion dispatcher map,
but absent from `internal/config/help.go`'s `commandIndex`. Three gates failed
on that single omission:

| gate | what it said |
|---|---|
| `TestTopLevelUsageAdvertisesEveryDispatchedVerb` | `gopi doccheck` is dispatchable (DocCheckSubcommand in main.go) but missing from the top-level command index — add it to config.commandIndex (memory #1252: an undiscoverable subcommand is a P0) |
| `TestTopLevelHelpCoversEveryDispatchedSubcommand` | predicate `DocCheckSubcommand` has no help mapping; `topLevelCommandNames` offers "doccheck" but gopi -h never mentions it |
| `TestVerbCensusMatchesTheDispatch` | the census row's `advertised` cell disagreed with the binary's own `-h` |

The fix is two places: the index row in `internal/config/help.go`, and the
**predicate → help-word map** in `cmd/agent/usage_test.go`. That map is the
CLASS-level half — a new dispatcher without an entry fails there by name, which
is what kept this defect from being a one-off. The doccheck detail itself stays
in `gopi doccheck -h`; the index row is one line pointing at it.

## Defect 2 — the page was ALREADY over budget before the new row existed

`topLevelUsageMaxLines = 160` is the assertion, not a suggestion, so it was not
widened. Measured on the pre-change binary (`gopi -h | wc -l`):

```
176    baseline (no doccheck row yet)
166    after adding the row and trimming the intro + Inputs
157    final
```

The page has to make room for a new command by giving up detail, and the trim
had to be surgical: three claims were test-pinned and were RESTORED after the
first trim pass removed them —

- `gopi vision_analyze <out.png>` (`TestUsageAdvertisesShot`),
- `under $GOPI_HOME` in the `--home` row (`TestTopLevelUsageShowsNativeImageInput`),
- the `gopi memory backup` half of the memory row
  (`TestTopLevelHelpCoversEveryDispatchedSubcommand`).

**The lesson for the next trim**: the top-level page's prose is partly a
contract. Trim, then re-render and re-run the pinned gates — do not assume a
shortened sentence is free.

## Defect 3 — the census READER broke on a constant that moved a day earlier

`bash scripts/g7-verb-census.sh` refused to run at all:

```
g7-verb-census.sh: constant CliVerbMerge (from isMergeSubcommandSubcommand) resolves to no verb word
```

and `TestVerbCensusMatchesTheDispatch` was red on exactly one verb: `merge`.

**Root cause.** RP-1 (commit `ee8aff27`, same day) introduced the leaf constant
package `internal/cliverbs` and made `cmd/agent`'s `CliVerbMerge` an ALIAS
(`CliVerbMerge = cliverbs.Merge`). Claude the leaf is *deliberately
prefix-free* — its Rule 14 point is unshadowed bare names (`Merge`, `Log`,
`Task`, `UI`) — while BOTH census readers recognised a verb constant only by the
`CliVerbX` / `ToolX` prefix. The alias resolution then looked up
`cliverbs.Merge` in a map that could never contain it, and the census reported
the predicate as naming no verb.

Two consequences, and the second is the one worth remembering: the census could
not be REGENERATED (the script is the documented recipe, and it exited 1), so
the checked-in table could only have been hand-edited — the exact thing its own
header forbids.

**Fix, in both readers** (`scripts/g7-verb-census.sh`, `cmd/agent/cli_verb_census_test.go`):
scan `internal/cliverbs`, and for that package alone accept a prefix-free
constant whose LITERAL has the shape of a verb word (lowercase, ≥2 chars, no
leading dash, no whitespace). The name rule is scoped by package
(`censusVerbWord(root, name, value)`), so it cannot widen the map of an
unrelated package that happens to hold an uppercase constant.

**Verification that the repair is real, not cosmetic**: the same script now
reproduces the checked-in table's class distribution exactly — 37 verbs, 32
`a:test`, 1 `c:neither`. Its numeric columns are GOPI_HOME-corpus-dependent
(`usage_log` reads `$GOPI_HOME/sessions`, `doc_recipe` walks `devdoc/`), so the
run's counts differ from the checked-in ones for every verb; only the row that
actually CHANGED (`doccheck`: `advertised` 0 → 1) and the prose were taken from
it. That rule is the census's own documented pitfall and it still holds.

## What this run did NOT do

- **`TestToolsMaintenanceHelpSurface*`** (the briefing's out-of-scope 9th/10th
  names) were **left red**. They are NOT this block: the derivation reports
  `` `gopi task.txt` is advertised by the help ((top-level)) but the inventory
  does not list it ``. `scripts/g6-top-level.awk` emits one row per Inputs row
  whose entry column is ≥16, and the `task.txt` row's NAME is exactly 14
  characters of column, so the row's prose begins before col 16 and the form is
  cut to `task.txt` — which the inventory (generated when the row was shaped
  differently) does not list. Widening the awk's 16-column rule would change
  every page it reads; that is a G6 decision, not this one — and this
  run's own 16-column reading was later REFUTED (awk is ruled out), so the
  mechanism is left UNLOCALISED rather than guessed. Filed as its own
  work item.
- **The `Ran:`-line ceiling on this row's class.** `doccheck` stays `c:neither`
  in the census: its own suite drives it in process (`runDocCheckInProcess`) and
  a help page is never a run (Rule 31). A binary-drive test is what would move
  it, and it does not exist.
- **No new dependency.** No script, daemon, unit, state file or cron line was
  added (Rule 35). The two edited files are an existing reader and an existing
  gate.

## Verification (all on the committed tree, binary rebuilt from it)

```
go test ./cmd/agent/ -run '<the 7 gate names>' -count=1     # PASS, 3 consecutive reps
go test ./internal/standards/ ./internal/config/ -count=1   # both ok
bash scripts/g7-verb-census.sh ./bin/gopi                   # exit 0, 37 verbs / 32 a:test / 1 c:neither
./bin/gopi --timeout 3 doccheck                             # exit 1, refused BY NAME, 0 new sessions
./bin/gopi doccheck                                         # exit 0, "corpus is consistent"
./bin/gopi doccheck -h                                      # its own page
./bin/gopi -h | wc -l                                       # 157 (budget 160)
```

## Whole-package A/B: none of the package run's reds is this diff's

The whole `cmd/agent` suite ran twice as background jobs (`-count=1`, ~604s each).
Every red was A/B-checked against this branch's base `595bd8bd` in a **detached**
worktree with `bin/gopi` built THERE — required, because without the binary the e2e
tests SKIP and the A/B says nothing (a first attempt did exactly that).

| red | this branch | base `595bd8bd` | verdict |
|---|---|---|---|
| `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation` | FAIL | FAIL (same message) | pre-existing → `01a0df57` |
| `TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason` | FAIL | FAIL (same message) | pre-existing → `01a0df57` |
| `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | FAIL | FAIL (identical `HEAD` refusal) | pre-existing → corroborates `01a0df40-5617` (an unborn `HEAD` in the e2e fixture repo, so the ground refusal is correct) |
| `TestChatCLIRealBinaryRefusesAnUnknownFlag` | FAIL (job-4) / PASS (job-5) | PASS | flake — passes in isolation here (`-run` pair → ok) |
| `TestMergeEveryLoopWritesNoStateFile` | FAIL (job-4) / PASS (job-5) | PASS | flake — passes in isolation here |

The diff touches only `cmd/agent/cli_verb_census_test.go`, `cmd/agent/usage_test.go`,
`internal/config/help.go`, `scripts/g7-verb-census.sh` and devdocs; the e2e red's file
appears in **no** file this branch touches.
