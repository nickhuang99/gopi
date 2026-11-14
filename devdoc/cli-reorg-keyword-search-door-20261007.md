<!-- doc-class: historical -->
# Task Report: the keyword_search family's test driver follows its door (re-derived on current master)

**Task** `01a114cb30837be6a0ca420687a7ee75` (re-derivation of archived
`01a11461-daeb-7287-94a4-de9ff55fc844`, baseline `3255973cc`),
branch `task/01a114cb30837be6a0ca420687a7ee75`, re-derived against master tip
`93848040e` (**post**-`6685f7f83`, which rewrote the same file's drives for two of
its three call sites).

**Why a re-derivation and not a hunk resolution.** The archived branch is
unmergeable: `cmd/agent/cli_keyword_search_test.go` collides **semantically** with
master's post-`6685f7f83` file — master moved the *same* call sites a *different
way*, so a three-way merge would have produced a file with one door form in some
sites and another in the rest, both "applying cleanly". The archived branch was
therefore read **as information only** (`git show e5f5ff8cd:…`; the tip is still
in the object store, Rule 23), never committed on and never checked out.

## Completed

- **Diagnosed the family's red as a stale TEST DRIVER, not a product defect.**
  Measured on the real binary at this commit (Rule 31):

  ```
  $ ./bin/gopi tool keyword_search EUAUDIT_NEEDLE_ZZZ --root $D     → rc=0
  …/_tmp/tmp.RxfvJOvEno/fixture.txt:2: EUAUDIT_NEEDLE_ZZZ lives here
  $ ./bin/gopi keyword_search EUAUDIT_NEEDLE_ZZZ --root $D          → rc=1, by name
  gopi: `keyword_search` is RETIRED — use `gopi tool keyword_search` (`gopi tool keyword_search -h` for its help). …
  ```

  The flat word is refused **by name** (the current, intended implementation —
  `devdoc/gopi-verb-census.md` records the demotion as LANDED). A test that drives
  the flat word while asserting a *dispatch* reads that refusal as "the feature is
  missing" — which is how a run-path test stays green while testing nothing.

- **Master's own state, exactly (this is what made the rework non-trivial).**
  `6685f7f83` already moved **two** of the family's three drives to the door, but
  spelled the leaf word as a bare literal:

  | site (master, pre-this-change) | form |
  |---|---|
  | `TestKeywordSearchHelpPageDiffersFromTopLevel` | `probeRaw(t, bin, home, CliVerbTool, "keyword_search", "-h")` |
  | `TestKeywordSearchRunPrintsHitsAndNoMatches` (hit) | `argv := []string{CliVerbTool, "keyword_search", needle, "--root", root}` |
  | `TestKeywordSearchRunPrintsHitsAndNoMatches` (miss) | `probeEnv(t, bin, home, CliVerbTool, "keyword_search", "EUAUDIT_NOTHING_HERE_ZZZ", …)` |

  So the archived branch's diff could not be applied verbatim: all three sites were
  **re-derived from master's own text**, each moving only the leaf word onto the
  constant that owns it:

  | site | after |
  |---|---|
  | `TestKeywordSearchHelpPageDiffersFromTopLevel` | `probeRaw(t, bin, home, CliVerbTool, toolnames.ToolKeywordSearch, "-h")` |
  | `TestKeywordSearchRunPrintsHitsAndNoMatches` (hit) | `argv := []string{CliVerbTool, toolnames.ToolKeywordSearch, needle, "--root", root}` |
  | `TestKeywordSearchRunPrintsHitsAndNoMatches` (miss) | `probeEnv(t, bin, home, CliVerbTool, toolnames.ToolKeywordSearch, …)` |

  **Not one assertion changed** — same page-vs-top-level comparison, same
  `fixture.txt:2:` hit-shape check, same `No matches.` + non-zero-exit miss check,
  same `assertNoCLIContractSession` no-fallthrough checks. Nothing was relaxed to
  reach green (Rules 15/20). The door words come from the constants that own them
  (`CliVerbTool`, `cmd/agent/cli_verbs.go`; `toolnames.ToolKeywordSearch`,
  `internal/toolnames/names.go`) — never re-typed (Rules 1/13/14).

- **Added the family's regression guard (Rule 10), because the generic gates do
  NOT cover this shape — MEASURED, not assumed.** `TestKeywordSearchFamilyDrivesNoRetiredSpelling`
  (same file), scoped to the **raw/unmapped probes** this file uses
  (`runCLIContractProbeRaw`, `cliContractProbeWithTimeout`,
  `runCLIContractProbeEnvTimeout`), read with `go/parser` at the **probe-call
  level**. Its vocabulary is **read, never copied**: retired words from
  `verbAliasWords()` (`cli_verb_aliases.go`; the flat leaf spelling is a row in that
  table, which is why no second list exists here), the door from `CliVerbTool` +
  `toolnames.ToolKeywordSearch`. It carries **both** halves needed to be
  non-vacuous — the no-retired-word half and the *file still drives the family*
  half (`doorDrives == 0` ⇒ error), so a file that stopped calling the binary
  would fail rather than pass (Rule 15).

- **The guard's shape was CORRECTED BY MEASUREMENT, and the correction is the
  interesting part.** The archived branch's guard read **argv[0] alone**. Its first
  red proof here re-introduced the stale drive in the form `CliVerbTool,
  "keyword_search"` — master's own pre-change form — and **the argv[0]-only guard
  PASSED on it**: the group word was still the constant, so the first word looked
  correct and the retired leaf slipped through in **second** position. An
  argv[0]-only guard cannot catch the very defect this file was in. The guard now
  reads the **drive head** (the leading word plus, when the first word is the group
  door, the leaf word that follows it), which is where a spelling can be
  re-typed — while the words after the head stay payload (a query needle, a flag),
  where a word equal to a retired spelling is a *query*, not a drive. Reading the
  whole argv instead was tried in the same sweep and produced exactly those two
  false positives on the fixed file, so it was rejected for the same reason the
  false negative was.

## Not Done (honest)

- **A pre-existing red on master, out of this row's scope, reported not
  repaired.** `go test ./internal/standards/ -run 'Doc|Census'` fails
  `TestCommandsDocGeneratedBlockMatchesSource` (and the red-proof test that has a
  green-block precondition, `TestCommandsDocGateRedProofStaleGeneratedBlock`)
  because `COMMANDS.md`'s generated verb index is stale at line 10
  (`cmd/agent/cli_checkpoint.go:167` committed vs a regenerated `:185`, 44 verbs).
  **Measured as master's own content, not this change's effect**: this diff
  touches `cmd/agent/cli_keyword_search_test.go` + `devdoc/` only, and the gate's
  two inputs (`COMMANDS.md`, `cmd/agent/cli_checkpoint.go`) are byte-identical to
  `HEAD` (`git diff --name-only HEAD -- COMMANDS.md cmd/agent/cli_checkpoint.go`
  → empty). The generator itself names the fix
  (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`), which
  belongs to whoever owns that doc's regeneration — running it here would land an
  unrelated doc change in this branch (Rule 26: it wants its own dispatch).
  The gates that DO cover this change are green:
  `go test ./internal/standards/ -run 'DocIndex|Docclass|Manifest'` → `ok`, and
  `go test ./internal/docindex/` → `ok`.
- **The generic gate's blind spot is reported, not repaired.** In
  `retired_verb_test_driver_gate_test.go`, neither arm sees this shape: the
  **direct** arm reads argv[0] only beside an `exec.Command` call site (a probe
  call site spells no exec), and the **helper** arm reads call sites of a helper
  that forwards its argv **verbatim**, while `runCLIContractProbeRaw` is classified
  **TRANSFORMED** (it hands its argv to `cliContractProbeWithTimeout` first) and is
  skipped. The exclusion is deliberate *in general* (a transformed argv may have
  been rewritten) and decidable *for this shape* — which is what the family guard
  does. Widening the tree-wide gate governs every package and wants its own review;
  the archived branch's own report carries the same finding and it remains unfiled
  here (Rule 26: it needs a dispatch, not a note).
- **Sibling rows untouched by design** — the retirement table's treatment of
  canonical spellings and the other families' doors are separate rows.
- **The archived branch's other two changed files were deliberately NOT
  re-derived**: `git diff master e5f5ff8cd -- cmd/agent/cli_keyword_search_test.go`
  is the branch's *whole* surviving contribution to this deliverable (its
  `cli_sandbox_ledger_test.go` / `cli_tool.go` / help-surface files came from the
  merge side and are already on master in their own commits).

## Verification

- Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `GOPI_TEST_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'KeywordSearch' -count=1 -v`

  ```
  --- PASS: TestParseKeywordSearchArgs (0.00s)
  --- PASS: TestIsKeywordSearchSubcommandIgnoresSiblings (0.00s)
  --- PASS: TestKeywordSearchVerbIsWiredEverywhere (0.00s)
  --- PASS: TestKeywordSearchHelpPageDiffersFromTopLevel (0.02s)
  --- PASS: TestKeywordSearchRunPrintsHitsAndNoMatches (0.04s)
  --- PASS: TestKeywordSearchFamilyDrivesNoRetiredSpelling (0.00s)   ← the new family guard
  --- PASS: TestKeywordSearchUsageAdvertisesRootFlag (0.00s)
  --- PASS: TestKeywordSearchRefusalMatchesCodeSearchOnTheRealBinary (0.06s)  ← sibling refusal contract
  ok  github.com/nick/gopi/cmd/agent  2.263s
  ```
- Tests (branch-scoped wider set, the families whose contracts touch this door):
  `GOPI_TEST_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'KeywordSearch|RetiredVerb|VerbCensus|Census|SearchRoot|UndispatchedToolWord|VerbAlias|FamilyAddress|PositionalVerbGuard' -count=1`
  → `ok github.com/nick/gopi/cmd/agent` (see the run artifacts named in Evidence).
  This is the observation that the door change disturbed neither the generic
  retirement gates nor the verb census.
- Doc corpus: `./bin/gopi tool doccheck` → rc=0, including this report's own row.
- Ran: **the surface itself, verbatim, with real arguments** (Rule 31) — the door
  the migrated tests drive, and the refusal the flat word earns:
  ```
  $ D=$(mktemp -d); printf 'first line\nEUAUDIT_NEEDLE_ZZZ lives here\n' > $D/fixture.txt
  $ ./bin/gopi tool keyword_search EUAUDIT_NEEDLE_ZZZ --root $D          # → rc=0
  /home/user/workspace/_tmp/tmp.RxfvJOvEno/fixture.txt:2: EUAUDIT_NEEDLE_ZZZ lives here
  $ ./bin/gopi keyword_search EUAUDIT_NEEDLE_ZZZ --root $D               # → rc=1
  gopi: `keyword_search` is RETIRED — use `gopi tool keyword_search` (`gopi tool keyword_search -h` for its help). …
  ```
- **Red proof, RE-RUN on this tree, three reintroduction shapes** (Rule 10 minimal
  sample; each: stale drive back in → guard fails **by name**, restore → passes):

  | # | reintroduced form | guard verdict |
  |---|---|---|
  | R1 | `CliVerbTool, "keyword_search", "-h"` (master's pre-change form — the argv[0]-only escape) | `--- FAIL`, names line 162 + the spelling |
  | R2 | all three sites reverted to master's pre-change form | `--- FAIL`, names lines 162 and 207, plus `no probe call … drives nothing` |
  | R3 | fully flat head `"keyword_search", "-h"` (the original pre-`6685f7f83` shape) | `--- FAIL`, names line 162 as the retired spelling |

  R1's guard output, verbatim:
  ```
  --- FAIL: TestKeywordSearchFamilyDrivesNoRetiredSpelling (0.00s)
      cli_keyword_search_test.go:329: cli_keyword_search_test.go:162 drives the binary
      with the door spelled "keyword_search" — the leaf word is re-typed as a bare
      literal, which is the RETIRED flat spelling and is refused by name
      (`gopi keyword_search` → exit 1). Take it from the constant that owns it:
      `tool toolnames.ToolKeywordSearch`
  FAIL  github.com/nick/gopi/cmd/agent  0.756s
  ```
  Restored file → `--- PASS: TestKeywordSearchFamilyDrivesNoRetiredSpelling (0.00s)`.
  **Note the R1 row: this is the shape the ARCHIVED branch's argv[0]-only guard
  passed on** — the red proof is what found it, and it is why the guard reads the
  drive head.
- Commit: `ef08f1a22` — `test(cli): keyword_search family drives its door, not the
  retired spelling (re-derived)`, on branch
  `task/01a114cb30837be6a0ca420687a7ee75` (this run's own worktree). That commit
  lands the test change, this report and the `DOC-INDEX.tsv` row together (Rule
  36); the commit-line refresh at the top of this bullet is the run's follow-up
  commit, so `git log --oneline -2` shows both and the change's hash stays stable.
- Worktree: `git status --porcelain` → empty.

## Evidence (self-filed)

- `go build ./...` → `BUILD ALL OK`; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
  → `BIN OK`, `-rwxrwxr-x … 64094660 … bin/gopi`.
- `GOPI_TEST_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'KeywordSearch' -count=1 -v`
  → rc=0; the eight `--- PASS` lines above are that run's output
  (`…/artifacts/green-run.txt`).
- R1/R2/R3 red proofs and the post-restore green: `artifacts/red-proof-1-halfmigrated.txt`,
  `artifacts/red-proof-2-flat.txt`, `artifacts/red-proof-3-flathead.txt`,
  `artifacts/green-after-restore.txt` (all under this run's session directory).
- Rule 31 door/refusal runs: the two commands and their output quoted above, run on
  the freshly built `bin/gopi`.
- Wider branch-scoped set + standards gates: `artifacts/wider-run.txt`,
  `artifacts/standards-run.txt` (each ends with its own `rc=` line).

## The guard's shape, so a later reader does not "simplify" it back

1. **Probe-call level, drive head.** A probe's leading word(s) address this binary;
   the rest is payload. `CliVerbTool` + `toolnames.ToolKeywordSearch` = a door
   drive (credited as `doorDrives`); `CliVerbTool` + a bare leaf literal = the
   retired flat spelling in disguise (R1); anything else starting with a retired
   word = the fully flat drive (R3).
2. **Vocabulary read, never copied.** `verbAliasWords()` and the two door
   constants — so a word the binary stops retiring (or starts retiring) is picked
   up with no edit here.
3. **Non-vacuity is half the test.** `doorDrives == 0` is an error, so deleting the
   binary calls makes the guard fail rather than pass.
4. **`retiredVerbDriveGateSliceLiterals` is REUSED, not re-implemented** (Rule 8):
   the `argv := []string{…}` / `probe(…, argv...)` shape is expanded with the
   gate file's own reader, so the two readers cannot drift.
