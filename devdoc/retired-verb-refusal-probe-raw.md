<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: refusal probes drive argv RAW — the family door made 15+ refusal sub-cases answer about a command that dispatches

Task `01a10f17d3227c39a3630cb233d8bb3a` · branch `task/01a10f17d3227c39a3630cb233d8bb3a` ·
tip base `561878fd` (master).

## Completed

- **The defect, precisely.** `runCLIContractProbeEnv`
  (`cmd/agent/cli_contracts_test.go:553`) is the probe that drives the real
  binary through the **family door**: it passes `argv` through
  `familyAddressArgs` (`cli_family_address_test.go:61`), which rewrites `argv[0]`
  from a retired flat word to the family address that **dispatches** it
  (`log` → `session log`, `buildinfo` → `version`, `capture` → `tool capture`).
  That is right for the ~140 BEHAVIOUR call sites the verb-retirement epic
  migrated, and wrong for every **refusal** call site: a refusal test's subject
  IS the spelling that must not dispatch, so the door rewrite made it run the
  *working* command and then assert that it was refused.
- **The fix (surgical, 5 call sites, one line each).** The refused-spelling
  probes now call `runCLIContractProbeRaw` — the sibling the file already ships
  for exactly this reason ("what a REFUSAL test drives, because there the
  spelling that must NOT dispatch is the subject"). `familyAddressArgs` is
  **untouched**, so the 147 migrated behaviour call sites keep their door.

  | file | line (after) | refusal subject |
  |---|---|---|
  | `cmd/agent/cli_verb_aliases_test.go` | 95 | `TestVerbAliasIsRefusedByNameNoRun` — argv[0] alias (`log`, `buildinfo`, `capture`, …) |
  | `cmd/agent/cli_undispatched_tool_words_test.go` | 96 | `TestUndispatchedToolWordIsRefusedByNameNoRun` — spelling table (`code_search`, …) |
  | `cmd/agent/cli_tool_test.go` | 151 | `TestToolGroupForwardsToTheSameImplementation` — the BARE spelling must refuse |
  | `cmd/agent/cli_verb_guard_test.go` | 265 | `TestRunPathRefusesVerbAfterRunFlag` — positional-verb guard |
  | `cmd/agent/cli_verb_hint_guard_test.go` | 89 | `TestUnknownNearMissIsRefusedByNameNoRun` — near-miss vocabulary |

  Each site carries a one-line comment saying WHY it is raw (a later reader
  will otherwise see an inconsistency between two probes and "unify" them back
  into the defect).
- **The hidden-red count was larger than the ticket's 15** — the same seam hid
  `TestUndispatchedToolWordIsRefusedByNameNoRun` (24 sub-cases),
  `TestUndispatchedToolWordRefusalHasNoLedgerRow`,
  `TestUndispatchedToolWordRefusalHasNoLedgerRow`'s ledger assertion, and 18 of
  19 `TestToolGroupForwardsToTheSameImplementation` sub-cases. All are green
  now (see Tests below). The family's hidden red count was 106 sub-cases, not the
  15 the ticket recorded — the same seam hid the spelling table's 24 shapes and
  18 of the 19 tool-group faces.
- **Verified before and after on the real binary** (Rule 31), not only in the
  suite — see `Ran:` below.

## Not Done (honest)

- **The only failures left in the refusal family are PRE-EXISTING and lie
  outside this seam.** Measured: after the fix, `TestVerbAliasIsRefusedByNameNoRun`
  is **84/84 PASS** (including every `*_extra` shape — `gopi capture extra` is
  refused by the capture predicate itself, which accepts a filename only via an
  explicit mode flag), and the family run's only remaining `FAIL` sub-case is
  `TestToolGroupForwardsToTheSameImplementation/web`. Both failures below fail
  IDENTICALLY on the pre-fix tree (same command, same output, before the probe
  was changed), in vocabularies that have nothing to do with the family door.
  They need a decision about a TABLE, not about a probe, so repairing them here
  would have widened this diff into a different question:
  - `TestToolGroupWordsAreTheOnlyDoor` — the table claims `web` is
    group-only, but `web` is ALSO a dispatched top-level verb
    (`isWebSubcommand`, `cmd/agent/main.go:553`), which the test's own comment
    acknowledges as the "browser chat carrier". A vocabulary disagreement
    between the tool group table and the dispatch chain.
  - `TestUndispatchedToolWordsNameADispatchingCanonical` — line 62's
    `dashTwin(canonical) == dashTwin(word)` check reads the RETIRED flat tool
    words as "refuses itself" (`code_search` with canonical `code-search`),
    which is now the *correct* state after the retirement: both spellings point
    at `gopi tool …` and the assertion has outlived its premise.
  Both need a decision about a table, not about a probe.
- **Four further failures in `-run 'Contract|VerbGuard|Verb'` are pre-existing and
  were MEASURED as such**: with this change STASHED (the pre-fix tree), the same
  command produced the same four failures —
  `TestCLIContractInventoryMatchesDispatch` ("main.go has 20 … contract suite
  tracks 18"), `TestVoiceInputIsRegisteredInContractMirrors`,
  `TestVoiceInputIsAContractToolMirror`, and
  `TestAnnotateVerbRunsARealArgvEndToEnd/-h…` (the page-vs-truncated-output
  comparison). None is on the seam this task touched, so none was repaired here
  (the branch's own hard constraint: a narrow, surgical diff).
- **No new test was added.** The task is a repair of the test seam itself; the
  red→green evidence IS the regression guard (the pre-fix run is reproduced in
  the session artifacts, and the fixed probe cannot silently revert without
  these tests going red again).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (and
  `go build ./...` → `BUILD_OK`).
- Tests (branch-scoped, `-count=1`, `-v`):
  - RED, before the change — `go test ./cmd/agent/ -count=1 -v -run
    TestVerbAliasIsRefusedByNameNoRun` → **84 `--- FAIL` sub-cases** (38 alias
    rows × shapes + the argv-after-flag family), the first being
    `gopi buildinfo: exit 0 — the alias was accepted instead of refused`.
    The wider refusal family (same command, other test names) → **102 sub-case
    FAILs + 4 top-level** (`TestToolGroupForwardsToTheSameImplementation`,
    `TestUndispatchedToolWordsNameADispatchingCanonical`,
    `TestToolGroupWordsAreTheOnlyDoor`, `TestVerbAliasIsRefusedByNameNoRun`).
  - GREEN, after the change — `go test ./cmd/agent/ -count=1 -run
    TestVerbAliasIsRefusedByNameNoRun` → **`ok github.com/nick/gopi/cmd/agent
    8.004s`**; the same run with `-v` → **84/84 PASS** in that test.
  - GREEN, acceptance regex — `go test ./cmd/agent/ -count=1 -run
    'TestVerbAlias|TestUndispatchedToolWord|TestUnknownNearMiss|TestNearMiss|
    TestUnknownWordIsStillPromptText|TestToolGroup|TestRefusedSpellingWords|
    TestWebToolFlatTwins|TestWebUsagePages|TestRetiredMerger|
    TestRunPathRefusesVerbAfterRunFlag|TestTermUnknown|TestNoTestDrivesTheRealBinaryWithARetiredWord|
    TestRetiredVerbGate'` → **273 `--- PASS`**, and the failure list is exactly
    the two pre-existing vocabulary defects below and
    nothing else (was 106 failing sub-cases + 4 top-level before).
  - The anti-regression half stays green: `TestNoTestDrivesTheRealBinaryWithARetiredWord`
    PASS and `TestRetiredVerbGateHelperArmSeesThroughTheMapper` PASS — the raw
    probes are not themselves flagged as driving a retired word.
  - The acceptance regex `'Contract|VerbGuard|Verb'` (137s, whole set) →
    the four failures listed in `## Not Done`, ALL measured pre-existing by
    stashing this change and re-running the same four tests (same output,
    pre-fix tree), and **no new red from this diff**.
- Ran (real binary, manual; Rule 31 — `-h` is not a run):

  ```
  H=$(mktemp -d); for w in log capture stats raw buildinfo; do
      GOPI_HOME=$H ./bin/gopi $w; echo "exit=$?"; done
  GOPI_HOME=$H ./bin/gopi tool capture -h   # the address each refusal prints
  ```

- Result (verbatim):

  ```
  gopi log        → exit=1  gopi: `log` is RETIRED — use `gopi session log` …
  gopi capture    → exit=1  gopi: `capture` is RETIRED — use `gopi tool capture` …
  gopi stats      → exit=1  gopi: `stats` is RETIRED — use `gopi session stats` …
  gopi raw        → exit=1  gopi: `raw` is RETIRED — use `gopi session raw` …
  gopi buildinfo  → exit=1  gopi: "buildinfo" is not a gopi command — this one is
                            spelled `gopi version` …
  gopi tool capture -h → exit=0        # the printed address really dispatches
  sessions minted under $H/.gopi/sessions: 0   # the refusal never reached the run path
  ```

- Commit: on `task/01a10f17d3227c39a3630cb233d8bb3a` (this branch), see
  `git log --oneline -1`.

## Evidence (self-filed)

- `familyAddressArgs` is UNTOUCHED by this change: `git diff 561878fd --stat`
  lists exactly five `*_test.go` files plus this report and its DOC-INDEX row —
  `cmd/agent/cli_family_address_test.go` is not among them.
- The five edited call sites, by grep on the tree:
  `grep -c "runCLIContractProbeRaw(" cmd/agent/cli_verb_aliases_test.go
  cmd/agent/cli_undispatched_tool_words_test.go cmd/agent/cli_tool_test.go
  cmd/agent/cli_verb_guard_test.go cmd/agent/cli_verb_hint_guard_test.go` → 1 each.
- The pre-fix run and the post-fix run are the two files
  `red-verb-alias.txt` / `red-family.txt` (pre-fix, 84 and 102 failing
  sub-cases) and `green-family.txt` / `green-regex.txt` (post-fix) under this
  run's session directory (`~/.gopi/sessions/<this session>/`), the paths
  `-run` printed above.
- `git status --porcelain` in the worktree: empty after the commit (Rule 30).
- Rule 36 (the doc corpus): `./bin/gopi doccheck` and
  `python3 tools/docclass/classify.py . --check` each report exactly ONE gap,
  and it is the SAME gap on master — the row
  `devdoc/scripts-retired-verb-sweep.md normative` with an empty evidence cell,
  text-identical at `HEAD~1` (`git show HEAD~1:devdoc/DOC-INDEX.tsv | grep
  scripts-retired-verb-sweep` → the same row, empty evidence). The new row for
  THIS report was written by the corpus's own writer
  (`classify.py --add-missing --only devdoc/retired-verb-refusal-probe-raw.md`,
  one row added, no other row touched) and this change introduces NO new gap.
- The four `internal/standards/` failures (`TestCommandsDocGeneratedBlockMatchesSource`,
  `…RedProofStaleGeneratedBlock`, `TestDocManifestRowsAreWellFormed`,
  `TestDocClassBannerMatchesManifest`) are the same master-resident defects:
  this commit touches neither `COMMANDS.md` nor
  `devdoc/scripts-retired-verb-sweep.md` (`git show --stat HEAD`).
