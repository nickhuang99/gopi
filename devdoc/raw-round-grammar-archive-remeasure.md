<!-- doc-class: historical -->
# Raw-round-grammar archive re-measure: tag `archive/feat/raw-round-grammar`

## Question

The `archive/*` audit (`98a4d342`) classified tag `archive/feat/raw-round-grammar`
as UNLANDED, with an ABSENT-SYMBOL list (`httpSelectorGrammar`,
`rawRoundPosition`). An absent symbol is a NAME test. This report re-measures
the tag by CONTENT (`git log -S` on master, `git grep`, blob identity) and
delivers a per-unit verdict.

## Instruments (re-run, verbatim)

Tag tip and base:

```
$ git rev-parse archive/feat/raw-round-grammar
d9a5ea41afc6e35039edbcf6377c5fdb056dff26

$ git merge-base master archive/feat/raw-round-grammar
b7b8000dd91aed64eea8ee32e503b80101525625
```
(`b7b8000d` = "Merge branch 'feat/coding-standards-checklist'", 2026-09-11 — the
tag's content is post-`e23d506` (2026-09-05, "feat(cli): gopi stats + gopi raw;
move log --http/--raw out"), which IS an ancestor of both master and the tag.)

```
$ git cherry -v master archive/feat/raw-round-grammar
+ 71b3afc1215259e2e8a85ccf455bafaf546273c9 test(gopilog): function-level coverage for 5 uncovered exported funcs (covHp)
+ d9b26fcd5033d784f6de32e3dd6fc32a25a5a5ad docs(gopilog): report memory entries + third bug (read-tool 8KB UTF-8 sample boundary)
+ 5d66903146ac18a43a1e4832a6827b27d5072ba9 fix(gopilog,raw): one round-selector grammar for --http and gopi raw; correct the error text
+ 06fe74842ac13e5c809409b7f9c38f2140095df8 docs(gopilog): report + devdoc for the shared round-selector grammar
+ d19aca3996e39c04c3193eeba390c6d8a4a7094a docs(raw-round-grammar): independent re-verification of the committed fix
+ dddda3abea52673a11dba7da4b13e199cee8a791 docs(raw-round-grammar): merge note + the strengthened full-message pin (literal, mutation-verified)
+ d9a5ea41afc6e35039edbcf6377c5fdb056dff26 docs(raw-round-grammar): E2E re-run on the merged tip binary (56-round real log)

$ git rev-list --count master..archive/feat/raw-round-grammar
8
```

(`git log --oneline master..archive/...` prints the 8 rows above plus
`949ddbc4 chore(gopilog): merge feat/cov-gopilog-hp; pin the full corrected
--http error message` — the counts agree; cherry marks every patch `+`, i.e. no
patch-equivalent commit on master.)

## Per-unit verdicts (content test)

### 1. `httpSelectorGrammar` (the rendered grammar text + the corrected message) — **SALVAGE**

- `git grep -w httpSelectorGrammar master` → exit 1, no hit anywhere.
- `git log --oneline -S'httpSelectorGrammar' master` → only `98a4d342`
  ("docs(audit): full archive/* tag audit …"), i.e. the audit's own mention.
  The symbol never existed on master.
- MASTER'S SURVIVING TEXT IS THE DEFECT: `internal/gopilog/httpview.go` on
  master still hand-types the grammar in `ParseHTTPSelector`'s error —
  `fmt.Errorf("--http takes %s|%s|N (a round number), got %q", HTTPSelectorAll,
  HTTPSelectorLast, arg)`. The string is composed at runtime, so the audit's
  NAME test could not see it; the CONTENT test does.
- The replacement landed on master (`4d9b7797`, 2026-09-11) only for the
  *cmd-side duplication* (`roundArg`'s second grammar was removed) —
  `internal/gopilog/httpview.go` was never re-landed with the rendered
  `httpSelectorGrammar` constant.

### 2. `rawRoundPosition` (raw's shared-grammar resolver + from-the-end rounds) — **SALVAGE**

- `git grep -w rawRoundPosition master` → exit 1; `git grep -w HTTPDetailIndex
  master` → nothing (`git show master:internal/gopilog/httpview.go` has no such
  function).
- `git log -S'HTTPDetailIndex' master` → `60d7e5f3` ("chore(gopilog): delete the
  orphaned per-round digest view API", 2026-09-11) is the commit that DELETED
  it, together with the digest renderers. It was deleted as part of a rendering
  cleanup although it is a RESOLVER, not a renderer.
- BEHAVIOURAL PROOF (not a name test): on clean master,
  `git show master:cmd/agent/cli_raw.go` parses `<round>` itself —
  `n, err := strconv.Atoi(positionals[roundIdx]); if err != nil || n <= 0 {
  return nil, fmt.Errorf("<round> must be a positive round number, got %q", …) }`
  and resolves with `rounds[opts.round-1]`, so `last`, `0` and `-N` (the whole
  documented grammar) are REFUSED or silently answered with the last round.
- DOCUMENTED-BUT-ABSENT SURFACE: `HTTPRoundSelector`'s doc comment on master
  still promises "a negative -N → the Nth-from-last round's detail" and the
  file's own header comment still claims the grammar is "shared with `gopi raw
  <round>` — one grammar, two surfaces". Neither holds on master; the tag's
  re-derivation makes both true.

### 3. `cmd/agent/cli_log.go` edits (removing the second grammar + pointer) — **OBSOLETE**

- Superseded by master commit `4d9b7797` ("fix(gopilog): single-source the round
  grammar in cmd, deprecate the orphaned --http view", 2026-09-11), whose
  `cmd/agent/cli_log.go` hunk removes cmd's own round grammar in favour of
  `gopilog.ParseHTTPSelector` and adds `cli_log_roundgrammar_test.go` + the
  delegation test. That is the same content, so nothing here is lost.
- The tag's EXTRA refinement (`roundArg` keeping `last`/`0`/`-N` instead of
  normalizing them to `""`) did not land and is not superseded — it is carried
  by unit 2's SALVAGE, since raw must accept those selectors for the pointer to
  name them. Consolidated there, it is not silently dropped.
- The tag's `rawPartRequest`/`rawPartResponse` use in the pointer is also part
  of unit 1/2's re-derivation (see Files).

### 4. `cmd/agent/cli_raw.go` + `internal/gopilog/httpview.go` edits — **SALVAGE** (bulk of unit 1/2)
- `git diff master:cmd/agent/cli_raw.go archive/feat/...:cmd/agent/cli_raw.go`
  shows the tag's `round int` → `sel gopilog.HTTPRoundSelector`, the flag/part
  constants (`rawFlagFile` … `rawFlagLast`, `rawPartRequest`/`rawPartResponse`),
  the shared-grammar parse of `<round>`, the `--file` exclusivity test through
  the shared grammar, and `rawRoundPosition` with the explicit both-direction
  range check. NONE of it is on master: `git grep -w rawFlagRounds master` → no
  hit, `git grep 'positive round number' master -- cmd/agent/cli_raw.go` →
  `e23d5061` only (the commit that INTRODUCED the positive-only rule).
- `cmd/agent/cli_raw_selector_test.go` is ABSENT on master
  (`git cat-file -e master:cmd/agent/cli_raw_selector_test.go` → fatal).

### 5. `coverage_hp_test.go` / `coverage_hp_selector_test.go` (the cov-gopilog test unit) — **OBSOLETE**

- The tag's `internal/gopilog/coverage_hp_test.go` (408 lines) is the sibling of
  master's `internal/gopilog/cov_gopilog_hp_test.go` (392 lines), which landed
  from the SAME lineage branch: `git log -1 master -- internal/gopilog/cov_gopilog_hp_test.go`
  → `51668406 test(gopilog): close function-level coverage 91.4% -> 100% (5
  exported funcs)`.
- The tag's variant asserts only the STABLE PARTS of `--http`'s message —
  precisely because the duplicated-`last` typo was live when it was written
  (recorded in the tag's own report). Its content is therefore subsumed by
  master's version once the message is fixed; the message's full-string pin is
  delivered by unit 1's `TestParseHTTPSelectorMessageIsSingleSourced` instead.
- No add/add conflict risk remains and no distinct coverage is lost.

### 6. Docs (`devdoc/cov-gopilog-hp.md`, `devdoc/log-http.md`, `devdoc/raw-round-grammar-report.md`) — **OBSOLETE (historical)**

- `devdoc/cov-gopilog-hp.md`: master holds a DIFFERENT blob
  (`master:d0e28097…` vs tag `b8a19874…`) and the file is tracked on master
  (`5faf7daa docs(devdoc): classify every devdoc file by truth class, banner the
  historical ones`). Master's copy is the landed one.
- `devdoc/raw-round-grammar-report.md` is ABSENT on master (no landing);
  its still-true content is superseded by THIS report, and its obsolete halves
  describe the digest view `60d7e5f3` deleted. It is deliberately NOT
  re-landed (a second report claiming a state master does not have is worse
  than none).

## Summary

| unit | verdict | evidence |
|---|---|---|
| `httpSelectorGrammar` rendered text | **SALVAGE** | `git grep` → 0 hits; master hand-types the message; `-S` hits only `98a4d342` (the audit) |
| `rawRoundPosition` / `HTTPDetailIndex` | **SALVAGE** | `git grep -w` → 0 hits; `60d7e5f3` deleted the resolver; master's `cli_raw.go` refuses `last`/`0`/`-N` (positive-only rule from `e23d5061`) |
| `cmd/agent/cli_log.go` second-grammar removal | **OBSOLETE** | landed as `4d9b7797` |
| `cmd/agent/cli_raw.go` + `httpview.go` edits | **SALVAGE** | not on master (no `rawFlag*` constants, no shared parse, no resolver) |
| cov-gopilog test unit | **OBSOLETE** | sibling landed as `51668406` (`cov_gopilog_hp_test.go`); its variant was written around the live typo |
| docs | **OBSOLETE** (historical) | `cov-gopilog-hp.md` landed (different blob); the tag report is superseded by this one |

## Deliverable

- Branch: `fix/raw-round-grammar` (worktree
  `/home/user/workspace/gopi-raw-round-fix`, linked to `/home/user/work/GoPi`,
  based on master `fa12d97f`). Recovery: `git show fix/raw-round-grammar:devdoc/raw-round-grammar-archive-remeasure.md`.
- The tag `archive/feat/raw-round-grammar` and its branch are UNTOUCHED (read
  only via `git show` / `git grep` / `git log -S`). No merge was performed.

## Completed

- Re-measured the tag: instruments re-run and quoted above.
- Per-unit content verdicts with `git grep` / `git log -S` / blob-identity
  evidence, and a NAMED master commit or surface for every OBSOLETE verdict.
- SALVAGE re-derived on a fresh branch off master (`fix/raw-round-grammar`):
  - `internal/gopilog/httpview.go`: the grammar keywords
    `RoundKeywordAll` / `RoundKeywordLast` / `LastRound`, the rendered
    `httpSelectorGrammar` (one `last`, not two), `ParseHTTPSelector`'s message
    rendered from it, and the restored RESOLVER `HTTPDetailIndex` (0/-1 = last,
    -2 = 2nd-from-last, clamped at both ends).
  - `cmd/agent/cli_raw.go`: the `<round>` positional parsed by the shared
    `gopilog.ParseHTTPSelector`, resolved by `rawRoundPosition` →
    `gopilog.HTTPDetailIndex` with an explicit both-direction range check;
    `all` refused as a listing selector; the flag/part constants
    (`rawFlagFile`…`rawFlagLast`, `rawPartRequest`/`rawPartResponse`)
    referenced everywhere the literals were; the usage text documents the
    shared selector.
  - `cmd/agent/cli_log.go`: `roundArg` keeps any selector the shared grammar
    accepts (`last`/`0`/`-N`), so `gopi log <id> --http last` points at
    `gopi raw <id> last`; the pointer's part keywords come from the constants.
  - Tests: `httpview_selector_grammar_test.go` (keyword/grammar pin, whole
    message pin, resolver matrix, Rule 13 literal gate) and the new
    `cmd/agent/cli_raw_selector_test.go` (shared-grammar identity, listing
    refusal, `--file` form, from-the-end matrix, out-of-range, pointer table,
    end-to-end byte comparison on the real fixture).

## Not Done (honest)

- **No E2E run against a real 56-round session log** (the tag's own
  `d19aca39`/`d9a5ea41` did that). The end-to-end byte comparison here runs on
  the checked-in `--rounds` fixture, which is the smallest sample that proves
  the selector→span mapping; a real multi-session log's `--last N` interaction
  was not re-run.
- The tag's `devdoc/raw-round-grammar-report.md` is NOT re-landed (see unit 6).
- The full `cmd/agent` suite is long on this host; the verdict-relevant
  packages are run branch-scoped (Rule 27). No whole-tree run was made.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0 (`BUILD_OK`).
- Tests (branch-scoped, Rule 27):
  - `go test ./internal/gopilog/ -count=1` → `ok  github.com/nick/gopi/internal/gopilog 3.466s`.
  - `go test ./cmd/agent/ -run 'TestParseRawArgs|TestRaw|TestLogMovedPointer|TestRoundGrammar|TestRoundArg|TestLogUsage' -count=1`
    → `ok  github.com/nick/gopi/cmd/agent 5.239s`.
  - The WHOLE `./internal/gopilog/ ./cmd/agent/` run was also started; its
    `cmd/agent` half is long on this host and the targeted run above is the
    branch-scoped verification actually observed (no whole-tree run — Rule 27).
- Red proof (throwaway detached worktree on clean master `fa12d97f`, probes
  written to disk there and then deleted):
  - behavioural: `parseRawArgs(sess "last"|"0"|"-1"|"-2")` → `<round> must be a
    positive round number, got …`; `logMovedPointer(--http last)` → the
    `gopi stats <id> --rounds` pointer (the selector erased) — FAIL on master.
  - symbol-level: the branch's new test files on clean master fail to build
    with `undefined: RoundKeywordAll / RoundKeywordLast / LastRound`; the
    message pin is red for a different reason — the string `all|last|last|N` is
    composed at runtime on master, and the branch renders `all|last|N`.
- Commit: see `## Deliverable` below.

## Evidence (self-filed)

- `git cherry -v master archive/feat/raw-round-grammar` → all 8 rows `+`
  (output quoted in full above).
- `git rev-list --count master..archive/feat/raw-round-grammar` → `8`.
- `git merge-base master archive/feat/raw-round-grammar` →
  `b7b8000dd91aed64eea8ee32e503b80101525625`.
- `git grep -w -l -e httpSelectorGrammar -e rawRoundPosition master -- '*.go'`
  → no output, exit 1 (both symbols absent from master).
- `git log --oneline -S'HTTPDetailIndex' master` → `60d7e5f3` delete,
  `4d9b7797`/`e23d5061`/`e51eef8e`/`6a5a33a3` (its history); `git show
  master:internal/gopilog/httpview.go` contains no `HTTPDetailIndex`.
- `git cat-file -e master:cmd/agent/cli_raw_selector_test.go` → fatal: Not a
  valid object name (absent on master).
- `git grep -n '<round> must be a positive round number' master --
  cmd/agent/cli_raw.go` → `e23d5061` (the rule master still enforces).
- `git log -1 --format='%h %ad %s' --date=short 4d9b7797 60d7e5f3` → both
  2026-09-11; `git merge-base --is-ancestor e23d506 master` → yes.
- Blob identity: `git rev-parse master:devdoc/cov-gopilog-hp.md
  archive/feat/raw-round-grammar:devdoc/cov-gopilog-hp.md` →
  `d0e2809721e76447f6a0f3b007133b2f1d935a78` vs
  `b8a19874204524ca445db98077f32df8f524dc5b` (different content).
- `gofmt -l cmd/agent internal/gopilog` → no output.
- `go test ./internal/stylegate/ -run TestStyleGate` reports the SAME six
  residue rows in the verification worktree and in a pristine master checkout
  (`/home/user/gopi-pristine-gate`, `fa12d97f`) — pre-existing, not introduced
  here.
