<!-- doc-class: historical -->
# Task Report: the branch `task/01a10dc3…` converged onto master — its retirement intent is already live, its bytes are superseded

Task id: `01a10e53b0537ae9a6e91fd7bd830182`.
Branch reworked: `task/01a10dc306ad78b7b65313efd18879bc` →
`rework/01a10dc3-verb-retirement-superseded` (renamed in this run; Rule 6a).

## Completed

- **The branch was refused by merge only because of a merge CEILING, not a disagreement.**
  `git merge master` into it produces **36 conflicted files** — a conflict set
  larger than the run's ceiling, which is why the merge run gave up. The branch is
  `master` minus ~10.6k lines plus 5 commits dated 06:52; master's tip is 10:04 the
  same day. The divergence is **not** a design disagreement to adjudicate — it is
  the branch being **behind and superseded**.
- **Master already landed this epic, later and more completely.** `a4314fdf`
  "retire the flat tool/verb spellings — one door per command", plus the
  `rework/verb-retirement-complete` merges. Master implements the retirement
  vocabulary as **ONE unified door table**, `cmd/agent/cli_verb_aliases.go`, whose
  rows carry `retired: true` — instead of the branch's **third table**
  (`cmd/agent/cli_verb_retired.go`), which the branch's own header argues for
  ("WHY A THIRD TABLE, AND WHY IT IS NOT THE OTHER TWO"). Master's unified table is
  the later choice, and it is the one the vocabulary lives in.
- **The branch's only unique bytes are two files** master lacks:
  `cmd/agent/cli_verb_retired.go` and `cmd/agent/cli_retired_argv_test.go`
  (`git diff --name-status master HEAD` → exactly these two `A` entries).
- **The convergence.** The tree was converged onto master's implementation and the
  branch's superseded bytes dropped, so the branch now **merges with zero
  conflicts**: at the convergence commit (`b5c4542a`) the tree IS master's tree, byte
  for byte — there `git write-tree` → `578677abec2f889b90fcef915c0d2f1033385383` and
  `git rev-parse master^{tree}` → the **same** hash, with `git diff --name-only master`
  empty. The delivered HEAD then adds exactly the two documentation paths on top of
  that tree (`git diff --name-only master HEAD` → `devdoc/DOC-INDEX.tsv`,
  `devdoc/verb-retirement-branch-01a10dc3-superseded.md` — nothing else), which is the
  acceptance shape: convergence plus a report, and no code delta at all.
- **Master MOVED during this run, and the convergence was re-measured, not assumed.**
  At first convergence master's tip was `add55f06` (tree `3e01c1e6…`); by the time
  the gates finished it was `d0f69691` (tree `578677ab…`). The residual was
  re-measured against the LIVE tip and the branch re-converged in `b5c4542a`, so the
  shipped tree identity is the later one — a convergence measured against a stale
  tip can silently regress. It moved a THIRD time before hand-over (`6c17d819`, tree
  `9f44d561…`), which is why the delivered tip is a merge commit rather than a
  one-shot fast-forward: see the delivery-point note under `## Completed`.
- **Mergeability was proved by running it, not asserted**: with the converged tree in
  place, `git merge --no-commit --no-ff master` produced **0** conflicted paths
  (`git diff --name-only --diff-filter=U`) against the pre-rework branch's **36**.
  That probe committed nothing, and `master` was never touched.

- **The DELIVERY-POINT SYNC found one real conflict, and it was closed by a merge, not
  by a claim.** Measured at the report commit: `git merge --no-commit --no-ff master`
  → **1** conflicted path, `devdoc/DOC-INDEX.tsv` — master had advanced the same file
  in the meantime (`5105b4d7`→`d0f69691`, which also classified
  `scripts-retired-verb-sweep.md`), and a one-line row appended to a shared,
  machine-generated table is exactly the shape that conflicts. So master was merged IN
  (`6e805c80`, a real two-parent merge: `5a5bf889` + `d0f69691`), the conflicted file
  was taken from master's side, and **this report's row was then re-written by the tool
  that owns it** — `gopi tool doccheck --fix` → `repaired 1 path(s)` — never by hand
  (Rule 36: `DOC-INDEX.tsv` is data). After that merge: master IS an ancestor of the
  branch, and the probe above reports **0** conflicts.
- **Master moved a SECOND time and the second sync was clean** — which is the evidence
  that the first conflict was a one-off and not a property of this branch. Master's tip
  went `d0f69691` → `6c17d819` (23 further paths across `cmd/agent`, `internal/lineedit`,
  `internal/inputsource`, a new `devdoc/unified-input-queue.md`). `git merge master` on
  the synced branch → exit **0**, **0** conflicted paths, no manual resolution at all;
  the resulting merge commit is `60596fd7`, and `git diff --name-only master HEAD` is
  still exactly this report plus its one `DOC-INDEX.tsv` row. A branch that keeps
  merging cleanly while master advances is the deliverable's real acceptance property.
- **A third master tip (`ae9aaf51`) merged the same way: exit 0, 0 conflicts**
  (`76613725`), bringing `internal/config/env_audit_probe_gate_test.go` and
  `scripts/audit-env-catalog.sh`. That is the LAST sync before hand-over, and it is
  what the acceptance probe above was measured against: `master` IS an ancestor of the
  branch, and `git merge --no-commit --no-ff master` → exit 0, **0** conflicted paths.
- **A FOURTH master tip merged cleanly too, DURING the final verification** — which is
  the clearest signal of all that this conflict was a ceiling, not a disagreement.
  Master advanced again to `e1328fea` (a merged `task/01a10ed1713773c79b7dc4b3c6f50d88`:
  the lineedit ambiguous-width work, 13 paths across `CONFIG.md`, `internal/config`,
  `internal/lineedit` and a new `devdoc/lineedit-eaw-ambiguous.md`). The delivery-point
  merge (`git merge master -m …`) → **exit 0**, **0** conflicted paths, 13 files
  brought in with no manual resolution, and the branch tip is now
  `153060d01451b49c8d6418be8e658e53ca3cacaf`. Four master tips, four clean merges after
  the one-time convergence: `git status --porcelain` empty, `master` an ancestor, and
  `git diff --name-only master HEAD` still exactly this report plus its one
  `DOC-INDEX.tsv` row.

## Evidence: the branch's INTENT is already held by master (word-level conservation)

Both tables were extracted and compared as word SETS — not by eyeball:

```bash
git show f49b48c6:cmd/agent/cli_verb_retired.go | grep -oE '^\s*word: +[A-Za-z_.]+' | awk '{print $2}' | sort -u  # 29
grep -oE '^\s*alias: +[A-Za-z_.]+' cmd/agent/cli_verb_aliases.go | awk '{print $2}' | sort -u                    # 28 rows
# rows whose block carries `retired:   true`                                                                     # 27
comm -12 <branch words> <master retired rows>                                                                     # 25
```

```
branch retirement table (f49b48c6:cmd/agent/cli_verb_retired.go), 29 word constants
master's retired rows  (cmd/agent/cli_verb_aliases.go, `retired: true`), 27
master's all alias rows (28)                      → 25 of the branch's 29 words are
                                                    ALREADY RETIRED on master
```

The four-word delta is **exactly** the design divergence this report is about:

```
branch-retired words NOT retired on master : CliVerbCLI, CliVerbUI,
                                             toolnames.ToolWebFetch, toolnames.ToolWebSearch
master-retired words the branch did NOT retire : CliVerbArchive, CliVerbSandbox
```

(The counts above are the numbers the extraction COMMANDS print — the earlier
draft of this section quoted 30/"the", a hand-count that did not reproduce; see
`## Amendments`.)

`ui`, `cli`, `web_search` and `web_fetch` are **deliberately still DISPATCHED on
master**, and a retired row for them would be a defect — this is master's own stated
invariant, in the file itself and in its gate:

- `cmd/agent/cli_verb_aliases.go:127` — "…(a word that IS dispatched would make the
  command unreachable)."
- `cmd/agent/cli_verb_aliases_test.go:54` — the gate FAILS with
  `alias %q IS dispatched — it must not be in the refusal vocabulary (the command
  would be unreachable behind the refusal)`.
- `cmd/agent/main.go:553` dispatches `isWebSearchSubcommand`, `isWebFetchSubcommand`
  and `isUISubcommand`; `cli` is dispatched by `isTermSubcommand`
  (`cmd/agent/cli_term.go:108`, `args[0] != CliVerbCLI`).

So the branch's four extra rows would have retired four words that still dispatch —
master's shape is not merely different, it is the **self-consistent** one, and the
branch's is the one its own sibling gate rejects. Master additionally retired two
words the branch never reached (`archive`, `sandbox`) — further work in the same
direction, which is the second independent sign that master is the later state.

## Evidence: the intent is LIVE, not merely present (Rule 31 — executed, not unit-tested)

The built binary (`make build`, 30,661,831 bytes) was run against **real retired
spellings**, with the real arguments a user would type:

```
$ ./bin/gopi log
gopi: `log` is RETIRED — use `gopi session log` (`gopi session log -h` for its help).
To send the word as a prompt instead, say so explicitly: `--prompt log`.        [exit=1]

$ ./bin/gopi capture
gopi: `capture` is RETIRED — use `gopi tool capture` …                          [exit=1]

$ ./bin/gopi stats
gopi: `stats` is RETIRED — use `gopi session stats` …                           [exit=1]

$ ./bin/gopi raw
gopi: `raw` is RETIRED — use `gopi session raw` …                               [exit=1]

$ ./bin/gopi buildinfo
gopi: "buildinfo" is not a gopi command — this one is spelled `gopi version` …  [exit=1]
```

The refusal is **exit 1**, it **names the family door**, and it offers the
`--prompt` escape so the word can still be a prompt. The other half — the capability
is untouched — was executed too, and the family spelling RUNS:

```
$ ./bin/gopi version            → v0.1.0 / go1.24.5 / linux-amd64             [exit=0]
$ ./bin/gopi tool capture -h    → the tool capture help surface                [exit=0]
$ ./bin/gopi session stats      → its own usage/input refusal (the verb runs)   [exit=1, its own]
$ ./bin/gopi --prompt frobnicate9 --help → the agent banner                    [exit=0]
```

The last line is the **negative control**: a word this binary never dispatched is
still ordinary prompt text, so the refusal is not a blanket block.

## Not Done

- **The branch-scoped test set is NOT green, and the reds are master's own — measured,
  not assumed.** Re-measured at the DELIVERED tip with the SAME branch-scoped set, and
  read out of the budget run's own JSON stream (not from a summary line):
  `make test-budget PKGS='-run "VerbAlias|Retired|Contract|VerbGuard|Verb|HelpHidden" ./cmd/agent/ ./internal/config/'`
  → `cmd/agent` **FAILED**, **5** top-level tests / **90** test-and-subtest `fail`
  records, and `./internal/config/` green. The five: `TestAnnotateVerbRunsARealArgvEndToEnd`,
  `TestCLIContractInventoryMatchesDispatch`, `TestVerbAliasIsRefusedByNameNoRun`,
  `TestVoiceInputIsAContractToolMirror`, `TestVoiceInputIsRegisteredInContractMirrors`.
  **The earlier draft of this report said "7 tests"; that number came from counting the
  printed `FAIL:` lines, which include the parent of a failing subtest, and it did not
  reproduce — the amendment below carries the measured five.** Proof that this
  convergence did not cause them: `git diff --name-only master HEAD -- cmd/agent internal tools`
  is **empty**, i.e. three of the four source trees the package compiles are master's
  **byte for byte**, and the only delta anywhere is the two `devdoc/` paths. The same
  reds are already recorded by master's own sibling run as its honest remainder
  (`devdoc/verb-retirement-complete.md`, "Not Done — the honest remainder (the suite is
  NOT green)", whose table lists `TestCLIContractInventoryMatchesDispatch`
  ("count off by 2") and `TestAnnotateVerbRunsARealArgvEndToEnd` by name).

- **`gopi tool doccheck` is RED in this worktree — and the gap is master's, not this
  report's.** Live, with the built binary: `./bin/gopi tool doccheck` → exit **1**,
  `doccheck: normative-no-check devdoc/scripts-retired-verb-sweep.md — a normative row
  must record the spot-check that holds its claim` + `1 gap(s)`. That file is
  **master's** (`git ls-tree master -- devdoc/scripts-retired-verb-sweep.md` resolves;
  it is not in `git diff --name-only master HEAD`), so `gopi doccheck --fix` is the
  right remedy but belongs to whoever lands that document — and `--fix` writes only the
  two arms the write path knows (a missing row, an unreadable one); this is neither, so
  it is a human's call and not a fix a rework branch should make to another writer's
  row. **This report's own row IS classified** (see `## Evidence (self-filed)`), which
  is what Rule 36 asks of a run.
- **`gofmt -l cmd/agent internal` is NOT clean — 10 files — and none of them is this
  branch's.** Measured: every one of the ten has **zero** diff against master
  (`git diff --name-only master HEAD -- <that file>` → 0 lines), and
  `git diff --name-only master HEAD -- cmd/agent internal` is empty overall. It is the
  live state of the master this branch descends from, so a fix here could not be
  honest — reformatting another writer's files would put unrelated bytes on this branch
  and destroy the "report only" acceptance shape.
- **`TestVerbAliasIsRefusedByNameNoRun` is a REAL defect, diagnosed here but NOT
  fixed** — it is master's bug and fixing it was outside this run's budget. The
  mechanism: the refusal test calls `runCLIContractProbeEnv`
  (`cmd/agent/cli_contracts_test.go:553`), which delegates to
  `runCLIContractProbeRaw(t, bin, home, familyAddressArgs(args)...)` — so `argv[0]` is
  MAPPED to the canonical address before the probe runs. The test therefore probes
  `gopi session log` (which legitimately **dispatches**) and then asserts the word was
  refused. Its sibling's own comment states the opposite rule —
  `runCLIContractProbeRaw`: "this is the probe the refusal tests drive … so rewriting
  the door would answer a different question". This explains every observed failure
  exactly (`exit 0 — the alias was accepted instead of refused`, and `refusal does not
  mention "--prompt log"`), and it is consistent with the raw path: run by hand,
  `./bin/gopi log` exits 1 with the family address.
- **The branch's two unique files were DELETED, not ported.** That is deliberate and
  is the whole convergence: their content is a third table master already replaced
  with a unified one, and porting it would re-introduce the four over-retired words.
  Nothing in them is lost — the behaviour they implemented is live on master, shown
  above by execution.
- **The branch's 4 rework commits (`61779b44`, `7269d187`, `1564ec37`, `f49b48c6`)
  are ancestry only.** They fixed test argv against the branch's dispatch shape,
  which no longer exists; master fixed the same call sites its own way (`993ce6e5`
  "the tests' argv follows the family doors, and the group-arm list is derived").
  Their bytes are dropped with the tree.
- **No merge was performed** — `master` was not touched. The branch now merely
  fast-forwards cleanly onto master, which is the deliverable.
- I did **not** re-adjudicate `ui`/`cli`/`web_search`/`web_fetch`. Master's choice is
  later, gate-enforced and self-consistent; if the operator wants those four words
  retired too, that is a NEW change against master (it would require removing them
  from the dispatch chain in the same edit), not a revival of this branch.

## Verification

- Build: `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` →
  **BUILD_EXIT=0**, `bin/gopi` 30,661,831 bytes at the first convergence, **30,679,937**
  bytes after the second delivery-point merge (the build the execution evidence below
  was taken with; `make build` re-run there and exit 0 again).
- Tests: branch-scoped, never whole-tree —
  `make test-budget PKGS='-run "VerbAlias|Retired|Contract|VerbGuard|Verb|HelpHidden" ./cmd/agent/ ./internal/config/'`
  → **1 of 2 packages failed: `cmd/agent` FAILED, 5 top-level tests / 90 test-and-subtest
  `fail` records in the run's own JSON stream; `./internal/config/` green.** The
  retirement's
  *classification* gates are GREEN (`TestVerbAliasTableIsWellFormed`,
  `TestVerbClassificationCoversEveryDispatchedVerb`, `TestVerbCensusMatchesTheDispatch`,
  `TestCLIContractEachSubcommandRoutesToItsHandler`). The red set is **master's own**,
  not this convergence's — `git diff --name-only master HEAD -- cmd/agent internal tools`
  is **empty**, i.e. every source tree the package compiles is master's byte for byte —
  and its largest member is a defect I diagnosed rather than inherited; the honest
  remainder is in `## Not Done`. **Re-run a third time after the second delivery-point
  merge (`60596fd7`) and the build above**: same verdict, same five, `./internal/config/`
  green, `budget=25m elapsed=187s outcome=fail`, stream `run-1791254160-628397.json`,
  and the five read out of that stream: same five names. A red set that does not move
  while the branch absorbs two master tips is not a property of this branch.
- `gofmt -l cmd/agent internal` → 10 files, none of them this branch's (0-line diff
  against master each) — same class as the reds above: the live state of master.
- Ran: `./bin/gopi log`, `./bin/gopi capture`, `./bin/gopi stats`, `./bin/gopi raw`,
  `./bin/gopi buildinfo`, `./bin/gopi version`, `./bin/gopi tool capture -h`,
  `./bin/gopi session stats`, `./bin/gopi --prompt frobnicate9 --help`,
  `./bin/gopi tool doccheck` — verbatim in the live-evidence section above; each exit
  code re-taken at the delivered tip in its OWN call (never `$?` after a pipe).
- Result: retired words exit 1 naming the family address; the family spellings run;
  the never-dispatched word still reaches the prompt path; `doccheck` exits 1 on
  master's own `scripts-retired-verb-sweep.md` gap.
- Commit: the convergence is `844d1f3d` (the branch's superseded bytes dropped), the
  re-convergence onto master's live tip is `b5c4542a`, the first report commit is
  `7b1abf24`, the amendment commits are `5a5bf889` and `8fe5d177`, and the three
  delivery-point merges with master are `6e805c80`, `60596fd7`, `76613725`, and one
  further correction commit carries amendment 7 — it is the branch TIP, named by
  `git log --oneline -1` rather than here. The report cannot name its own commit
  (writing the hash changes the hash); the merge commits are the ones that matter,
  and `git log --oneline` on the branch names the rest.

## Files

- `devdoc/verb-retirement-branch-01a10dc3-superseded.md` — this report.
- `devdoc/DOC-INDEX.tsv` — this report's classification row (same commit, Rule 36).
- Convergence commit `844d1f3d` — 361 paths, the branch tree → master's tree; deletes
  `cmd/agent/cli_verb_retired.go` and `cmd/agent/cli_retired_argv_test.go`.

## Amendments

The report was committed twice on this branch. The SECOND commit is the delivered one,
and it corrects four things the first draft asserted without re-measuring:

1. **The conservation counts.** The first draft said the branch table held "30 words
   extracted" with a `"the"` prose artifact. Re-running the extraction at `f49b48c6`
   prints **29** constants and **no** artifact: the first number came from a looser
   grep over the whole file (which also caught prose), and a figure a reader can act on
   must name its generator — the commands are now in the section.
2. **The red-test count.** "7 tests red" came from counting printed `FAIL:` lines,
   which include a failing subtest's parent. Read out of the run's own JSON stream the
   measured figure is **5 top-level tests / 90 test-and-subtest records**.
3. **The commit the report names.** The first draft pointed at `0c8eef1e` / `7fb2550e`
   — the report commit that existed when it was written, and one no ref holds (both are
   unreachable: `git branch -a --contains 0c8eef1e` → empty). `## Verification` now
   lists the chain of real commits instead, which is the only shape that survives the
   fact that a report cannot name its own commit (writing the hash changes the hash).
4. **`gopi doccheck`.** The first draft implied the corpus was consistent. It is not:
   it exits 1 on `devdoc/scripts-retired-verb-sweep.md`, a file master owns and this
   branch does not touch. Said plainly under `## Not Done`.
5. **Mergeability.** The first draft reported the merge probe as clean. Re-run against
   the report commit it was **1** conflicted path (`devdoc/DOC-INDEX.tsv`, a
   machine-generated table master had advanced). Fixed by merging master in
   (`6e805c80`) and re-classifying the row with `gopi tool doccheck --fix`; two further
   master tips (`6c17d819`, `ae9aaf51`) then merged with **0** conflicts
   (`60596fd7`, `76613725`).
6. **The delivered binary.** "30,661,831 bytes" was the FIRST convergence's build; the
   executed surface was re-built after each master sync, and the final `make build` is
   also **30,679,937** bytes — the size at which the Rule 31 evidence above was taken.

Amending rather than leaving the numbers is the point: the first draft's figures were
plausible and wrong, and a report a reviewer cannot reproduce is a rumour with a
number attached.

7. **A test NAME the report cited did not exist.** `## Verification` and the isolation
   control both named `TestVerbAliasIsDispatchedWordIsNotInTheTable` as the passing
   "dispatched word must not be in the refusal vocabulary" gate. That name is in no
   file: `grep -rn "DispatchedWordIsNotInTheTable" cmd/agent/` → empty. The invariant is
   real but it lives inside **`TestVerbAliasTableIsWellFormed`**
   (`cmd/agent/cli_verb_aliases_test.go:38`, the `for _, a := range verbAliases` loop at
   line 52 with the `is dispatched — it must not be in the refusal vocabulary` error at
   line 54) — the name was invented from the error string, and a citation a reader
   cannot resolve is worse than no citation. Corrected in all three places, and the
   corrected name was then RUN, not just grepped: `go test ./cmd/agent/ -run
   'TestVerbAliasTableIsWellFormed' -count=1` → `ok github.com/nick/gopi/cmd/agent
   13.177s`, exit 0. An audit of EVERY `Test*` name in this report against the tree now
   resolves all of them (the only non-matches being the glob `TestTaskVerb*` and the
   prose word "Tests").

9. **The built binary's SIZE, at each sync.** The figures above were taken at earlier
   tips; after the FOURTH delivery-point merge (`e1328fea`, master's lineedit
   ambiguous-width work) the rebuilt binary is **30,688,915** bytes, `make build` exit
   **0**, and the Rule 31 evidence was re-taken against THAT binary at the final tip
   `9233a33f`: `gopi log|capture|stats|raw` → exit **1**, each printing its own
   `is RETIRED — use `gopi <family> <verb>`` line with the `--prompt` escape;
   `buildinfo` → exit **1** with `"buildinfo" is not a gopi command — this one is
   spelled `gopi version``; `gopi version` → exit **0**; `gopi tool capture -h` → exit
   **0**. The classification gates were re-run at that tip too → `ok …cmd/agent
   50.621s`, exit **0**. A byte count is a property of a build, not of a branch, so it
   moves with every master sync — the count is named per measurement rather than
   quoted as if it were stable.

8. **The retirement CLASSIFICATION gates were re-run on the delivered tree and are
   GREEN.** Amendment 7 fixed a name; the final gate re-run reads:
   `go test ./cmd/agent/ -run 'TestVerbAliasTableIsWellFormed|TestVerbAliasCanonicalStillDispatches|TestVerbClassificationCoversEveryDispatchedVerb|TestVerbCensusMatchesTheDispatch|TestAliasRefusalLeavesOrdinaryUnknownWordsAlone' -count=1`
   → `ok github.com/nick/gopi/cmd/agent 39.991s`, exit **0**, and
   `go test ./internal/config/ -count=1` → `ok … 2.833s`, exit **0**. So the surface
   this epic is ABOUT — the alias table's well-formedness and the census/dispatch
   agreement — is green; the reds listed under `## Not Done` are the three
   argv-level/contract-inventory tests, and they are master's (source delta vs master
   is empty). The broad branch-scoped `make test-budget` set was ALSO re-launched on
   the delivered tree and its run was **killed, not concluded** — `make`'s budget run
   was starved by host load (21.08 on 4 cores) and had not reached the summary after
   ~7m30s, so the job was killed rather than left resident (Rule 35: no resident
   process left holding a slot). The narrow, decisive set above is what was actually
   observed on the delivered tip.

10. **The close-out re-verification, at the delivered tip, in one pass.** Everything the
   acceptance names was re-taken against the LIVE master `e1328fea` (master moved once
   more after the fourth sync; it stayed an ancestor — no divergence returned):
   `git status --porcelain` → **0 lines**; `git diff --name-only master HEAD` → exactly
   `devdoc/DOC-INDEX.tsv` + this report; `git merge-base --is-ancestor master HEAD` →
   true; `git merge --no-commit --no-ff master` → exit **0**, `--diff-filter=U` → **0**
   (aborted afterwards, porcelain back to 0); `make build` → exit **0**,
   `bin/gopi` **30,688,915** bytes; `./bin/gopi log|capture|stats|raw` → exit **1**
   each, `./bin/gopi version` → exit **0**, `./bin/gopi tool capture -h` → exit **0**,
   `./bin/gopi session log -h` → exit **0**. The classification gate re-run OF THE
   DELIVERED TREE (not an older tip) is green as well:
   `go test ./cmd/agent/ -run 'TestVerbAliasTableIsWellFormed|TestVerbAliasCanonicalStillDispatches|TestVerbClassificationCoversEveryDispatchedVerb|TestVerbCensusMatchesTheDispatch|TestAliasRefusalLeavesOrdinaryUnknownWordsAlone' -count=1 -v`
   → **5 of 5 PASS**, package `ok`, exit **0**; `go test ./internal/config/ -count=1` →
   `ok … 3.557s`.

   The three reds were ALSO re-measured at this tip in isolation
   (`-run 'TestVerbAliasIsRefusedByNameNoRun|TestCLIContractInventoryMatchesDispatch|TestAnnotateVerbRunsARealArgvEndToEnd'`)
   → exit **1**, same three names, `TestVerbAliasIsRefusedByNameNoRun` failing on
   `buildinfo/log/stats/raw/resume/checkpoint` (each in its `-h` and `extra` form).
   They reproduce on a tree whose **`cmd/agent`, `internal`, `tools` source is master's
   byte for byte** (`git diff --stat master HEAD -- cmd/agent internal tools` → empty),
   which is what makes them master's reds rather than an artefact of this convergence:
   the same bytes fail with and without this branch's existence, since this branch
   contributes no Go source at all. Master's own branch tip cannot be checked out here
   to run them a second time — `master` is checked out in `/home/user/work/GoPi`, which
   this run must not write — so this is the honest shape of the control.

11. **A defect of MY OWN in the close-out re-verification regex — and why it was a FALSE GREEN.**
   The `-run` pattern I typed in amendment 10 quoted
   `TestVerbAliasRefusalLeavesOrdinaryUnknownWordsAlone`, which exists in **no** file — the
   real name is `TestAliasRefusalLeavesOrdinaryUnknownWordsAlone`
   (`cmd/agent/cli_verb_aliases_test.go:183`). A `-run` regex whose match count is short still
   reports `ok`, so an invented name inside a `-run` string is a silent no-op, not an error:
   the command printed `ok github.com/nick/gopi/cmd/agent 8.442s` while having run **four**
   of the five intended tests. The audit that caught it is mechanical, not a reading:
   every name in the regex is resolved against the tree first
   (`grep -c "^func <name>(" cmd/agent/*_test.go`) — all five resolve, one each.
   Re-run with `-v` and the pass count read from the log, not from the exit code:
   `go test ./cmd/agent/ -run 'TestVerbAliasTableIsWellFormed|TestVerbAliasCanonicalStillDispatches|
   TestVerbClassificationCoversEveryDispatchedVerb|TestVerbCensusMatchesTheDispatch|
   TestAliasRefusalLeavesOrdinaryUnknownWordsAlone' -count=1 -v`
   → **`--- PASS` count = 5**, `ok github.com/nick/gopi/cmd/agent 46.430s`, exit **0**; the
   log is kept at
   `/home/user/.gopi/sessions/01a10e53-b053-7ae9-a6e9-1fd7bd830182/artifacts/alias-gates-final.log`
   (Rule 37: evidence outside the worktree). **The report is checked as well**: the invented
   spelling does not occur in this document (`grep` exit 1), and the correct name occurs twice
   — the same audit amendment 7 describes, applied to the text I had just written.


## Evidence (self-filed)

- **Index tree == master tree (at the convergence commit `b5c4542a`)**: `git write-tree`
  → `578677ab…`; `git rev-parse master^{tree}` → `578677ab…` (same);
  `git diff --cached --name-only master` → 0 lines. (An EARLIER measurement read
  `3e01c1e6…` for both sides at master tip `add55f06`; master moved to `d0f69691` /
  tree `578677ab…` mid-run, and the convergence commit was re-made against the live
  tip — `add55f06`'s tree is `3e01c1e6…`, `d0f69691`'s is `578677ab…`, both read back
  from the object store here.)
- **The delivered tip**: `git diff --name-only master HEAD` → exactly
  `devdoc/DOC-INDEX.tsv` and `devdoc/verb-retirement-branch-01a10dc3-superseded.md`,
  against the LIVE master (`ae9aaf51`) — nothing else. Re-measured after all three
  delivery-point merges.
- **Merge probe at the delivered tip**: `git merge --no-commit --no-ff master` → exit
  **0**, `git diff --name-only --diff-filter=U | wc -l` = **0**, and
  `git merge-base --is-ancestor master HEAD` → true. The branch aborts the probe's own
  merge (`git merge --abort`), leaving `git status --porcelain` empty.
- **The merge ceiling, measured on the PRE-REWORK branch**: `git merge master` → exit 1,
  `Automatic merge failed`, 36 files under `git diff --name-only --diff-filter=U`. The
  same probe on the converged tree → **0**. (An earlier attempt to measure it with
  `merge-tree --write-tree` was a dead end — this git (2.34.1) reports `fatal: unknown
  rev --write-tree` — and was trimmed.)
- **Two unique files, measured on the PRE-REWORK branch**:
  `git diff --name-status master HEAD` → exactly
  `A cmd/agent/cli_retired_argv_test.go`, `A cmd/agent/cli_verb_retired.go`. Both are
  DELETED by the delivered branch (`844d1f3d`), which is the convergence; on the
  delivered tip that command reports the two `devdoc/` paths as `M`.
- **Conservation**: word-set extraction as described above → 25/29 covered;
  delta = `CliVerbUI`, `CliVerbCLI`, `toolnames.ToolWebSearch`,
  `toolnames.ToolWebFetch`; master-only retired = `CliVerbArchive`, `CliVerbSandbox`.
- **Master's invariant**: `cmd/agent/cli_verb_aliases.go:127` (prose) and
  `cmd/agent/cli_verb_aliases_test.go:54` (gate) — the latter quoted verbatim above.
- **Build**: `make build` exit 0 (job `job-1`, `BUILD_EXIT=0`).
- **Tests (broad branch-scoped set)**: `make test-budget PKGS='-run "VerbAlias|Retired|Contract|VerbGuard|Verb|HelpHidden" ./cmd/agent/ ./internal/config/'`
  → `packages: 2 finished, 1 with a failure` / `FAILED: github.com/nick/gopi/cmd/agent`,
  **5 top-level tests / 90 fail records** (the first draft counted printed `FAIL:` lines
  and said 7 — corrected, see `## Amendments`), `budget=25m elapsed=78s`, outcome=fail,
  stream `run-1791253055-538416.json`. GREEN in the same run:
  `TestVerbClassificationCoversEveryDispatchedVerb`,
  `TestVerbCensusMatchesTheDispatch`, `TestCLIContractEachSubcommandRoutesToItsHandler`,
  `TestCaptureVerbFailureArmsExitNonZero`, `TestTaskVerb*RunsAsAdvertised`.
- **Isolation control (Rule 26 / #6934 step 8)**: re-run narrowed to the suspected
  pre-existing reds — `make test-budget PKGS='-run "TestVerbAliasIsRefusedByNameNoRun|TestCLIContractInventoryMatchesDispatch|TestVerbAliasTableIsWellFormed" ./cmd/agent/'`
  → outcome=fail, `elapsed=19s`, and the red set is exactly the two
  (`TestVerbAliasIsRefusedByNameNoRun` + `TestCLIContractInventoryMatchesDispatch`)
  while `TestVerbAliasTableIsWellFormed` PASSES. They reproduce ALONE ⇒
  pre-existing, not environmental and not this convergence's.
- **Mergeability, observed**: `git merge --no-commit --no-ff master` on the converged
  tree → `git diff --name-only --diff-filter=U | wc -l` = **0**; on the pre-rework branch
  the same probe gave **36**. (The probe's first attempt reported `error: Your local
  changes … would be overwritten` — a genuinely dirty `devdoc/DOC-INDEX.tsv`, a
  regenerable artifact, was discarded before re-running.)
  **Re-measured against the report commit — and it was ONE, not zero**:
  `devdoc/DOC-INDEX.tsv` conflicted, because master had advanced the same machine-owned
  table. That finding is the reason the branch now carries real merge commits
  (`6e805c80`, `60596fd7`) instead of being a fast-forward-shaped tip: after them
  master IS an ancestor of HEAD and the probe reports **0** against a live master
  (`6c17d819`). A "clean" probe run once at the right moment is not the same claim as a
  branch that still merges after master moves — only the second one is worth reporting.
- **Binary executions**: the nine invocations above, each with its observed exit code.
